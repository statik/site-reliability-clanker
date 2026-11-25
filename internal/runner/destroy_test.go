package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordingTofu writes the directory for its working data and its arguments to
// $FAKE_TOFU_CALLS. Each command works.
const recordingTofu = `#!/bin/sh
echo "$TF_DATA_DIR|$*" >> "$FAKE_TOFU_CALLS"
`

// destroyCalls runs a destroy of one layer of the sources with recordingTofu. It returns
// the lines that the fake wrote.
func destroyCalls(t *testing.T, r *Runner, sources string) []string {
	t.Helper()
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("FAKE_TOFU_CALLS", calls)
	err := r.Destroy(context.Background(), Job{Env: "local", Cloud: "local", Layer: "50-app",
		Checkout: Checkout{Dir: sources}, Log: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(recorded)), "\n")
}

func TestDestroyKeepsTheWorkingDataOfTofuBelowTheDataDirectory(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tofu"), []byte(recordingTofu), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// The sources are the same for each data directory, as in a checkout of the repository.
	sources := t.TempDir()
	stack := filepath.Join(sources, "platform", "stacks", "local", "50-app")
	if err := os.MkdirAll(stack, 0o755); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range []*Runner{newRunner(t), newRunner(t)} {
		lines := destroyCalls(t, r, sources)
		dataDir := filepath.Join(r.WorkRoot, "destroy", "local", "50-app")
		state := filepath.Join(r.StateDir("local"), "50-app.tfstate")
		want := []string{
			dataDir + "|init -no-color -input=false -backend-config=path=" + state,
			dataDir + "|destroy -no-color -input=false -auto-approve",
		}
		if strings.Join(lines, "\n") != strings.Join(want, "\n") {
			t.Errorf("tofu was called with %q, want %q", lines, want)
		}
		seen[dataDir] = true
	}
	if len(seen) != 2 {
		t.Fatal("two data directories share one directory for the working data of tofu")
	}
}
