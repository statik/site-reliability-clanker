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

// operatorTofu stands in for tofu in the commands of the operator. For each call it appends
// one line to $FAKE_TOFU_CALLS: TF_DATA_DIR, TF_VAR_state_location and the arguments,
// separated by "|". `output` prints $FAKE_TOFU_OUTPUTS and `state pull` prints
// $FAKE_TOFU_STATE.
const operatorTofu = `#!/bin/sh
echo "${TF_DATA_DIR-unset}|${TF_VAR_state_location-unset}|$*" >> "$FAKE_TOFU_CALLS"
case "$1" in
output) cat "$FAKE_TOFU_OUTPUTS" ;;
state) cat "$FAKE_TOFU_STATE" ;;
esac
`

// operatorSetup puts operatorTofu first on the PATH, with the state and the outputs that it
// prints. It returns a checkout with the stack of layer 00-bootstrap on AWS and the file of
// the calls.
func operatorSetup(t *testing.T, state, outputs string) (string, string) {
	t.Helper()
	bin, files := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tofu"), []byte(operatorTofu), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"state.json": state, "outputs.json": outputs} {
		if err := os.WriteFile(filepath.Join(files, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	calls := filepath.Join(files, "calls")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_TOFU_CALLS", calls)
	t.Setenv("FAKE_TOFU_STATE", filepath.Join(files, "state.json"))
	t.Setenv("FAKE_TOFU_OUTPUTS", filepath.Join(files, "outputs.json"))
	for _, name := range []string{"TF_DATA_DIR", "TF_VAR_state_location"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "platform", "stacks", "aws", "00-bootstrap"),
		0o755); err != nil {
		t.Fatal(err)
	}
	return checkout, calls
}

func bootstrapJob(checkout string) Job {
	return Job{Env: "prod", Cloud: "aws", Layer: "00-bootstrap", Checkout: Checkout{Dir: checkout},
		Log: &bytes.Buffer{}}
}

func TestAnS3BackendThatIsNotCompleteRunsNoTofu(t *testing.T) {
	cases := map[string]func(map[string]string){
		"a local path":            func(b map[string]string) { b["path"] = "/tmp/state" },
		"no encryption":           func(b map[string]string) { delete(b, "encrypt") },
		"encryption turned off":   func(b map[string]string) { b["encrypt"] = "false" },
		"no locking":              func(b map[string]string) { b["use_lockfile"] = "false" },
		"no key":                  func(b map[string]string) { delete(b, "key") },
		"an empty bucket":         func(b map[string]string) { b["bucket"] = "" },
		"a setting of no backend": func(b map[string]string) { b["dynamodb_table"] = "locks" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			checkout, calls := operatorSetup(t, "", "")
			job := bootstrapJob(checkout)
			job.Backend = S3Backend("state-of-prod", "eu-west-1", "prod", "00-bootstrap")
			change(job.Backend)
			_, err := newRunner(t).ApplyDirect(context.Background(), job)
			if err == nil || !strings.Contains(err.Error(), "s3 backend") {
				t.Fatalf("error = %v, want a refusal of the s3 settings", err)
			}
			if got := readCalls(t, calls); len(got) != 0 {
				t.Fatalf("tofu was called with %q", got)
			}
		})
	}
}

func TestALocalStateOnAWSUsesTheStateFileAndNoStateLocation(t *testing.T) {
	checkout, calls := operatorSetup(t, "", `{}`)
	r := newRunner(t)
	job := bootstrapJob(checkout)
	job.LocalState = true
	job.DataDir = filepath.Join(t.TempDir(), "tofu")
	job.Vars = map[string]string{"state_location": "state-of-prod", "env_name": "prod"}
	if _, err := r.ApplyDirect(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	want := []string{
		job.DataDir + "|unset|init -no-color -input=false -backend-config=path=" +
			r.StatePath("prod", "00-bootstrap"),
		job.DataDir + "|unset|apply -no-color -input=false -auto-approve -var=env_name=prod " +
			"-var=state_location=state-of-prod",
		job.DataDir + "|unset|output -json",
	}
	if got := readCalls(t, calls); !reflect.DeepEqual(got, want) {
		t.Fatalf("tofu was called with\n%q\nwant\n%q", got, want)
	}
}

func TestALocalStateWithBackendSettingsIsRefused(t *testing.T) {
	checkout, calls := operatorSetup(t, "", "")
	job := bootstrapJob(checkout)
	job.LocalState = true
	job.Backend = S3Backend("state-of-prod", "eu-west-1", "prod", "00-bootstrap")
	if _, err := newRunner(t).ApplyDirect(context.Background(), job); err == nil {
		t.Fatal("no error for a job with a local state and backend settings")
	}
	if got := readCalls(t, calls); len(got) != 0 {
		t.Fatalf("tofu was called with %q", got)
	}
}

func TestADestroyGivesTheVariablesOfTheJob(t *testing.T) {
	checkout, calls := operatorSetup(t, "", "")
	r := newRunner(t)
	job := bootstrapJob(checkout)
	job.Backend = S3Backend("state-of-prod", "eu-west-1", "prod", "00-bootstrap")
	job.Vars = map[string]string{"region": "eu-west-1", "broker_principal": "arn"}
	if err := r.Destroy(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	got := readCalls(t, calls)
	want := r.destroyDataDir("prod", "00-bootstrap") + "|state-of-prod|destroy -no-color " +
		"-input=false -auto-approve -var=broker_principal=arn -var=region=eu-west-1"
	if got[len(got)-1] != want {
		t.Fatalf("the last call is %q, want %q", got[len(got)-1], want)
	}
}

// pulledState is a state as `tofu state pull` prints it: two managed resources, one of
// them in a module, a data source and a resource with no instance.
const pulledState = `{"version": 4, "resources": [
  {"mode": "managed", "type": "terraform_data", "name": "a", "instances": [{}]},
  {"module": "module.state_bucket", "mode": "managed", "type": "aws_s3_bucket",
   "name": "this", "instances": [{}]},
  {"mode": "data", "type": "aws_caller_identity", "name": "current", "instances": [{}]},
  {"mode": "managed", "type": "terraform_data", "name": "gone", "instances": []}
]}`

func TestResourcesReadsTheStateFromTheBackend(t *testing.T) {
	checkout, calls := operatorSetup(t, pulledState, "")
	r := newRunner(t)
	job := bootstrapJob(checkout)
	job.Backend = S3Backend("state-of-prod", "eu-west-1", "prod", "00-bootstrap")
	job.Log = nil
	got, err := r.Resources(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"terraform_data.a", "module.state_bucket.aws_s3_bucket.this"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resources = %q, want %q", got, want)
	}
	dataDir := r.destroyDataDir("prod", "00-bootstrap")
	wantCalls := []string{
		dataDir + "|state-of-prod|init -no-color -input=false " +
			"-backend-config=bucket=state-of-prod " +
			"-backend-config=encrypt=true -backend-config=key=prod/00-bootstrap.tfstate " +
			"-backend-config=region=eu-west-1 -backend-config=use_lockfile=true",
		dataDir + "|state-of-prod|state pull",
	}
	if calls := readCalls(t, calls); !reflect.DeepEqual(calls, wantCalls) {
		t.Errorf("tofu was called with\n%q\nwant\n%q", calls, wantCalls)
	}
}

func TestResourcesOfALayerThatWasNeverAppliedIsNone(t *testing.T) {
	empty := `{"version":4,"serial":0,"lineage":"","outputs":{},"resources":[]}`
	for _, state := range []string{empty, ""} {
		checkout, _ := operatorSetup(t, state, "")
		job := bootstrapJob(checkout)
		job.Backend = S3Backend("state-of-prod", "eu-west-1", "prod", "00-bootstrap")
		got, err := newRunner(t).Resources(context.Background(), job)
		if err != nil || len(got) != 0 {
			t.Errorf("state %q: resources = %q, error = %v, want none", state, got, err)
		}
	}
}

func TestResourcesFailsForAStateThatIsNotJSON(t *testing.T) {
	checkout, _ := operatorSetup(t, "not json", "")
	job := bootstrapJob(checkout)
	job.Backend = S3Backend("state-of-prod", "eu-west-1", "prod", "00-bootstrap")
	if _, err := newRunner(t).Resources(context.Background(), job); err == nil ||
		!strings.Contains(err.Error(), "prod/00-bootstrap") {
		t.Fatalf("error = %v, want one that names the layer", err)
	}
}

func TestMoveStateMigratesIntoTheBackendOfTheJob(t *testing.T) {
	checkout, calls := operatorSetup(t, "", "")
	r := newRunner(t)
	job := bootstrapJob(checkout)
	job.Backend = S3Backend("state-of-prod", "eu-west-1", "prod", "00-bootstrap")
	if err := r.MoveState(context.Background(), job); err == nil {
		t.Fatal("no error for a move with no DataDir")
	}
	job.DataDir = filepath.Join(t.TempDir(), "tofu")
	if err := r.MoveState(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	want := []string{job.DataDir + "|state-of-prod|init -no-color -input=false -migrate-state " +
		"-force-copy -backend-config=bucket=state-of-prod -backend-config=encrypt=true " +
		"-backend-config=key=prod/00-bootstrap.tfstate -backend-config=region=eu-west-1 " +
		"-backend-config=use_lockfile=true"}
	if got := readCalls(t, calls); !reflect.DeepEqual(got, want) {
		t.Fatalf("tofu was called with\n%q\nwant\n%q", got, want)
	}
}

func TestPrintedOutputsKeepTheFormOfTofuWithoutTheSensitiveOnes(t *testing.T) {
	printed := `{
  "identities": {"sensitive": false, "type": ["map", "string"], "value": {"planner": "arn"}},
  "secret": {"sensitive": true, "type": "string", "value": "secret-from-tofu"}
}`
	checkout, _ := operatorSetup(t, "", printed)
	job := bootstrapJob(checkout)
	job.Backend = S3Backend("state-of-prod", "eu-west-1", "prod", "00-bootstrap")
	var log bytes.Buffer
	job.Log = &log
	got, err := newRunner(t).PrintedOutputs(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"identities":{"sensitive":false,"type":["map","string"],"value":{"planner":"arn"}}}`
	if string(got) != want {
		t.Errorf("outputs = %s, want %s", got, want)
	}
	if strings.Contains(log.String(), "secret-from-tofu") {
		t.Error("the log has the value of a sensitive output")
	}
}
