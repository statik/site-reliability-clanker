// Package launch starts runners. It is not part of the core: it starts programs.
package launch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/statik/site-reliability-clanker/internal/dispatch"
	"github.com/statik/site-reliability-clanker/internal/runner"
)

// Local starts each runner as a process on the machine of the control plane. It is the
// launcher of the local target.
type Local struct {
	// Binary is the clanker program.
	Binary string
	// Dir is the directory of the runner: its state, its checkouts and its provider cache.
	Dir string
	// CoreURL is the address of the control plane as the runner reaches it.
	CoreURL string
}

// Launch runs one runner process and returns when it ends.
func (l Local) Launch(ctx context.Context, token string) error {
	cmd := exec.CommandContext(ctx, l.Binary, "runner", "--data", l.Dir)
	// A stop asks the runner to end, as ECS asks a runner task on Fargate: the runner
	// interrupts the program of its job and waits for it. A kill could leave OpenTofu no time
	// to save its state.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = runner.TaskStopTime
	cmd.Env = append(os.Environ(), dispatch.CoreURLEnv+"="+l.CoreURL,
		dispatch.TokenEnv+"="+token)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(stderr.String())
		if len(text) > 800 {
			text = text[len(text)-800:]
		}
		return fmt.Errorf("the runner process ended with an error: %w: %s", err, text)
	}
	return nil
}
