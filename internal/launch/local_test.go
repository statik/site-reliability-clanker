package launch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// terminatedRunner stands in for the clanker program. It records a SIGTERM and ends as a
// runner ends after it stopped its job.
const terminatedRunner = `#!/bin/sh
trap 'echo terminated >>"$MARK"; exit 0' TERM
echo started >"$MARK.started"
while :; do sleep 0.05; done
`

func TestAStoppedLaunchAsksTheRunnerToEndAndWaitsForIt(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "clanker")
	if err := os.WriteFile(binary, []byte(terminatedRunner), 0o755); err != nil {
		t.Fatal(err)
	}
	mark := filepath.Join(dir, "mark")
	t.Setenv("MARK", mark)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			if _, err := os.Stat(mark + ".started"); err == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	started := time.Now()
	err := Local{Binary: binary, Dir: dir, CoreURL: "http://127.0.0.1:1"}.Launch(ctx, "token")
	if err == nil {
		t.Fatal("a stopped launch reported success")
	}
	if took := time.Since(started); took > 10*time.Second {
		t.Errorf("the stopped launch took %s", took)
	}
	recorded, readErr := os.ReadFile(mark)
	if readErr != nil || string(recorded) != "terminated\n" {
		t.Errorf("the runner recorded %q, %v; want one SIGTERM", recorded, readErr)
	}
}
