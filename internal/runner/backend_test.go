package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTofu writes its arguments to $FAKE_TOFU_CALLS. Only `init` works, so a plan stops
// right after the step that these tests look at.
const fakeTofu = `#!/bin/sh
echo "$*" >> "$FAKE_TOFU_CALLS"
[ "$1" = init ]
`

// initArguments runs a plan job with fakeTofu and returns the arguments of `tofu init`.
func initArguments(t *testing.T, r *Runner, backend map[string]string) string {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tofu"), []byte(fakeTofu), 0o755); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_TOFU_CALLS", calls)
	checkout := t.TempDir()
	stack := filepath.Join(checkout, "platform", "stacks", "aws", "10-network")
	if err := os.MkdirAll(stack, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := r.Plan(context.Background(), Job{Env: "prod", Cloud: "aws", Layer: "10-network",
		Checkout: Checkout{Dir: checkout}, Backend: backend, Log: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "tofu plan") {
		t.Fatalf("error = %v, want the failure of the plan step of the fake", err)
	}
	recorded, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(string(recorded), "\n")
	return first
}

func TestInitKeepsTheStateInTheStateDirectoryWithoutABackend(t *testing.T) {
	r := newRunner(t)
	got := initArguments(t, r, nil)
	want := "init -no-color -input=false -backend-config=path=" +
		filepath.Join(r.StateDir("prod"), "10-network.tfstate")
	if got != want {
		t.Fatalf("tofu was called with %q, want %q", got, want)
	}
}

func TestInitPassesTheBackendOfTheJobInAFixedOrder(t *testing.T) {
	got := initArguments(t, newRunner(t), map[string]string{
		"region": "eu-west-1", "bucket": "state-of-prod", "key": "prod/10-network.tfstate"})
	want := "init -no-color -input=false -backend-config=bucket=state-of-prod " +
		"-backend-config=key=prod/10-network.tfstate -backend-config=region=eu-west-1"
	if got != want {
		t.Fatalf("tofu was called with %q, want %q", got, want)
	}
}
