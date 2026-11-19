package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
	"github.com/statik/site-reliability-clanker/internal/policy"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// saturdayWindow is one window: Saturday from 02:00 to 04:00 UTC.
const saturdayWindow = `{"windows": [{"days": ["sat"], "start": "02:00", "minutes": 120,
	"time_zone": "UTC"}]}`

var (
	wednesday    = time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	saturday     = time.Date(2026, 11, 21, 2, 0, 0, 0, time.UTC)
	nextSaturday = time.Date(2026, 11, 28, 2, 0, 0, 0, time.UTC)
)

// movingClock is a clock that a test sets.
type movingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *movingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *movingClock) set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

// windowRunner records the time of each apply job. It answers an apply job with applyErr
// and a plan job with a plan that changes nothing.
type windowRunner struct {
	clock    Clock
	mu       sync.Mutex
	applies  []time.Time
	applyErr error
}

func (r *windowRunner) Run(_ context.Context, job jobspec.Job) (jobspec.Result, error) {
	if job.Kind == jobspec.KindApply {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.applies = append(r.applies, r.clock.Now())
		return jobspec.Result{}, r.applyErr
	}
	plan := []byte("the plan after the state changed")
	return jobspec.Result{Plan: plan, PlanSHA256: jobspec.SHA256(plan),
		PlanJSON: []byte(`{"resource_changes": []}`)}, nil
}

func (r *windowRunner) applyTimes() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time{}, r.applies...)
}

type windowHarness struct {
	*Orchestrator
	clock   *movingClock
	runners *windowRunner
	commit  string
}

// windowOrchestrator has the runbooks of this repository in its forge, and beside them
// the given windows file when it is not empty. The time is Wednesday 10:00 UTC. The
// approvers are alice and bob.
func windowOrchestrator(t *testing.T, windows string) windowHarness {
	t.Helper()
	db := storetest.Open(t)
	clock := &movingClock{now: wednesday}
	f := forge.New(db)
	if err := f.Seed(RunbooksRepo, map[string]fs.FS{".": os.DirFS("../../runbooks")},
		"Seed runbooks"); err != nil {
		t.Fatal(err)
	}
	code := fstest.MapFS{"README.md": {Data: []byte("Platform\n")}}
	if err := f.Seed(PlatformRepo, map[string]fs.FS{".": code}, "Seed platform"); err != nil {
		t.Fatal(err)
	}
	commit, err := f.Head(PlatformRepo, "main")
	if err != nil {
		t.Fatal(err)
	}
	key, err := broker.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := broker.New(db, clock, key, &refusingIssuer{},
		func() (policy.Approvals, error) { return ApprovalSettings(f) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (name, display, role, email) VALUES
		('alice', 'Alice Ng', 'approver', 'alice@example.com'),
		('bob', 'Bob Okafor', 'approver', 'bob@example.com')`); err != nil {
		t.Fatal(err)
	}
	runners := &windowRunner{clock: clock}
	o := &Orchestrator{DB: db, Audit: audit.New(db, clock), Forge: f, Broker: b, Clock: clock,
		Runners: runners, Envs: map[string]EnvConfig{"local": {Name: "local", Cloud: "local"}}}
	t.Cleanup(o.WaitForApplies)
	h := windowHarness{Orchestrator: o, clock: clock, runners: runners, commit: commit}
	if windows != "" {
		h.setWindows(t, windows)
	}
	return h
}

func (h windowHarness) setWindows(t *testing.T, content string) {
	t.Helper()
	if _, err := h.Forge.Commit(forge.CommitRequest{Repo: RunbooksRepo, Branch: "main",
		Author: "test", Message: "Change the maintenance windows",
		Files: map[string]string{policy.WindowsPath: content}}); err != nil {
		t.Fatal(err)
	}
}

// changingItem makes a work item of the tier that waits for approval. Its one saved plan
// adds a resource, so that its apply needs a grant and a job.
func (h windowHarness) changingItem(t *testing.T, id, tier string) {
	t.Helper()
	plan := []byte("the approved plan of " + id)
	if _, err := h.DB.Exec(`INSERT INTO work_items (id, title, kind, env, layers, state, tier,
		commit_sha, created_at, updated_at) VALUES ($1, 'Replace the database', 'manual',
		'local', '["00-bootstrap"]', $2, $3, $4, 'then', 'then')`, id, StateAwaitingApproval,
		tier, h.commit); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DB.Exec(`INSERT INTO plans (work_item, layer, commit_sha, plan_sha256, plan,
		plan_json, facts, created_at) VALUES ($1, '00-bootstrap', $2, $3, $4, '{}',
		'{"add": 1}', 'then')`, id, h.commit, jobspec.SHA256(plan), plan); err != nil {
		t.Fatal(err)
	}
}

// approveBoth records the approvals of alice and bob and waits for an apply that they
// start.
func (h windowHarness) approveBoth(t *testing.T, id string) WorkItem {
	t.Helper()
	for _, user := range []string{"alice", "bob"} {
		if _, err := approve(t, h.Orchestrator, id, user); err != nil {
			t.Fatal(err)
		}
	}
	return h.settled(t, id)
}

// settled waits for the applies that run and returns the stored work item.
func (h windowHarness) settled(t *testing.T, id string) WorkItem {
	t.Helper()
	h.WaitForApplies()
	wi, err := h.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return wi
}

// runDue moves the clock, starts the work that is due and waits for it.
func (h windowHarness) runDue(t *testing.T, id string, now time.Time) WorkItem {
	t.Helper()
	h.clock.set(now)
	if err := h.RunDue(); err != nil {
		t.Fatal(err)
	}
	return h.settled(t, id)
}

func (h windowHarness) events(t *testing.T, id, eventType string) []audit.Event {
	t.Helper()
	events, err := h.Audit.Query(audit.Filter{WorkItem: id, Types: []string{eventType}})
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func wantWait(t *testing.T, wi WorkItem, applyAt, windowStart time.Time) {
	t.Helper()
	if wi.State != StateScheduled || wi.ScheduledFor != store.FormatTime(applyAt) ||
		wi.MaintenanceWindow != store.FormatTime(windowStart) {
		t.Fatalf("want %s scheduled for %s in the window of %s, got state %s, scheduled for "+
			"%q, window %q", wi.ID, store.FormatTime(applyAt), store.FormatTime(windowStart),
			wi.State, wi.ScheduledFor, wi.MaintenanceWindow)
	}
}

func TestTierT3AppliesAtOnceWithoutWindows(t *testing.T) {
	if _, err := os.Stat("../../runbooks/" + policy.WindowsPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the runbooks that the product seeds must have no windows file: %v", err)
	}
	for name, windows := range map[string]string{"no file": "", "no window": `{"windows": []}`} {
		h := windowOrchestrator(t, windows)
		h.changingItem(t, "WI-0001", "T3")
		wi := h.approveBoth(t, "WI-0001")
		if wi.State != StateDone || wi.ScheduledFor != "" || wi.MaintenanceWindow != "" {
			t.Errorf("%s: want Done with no wait, got %s, scheduled for %q, window %q", name,
				wi.State, wi.ScheduledFor, wi.MaintenanceWindow)
		}
		if times := h.runners.applyTimes(); len(times) != 1 || !times[0].Equal(wednesday) {
			t.Errorf("%s: want one apply at the time of the approval, got %v", name, times)
		}
		if waits := h.events(t, "WI-0001", "apply.waits_for_window"); len(waits) != 0 {
			t.Errorf("%s: want no wait event, got %d", name, len(waits))
		}
	}
}

func TestTierT3WaitsForTheWindowAndGetsItsGrantThere(t *testing.T) {
	h := windowOrchestrator(t, saturdayWindow)
	h.changingItem(t, "WI-0001", "T3")
	wantWait(t, h.approveBoth(t, "WI-0001"), saturday, saturday)
	waits := h.events(t, "WI-0001", "apply.waits_for_window")
	if len(waits) != 1 || waits[0].Data["window_start"] != store.FormatTime(saturday) {
		t.Fatalf("want one wait event with the start of the window, got %+v", waits)
	}
	// One minute before the window, nothing starts and no grant exists.
	wantWait(t, h.runDue(t, "WI-0001", saturday.Add(-time.Minute)), saturday, saturday)
	if n := count(t, h.Orchestrator, "grants"); n != 0 || len(h.runners.applyTimes()) != 0 {
		t.Fatalf("want no grant and no apply before the window, got %d grants and applies %v",
			n, h.runners.applyTimes())
	}
	wi := h.runDue(t, "WI-0001", saturday)
	if wi.State != StateDone || wi.MaintenanceWindow != "" {
		t.Fatalf("want Done and no window after the apply, got %s and %q", wi.State,
			wi.MaintenanceWindow)
	}
	grants := h.events(t, "WI-0001", "grant.issued")
	if len(grants) != 1 || grants[0].Data["not_before"] != store.FormatTime(saturday) ||
		grants[0].Data["expires"] != store.FormatTime(saturday.Add(policy.WindowReserve)) {
		t.Errorf("want one grant from the start of the window for the reserve, got %+v", grants)
	}
	if times := h.runners.applyTimes(); len(times) != 1 || !times[0].Equal(saturday) {
		t.Errorf("want one apply at the start of the window, got %v", times)
	}
	if waits := h.events(t, "WI-0001", "apply.waits_for_window"); len(waits) != 1 {
		t.Errorf("want one wait event in total, got %d", len(waits))
	}
}

func TestTheShortestWindowIsUsedWhenTheSchedulerComesLate(t *testing.T) {
	h := windowOrchestrator(t, strings.Replace(saturdayWindow, "120", "60", 1))
	h.changingItem(t, "WI-0001", "T3")
	wantWait(t, h.approveBoth(t, "WI-0001"), saturday, saturday)
	// The scheduler looks every five seconds, and the apply can wait for another apply.
	late := saturday.Add(30 * time.Minute)
	if wi := h.runDue(t, "WI-0001", late); wi.State != StateDone {
		t.Fatalf("want Done in the window, got %s scheduled for %s", wi.State, wi.ScheduledFor)
	}
	if times := h.runners.applyTimes(); len(times) != 1 || !times[0].Equal(late) {
		t.Errorf("want one apply 30 minutes after the start, got %v", times)
	}
}

func TestTheFirstDeploymentDoesNotWaitForAWindow(t *testing.T) {
	h := windowOrchestrator(t, saturdayWindow)
	h.changingItem(t, "WI-0001", "T3")
	if _, err := h.DB.Exec(`UPDATE work_items SET kind=$1 WHERE id='WI-0001'`,
		KindBlankSlate); err != nil {
		t.Fatal(err)
	}
	wi := h.approveBoth(t, "WI-0001")
	if wi.State == StateScheduled || wi.MaintenanceWindow != "" || wi.ScheduledFor != "" {
		t.Fatalf("want no wait for the first deployment, got %s, window %q, time %q",
			wi.State, wi.MaintenanceWindow, wi.ScheduledFor)
	}
	if waits := h.events(t, "WI-0001", "apply.waits_for_window"); len(waits) != 0 {
		t.Errorf("want no wait event, got %d", len(waits))
	}
}

func TestTierT2DoesNotWaitForAWindow(t *testing.T) {
	for name, windows := range map[string]string{"a window": saturdayWindow,
		"a file that is not valid": `{"windows": "always"}`} {
		h := windowOrchestrator(t, windows)
		h.changingItem(t, "WI-0002", "T2")
		if _, err := approve(t, h.Orchestrator, "WI-0002", "alice"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if wi := h.settled(t, "WI-0002"); wi.State != StateDone || wi.MaintenanceWindow != "" {
			t.Errorf("%s: want Done at once, got %s and window %q", name, wi.State,
				wi.MaintenanceWindow)
		}
	}
}

func TestApprovalInsideAWindow(t *testing.T) {
	inside := saturday.Add(45 * time.Minute)
	h := windowOrchestrator(t, saturdayWindow)
	h.clock.set(inside)
	h.changingItem(t, "WI-0001", "T3")
	if wi := h.approveBoth(t, "WI-0001"); wi.State != StateDone {
		t.Errorf("want an apply at once with 75 minutes left, got state %s", wi.State)
	}
	if times := h.runners.applyTimes(); len(times) != 1 || !times[0].Equal(inside) {
		t.Errorf("want one apply at the time of the approval, got %v", times)
	}
	// With less than the reserve left, the change waits for the window of the next week.
	h.clock.set(saturday.Add(119 * time.Minute))
	h.changingItem(t, "WI-0002", "T3")
	wantWait(t, h.approveBoth(t, "WI-0002"), nextSaturday, nextSaturday)
}

func TestApproverTimeAndWindowTogether(t *testing.T) {
	h := windowOrchestrator(t, saturdayWindow)
	h.changingItem(t, "WI-0001", "T3")
	h.changingItem(t, "WI-0002", "T3")
	decide := func(id, user string, at time.Time) {
		t.Helper()
		if _, err := h.Decide(context.Background(), id, Decision{User: user, Decision: "approve",
			ScheduledFor: store.FormatTime(at)}); err != nil {
			t.Fatal(err)
		}
	}
	// A time inside the window is kept. A time outside moves to the next window.
	decide("WI-0001", "alice", saturday.Add(time.Hour))
	wantWait(t, h.approveBoth(t, "WI-0001"), saturday.Add(time.Hour), saturday)
	decide("WI-0002", "alice", saturday.Add(48*time.Hour))
	wantWait(t, h.approveBoth(t, "WI-0002"), nextSaturday, nextSaturday)
}

func TestStalePlanAtTheWindowGoesBackForApproval(t *testing.T) {
	h := windowOrchestrator(t, saturdayWindow)
	h.changingItem(t, "WI-0001", "T3")
	wantWait(t, h.approveBoth(t, "WI-0001"), saturday, saturday)
	h.runners.applyErr = fmt.Errorf("%w: the state has a new serial", jobspec.ErrStalePlan)
	wi := h.runDue(t, "WI-0001", saturday)
	if wi.State != StateAwaitingApproval || wi.MaintenanceWindow != "" {
		t.Fatalf("want AwaitingApproval and no window after a stale plan, got %s and %q",
			wi.State, wi.MaintenanceWindow)
	}
	if refused := h.events(t, "WI-0001", "apply.refused"); len(refused) != 1 {
		t.Errorf("want one apply.refused event, got %d", len(refused))
	}
	approvers, err := h.ValidApprovers(wi)
	if err != nil || len(approvers) != 0 {
		t.Errorf("want no approval that covers the new plan, got %v (%v)", approvers, err)
	}
	// The window is over when the second approval of the new plan comes. The time of the
	// first wait is past and must not start the apply.
	h.runners.applyErr = nil
	h.clock.set(saturday.Add(3 * time.Hour))
	wantWait(t, h.approveBoth(t, "WI-0001"), nextSaturday, nextSaturday)
	if times := h.runners.applyTimes(); len(times) != 1 {
		t.Errorf("want only the one refused apply, got %v", times)
	}
}

func TestAWindowThatWasMissedIsNotUsed(t *testing.T) {
	h := windowOrchestrator(t, saturdayWindow)
	h.changingItem(t, "WI-0001", "T3")
	wantWait(t, h.approveBoth(t, "WI-0001"), saturday, saturday)
	// The control plane did not run during the window.
	wantWait(t, h.runDue(t, "WI-0001", saturday.Add(5*time.Hour)), nextSaturday, nextSaturday)
	if times := h.runners.applyTimes(); len(times) != 0 {
		t.Errorf("want no apply outside the window, got %v", times)
	}
	if waits := h.events(t, "WI-0001", "apply.waits_for_window"); len(waits) != 2 {
		t.Errorf("want a second wait event for the next window, got %d", len(waits))
	}
	// The windows are removed during the wait: the change applies at the time it has.
	h.setWindows(t, `{"windows": []}`)
	wantWait(t, h.runDue(t, "WI-0001", nextSaturday.Add(-time.Hour)), nextSaturday, nextSaturday)
	if wi := h.runDue(t, "WI-0001", nextSaturday); wi.State != StateDone {
		t.Errorf("want Done at the scheduled time, got %s", wi.State)
	}
}

func TestARejectEndsTheWait(t *testing.T) {
	h := windowOrchestrator(t, saturdayWindow)
	h.changingItem(t, "WI-0001", "T3")
	wantWait(t, h.approveBoth(t, "WI-0001"), saturday, saturday)
	for _, decision := range []string{"approve", "request_changes"} {
		_, err := h.Decide(context.Background(), "WI-0001", Decision{User: "alice",
			Decision: decision, Comment: "not this week"})
		if !errors.Is(err, ErrConflict) {
			t.Errorf("%s on a scheduled work item: want a conflict, got %v", decision, err)
		}
	}
	if _, err := h.Decide(context.Background(), "WI-0001", Decision{User: "alice",
		Decision: "reject", Comment: "not this week"}); err != nil {
		t.Fatal(err)
	}
	recorded := h.events(t, "WI-0001", "approval.recorded")
	if last := recorded[len(recorded)-1]; last.Data["decision"] != "reject" ||
		last.Actor != "alice" {
		t.Errorf("want an audit event for the reject by alice, got %+v", last)
	}
	wi := h.runDue(t, "WI-0001", saturday)
	if wi.State != StateRejected || wi.MaintenanceWindow != "" {
		t.Errorf("want Rejected and no window, got %s and %q", wi.State, wi.MaintenanceWindow)
	}
	if times := h.runners.applyTimes(); len(times) != 0 || count(t, h.Orchestrator, "grants") != 0 {
		t.Errorf("want no apply and no grant after the reject, got applies %v", times)
	}
}

func TestWindowsFileThatIsNotValidStopsTierT3(t *testing.T) {
	h := windowOrchestrator(t, `{"windows": "always"}`)
	h.changingItem(t, "WI-0001", "T3")
	if _, err := approve(t, h.Orchestrator, "WI-0001", "alice"); err != nil {
		t.Fatal(err)
	}
	_, err := approve(t, h.Orchestrator, "WI-0001", "bob")
	if err == nil || !strings.Contains(err.Error(), policy.WindowsPath) {
		t.Fatalf("want an error that names the windows file, got %v", err)
	}
	if wi := h.settled(t, "WI-0001"); wi.State != StateAwaitingApproval {
		t.Errorf("want AwaitingApproval while the file is not valid, got %s", wi.State)
	}
	// With a correct file, an approval starts the wait.
	h.setWindows(t, saturdayWindow)
	if _, err := approve(t, h.Orchestrator, "WI-0001", "bob"); err != nil {
		t.Fatal(err)
	}
	wantWait(t, h.settled(t, "WI-0001"), saturday, saturday)
	// The file is not valid again when the window opens: nothing applies.
	h.setWindows(t, `{}`)
	h.clock.set(saturday)
	if err := h.RunDue(); err == nil || !strings.Contains(err.Error(), policy.WindowsPath) {
		t.Errorf("want an error that names the windows file, got %v", err)
	}
	if wi := h.settled(t, "WI-0001"); wi.State != StateScheduled {
		t.Errorf("want Scheduled while the file is not valid, got %s", wi.State)
	}
}
