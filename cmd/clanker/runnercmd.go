package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
	coreURL, token := os.Getenv(dispatch.CoreURLEnv), os.Getenv(dispatch.TokenEnv)
	if *dir == "" || coreURL == "" || token == "" {
		return fmt.Errorf("the runner needs --data, %s and %s: the control plane starts the "+
			"runner, a person does not", dispatch.CoreURLEnv, dispatch.TokenEnv)
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

// fargateStartGrace is how long a runner task on Fargate has from its launch to its first
// request.
const fargateStartGrace = 5 * time.Minute

// noLauncher is the launcher of a cloud that has none yet, and of AWS without the setting
// of the runners.
type noLauncher struct{ cloud string }

func (n noLauncher) Launch(context.Context, string) error {
	return errors.New("the control plane cannot start a runner on " + n.cloud + " yet")
}

// newDispatcher returns the dispatcher of the jobs. On the local target each runner is a
// process on this machine. On AWS each runner is a task on Fargate.
func newDispatcher(ctx context.Context, o options) (*dispatch.Dispatcher, error) {
	switch o.cloud {
	case "local":
		binary, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("find the clanker program to start runners: %w", err)
		}
		return dispatch.New(launch.Local{Binary: binary, Dir: o.dataDir,
			CoreURL: "http://" + o.addr}), nil
	case "aws":
		fargate, err := awsLauncher(ctx, o, awsClients{ecs: o.aws.ecs, metadata: o.aws.metadata})
		if err != nil {
			return nil, err
		}
		if fargate != nil {
			d := dispatch.New(fargate)
			// A task must get a machine and pull the runner image before its first request.
			d.StartGrace = fargateStartGrace
			return d, nil
		}
	}
	return dispatch.New(noLauncher{cloud: o.cloud}), nil
}
