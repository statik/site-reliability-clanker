package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/runner"
)

func destroy(ctx context.Context, o options) error {
	switch o.cloud {
	case "local":
		return destroyLocal(ctx, o)
	case "aws":
		return destroyAWS(ctx, o, runAWS, os.Stdout)
	default:
		return fmt.Errorf("destroy for cloud %q is not enabled: only the local and the aws "+
			"targets have it", o.cloud)
	}
}

// destroyLocal destroys each layer of a local environment that has a state file in the
// state directory, the top layer first.
func destroyLocal(ctx context.Context, o options) error {
	r, err := runner.New(o.dataDir)
	if err != nil {
		return err
	}
	layers := slices.Clone(orchestrator.Layers)
	slices.Reverse(layers)
	// The teardown uses the administrator kubeconfig of the operator. Before the cluster
	// exists there is none, and the layers below the cluster need none.
	admin, err := os.ReadFile(filepath.Join(r.StateDir(o.env), "kubeconfig"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read the administrator kubeconfig of %s: %w", o.env, err)
	}
	for _, layer := range layers {
		// A layer with no state file in the state directory was never applied here.
		statePath := r.StatePath(o.env, layer)
		if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("check the state file %s: %w", statePath, err)
		}
		fmt.Printf("Destroying %s/%s\n", o.env, layer)
		job := runner.Job{Env: o.env, Cloud: o.cloud, Layer: layer,
			Checkout: runner.Checkout{Dir: o.sourceDir}, KubeConfig: string(admin), Log: os.Stdout}
		if err := r.Destroy(ctx, job); err != nil {
			return err
		}
		if err := os.Remove(statePath); err != nil {
			return fmt.Errorf("remove state file %s: %w", statePath, err)
		}
	}
	fmt.Printf("Environment %s is destroyed.\n", o.env)
	return nil
}
