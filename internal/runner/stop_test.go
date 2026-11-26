package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// interruptedTofu stands in for tofu. `init` works. Any other command waits for an
// interrupt, records it, takes a moment to save its state and ends as tofu does after an
// interrupt.
const interruptedTofu = `#!/bin/sh
[ "$1" = init ] && exit 0
trap 'echo interrupted >>"$MARK"; sleep 0.2; echo saved >>"$MARK"; exit 1' INT
echo started >"$MARK.started"
while :; do sleep 0.05; done
`

// stubbornTofu stands in for a tofu that does not end on an interrupt. It records each
// interrupt and goes on. It starts a program that ignores the interrupt too and outlives
// it, and writes the process ID of that program to $MARK.child.
const stubbornTofu = `#!/bin/sh
[ "$1" = init ] && exit 0
trap 'echo interrupted >>"$MARK"' INT
sleep 300 &
echo $! >"$MARK.child"
echo started >"$MARK.started"
while :; do sleep 0.05; done
`

// withStandIn puts a stand-in for program first on the PATH and returns the file in which
// the stand-in records what it saw.
func withStandIn(t *testing.T, program, script string) string {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, program), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	mark := filepath.Join(t.TempDir(), "mark")
	t.Setenv("MARK", mark)
	return mark
}

// stopWhenStarted returns a context that ends when the stand-in wrote $MARK.started.
func stopWhenStarted(t *testing.T, mark string) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		for ctx.Err() == nil {
			if _, err := os.Stat(mark + ".started"); err == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	return ctx
}

// stoppedPlan runs a plan with the stand-in for tofu and stops it once tofu runs. It returns
// the error of the plan, what the stand-in recorded and how long the plan took.
func stoppedPlan(t *testing.T, r *Runner, script string) (error, string, time.Duration) {
	t.Helper()
	mark := withStandIn(t, "tofu", script)
	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "platform", "stacks", "local", "10-network"),
		0o755); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err := r.Plan(stopWhenStarted(t, mark), Job{Env: "local", Cloud: "local",
		Layer: "10-network", Checkout: Checkout{Dir: checkout}, Log: &bytes.Buffer{}})
	took := time.Since(started)
	recorded, readErr := os.ReadFile(mark)
	if readErr != nil {
		t.Fatalf("the stand-in recorded nothing: %v", readErr)
	}
	return err, string(recorded), took
}

func TestAStoppedJobInterruptsTofuOnceAndWaitsForItToSaveItsState(t *testing.T) {
	r := newRunner(t)
	err, recorded, took := stoppedPlan(t, r, interruptedTofu)
	if err == nil {
		t.Fatal("a stopped plan reported success")
	}
	if recorded != "interrupted\nsaved\n" {
		t.Errorf("tofu recorded %q, want one interrupt and then its saved state", recorded)
	}
	// The grace of the operator is 30 minutes. Tofu ended on its own, long before it.
	if took > 10*time.Second {
		t.Errorf("the stopped plan took %s", took)
	}
}

func TestATofuThatIgnoresTheInterruptIsKilledAfterTheGraceWithWhatItStarted(t *testing.T) {
	r := newRunner(t)
	r.StopGrace = 700 * time.Millisecond
	err, recorded, took := stoppedPlan(t, r, stubbornTofu)
	if err == nil {
		t.Fatal("a killed plan reported success")
	}
	if recorded != "interrupted\n" {
		t.Errorf("tofu recorded %q, want one interrupt", recorded)
	}
	if took < r.StopGrace || took > r.StopGrace+5*time.Second {
		t.Errorf("the plan ended %s after its start, want just after the grace of %s", took,
			r.StopGrace)
	}
	raw, readErr := os.ReadFile(os.Getenv("MARK") + ".child")
	if readErr != nil {
		t.Fatal(readErr)
	}
	child, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if convErr != nil {
		t.Fatal(convErr)
	}
	// The killed child is gone once init collected it.
	deadline := time.Now().Add(5 * time.Second)
	for !errors.Is(syscall.Kill(child, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(child, syscall.SIGKILL)
			t.Fatalf("the program %d that tofu started still runs after the kill", child)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// interruptedTool stands in for a tool in a runbook, such as helm. It records an interrupt
// and ends with an error.
const interruptedTool = `#!/bin/sh
trap 'echo tool interrupted >>"$MARK"; exit 130' INT
echo started >"$MARK.started"
while :; do sleep 0.05; done
`

// A shell that runs a script waits for the program in the foreground before it acts on an
// interrupt, so the runner interrupts the program too, as Ctrl+C in a terminal does.
func TestAStoppedRunbookInterruptsTheToolThatItRuns(t *testing.T) {
	mark := withStandIn(t, "tool", interruptedTool)
	r := newRunner(t)
	r.StopGrace = 5 * time.Second
	checkout := Checkout{ID: "runbooks/abc", Archive: archive(t, entry{"jobs/job.sh",
		"#!/bin/bash\nset -euo pipefail\ntool\necho after the tool >>\"$MARK\"\n", 0o755})}
	started := time.Now()
	_, err := r.Runbook(stopWhenStarted(t, mark), RunbookJob{Env: "local", Checkout: checkout,
		Name: "job", Log: &bytes.Buffer{}})
	took := time.Since(started)
	if err == nil {
		t.Fatal("a stopped runbook reported success")
	}
	recorded, readErr := os.ReadFile(mark)
	if readErr != nil || string(recorded) != "tool interrupted\n" {
		t.Errorf("the runbook recorded %q, %v; want the interrupt of the tool and no step "+
			"after it", recorded, readErr)
	}
	if took >= r.StopGrace {
		t.Errorf("the runbook ended %s after its start: it was killed after the grace, not "+
			"interrupted", took)
	}
}
