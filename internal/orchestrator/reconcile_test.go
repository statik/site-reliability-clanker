package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 11, 21, 2, 0, 0, 0, time.UTC) }

// refusingIssuer is a target that does not answer.
type refusingIssuer struct{ withdrawn []string }

func (r *refusingIssuer) Issue(context.Context, broker.Grant, time.Duration) (broker.Credential, error) {
	return broker.Credential{}, nil
}

func (r *refusingIssuer) Withdraw(_ context.Context, g broker.Grant) error {
	r.withdrawn = append(r.withdrawn, g.ID)
	return errors.New("the target does not answer")
}

func TestReconcileEndsWhatAStopInterrupted(t *testing.T) {
	db := storetest.Open(t)
	clock := fixedClock{}
	key, err := broker.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	issuer := &refusingIssuer{}
	b, err := broker.New(db, clock, key, issuer)
	if err != nil {
		t.Fatal(err)
	}
	o := &Orchestrator{DB: db, Audit: audit.New(db, clock), Broker: b, Clock: clock}
	states := map[string]string{"WI-0001": StateApplying, "WI-0002": StateVerifying,
		"WI-0003": StateAwaitingApproval, "WI-0004": StateApproved, "WI-0005": StateDone}
	for id, state := range states {
		if _, err := db.Exec(`INSERT INTO work_items (id, title, kind, env, state, created_at,
			updated_at) VALUES ($1, 'A change', 'app-upgrade', 'local', $2, 'then', 'then')`, id,
			state); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO jobs (work_item, kind, status, log, started_at) VALUES
		('WI-0001', 'apply', 'running', 'half a log', 'then'),
		('WI-0005', 'apply', 'succeeded', 'a whole log', 'then')`); err != nil {
		t.Fatal(err)
	}
	open, err := b.Issue(broker.Request{WorkItem: "WI-0001", Env: "local", Layer: "50-app",
		Kind: broker.KindApply, Commit: "9f3c1e7", PlanSHA256: "5b1d", Tier: "T2",
		Approvers: []string{"alice"}, Duration: time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	done, err := o.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if done.Jobs != 1 || done.Grants != 1 || len(done.WorkItems) != 2 {
		t.Fatalf("reconciled %+v, want 1 job, 1 grant and 2 work items", done)
	}
	want := map[string]string{"WI-0001": StateFailed, "WI-0002": StateFailed,
		"WI-0003": StateAwaitingApproval, "WI-0004": StateApproved, "WI-0005": StateDone}
	for id, state := range want {
		wi, err := o.Get(id)
		if err != nil || wi.State != state {
			t.Errorf("%s is %s, %v; want %s", id, wi.State, err, state)
		}
	}
	var status, log string
	if err := db.QueryRow(`SELECT status, log FROM jobs WHERE work_item = 'WI-0001'`).Scan(&status,
		&log); err != nil {
		t.Fatal(err)
	}
	if status != "lost" || !strings.HasPrefix(log, "half a log") || !strings.Contains(log, "JOB LOST") {
		t.Errorf("the interrupted job is %s with log %q", status, log)
	}
	if err := db.QueryRow(`SELECT status FROM jobs WHERE work_item = 'WI-0005'`).Scan(&status); err != nil ||
		status != "succeeded" {
		t.Errorf("the finished job is %s, %v", status, err)
	}
	// The grant is ended although the target did not answer.
	stored, err := b.Get(open.ID)
	if err != nil || stored.Status != "revoked" {
		t.Errorf("the open grant is %s, %v; want revoked", stored.Status, err)
	}
	if len(issuer.withdrawn) != 1 || issuer.withdrawn[0] != open.ID {
		t.Errorf("want one withdraw of %s, got %v", open.ID, issuer.withdrawn)
	}

	again, err := o.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again.Jobs != 0 || again.Grants != 0 || len(again.WorkItems) != 0 {
		t.Fatalf("a second reconcile ended %+v, want nothing", again)
	}
}
