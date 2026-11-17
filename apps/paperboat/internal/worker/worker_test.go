package worker_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/store"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/worker"
)

var now = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

// fakeStore keeps the rows in memory and applies the same conditional writes as the SQL.
type fakeStore struct {
	aging       []store.AgingRow
	deliveries  map[int64]*store.Delivery
	tickets     []store.Ticket
	fish        []store.Fish
	manifests   map[int64]string
	failReads   error
	conflictOn  int64
	transitions []string
}

func (f *fakeStore) AgingRows(context.Context, time.Time) ([]store.AgingRow, error) {
	return f.aging, f.failReads
}

func (f *fakeStore) ApplyAging(_ context.Context, id int64, from string, r domain.AgingResult,
) error {
	for i := range f.aging {
		if f.aging[i].ID == id && f.aging[i].Input.Status == from {
			f.aging[i].Input.Status = r.Status
			f.aging[i].Input.Condition = r.Condition
		}
	}
	return nil
}

func (f *fakeStore) PendingDeliveries(context.Context) ([]store.Delivery, error) {
	var pending []store.Delivery
	for id := int64(1); id <= int64(len(f.deliveries)); id++ {
		d := f.deliveries[id]
		if d.Status == domain.DeliveryScheduled || d.Status == domain.DeliveryOut {
			pending = append(pending, *d)
		}
	}
	return pending, f.failReads
}

func (f *fakeStore) TransitionDelivery(_ context.Context, id int64, to string, at time.Time,
) (store.Delivery, error) {
	if id == f.conflictOn {
		return store.Delivery{}, domain.ValidateDeliveryTransition(domain.DeliveryDone, to)
	}
	d := f.deliveries[id]
	if err := domain.ValidateDeliveryTransition(d.Status, to); err != nil {
		return store.Delivery{}, err
	}
	d.Status = to
	if to == domain.DeliveryOut {
		d.StartedAt = &at
	}
	f.transitions = append(f.transitions, d.Kind+":"+to)
	return *d, nil
}

func (f *fakeStore) SetManifestPath(_ context.Context, id int64, path string) error {
	f.manifests[id] = path
	return nil
}

func (f *fakeStore) OpenTickets(context.Context) ([]store.Ticket, error) {
	return f.tickets, f.failReads
}

func (f *fakeStore) Escalate(_ context.Context, id int64, from, to string) error {
	for i := range f.tickets {
		if f.tickets[i].ID == id && f.tickets[i].Priority == from {
			f.tickets[i].Priority = to
		}
	}
	return nil
}

func (f *fakeStore) Inventory(context.Context, time.Time) ([]store.Fish, error) {
	return f.fish, f.failReads
}

func newWorker(t *testing.T, level int, db *fakeStore) *worker.Worker {
	t.Helper()
	db.manifests = map[int64]string{}
	return &worker.Worker{
		DB: db, Level: level, DataDir: t.TempDir(),
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now:    func() time.Time { return now },
	}
}

func agingRows() []store.AgingRow {
	longAgo := now.Add(-time.Hour)
	return []store.AgingRow{
		{ID: 1, Input: domain.AgingInput{Status: domain.FishAvailable, Condition: 100,
			AgeDays: 500, RetireAfterDays: 400, StatusChangedAt: longAgo}},
		{ID: 2, Input: domain.AgingInput{Status: domain.FishQuarantined, Condition: 100,
			AgeDays: 0, RetireAfterDays: 400, StatusChangedAt: longAgo}},
		{ID: 3, Input: domain.AgingInput{Status: domain.FishRented, Condition: 100,
			AgeDays: 200, RetireAfterDays: 400, StatusChangedAt: longAgo}},
	}
}

func TestAgingAtLevel1OnlyEndsQuarantine(t *testing.T) {
	db := &fakeStore{aging: agingRows()}
	changed, err := newWorker(t, 1, db).Aging(context.Background())
	if err != nil || changed != 1 {
		t.Fatalf("changed = %d, err = %v; want 1", changed, err)
	}
	if db.aging[0].Input.Status != domain.FishAvailable || db.aging[0].Input.Condition != 100 {
		t.Errorf("1.0.0 acted on the age of fish 1: %+v", db.aging[0].Input)
	}
	if db.aging[1].Input.Status != domain.FishAvailable {
		t.Errorf("fish 2 is still %s", db.aging[1].Input.Status)
	}
}

func TestAgingAtLevel2RetiresAndScores(t *testing.T) {
	db := &fakeStore{aging: agingRows()}
	w := newWorker(t, 2, db)
	changed, err := w.Aging(context.Background())
	if err != nil || changed != 3 {
		t.Fatalf("changed = %d, err = %v; want 3", changed, err)
	}
	retired := domain.AgingInput{Status: domain.FishRetired, Condition: 13, AgeDays: 500,
		RetireAfterDays: 400, StatusChangedAt: db.aging[0].Input.StatusChangedAt}
	if db.aging[0].Input != retired {
		t.Errorf("fish 1 = %+v, want %+v", db.aging[0].Input, retired)
	}
	rented := domain.AgingInput{Status: domain.FishRented, Condition: 65, AgeDays: 200,
		RetireAfterDays: 400, StatusChangedAt: db.aging[2].Input.StatusChangedAt}
	if db.aging[2].Input != rented {
		t.Errorf("fish 3 = %+v, want %+v", db.aging[2].Input, rented)
	}
	changed, err = w.Aging(context.Background())
	if err != nil || changed != 0 {
		t.Errorf("second pass changed %d fish, err = %v; want 0", changed, err)
	}
}

func cycleStore() *fakeStore {
	return &fakeStore{deliveries: map[int64]*store.Delivery{
		1: {ID: 1, Kind: domain.KindDelivery, Status: domain.DeliveryScheduled,
			WindowStart: now.Add(-time.Second), Fish: "Dory", Customer: "Ada Brine"},
		2: {ID: 2, Kind: domain.KindCollection, Status: domain.DeliveryScheduled,
			WindowStart: now.Add(time.Hour)},
	}}
}

func TestDeliveryProgressSendsADueJobOutWithAManifest(t *testing.T) {
	db := cycleStore()
	w := newWorker(t, 2, db)
	moved, err := w.DeliveryProgress(context.Background())
	if err != nil || moved != 1 {
		t.Fatalf("moved = %d, err = %v; want 1", moved, err)
	}
	if db.manifests[1] != "manifests/delivery-1.txt" {
		t.Fatalf("manifest path = %q", db.manifests[1])
	}
	content, err := os.ReadFile(filepath.Join(w.DataDir, db.manifests[1]))
	if err != nil || !strings.Contains(string(content), "Dory") {
		t.Fatalf("manifest file: %v\n%s", err, content)
	}
}

func TestDeliveryProgressFinishesAJobAfterTheTransitTime(t *testing.T) {
	db := cycleStore()
	w := newWorker(t, 2, db)
	steps := []struct {
		at   time.Time
		want int
	}{
		{now, 1},
		{now.Add(domain.TransitTime - time.Second), 0},
		{now.Add(domain.TransitTime), 1},
		{now.Add(2 * domain.TransitTime), 0},
	}
	for i, step := range steps {
		w.Now = func() time.Time { return step.at }
		moved, err := w.DeliveryProgress(context.Background())
		if err != nil || moved != step.want {
			t.Fatalf("step %d: moved = %d, err = %v; want %d", i, moved, err, step.want)
		}
	}
	if got := strings.Join(db.transitions, ","); got != "delivery:out,delivery:done" {
		t.Errorf("transitions = %s", got)
	}
	if db.deliveries[2].Status != domain.DeliveryScheduled {
		t.Errorf("job 2 moved before its window: %s", db.deliveries[2].Status)
	}
}

func TestDeliveryProgressSkipsAJobThatAnotherWorkerMoved(t *testing.T) {
	db := &fakeStore{conflictOn: 1, deliveries: map[int64]*store.Delivery{
		1: {ID: 1, Kind: domain.KindDelivery, Status: domain.DeliveryScheduled, WindowStart: now},
		2: {ID: 2, Kind: domain.KindDelivery, Status: domain.DeliveryScheduled, WindowStart: now},
	}}
	moved, err := newWorker(t, 2, db).DeliveryProgress(context.Background())
	if err != nil || moved != 1 {
		t.Errorf("moved = %d, err = %v; want 1 and no error", moved, err)
	}
}

func TestDeliveryProgressReportsAManifestItCannotWrite(t *testing.T) {
	db := &fakeStore{deliveries: map[int64]*store.Delivery{
		1: {ID: 1, Kind: domain.KindDelivery, Status: domain.DeliveryScheduled, WindowStart: now},
	}}
	w := newWorker(t, 2, db)
	blocker := filepath.Join(w.DataDir, "not-a-directory")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	w.DataDir = blocker
	moved, err := w.DeliveryProgress(context.Background())
	if err == nil || moved != 1 {
		t.Errorf("moved = %d, err = %v; want 1 and an error about the manifest", moved, err)
	}
}

func TestEscalation(t *testing.T) {
	old := now.Add(-domain.EscalateAfter)
	db := &fakeStore{tickets: []store.Ticket{
		{ID: 1, Status: domain.TicketOpen, Priority: "low", EscalatedAt: old},
		{ID: 2, Status: domain.TicketOpen, Priority: "urgent", EscalatedAt: old},
		{ID: 3, Status: domain.TicketOpen, Priority: "normal", EscalatedAt: now},
	}}
	raised, err := newWorker(t, 2, db).Escalation(context.Background())
	if err != nil || raised != 1 {
		t.Fatalf("raised = %d, err = %v; want 1", raised, err)
	}
	if db.tickets[0].Priority != "normal" || db.tickets[2].Priority != "normal" {
		t.Errorf("priorities = %s, %s", db.tickets[0].Priority, db.tickets[2].Priority)
	}
}

func TestCertificates(t *testing.T) {
	condition := 65
	db := &fakeStore{fish: []store.Fish{
		{ID: 1, Name: "Dory", Species: "Betta", Status: domain.FishAvailable, AgeDays: 200,
			Condition: &condition},
		{ID: 2, Name: "Gill", Species: "Koi", Status: domain.FishRented, AgeDays: 10},
	}}
	w := newWorker(t, 2, db)
	written, err := w.Certificates(context.Background())
	if err != nil || written != 2 {
		t.Fatalf("written = %d, err = %v; want 2", written, err)
	}
	content, err := os.ReadFile(filepath.Join(w.DataDir, "certificates", "fish-1.txt"))
	if err != nil || !strings.Contains(string(content), "Condition: 65 of 100") {
		t.Errorf("certificate: %v\n%s", err, content)
	}
}

func TestJobsReturnTheReadError(t *testing.T) {
	db := &fakeStore{failReads: errors.New("connection refused")}
	w := newWorker(t, 2, db)
	jobs := map[string]func(context.Context) (int, error){
		"aging": w.Aging, "delivery": w.DeliveryProgress, "escalation": w.Escalation,
		"certificates": w.Certificates,
	}
	for name, run := range jobs {
		if _, err := run(context.Background()); err == nil {
			t.Errorf("%s: no error when the database is down", name)
		}
	}
}
