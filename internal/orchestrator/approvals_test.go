package orchestrator

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/policy"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// noSettingsFile is the content for a runbooks repository that has no approval settings.
const noSettingsFile = ""

// approvalOrchestrator has a runbooks repository whose approval settings file has the
// given content, the approvers alice and bob, and a broker that reads the same settings.
func approvalOrchestrator(t *testing.T, settings string) *Orchestrator {
	t.Helper()
	db := storetest.Open(t)
	clock := fixedClock{}
	f := forge.New(db)
	files := fstest.MapFS{"README.md": {Data: []byte("Runbooks\n")}}
	if settings != noSettingsFile {
		files[policy.ApprovalsPath] = &fstest.MapFile{Data: []byte(settings)}
	}
	if err := f.Seed(RunbooksRepo, map[string]fs.FS{".": files}, "Seed runbooks"); err != nil {
		t.Fatal(err)
	}
	key, err := broker.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := broker.New(db, clock, key, nil,
		func() (policy.Approvals, error) { return ApprovalSettings(f) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (name, display, role, email) VALUES
		('alice', 'Alice Ng', 'approver', 'alice@example.com'),
		('bob', 'Bob Okafor', 'approver', 'bob@example.com')`); err != nil {
		t.Fatal(err)
	}
	o := &Orchestrator{DB: db, Audit: audit.New(db, clock), Forge: f, Broker: b, Clock: clock}
	// An approval starts the apply in the background. The apply must end before the
	// database of the test goes away.
	t.Cleanup(o.WaitForApplies)
	return o
}

// tieredItem makes a work item in the given state with one saved plan that changes
// nothing, so that an apply after the approval ends at once.
func tieredItem(t *testing.T, o *Orchestrator, id, tier, state string) WorkItem {
	t.Helper()
	if _, err := o.DB.Exec(`INSERT INTO work_items (id, title, kind, env, layers, state, tier,
		commit_sha, created_at, updated_at) VALUES ($1, 'Replace the database', 'manual',
		'local', '["30-data"]', $2, $3, 'c0ffee', 'then', 'then')`, id, state, tier); err != nil {
		t.Fatal(err)
	}
	if _, err := o.DB.Exec(`INSERT INTO plans (work_item, layer, commit_sha, plan_sha256, plan,
		plan_json, created_at) VALUES ($1, '30-data', 'c0ffee', 'first-plan', '', '{}', 'then')`,
		id); err != nil {
		t.Fatal(err)
	}
	wi, err := o.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return wi
}

func setSettings(t *testing.T, o *Orchestrator, content string) {
	t.Helper()
	if _, err := o.Forge.Commit(forge.CommitRequest{Repo: RunbooksRepo, Branch: "main",
		Author: "test", Message: "Change the approval settings",
		Files: map[string]string{policy.ApprovalsPath: content}}); err != nil {
		t.Fatal(err)
	}
}

func approve(t *testing.T, o *Orchestrator, id, user string) (WorkItem, error) {
	t.Helper()
	return o.Decide(context.Background(), id, Decision{User: user, Decision: "approve"})
}

func storedState(t *testing.T, o *Orchestrator, id string) string {
	t.Helper()
	wi, err := o.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return wi.State
}

func TestApprovalSettingsOfTheRepositoryOfThisCheckout(t *testing.T) {
	o, _ := runbookOrchestrator(t, nil)
	for tier, want := range map[string]int{"T3": 2, "T2": 1, "T1": 0} {
		if got, err := o.ApproversRequired(tier); err != nil || got != want {
			t.Errorf("tier %s: %d approvers and error %v, want %d", tier, got, err, want)
		}
	}
}

func TestApproversRequiredReadsTheSettingsAtEachCall(t *testing.T) {
	o := approvalOrchestrator(t, noSettingsFile)
	for _, step := range []struct {
		content string
		want    int
	}{{noSettingsFile, 2}, {`{"t3_approvers": 1}`, 1}, {`{"t3_approvers": 2}`, 2}} {
		if step.content != noSettingsFile {
			setSettings(t, o, step.content)
		}
		if got, err := o.ApproversRequired("T3"); err != nil || got != step.want {
			t.Errorf("settings %q: %d approvers and error %v, want %d", step.content, got, err,
				step.want)
		}
	}
}

func TestApproversRequiredRefusesSettingsThatAreNotValid(t *testing.T) {
	for _, content := range []string{`{"t3_approvers": 0}`, `{"t3_approvers": 3}`, `{}`, `x`} {
		o := approvalOrchestrator(t, content)
		for _, tier := range []string{"T3", "T2"} {
			_, err := o.ApproversRequired(tier)
			if err == nil || !strings.Contains(err.Error(), policy.ApprovalsPath) {
				t.Errorf("settings %q, tier %s: want an error that names the file, got %v",
					content, tier, err)
			}
		}
	}
}

func TestApprovalSettingsNeedTheRunbooksRepository(t *testing.T) {
	_, err := ApprovalSettings(forge.New(storetest.Open(t)))
	if err == nil || !strings.Contains(err.Error(), "read the approval settings") {
		t.Errorf("want an error for a forge with no runbooks repository, got %v", err)
	}
}

func TestOneApproverApprovesT3WhenTheSettingsSayOne(t *testing.T) {
	o := approvalOrchestrator(t, `{"t3_approvers": 1}`)
	tieredItem(t, o, "WI-0001", "T3", StateAwaitingApproval)
	wi, err := approve(t, o, "WI-0001", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if wi.State != StateApproved {
		t.Errorf("state after one approver = %s, want %s", wi.State, StateApproved)
	}
}

func TestOneApproverDoesNotApproveT3WithTwoNeeded(t *testing.T) {
	for _, settings := range []string{noSettingsFile, `{"t3_approvers": 2}`} {
		o := approvalOrchestrator(t, settings)
		tieredItem(t, o, "WI-0001", "T3", StateAwaitingApproval)
		wi, err := approve(t, o, "WI-0001", "alice")
		if err != nil {
			t.Fatal(err)
		}
		if wi.State != StateAwaitingApproval {
			t.Errorf("settings %q: state after one approver = %s, want %s", settings, wi.State,
				StateAwaitingApproval)
		}
		// The same person again is not a second approver.
		if wi, err = approve(t, o, "WI-0001", "alice"); err != nil || wi.State !=
			StateAwaitingApproval {
			t.Errorf("settings %q: the same approver again gave state %s and error %v",
				settings, wi.State, err)
		}
		if wi, err = approve(t, o, "WI-0001", "bob"); err != nil || wi.State != StateApproved {
			t.Errorf("settings %q: the second approver gave state %s and error %v, want %s",
				settings, wi.State, err, StateApproved)
		}
	}
}

// refusedForSettings reports whether err is the error of the settings file with the value 0.
func refusedForSettings(err error) bool {
	return err != nil && strings.Contains(err.Error(), "t3_approvers = 0") &&
		strings.Contains(err.Error(), policy.ApprovalsPath)
}

func TestNoApprovalCompletesWhenTheSettingsAreNotValid(t *testing.T) {
	o := approvalOrchestrator(t, `{"t3_approvers": 0}`)
	tieredItem(t, o, "WI-0001", "T3", StateAwaitingApproval)
	tieredItem(t, o, "WI-0002", "T2", StateAwaitingApproval)
	for _, decision := range [][2]string{{"WI-0001", "alice"}, {"WI-0001", "bob"},
		{"WI-0002", "alice"}} {
		id, user := decision[0], decision[1]
		if _, err := approve(t, o, id, user); !refusedForSettings(err) {
			t.Errorf("%s by %s: want the error of the settings file, got %v", id, user, err)
		}
		if got := storedState(t, o, id); got != StateAwaitingApproval {
			t.Errorf("%s after %s: state = %s, want %s", id, user, got, StateAwaitingApproval)
		}
	}
	grants, err := o.Broker.List(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 0 {
		t.Errorf("%d grants issued, want none", len(grants))
	}
}

// A decision that came while the settings were not valid stays on record. This test pins
// that: after a repair of the file, the next decision counts it.
func TestADecisionUnderSettingsThatAreNotValidCountsAfterTheRepair(t *testing.T) {
	o := approvalOrchestrator(t, `{"t3_approvers": 0}`)
	tieredItem(t, o, "WI-0001", "T3", StateAwaitingApproval)
	if _, err := approve(t, o, "WI-0001", "alice"); !refusedForSettings(err) {
		t.Fatalf("want the error of the settings file, got %v", err)
	}
	setSettings(t, o, `{"t3_approvers": 2}`)
	wi, err := approve(t, o, "WI-0001", "bob")
	if err != nil || wi.State != StateApproved {
		t.Errorf("after the repair: state %s and error %v, want %s", wi.State, err,
			StateApproved)
	}
}

func TestRequestApprovalSaysTheNumberOfTheSettings(t *testing.T) {
	for settings, want := range map[string]float64{noSettingsFile: 2,
		`{"t3_approvers": 2}`: 2, `{"t3_approvers": 1}`: 1} {
		o := approvalOrchestrator(t, settings)
		wi := tieredItem(t, o, "WI-0001", "T3", StatePlanned)
		wi, err := o.requestApproval(context.Background(), wi, false)
		if err != nil {
			t.Fatal(err)
		}
		if wi.State != StateAwaitingApproval {
			t.Errorf("settings %q: state = %s, want %s", settings, wi.State,
				StateAwaitingApproval)
		}
		if got := eventData(t, o, "approval.requested")["approvers_required"]; got != want {
			t.Errorf("settings %q: approvers_required = %v, want %v", settings, got, want)
		}
		wantReason := "tier T3 needs 2 approvers"
		if want == 1 {
			wantReason = "tier T3 needs 1 approvers"
		}
		if got := eventData(t, o, "workitem.state_changed")["reason"]; got != wantReason {
			t.Errorf("settings %q: reason = %v, want %q", settings, got, wantReason)
		}
	}
}

func TestRequestApprovalAgainSaysTheNumberOfTheSettings(t *testing.T) {
	o := approvalOrchestrator(t, `{"t3_approvers": 1}`)
	wi := tieredItem(t, o, "WI-0001", "T3", StatePlanned)
	if _, err := o.requestApproval(context.Background(), wi, true); err != nil {
		t.Fatal(err)
	}
	if got := eventData(t, o, "approval.requested")["approvers_required"]; got != 1.0 {
		t.Errorf("approvers_required = %v, want 1", got)
	}
}

func TestRequestApprovalFailsTheWorkItemWhenTheSettingsAreNotValid(t *testing.T) {
	for _, again := range []bool{false, true} {
		o := approvalOrchestrator(t, `{"t3_approvers": "two"}`)
		wi := tieredItem(t, o, "WI-0001", "T3", StatePlanned)
		wi, err := o.requestApproval(context.Background(), wi, again)
		if err != nil {
			t.Fatal(err)
		}
		if wi.State != StateFailed || storedState(t, o, "WI-0001") != StateFailed {
			t.Errorf("again %v: state = %s, want %s", again, wi.State, StateFailed)
		}
		reason, _ := eventData(t, o, "workitem.state_changed")["reason"].(string)
		if !strings.Contains(reason, policy.ApprovalsPath) {
			t.Errorf("again %v: the reason %q does not name the settings file", again, reason)
		}
		if n := count(t, o, "events WHERE type = 'approval.requested'"); n != 0 {
			t.Errorf("again %v: %d requests for approval, want none", again, n)
		}
	}
}

func TestTheBrokerOfTheControlPlaneReadsTheSameSettings(t *testing.T) {
	o := approvalOrchestrator(t, noSettingsFile)
	req := broker.Request{WorkItem: "WI-0001", Env: "local", Layer: "30-data",
		Kind: broker.KindApply, Commit: "c0ffee", PlanSHA256: "first-plan", Tier: "T3",
		Approvers: []string{"alice"}, Duration: time.Minute}
	if _, err := o.Broker.Issue(req); err == nil {
		t.Error("no settings file: want no grant for one approver")
	}
	setSettings(t, o, `{"t3_approvers": 1}`)
	if _, err := o.Broker.Issue(req); err != nil {
		t.Errorf("settings at one approver: %v", err)
	}
	setSettings(t, o, `{"t3_approvers": 5}`)
	if _, err := o.Broker.Issue(req); err == nil ||
		!strings.Contains(err.Error(), "t3_approvers = 5") {
		t.Errorf("settings that are not valid: want the error of the file, got %v", err)
	}
}
