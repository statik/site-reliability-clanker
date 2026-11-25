package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func runScript(t *testing.T, body string) (map[string]any, string, error) {
	t.Helper()
	return runScriptOn(t, body, "harbor", "harbor-store")
}

// runScriptOn runs a script on an environment with the given targets.
func runScriptOn(t *testing.T, body, namespace, dataNamespace string) (map[string]any,
	string, error) {
	t.Helper()
	r := newRunner(t)
	checkout := Checkout{ID: "runbooks/abc", Archive: archive(t,
		entry{"jobs/job.sh", "#!/bin/bash\nset -euo pipefail\n" + body, 0o755})}
	var log bytes.Buffer
	outputs, err := r.Runbook(context.Background(), RunbookJob{Env: "local", Checkout: checkout,
		Name: "job", Namespace: namespace, DataNamespace: dataNamespace, Log: &log})
	left, globErr := filepath.Glob(filepath.Join(r.WorkRoot, "outputs-*"))
	if globErr != nil || len(left) != 0 {
		t.Errorf("the output file was not removed: %v, %v", left, globErr)
	}
	return outputs, log.String(), err
}

func TestRunbookReturnsTheOutputsOfTheScript(t *testing.T) {
	outputs, log, err := runScript(t, `echo "working"
{
  echo "backup_file=/backups/a=b.dump"
  echo
  echo "objects=42"
} >>"${OUTPUT_FILE}"
`)
	if err != nil {
		t.Fatal(err)
	}
	// The value is all text after the first "=". The runner does not know the types.
	want := map[string]any{"backup_file": "/backups/a=b.dump", "objects": "42"}
	if !reflect.DeepEqual(outputs, want) {
		t.Errorf("outputs = %#v, want %#v", outputs, want)
	}
	if strings.Contains(log, "backup_file") {
		t.Errorf("the outputs are in the log: %q", log)
	}
}

// The targets are set for every script, also when one is empty, and they replace a value
// of the same name in the environment of the runner.
func TestRunbookGetsTheTargetsOfTheEnvironment(t *testing.T) {
	t.Setenv("TARGET_APPLICATION", "from-the-runner")
	t.Setenv("TARGET_DATA", "from-the-runner")
	body := `echo "application=[${TARGET_APPLICATION-unset}] data=[${TARGET_DATA-unset}]"` + "\n"
	cases := map[string]struct{ data, want string }{
		"a data namespace":  {"harbor-store", "application=[harbor] data=[harbor-store]"},
		"no data namespace": {"", "application=[harbor] data=[]"},
	}
	for label, tc := range cases {
		_, log, err := runScriptOn(t, body, "harbor", tc.data)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if !strings.Contains(log, tc.want) {
			t.Errorf("%s: log = %q, want %q", label, log, tc.want)
		}
	}
}

func TestRunbookRefusesOutputsThatItCannotRead(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"a line without a name":  {`echo "=42" >>"${OUTPUT_FILE}"`, "is not name=value"},
		"a line without a value": {`echo "objects" >>"${OUTPUT_FILE}"`, "is not name=value"},
		"an output written twice": {`echo "objects=1" >>"${OUTPUT_FILE}"
echo "objects=2" >>"${OUTPUT_FILE}"`, "is written twice"},
		"an output file that the script removed": {`rm "${OUTPUT_FILE}"`, "read the outputs"},
	}
	for label, tc := range cases {
		outputs, _, err := runScript(t, tc.body+"\n")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want one with %q", label, err, tc.want)
		}
		if outputs != nil {
			t.Errorf("%s: outputs = %v, want none", label, outputs)
		}
	}
}

func TestAFailedRunbookHasNoOutputs(t *testing.T) {
	outputs, _, err := runScript(t, `echo "objects=42" >>"${OUTPUT_FILE}"
exit 3
`)
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("error = %v, want the failure of the script", err)
	}
	if outputs != nil {
		t.Errorf("outputs = %v, want none from a failed script", outputs)
	}
}

// fakeKubectl answers what the runbooks of the repository ask. It is not a cluster: the
// tests below check what the scripts do with the answers, and nothing about PostgreSQL.
// It writes each command to the error output, which a script does not read, so the log has
// every command.
const fakeKubectl = `#!/bin/bash
echo "kubectl $*" >&2
case "$*" in
*"pg_restore --list"*) echo "      ${FAKE_OBJECTS}" ;;
*jsonpath*) echo "14" ;;
*"ls -1t"*) echo "/backups/paperboat-20261101T000000Z.dump" ;;
*"SELECT count"*) echo "7" ;;
esac
`

// runRepositoryRunbook runs a runbook of the example pack of this repository with
// fakeKubectl. The targets are not the namespaces of the example pack, so a namespace that
// a script names in its text shows in the log.
func runRepositoryRunbook(t *testing.T, name string, params map[string]string) (
	map[string]any, string, error) {
	t.Helper()
	return runRepositoryRunbookOn(t, RunbookJob{Name: name, Params: params,
		Namespace: "harbor", DataNamespace: "harbor-store"})
}

// runRepositoryRunbookOn runs a runbook of the example pack with the name, the parameters
// and the targets of job.
func runRepositoryRunbookOn(t *testing.T, job RunbookJob) (map[string]any, string, error) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fakeKubectl), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// A checkout of the runbooks repository has the runbooks of the pack in its jobs.
	runbooks := t.TempDir()
	jobs := filepath.Join("..", "..", "apps", "paperboat", "runbooks")
	if err := os.CopyFS(filepath.Join(runbooks, "jobs"), os.DirFS(jobs)); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	job.Env, job.Checkout, job.Log = "local", Checkout{Dir: runbooks}, &log
	outputs, err := newRunner(t).Runbook(context.Background(), job)
	return outputs, log.String(), err
}

func TestBackupRunbookReportsItsOutputs(t *testing.T) {
	t.Setenv("FAKE_OBJECTS", "31")
	outputs, log, err := runRepositoryRunbook(t, "db-backup", nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	file, _ := outputs["backup_file"].(string)
	if !strings.HasPrefix(file, "/backups/paperboat-") || !strings.HasSuffix(file, ".dump") {
		t.Errorf("backup_file = %q", file)
	}
	if outputs["objects"] != "31" || len(outputs) != 2 {
		t.Errorf("outputs = %#v", outputs)
	}
	for _, want := range []string{"objects in backup: 31", "-mtime +14",
		"kubectl -n harbor-store exec postgres-0 -- pg_dump"} {
		if !strings.Contains(log, want) {
			t.Errorf("log does not have %q: %q", want, log)
		}
	}
}

// A runbook that needs a target stops before its first command when the target is empty,
// as the data target is on a cloud, where the database is not in the cluster.
func TestRunbooksStopWithoutTheTargetThatTheyNeed(t *testing.T) {
	web := map[string]string{"component": "web"}
	cases := map[string]struct {
		job  RunbookJob
		want string
	}{
		"db-backup": {RunbookJob{Name: "db-backup", Namespace: "harbor"},
			"TARGET_DATA: is empty"},
		"db-maintenance": {RunbookJob{Name: "db-maintenance", Namespace: "harbor"},
			"TARGET_DATA: is empty"},
		"restore-drill": {RunbookJob{Name: "restore-drill", Namespace: "harbor"},
			"TARGET_DATA: is empty"},
		"restart-component": {RunbookJob{Name: "restart-component", Params: web,
			DataNamespace: "harbor-store"}, "TARGET_APPLICATION: is empty"},
	}
	for label, tc := range cases {
		outputs, log, err := runRepositoryRunbookOn(t, tc.job)
		if err == nil || outputs != nil {
			t.Errorf("%s: error = %v, outputs = %v; want a failure", label, err, outputs)
		}
		if !strings.Contains(log, tc.want) {
			t.Errorf("%s: log = %q, want %q", label, log, tc.want)
		}
		if strings.Contains(log, "kubectl ") {
			t.Errorf("%s: a command ran without the target: %q", label, log)
		}
	}
}

func TestBackupRunbookFailsOnABackupThatIsNearlyEmpty(t *testing.T) {
	t.Setenv("FAKE_OBJECTS", "4")
	outputs, log, err := runRepositoryRunbook(t, "db-backup", nil)
	if err == nil || outputs != nil {
		t.Fatalf("error = %v, outputs = %v; want a failure and no outputs\n%s", err, outputs, log)
	}
	if strings.Contains(log, "-delete") {
		t.Errorf("the retention policy ran after a bad backup: %q", log)
	}
}

func TestRestoreDrillRunbookReportsTheBackupThatItRestored(t *testing.T) {
	outputs, log, err := runRepositoryRunbook(t, "restore-drill", nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	want := map[string]any{"backup_file": "/backups/paperboat-20261101T000000Z.dump"}
	if !reflect.DeepEqual(outputs, want) {
		t.Errorf("outputs = %#v, want %#v", outputs, want)
	}
}

func TestRestartRunbookNeedsItsInput(t *testing.T) {
	outputs, log, err := runRepositoryRunbook(t, "restart-component",
		map[string]string{"component": "web"})
	if err != nil || len(outputs) != 0 {
		t.Fatalf("error = %v, outputs = %v\n%s", err, outputs, log)
	}
	if !strings.Contains(log, "kubectl -n harbor rollout restart deployment/paperboat-web") {
		t.Errorf("log = %q", log)
	}
	_, log, err = runRepositoryRunbook(t, "restart-component", nil)
	if err == nil || strings.Contains(log, "rollout restart") {
		t.Errorf("error = %v, want a failure before any command\n%s", err, log)
	}
}

func TestMaintenanceRunbookReportsNoOutput(t *testing.T) {
	outputs, log, err := runRepositoryRunbook(t, "db-maintenance", nil)
	if err != nil || len(outputs) != 0 {
		t.Fatalf("error = %v, outputs = %v\n%s", err, outputs, log)
	}
}

// No script of the example pack names a namespace: every command runs in a target.
func TestRepositoryRunbooksRunOnlyInTheTargets(t *testing.T) {
	web := map[string]string{"component": "web"}
	for _, job := range []RunbookJob{{Name: "db-backup"}, {Name: "db-maintenance"},
		{Name: "restore-drill"}, {Name: "restart-component", Params: web}} {
		t.Setenv("FAKE_OBJECTS", "31")
		job.Namespace, job.DataNamespace = "harbor", "harbor-store"
		_, log, err := runRepositoryRunbookOn(t, job)
		if err != nil {
			t.Fatalf("%s: %v\n%s", job.Name, err, log)
		}
		for line := range strings.Lines(log) {
			if !strings.HasPrefix(line, "kubectl ") {
				continue
			}
			if !strings.HasPrefix(line, "kubectl -n harbor ") &&
				!strings.HasPrefix(line, "kubectl -n harbor-store ") {
				t.Errorf("%s: a command outside the targets: %q", job.Name, line)
			}
		}
	}
}
