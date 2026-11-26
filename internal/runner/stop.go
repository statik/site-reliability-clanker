package runner

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// When the context of a job ends, the runner interrupts the program of the job and gives it
// a grace to end. Then it kills the program.
const (
	// TaskStopTime is how long a launcher waits for a runner to end after it asked the
	// runner to stop, before it kills the runner. On Fargate it is the stop timeout of the
	// runner task definition, which can be at most 120 seconds. The local launcher waits as
	// long.
	TaskStopTime = 120 * time.Second
	// TaskStopGrace is the grace of the programs of a runner that a launcher started. It
	// leaves the runner time to end before its launcher kills it.
	TaskStopGrace = TaskStopTime - 20*time.Second
	// operatorStopGrace is the grace in the commands of the operator, which no launcher
	// kills. As in the home stack of the installer, OpenTofu can finish a create of a
	// database, which takes minutes.
	operatorStopGrace = 30 * time.Minute
	// readStopGrace is the grace of a kubectl read, which has nothing to save.
	readStopGrace = 5 * time.Second
)

// stopGrace returns the grace of the programs of the jobs of r.
func (r *Runner) stopGrace() time.Duration {
	if r.StopGrace > 0 {
		return r.StopGrace
	}
	return operatorStopGrace
}

// interruptOnStop makes the end of the context of cmd act as Ctrl+C in a terminal: the
// program and the programs that it started get one interrupt. OpenTofu then finishes the
// calls that run, saves its state and releases its lock, where a kill could leave a resource
// that no state knows and a lock that blocks the next run. A program that has not ended
// after grace is killed; call killLeftovers after the wait.
//
// The program gets a process group of its own. Ctrl+C in the terminal of the runner, or a
// second stop, thus never gives it a second interrupt, which makes OpenTofu stop at once.
func interruptOnStop(cmd *exec.Cmd, grace time.Duration) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = grace
}

// killLeftovers kills the rest of the process group of a program that was killed at the end
// of its grace: the programs that it started and that did not end on the interrupt.
func killLeftovers(cmd *exec.Cmd) {
	if cmd.ProcessState == nil {
		return
	}
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if ok && status.Signaled() && status.Signal() == syscall.SIGKILL {
		// The group exists while one of its programs lives, so its number names no other
		// group. ESRCH means that none is left.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
