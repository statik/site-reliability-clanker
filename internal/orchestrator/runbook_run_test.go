package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// fakeRunners is a runner that answers each job with a fixed result.
type fakeRunners struct {
	jobs   []jobspec.Spec
	result jobspec.Result
	err    error
}

func (f *fakeRunners) Run(_ context.Context, job jobspec.Job) (jobspec.Result, error) {
	f.jobs = append(f.jobs, job.Spec)
	return f.result, f.err
}

// extraRunbooks are definitions that the repository does not have: one that a standing
// approval cannot cover, one that is not valid and one without a script.
var extraRunbooks = fstest.MapFS{
	"drop-database.json": {Data: []byte(`{"description":"Drop the database.","tier":"T3"}`)},
	"drop-database.sh":   {Data: []byte("#!/bin/bash\n"), Mode: 0o755},
	"broken.json":        {Data: []byte(`{"description":"No tier."}`)},
	"no-script.json":     {Data: []byte(`{"description":"Has no script.","tier":"T1"}`)},
}

// runbookOrchestrator has the runbooks of this repository in its forge, and the given
// definitions beside them. The user carol can sign.
func runbookOrchestrator(t *testing.T, extra fs.FS) (*Orchestrator, *fakeRunners) {
	t.Helper()
	db := storetest.Open(t)
	clock := fixedClock{}
	key, err := broker.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := broker.New(db, clock, key, &refusingIssuer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := forge.New(db)
	sources := packtest.Runbooks(t)
	if extra != nil {
		sources = packtest.Runbooks(t, extra)
	}
	if err := f.Seed(RunbooksRepo, sources, "Seed runbooks"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (name, display, role, email) VALUES
		('carol', 'Carol Reyes', 'admin', 'carol@example.com')`); err != nil {
		t.Fatal(err)
	}
	runners := &fakeRunners{}
	o := &Orchestrator{DB: db, Audit: audit.New(db, clock), Forge: f, Broker: b, Clock: clock,
		App:     packtest.Example(t),
		Runners: runners, Envs: map[string]EnvConfig{"local": {Name: "local", Cloud: "local"}},
	}
	setTargets(t, o, "local", map[string]any{"application": "shop", "data": "shop-data"})
	return o, runners
}

// setTargets records the output targets of the layer of the platform, as an apply of that
// layer writes it.
func setTargets(t *testing.T, o *Orchestrator, env string, targets map[string]any) {
	t.Helper()
	if err := store.SetLayerOutputs(o.DB, env, pack.TargetsLayer,
		map[string]any{pack.TargetsOutput: targets}, "2026-11-25T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, o *Orchestrator, table string) int {
	t.Helper()
	var n int
	if err := o.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func eventData(t *testing.T, o *Orchestrator, eventType string) map[string]any {
	t.Helper()
	var raw string
	if err := o.DB.QueryRow(`SELECT data FROM events WHERE type = $1`, eventType).Scan(
		&raw); err != nil {
		t.Fatalf("no %s event: %v", eventType, err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRunbooksListsTheDefinitionsOfTheRepository(t *testing.T) {
	o, _ := runbookOrchestrator(t, nil)
	definitions, err := o.Runbooks()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range definitions {
		names = append(names, d.Name)
	}
	want := "db-backup db-maintenance restart-component restore-drill"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("runbooks = %s, want %s", got, want)
	}
}

func TestRunbooksFailsOnADefinitionThatIsNotValid(t *testing.T) {
	o, _ := runbookOrchestrator(t, extraRunbooks)
	if _, err := o.Runbooks(); err == nil || !strings.Contains(err.Error(), "runbook broken") {
		t.Fatalf("error = %v, want one that names the runbook", err)
	}
	// A run reads only its own definition, so the other runbooks still work.
	if _, err := o.CheckRunbook("db-backup", nil); err != nil {
		t.Errorf("db-backup beside a bad definition: %v", err)
	}
}

func TestARequestIsRefusedBeforeAWorkItemOrAGrantExists(t *testing.T) {
	o, runners := runbookOrchestrator(t, extraRunbooks)
	sign(t, o, "restart-component", "db-backup")
	cases := map[string]struct {
		name   string
		params map[string]string
		want   string
	}{
		"a runbook that does not exist": {"delete-namespace", nil, "has no definition"},
		"a name that is not valid":      {"../policy/tier", nil, "is not valid"},
		"a script name":                 {"db-backup.sh", nil, "is not valid"},
		"a missing input":               {"restart-component", nil, `needs the input "component"`},
		"a value outside the set": {"restart-component",
			map[string]string{"component": "postgres"}, "is not one of"},
		"an input that is not declared": {"restart-component",
			map[string]string{"component": "web", "namespace": "kube-system"},
			`has no input "namespace"`},
		"a parameter for a runbook without inputs": {"db-backup",
			map[string]string{"file": "/etc/passwd"}, `has no input "file"`},
		"a tier above the standing approval": {"drop-database", nil, "covers only tier T1"},
	}
	for label, tc := range cases {
		_, err := o.RunRunbook(context.Background(), RunbookRequest{Env: "local", Name: tc.name,
			Params: tc.params, Actor: "carol"})
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want an invalid request with %q", label, err, tc.want)
		}
		if _, err := o.CheckRunbook(tc.name, tc.params); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: the check alone gives %v", label, err)
		}
	}
	wantNothingStarted(t, o, runners)
}

// TestARunStopsBeforeAWorkItemForOtherFaults has the faults that are not in the request:
// the definition is bad, nobody signed, the script is missing, the environment is not
// known, or the environment has no targets because its platform layer is not applied.
func TestARunStopsBeforeAWorkItemForOtherFaults(t *testing.T) {
	o, runners := runbookOrchestrator(t, extraRunbooks)
	o.Envs["bare"] = EnvConfig{Name: "bare", Cloud: "local"}
	sign(t, o, "restart-component", "no-script")
	web := map[string]string{"component": "web"}
	cases := map[string]struct {
		request RunbookRequest
		want    string
	}{
		"a definition that is not valid": {RunbookRequest{Env: "local", Name: "broken"},
			"is not valid"},
		"no standing approval": {RunbookRequest{Env: "local", Name: "restore-drill"},
			"has no standing approval"},
		"a definition without a script": {RunbookRequest{Env: "local", Name: "no-script"},
			"no script"},
		"an unknown environment": {RunbookRequest{Env: "moon", Name: "restart-component",
			Params: web}, "is not configured"},
		"an environment without targets": {RunbookRequest{Env: "bare",
			Name: "restart-component", Params: web}, "has no output targets"},
	}
	for label, tc := range cases {
		_, err := o.RunRunbook(context.Background(), tc.request)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want one with %q", label, err, tc.want)
		}
	}
	wantNothingStarted(t, o, runners)
}

func sign(t *testing.T, o *Orchestrator, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := o.SignStandingApproval(name, "carol"); err != nil {
			t.Fatal(err)
		}
	}
}

func wantNothingStarted(t *testing.T, o *Orchestrator, runners *fakeRunners) {
	t.Helper()
	for _, table := range []string{"work_items", "grants", "jobs"} {
		if n := count(t, o, table); n != 0 {
			t.Errorf("the table %s has %d rows after the refusals, want none", table, n)
		}
	}
	if len(runners.jobs) != 0 {
		t.Errorf("a runner got %d jobs after the refusals, want none", len(runners.jobs))
	}
}

func TestAStandingApprovalNeedsARunbookThatItCanCover(t *testing.T) {
	o, _ := runbookOrchestrator(t, extraRunbooks)
	for name, want := range map[string]string{"delete-namespace": "has no definition",
		"drop-database": "covers only tier T1", "broken": "is not valid"} {
		err := o.SignStandingApproval(name, "carol")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %v, want one with %q", name, err, want)
		}
		if approver, err := o.StandingApprover(name); approver != "" || err != nil {
			t.Errorf("%s has the standing approval of %q, %v", name, approver, err)
		}
	}
}

func TestAnApproverSignsAndRevokesAStandingApproval(t *testing.T) {
	o, runners := runbookOrchestrator(t, nil)
	if _, err := o.DB.Exec(`INSERT INTO users (name, display, role, email) VALUES
		('alice', 'Alice Chen', 'approver', 'alice@example.com')`); err != nil {
		t.Fatal(err)
	}
	if err := o.SignStandingApproval("db-backup", "alice"); err != nil {
		t.Fatal(err)
	}
	signed := StandingApproval{Runbook: "db-backup", Signer: "alice",
		SignedAt: "2026-11-21T02:00:00.000Z"}
	if got, err := o.StandingApproval("db-backup"); err != nil || got != signed {
		t.Errorf("standing approval = %+v, %v; want %+v", got, err, signed)
	}
	if err := o.RevokeStandingApproval("db-backup", "carol"); err != nil {
		t.Fatal(err)
	}
	if got, err := o.StandingApproval("db-backup"); err != nil ||
		got != (StandingApproval{Runbook: "db-backup"}) {
		t.Errorf("standing approval after the revocation = %+v, %v; want none", got, err)
	}
	if data := eventData(t, o, "standing_approval.revoked"); data["runbook"] != "db-backup" ||
		data["signer"] != "alice" {
		t.Errorf("revocation recorded %v, want the runbook and the signer alice", data)
	}
	err := o.RevokeStandingApproval("db-backup", "carol")
	if !errors.Is(err, ErrConflict) {
		t.Errorf("a second revocation: error = %v, want ErrConflict", err)
	}
	_, err = o.RunRunbook(context.Background(), RunbookRequest{Env: "local", Name: "db-backup",
		Actor: "carol"})
	if err == nil || !strings.Contains(err.Error(), "has no standing approval") {
		t.Errorf("a run after the revocation: error = %v, want a refusal", err)
	}
	wantNothingStarted(t, o, runners)
}

func TestOnlyAnApproverOrAnAdminSignsOrRevokesAStandingApproval(t *testing.T) {
	o, _ := runbookOrchestrator(t, nil)
	if _, err := o.DB.Exec(`INSERT INTO users (name, display, role, email) VALUES
		('dave', 'Dave Okafor', 'viewer', 'dave@example.com')`); err != nil {
		t.Fatal(err)
	}
	sign(t, o, "restart-component")
	for _, user := range []string{"dave", "mallory"} {
		if err := o.SignStandingApproval("db-backup", user); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s signs: error = %v, want ErrForbidden", user, err)
		}
		err := o.RevokeStandingApproval("restart-component", user)
		if !errors.Is(err, ErrForbidden) {
			t.Errorf("%s revokes: error = %v, want ErrForbidden", user, err)
		}
	}
	if approver, err := o.StandingApprover("db-backup"); approver != "" || err != nil {
		t.Errorf("db-backup has the standing approval of %q, %v", approver, err)
	}
	if approver, err := o.StandingApprover("restart-component"); approver != "carol" ||
		err != nil {
		t.Errorf("restart-component has the standing approval of %q, %v; want carol",
			approver, err)
	}
	err := o.RevokeStandingApproval("../kv", "carol")
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("a revocation of a name that is not valid: error = %v, want ErrInvalid", err)
	}
}

// runBackup runs db-backup with a runner that reports the given outputs.
func runBackup(t *testing.T, reported map[string]any) (*Orchestrator, *fakeRunners, WorkItem) {
	t.Helper()
	o, runners := runbookOrchestrator(t, nil)
	sign(t, o, "db-backup")
	runners.result = jobspec.Result{Status: jobspec.StatusOK, Outputs: reported}
	wi, err := o.RunRunbook(context.Background(), RunbookRequest{Env: "local", Name: "db-backup",
		Actor: "carol"})
	if err != nil {
		t.Fatal(err)
	}
	return o, runners, wi
}

var backupOutputs = map[string]any{"backup_file": "/backups/paperboat-1.dump", "objects": "31"}

func TestARunGivesTheRunnerTheRunbookAtTheCommitOfTheWorkItem(t *testing.T) {
	_, runners, wi := runBackup(t, backupOutputs)
	want := WorkItem{State: StateDone, Tier: "T1", Kind: "runbook:db-backup"}
	if got := (WorkItem{State: wi.State, Tier: wi.Tier, Kind: wi.Kind}); !reflect.DeepEqual(got,
		want) {
		t.Errorf("work item = %+v, want %+v", got, want)
	}
	if len(runners.jobs) != 1 {
		t.Fatalf("the runner got %d jobs", len(runners.jobs))
	}
	wantJob := jobspec.Spec{Kind: jobspec.KindRunbook, Env: "local", Runbook: "db-backup",
		CheckoutID: "runbooks/" + wi.Commit, Namespace: "shop", DataNamespace: "shop-data"}
	if !reflect.DeepEqual(runners.jobs[0], wantJob) {
		t.Errorf("runner job = %+v, want %+v", runners.jobs[0], wantJob)
	}
}

// On a cloud the data services are not in the cluster. The job has no data namespace, and
// the script decides whether it can run without one.
func TestARunOnAnEnvironmentWithoutADataNamespaceHasOnlyTheApplication(t *testing.T) {
	o, runners := runbookOrchestrator(t, nil)
	o.Envs["prod"] = EnvConfig{Name: "prod", Cloud: "aws"}
	setTargets(t, o, "prod", map[string]any{"application": "shop", "data": ""})
	sign(t, o, "restart-component")
	wi, err := o.RunRunbook(context.Background(), RunbookRequest{Env: "prod",
		Name: "restart-component", Params: map[string]string{"component": "api"},
		Actor: "carol"})
	if err != nil || wi.State != StateDone {
		t.Fatalf("state %s, %v; want a finished run", wi.State, err)
	}
	if got := runners.jobs[0]; got.Namespace != "shop" || got.DataNamespace != "" {
		t.Errorf("runner job targets = %q and %q, want shop and none", got.Namespace,
			got.DataNamespace)
	}
}

func TestARunRecordsItsTypedOutputs(t *testing.T) {
	o, _, wi := runBackup(t, backupOutputs)
	data := eventData(t, o, "runbook.succeeded")
	// The count is a number in the record, as the definition declares it.
	wantOutputs := map[string]any{"backup_file": "/backups/paperboat-1.dump",
		"objects": float64(31)}
	if !reflect.DeepEqual(data["outputs"], wantOutputs) {
		t.Errorf("recorded outputs = %#v, want %#v", data["outputs"], wantOutputs)
	}
	if data["commit"] != wi.Commit {
		t.Errorf("recorded commit = %v, want %s", data["commit"], wi.Commit)
	}
	jobs, err := o.Jobs(wi.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("the work item has %d jobs", len(jobs))
	}
	log, err := o.JobLog(jobs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"target application=shop\ntarget data=shop-data\n",
		"output objects=31"} {
		if !strings.Contains(log, line) {
			t.Errorf("job log does not have %q: %q", line, log)
		}
	}
}

func TestARunRecordsTheInputsThatItGot(t *testing.T) {
	o, runners := runbookOrchestrator(t, nil)
	if err := o.SignStandingApproval("restart-component", "carol"); err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"component": "worker"}
	if _, err := o.RunRunbook(context.Background(), RunbookRequest{Env: "local",
		Name: "restart-component", Params: params, Actor: "investigator"}); err != nil {
		t.Fatal(err)
	}
	if got := runners.jobs[0].Params["component"]; got != "worker" {
		t.Errorf("the runner got component %q", got)
	}
	recorded, _ := eventData(t, o, "runbook.succeeded")["params"].(map[string]any)
	if recorded["component"] != "worker" {
		t.Errorf("recorded params = %#v", recorded)
	}
}

func TestARunThatReportsOtherOutputsFails(t *testing.T) {
	cases := map[string]map[string]any{
		"an output is missing":      {"backup_file": "/backups/a.dump"},
		"an output is not declared": {"backup_file": "/backups/a.dump", "objects": "31", "x": "1"},
		"an output has a bad type":  {"backup_file": "/backups/a.dump", "objects": "many"},
		"no output":                 nil,
	}
	for label, reported := range cases {
		o, _, wi := runBackup(t, reported)
		if wi.State != StateFailed {
			t.Errorf("%s: state %s, want a failed work item", label, wi.State)
			continue
		}
		data := eventData(t, o, "runbook.failed")
		if _, recorded := data["outputs"]; recorded {
			t.Errorf("%s: the failed run has outputs on record: %#v", label, data)
		}
		if data["error"] == "" {
			t.Errorf("%s: the failed run has no error on record", label)
		}
		var open int
		if err := o.DB.QueryRow(`SELECT COUNT(*) FROM grants WHERE ended_at = ''`).Scan(
			&open); err != nil {
			t.Fatal(err)
		}
		if open != 0 {
			t.Errorf("%s: %d grants are open", label, open)
		}
	}
}

func TestAFailedScriptFailsTheWorkItem(t *testing.T) {
	o, runners := runbookOrchestrator(t, nil)
	if err := o.SignStandingApproval("db-maintenance", "carol"); err != nil {
		t.Fatal(err)
	}
	runners.err = errors.New("runbook db-maintenance: exit status 1")
	wi, err := o.RunRunbook(context.Background(), RunbookRequest{Env: "local",
		Name: "db-maintenance", Actor: "carol"})
	if err != nil || wi.State != StateFailed {
		t.Fatalf("state %s, %v; want a failed work item", wi.State, err)
	}
	if data := eventData(t, o, "runbook.failed"); data["error"] != runners.err.Error() {
		t.Errorf("event data = %#v", data)
	}
}
