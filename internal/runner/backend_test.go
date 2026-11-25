package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/jobspec"
)

// fakeTofu stands in for tofu. For each call it appends one line to $FAKE_TOFU_CALLS: the
// value of TF_VAR_state_location, the value of KIND_EXPERIMENTAL_DOCKER_NETWORK and the
// arguments, separated by "|". A variable that is not in the environment shows as "unset".
// Each command works. `output` prints the file $FAKE_TOFU_OUTPUTS.
const fakeTofu = `#!/bin/sh
echo "${TF_VAR_state_location-unset}|${KIND_EXPERIMENTAL_DOCKER_NETWORK-unset}|$*" >> "$FAKE_TOFU_CALLS"
if [ "$1" = output ]; then cat "$FAKE_TOFU_OUTPUTS"; fi
`

// printedOutputs is what `tofu output -json` prints for the layer: one output that is not
// sensitive and one that is.
const printedOutputs = `{
  "network_id": {"sensitive": false, "type": "string", "value": "vpc-from-tofu"},
  "admin_password": {"sensitive": true, "type": "string", "value": "secret-from-tofu"}
}`

// applied is what an apply with fakeTofu gave.
type applied struct {
	outputs map[string]any
	err     error
	log     string
	// calls has one line for each call of tofu, as fakeTofu writes it.
	calls []string
}

// withFakeTofu puts fakeTofu first on the PATH and returns a checkout with the stack of
// layer 10-network of the cloud, and the file of the calls.
func withFakeTofu(t *testing.T, cloud string) (string, string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tofu"), []byte(fakeTofu), 0o755); err != nil {
		t.Fatal(err)
	}
	printed := filepath.Join(t.TempDir(), "outputs.json")
	if err := os.WriteFile(printed, []byte(printedOutputs), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_TOFU_CALLS", calls)
	t.Setenv("FAKE_TOFU_OUTPUTS", printed)
	// The machine that runs the tests can have these variables. The runner alone sets them.
	for _, name := range []string{"TF_VAR_state_location", "KIND_EXPERIMENTAL_DOCKER_NETWORK"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "platform", "stacks", cloud, "10-network"),
		0o755); err != nil {
		t.Fatal(err)
	}
	return checkout, calls
}

// readCalls returns the lines that fakeTofu wrote, or none when it was not called.
func readCalls(t *testing.T, calls string) []string {
	t.Helper()
	recorded, err := os.ReadFile(calls)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(recorded)), "\n")
}

// applyWithFakeTofu applies a plan of layer 10-network of the environment prod on the
// cloud with fakeTofu.
func applyWithFakeTofu(t *testing.T, r *Runner, cloud string, backend map[string]string) applied {
	t.Helper()
	checkout, calls := withFakeTofu(t, cloud)
	plan := []byte("the approved plan")
	var log bytes.Buffer
	outputs, err := r.Apply(context.Background(), Job{Env: "prod", Cloud: cloud,
		Layer: "10-network", Checkout: Checkout{Dir: checkout}, Backend: backend, Log: &log},
		plan, jobspec.SHA256(plan))
	return applied{outputs: outputs, err: err, log: log.String(), calls: readCalls(t, calls)}
}

// argumentsOf returns the arguments of each call, without the values of the variables.
func argumentsOf(calls []string) []string {
	arguments := make([]string, 0, len(calls))
	for _, call := range calls {
		parts := strings.SplitN(call, "|", 3)
		arguments = append(arguments, parts[len(parts)-1])
	}
	return arguments
}

func s3Backend() map[string]string {
	return map[string]string{"region": "eu-west-1", "bucket": "state-of-prod",
		"key": "prod/10-network.tfstate", "use_lockfile": "true"}
}

func TestAnApplyWithABucketReportsTheOutputsThatTofuPrints(t *testing.T) {
	r := newRunner(t)
	// A state file in the state directory must not be read: the state is in the bucket.
	if err := os.MkdirAll(r.StateDir("prod"), 0o755); err != nil {
		t.Fatal(err)
	}
	decoy := `{"outputs": {"network_id": {"value": "vpc-from-a-file", "sensitive": false}}}`
	if err := os.WriteFile(filepath.Join(r.StateDir("prod"), "10-network.tfstate"),
		[]byte(decoy), 0o644); err != nil {
		t.Fatal(err)
	}
	got := applyWithFakeTofu(t, r, "aws", s3Backend())
	if got.err != nil {
		t.Fatal(got.err)
	}
	want := map[string]any{"network_id": "vpc-from-tofu"}
	if !reflect.DeepEqual(got.outputs, want) {
		t.Errorf("outputs = %v, want %v without the sensitive output", got.outputs, want)
	}
	arguments := argumentsOf(got.calls)
	if last := arguments[len(arguments)-1]; last != "output -json" {
		t.Errorf("the last call of tofu is %q, want \"output -json\"", last)
	}
	if strings.Contains(got.log, "secret-from-tofu") {
		t.Error("the job log has the value of a sensitive output")
	}
}

func TestAJobWithABucketGivesTheBucketAsTheStateLocationAndNoPath(t *testing.T) {
	r := newRunner(t)
	got := applyWithFakeTofu(t, r, "aws", s3Backend())
	if got.err != nil {
		t.Fatal(got.err)
	}
	wantInit := "init -no-color -input=false -backend-config=bucket=state-of-prod " +
		"-backend-config=key=prod/10-network.tfstate -backend-config=region=eu-west-1 " +
		"-backend-config=use_lockfile=true"
	if arguments := argumentsOf(got.calls); arguments[0] != wantInit {
		t.Errorf("tofu init was called with %q, want %q", arguments[0], wantInit)
	}
	for _, call := range got.calls {
		if !strings.HasPrefix(call, "state-of-prod|unset|") {
			t.Errorf("the call %q does not have the bucket as state_location and no kind "+
				"network", call)
		}
	}
	if _, err := os.Stat(r.StateDir("prod")); !os.IsNotExist(err) {
		t.Errorf("a job with a bucket made the local state directory: %v", err)
	}
}

func TestAJobOnTheLocalTargetKeepsItsStateInTheStateDirectory(t *testing.T) {
	r := newRunner(t)
	got := applyWithFakeTofu(t, r, "local", nil)
	if got.err != nil {
		t.Fatal(got.err)
	}
	wantInit := "init -no-color -input=false -backend-config=path=" +
		filepath.Join(r.StateDir("prod"), "10-network.tfstate")
	if arguments := argumentsOf(got.calls); arguments[0] != wantInit {
		t.Errorf("tofu init was called with %q, want %q", arguments[0], wantInit)
	}
	for _, call := range got.calls {
		if !strings.HasPrefix(call, r.StateDir("prod")+"|clanker-prod|") {
			t.Errorf("the call %q does not have the state directory and the kind network", call)
		}
	}
}

func TestAJobOffTheLocalTargetWithoutABackendRunsNoTofu(t *testing.T) {
	got := applyWithFakeTofu(t, newRunner(t), "aws", nil)
	if got.err == nil || !strings.Contains(got.err.Error(), "has no backend settings") {
		t.Fatalf("error = %v, want a refusal of a job with no backend", got.err)
	}
	if len(got.calls) != 0 {
		t.Fatalf("tofu was called with %q", got.calls)
	}
}

func TestABackendWithNoBucketGivesNoStateLocation(t *testing.T) {
	backend := map[string]string{"storage_account_name": "account", "container_name": "state"}
	got := applyWithFakeTofu(t, newRunner(t), "azure", backend)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if !strings.HasPrefix(got.calls[0], "unset|unset|") {
		t.Errorf("a backend with no bucket gave the call %q, want no state_location", got.calls[0])
	}
}

func TestAStateLocationInTheVariablesOfTheJobReplacesTheBucket(t *testing.T) {
	r := newRunner(t)
	r.ExtraEnv = []string{"TF_VAR_state_location=from-the-job"}
	got := applyWithFakeTofu(t, r, "aws", s3Backend())
	if got.err != nil {
		t.Fatal(got.err)
	}
	if !strings.HasPrefix(got.calls[0], "from-the-job|unset|") {
		t.Errorf("the variable of the job gave the call %q, want its state_location",
			got.calls[0])
	}
}

func TestABootstrapApplyReportsTheOutputsThatTofuPrints(t *testing.T) {
	checkout, calls := withFakeTofu(t, "local")
	outputs, err := newRunner(t).ApplyDirect(context.Background(), Job{Env: "prod",
		Cloud: "local", Layer: "10-network", Checkout: Checkout{Dir: checkout},
		Log: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"network_id": "vpc-from-tofu"}
	if !reflect.DeepEqual(outputs, want) {
		t.Errorf("outputs = %v, want %v without the sensitive output", outputs, want)
	}
	arguments := argumentsOf(readCalls(t, calls))
	if want := "output -json"; arguments[len(arguments)-1] != want {
		t.Errorf("the calls of tofu are %q, want %q last", arguments, want)
	}
}
