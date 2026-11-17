package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/statik/site-reliability-clanker/internal/dispatch"
	"github.com/statik/site-reliability-clanker/internal/launch"
	"github.com/statik/site-reliability-clanker/internal/runner"
)

// runRunner is the runner process: it runs the one job of its token and ends. The control
// plane starts it. A person does not.
func runRunner(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("clanker runner", flag.ContinueOnError)
	dir := fs.String("data", "", "directory of the runner: state, checkouts and provider cache")
	if err := fs.Parse(args); err != nil {
		return err
	}
	coreURL, token := os.Getenv(launch.CoreURLEnv), os.Getenv(launch.TokenEnv)
	if *dir == "" || coreURL == "" || token == "" {
		return fmt.Errorf("the runner needs --data, %s and %s: the control plane starts the "+
			"runner, a person does not", launch.CoreURLEnv, launch.TokenEnv)
	}
	absolute, err := filepath.Abs(*dir)
	if err != nil {
		return fmt.Errorf("resolve the runner directory: %w", err)
	}
	r, err := runner.New(absolute)
	if err != nil {
		return err
	}
	client := &runner.Client{CoreURL: coreURL, Token: token, Runner: r}
	return client.Serve(ctx)
}

// noLauncher is the launcher of a cloud that has none yet.
type noLauncher struct{ cloud string }

func (n noLauncher) Launch(context.Context, string) error {
	return errors.New("the control plane cannot start a runner on " + n.cloud + " yet")
}

// newDispatcher returns the dispatcher of the jobs. On the local target each runner is a
// process on this machine.
func newDispatcher(o options) (*dispatch.Dispatcher, error) {
	if o.cloud != "local" {
		return dispatch.New(noLauncher{cloud: o.cloud}), nil
	}
	binary, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find the clanker program to start runners: %w", err)
	}
	return dispatch.New(launch.Local{Binary: binary, Dir: o.dataDir, CoreURL: "http://" + o.addr}), nil
}
