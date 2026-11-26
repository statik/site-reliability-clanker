package orchestrator

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
)

// The tests use the second pack: layer 50-app, version variable harbor_release, records
// at apps/harbor/releases/<version>.json in the platform repository.
const (
	harborTfvars   = "platform/stacks/local/50-app/terraform.tfvars"
	schemaChange   = `{"schema_change": true}`
	noSchemaChange = `{"values": {"image": {"tag": "x"}}, "schema_change": false}`
)

// harborVars returns the terraform.tfvars of the layer of the second pack.
func harborVars(version string, replicas int) string {
	return fmt.Sprintf("env_name         = \"local\"\nharbor_release   = %q\n"+
		"gateway_replicas = %d\n", version, replicas)
}

// recordsOrchestrator has the second pack on the local target, its layer on main at
// version, and the records on main by version. No record makes no directory of records.
func recordsOrchestrator(t *testing.T, version string, records map[string]string) *Orchestrator {
	t.Helper()
	o, _ := cloudOrchestrator(t, localEnv, nil, "40-platform")
	o.App = packtest.Second(t)
	files := map[string]string{harborTfvars: harborVars(version, 2)}
	for release, content := range records {
		files[o.App.RecordPath(release)] = content
	}
	if _, err := o.Forge.Commit(forge.CommitRequest{Repo: PlatformRepo, Branch: "main",
		Author: "test", Message: "Seed the layer of harbor", Files: files}); err != nil {
		t.Fatal(err)
	}
	return o
}

// failVerify fails the verification of every work item except a rollback.
func failVerify(_ context.Context, wi WorkItem) (VerifyResult, error) {
	if wi.RollbackOf != "" {
		return VerifyResult{OK: true, Checks: []string{"version is back"}}, nil
	}
	return VerifyResult{OK: false, Reason: "the canary check failed"}, nil
}

// appliedChange opens a pull request with the files, attaches it to a new work item of
// tier T2 and merges it, as an apply does before the verification.
func appliedChange(t *testing.T, o *Orchestrator, files map[string]string) WorkItem {
	t.Helper()
	if _, err := o.Forge.Commit(forge.CommitRequest{Repo: PlatformRepo, Branch: "change",
		Base: "main", Author: "clanker-author", Message: "Change harbor",
		Files: files}); err != nil {
		t.Fatal(err)
	}
	pr, err := o.Forge.OpenPull(forge.PullRequest{Repo: PlatformRepo, Branch: "change",
		Title: "Change harbor", Author: "clanker-author"}, o.now())
	if err != nil {
		t.Fatal(err)
	}
	wi, err := o.Create(NewWorkItem{Title: "Change harbor", Kind: "app-upgrade", Env: "local",
		Trigger: "test", Actor: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if wi, err = o.AttachPull(wi.ID, pr.ID, "test"); err != nil {
		t.Fatal(err)
	}
	wi.Tier = "T2"
	if err := o.save(wi); err != nil {
		t.Fatal(err)
	}
	if err := o.mergeApplied(wi); err != nil {
		t.Fatal(err)
	}
	return wi
}

// verifyFails runs the verification of the work item with a check that fails.
func verifyFails(t *testing.T, o *Orchestrator, wi WorkItem) WorkItem {
	t.Helper()
	o.Hooks.Verify = failVerify
	wi, err := o.verify(context.Background(), wi, []string{"alice"})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return wi
}

// eventsOf returns the audit events of a work item by type.
func eventsOf(t *testing.T, o *Orchestrator, id string) map[string][]audit.Event {
	t.Helper()
	events, err := o.Audit.Query(audit.Filter{WorkItem: id})
	if err != nil {
		t.Fatal(err)
	}
	byType := map[string][]audit.Event{}
	for _, e := range events {
		byType[e.Type] = append(byType[e.Type], e)
	}
	return byType
}

// wantNoRollback checks that nothing started a rollback of the work item: no branch, no
// second pull request and no rollback work item.
func wantNoRollback(t *testing.T, o *Orchestrator, wi WorkItem) {
	t.Helper()
	branches, err := o.Forge.Branches(PlatformRepo)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(branches, "rollback/"+wi.ID) {
		t.Errorf("branches %v have the rollback branch", branches)
	}
	pulls, err := o.Forge.Pulls(PlatformRepo)
	if err != nil || len(pulls) != 1 {
		t.Errorf("pull requests %+v, %v; want only the change", pulls, err)
	}
	items, err := o.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.RollbackOf != "" {
			t.Errorf("work item %s rolls back %s", item.ID, item.RollbackOf)
		}
	}
	if len(eventsOf(t, o, wi.ID)["rollback.authorized"]) != 0 {
		t.Error("a rollback was authorized")
	}
}

func TestAFailedUpgradeToASchemaChangeEndsFailedAndIsNotRolledBack(t *testing.T) {
	o := recordsOrchestrator(t, "3.4.0", map[string]string{"3.4.0": noSchemaChange,
		"3.5.0": schemaChange})
	wi := verifyFails(t, o, appliedChange(t, o,
		map[string]string{harborTfvars: harborVars("3.5.0", 2)}))
	want := "release 3.5.0 changed the database schema; the control plane does not roll " +
		"back; restore the backup from before the apply (T3) or fix forward"
	if wi.State != StateFailed || storedState(t, o, wi.ID) != StateFailed {
		t.Fatalf("state %s, stored %s; want Failed", wi.State, storedState(t, o, wi.ID))
	}
	events := eventsOf(t, o, wi.ID)
	if len(events["verify.failed"]) != 1 {
		t.Errorf("verify.failed events %v; want one, for the usual notification",
			events["verify.failed"])
	}
	refused := events["rollback.refused"]
	if len(refused) != 1 || refused[0].Data["reason"] != want ||
		refused[0].Data["verify_reason"] != "the canary check failed" {
		t.Fatalf("rollback.refused events %+v; want one with the reason %q", refused, want)
	}
	changes := events["workitem.state_changed"]
	if last := changes[len(changes)-1]; last.Data["to"] != StateFailed ||
		last.Data["reason"] != want {
		t.Errorf("last state change %+v; want Failed with the reason %q", last.Data, want)
	}
	wantNoRollback(t, o, wi)
}

func TestAFailedChangeIsRolledBackWithoutASchemaChange(t *testing.T) {
	cases := map[string]struct {
		main    string
		records map[string]string
		change  map[string]string
	}{
		"a release whose record says false": {"3.4.0",
			map[string]string{"3.4.0": noSchemaChange, "3.5.0": noSchemaChange},
			map[string]string{harborTfvars: harborVars("3.5.0", 2)}},
		"a release with no record, as Paperboat": {"3.4.0", nil,
			map[string]string{harborTfvars: harborVars("3.5.0", 2)}},
		"a change of another variable, at a release that changed the schema": {"3.5.0",
			map[string]string{"3.4.0": noSchemaChange, "3.5.0": schemaChange},
			map[string]string{harborTfvars: harborVars("3.5.0", 3)}},
		"a change of a record only": {"3.4.0",
			map[string]string{"3.4.0": noSchemaChange, "3.5.0": noSchemaChange},
			map[string]string{"apps/harbor/releases/3.5.0.json": schemaChange}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			o := recordsOrchestrator(t, tc.main, tc.records)
			wi := verifyFails(t, o, appliedChange(t, o, tc.change))
			if wi.State != StateRolledBack {
				t.Fatalf("state %s; want RolledBack", wi.State)
			}
			events := eventsOf(t, o, wi.ID)
			if len(events["rollback.refused"]) != 0 || len(events["rollback.authorized"]) != 0 {
				t.Errorf("refused %v, authorized %v; want the authorization on the rollback "+
					"work item only", events["rollback.refused"], events["rollback.authorized"])
			}
			pulls, err := o.Forge.Pulls(PlatformRepo)
			if err != nil || len(pulls) != 2 {
				t.Fatalf("pull requests %+v, %v; want the change and its rollback", pulls, err)
			}
			restored, err := o.Forge.ReadFile(PlatformRepo, "main", harborTfvars)
			if err != nil || restored != harborVars(tc.main, 2) {
				t.Errorf("main has %q, %v; want the layer from before the change", restored, err)
			}
		})
	}
}

func TestARecordThatCannotBeReadStopsTheRollback(t *testing.T) {
	o := recordsOrchestrator(t, "3.4.0", map[string]string{"3.4.0": noSchemaChange,
		"3.5.0": `{"values": {}}`})
	wi := verifyFails(t, o, appliedChange(t, o,
		map[string]string{harborTfvars: harborVars("3.5.0", 2)}))
	if wi.State != StateFailed {
		t.Fatalf("state %s; want Failed", wi.State)
	}
	refused := eventsOf(t, o, wi.ID)["rollback.refused"]
	if len(refused) != 1 {
		t.Fatalf("rollback.refused events %+v; want one", refused)
	}
	reason, _ := refused[0].Data["reason"].(string)
	for _, part := range []string{"cannot tell", "apps/harbor/releases/3.5.0.json",
		"schema_change is required", "does not roll back"} {
		if !strings.Contains(reason, part) {
			t.Errorf("reason %q does not say %q", reason, part)
		}
	}
	wantNoRollback(t, o, wi)
}

// versionRunners answer each plan of the layer of the application with a plan that
// changes its version output.
type versionRunners struct{ before, after string }

func (r versionRunners) Run(_ context.Context, job jobspec.Job) (jobspec.Result, error) {
	plan := []byte("a plan of " + job.Spec.Layer)
	return jobspec.Result{Status: jobspec.StatusOK, Plan: plan, PlanSHA256: jobspec.SHA256(plan),
		PlanJSON: []byte(`{"output_changes":{"harbor_release":{"actions":["update"],` +
			`"before":"` + r.before + `","after":"` + r.after + `"}}}`)}, nil
}

func TestAPlanThatLowersTheVersionAcrossASchemaChangeWarnsTheApprover(t *testing.T) {
	records := map[string]string{"3.4.0": noSchemaChange, "3.5.0": schemaChange,
		"3.5.1": noSchemaChange, "3.6.0": schemaChange, "3.7.0": noSchemaChange}
	cases := map[string]struct {
		running, planned, want string
	}{
		"down across one":       {"3.5.1", "3.4.0", "across release 3.5.0, which changed"},
		"down across two":       {"3.7.0", "3.4.0", "across releases 3.5.0, 3.6.0, which each"},
		"down from the change":  {"3.6.0", "3.5.1", "across release 3.6.0, which changed"},
		"down after the change": {"3.7.0", "3.6.0", ""},
		"down with no change":   {"3.5.1", "3.5.0", ""},
		"up across one":         {"3.4.0", "3.5.1", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			o := recordsOrchestrator(t, tc.planned, records)
			o.Runners = versionRunners{before: tc.running, after: tc.planned}
			wi, err := o.Create(NewWorkItem{Title: "Drift check and reconciliation",
				Kind: "drift", Env: "local", Layers: []string{"50-app"}, Trigger: "test",
				Actor: "test"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := o.PlanWorkItem(context.Background(), wi.ID); err != nil {
				t.Fatal(err)
			}
			plans, err := o.Plans(wi.ID, false)
			if err != nil || len(plans) != 1 {
				t.Fatalf("plans %+v, %v; want one", plans, err)
			}
			warnings := plans[0].Facts.Warnings
			if tc.want == "" {
				if len(warnings) != 0 {
					t.Errorf("warnings %q; want none", warnings)
				}
				return
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0], tc.want) ||
				!strings.Contains(warnings[0], "lowers harbor_release from "+tc.running+
					" to "+tc.planned) {
				t.Errorf("warnings %q; want one with %q", warnings, tc.want)
			}
		})
	}
}

func TestAPlanThatLowersTheVersionWithNoRecordsHasNoWarning(t *testing.T) {
	o := recordsOrchestrator(t, "3.4.0", nil)
	o.Runners = versionRunners{before: "3.5.0", after: "3.4.0"}
	wi, err := o.Create(NewWorkItem{Title: "Drift", Kind: "drift", Env: "local",
		Layers: []string{"50-app"}, Trigger: "test", Actor: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.PlanWorkItem(context.Background(), wi.ID); err != nil {
		t.Fatal(err)
	}
	plans, err := o.Plans(wi.ID, false)
	if err != nil || len(plans) != 1 || len(plans[0].Facts.Warnings) != 0 {
		t.Fatalf("plans %+v, %v; want one with no warning", plans, err)
	}
}
