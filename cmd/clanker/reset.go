package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/setup"
)

// resetEntries are the files and directories of the data directory that hold the setup
// and the history of an environment. The reset removes only these. The plugin cache, the
// unpacked source files and the imported packs hold no state of an environment, so they
// stay.
var resetEntries = []string{
	"setup.json", "secrets.env", "database.password", "broker.key", "state", "work", "logs",
}

// resetter holds the steps of a reset that act outside the data directory.
type resetter struct {
	// destroy removes every layer of the environment.
	destroy func(ctx context.Context, o options) error
	// docker runs the docker program and returns its output.
	docker func(args ...string) ([]byte, error)
	out    io.Writer
}

func newResetter(out io.Writer) resetter {
	return resetter{
		destroy: destroy,
		docker: func(args ...string) ([]byte, error) {
			return exec.Command("docker", args...).CombinedOutput()
		},
		out: out,
	}
}

// run puts a local environment back to the state before the first setup. It destroys the
// environment, then removes the database of the control plane, then removes the saved
// setup. A step that fails stops the reset, so the files that a later try needs stay.
func (r resetter) run(ctx context.Context, o options) error {
	if o.cloud != "local" {
		return fmt.Errorf("reset is only for the local target, not for %s: use `clanker "+
			"destroy` for an environment on a cloud", o.cloud)
	}
	saved, err := savedSetup(o.dataDir)
	if err != nil {
		return err
	}
	if saved.Target != "" && saved.Target != setup.TargetLocal {
		return fmt.Errorf("the saved setup in %s is for the %s target: reset is only for "+
			"the local target", o.dataDir, saved.Target)
	}
	if saved.Env != "" {
		o.env = saved.Env
	}
	if err := r.destroy(ctx, o); err != nil {
		return fmt.Errorf("destroy environment %s: %w; nothing else was removed, so you "+
			"can try the reset again", o.env, err)
	}
	if err := r.removeDatabase(o.dataDir); err != nil {
		return err
	}
	for _, entry := range resetEntries {
		path := filepath.Join(o.dataDir, entry)
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	fmt.Fprintf(r.out, "Environment %s is reset. The next start begins with the setup.\n",
		o.env)
	return nil
}

// removeDatabase removes the database container of the data directory and its volume. A
// container or a volume that does not exist is not an error.
func (r resetter) removeDatabase(dataDir string) error {
	name := databaseName(dataDir)
	fmt.Fprintf(r.out, "Removing the database %s\n", name)
	out, err := r.docker("rm", "-f", "-v", name)
	if err != nil && !strings.Contains(string(out), "No such container") {
		return fmt.Errorf("remove the database container %s: %w: %s", name, err,
			strings.TrimSpace(string(out)))
	}
	if out, err := r.docker("volume", "rm", "-f", name); err != nil {
		return fmt.Errorf("remove the database volume %s: %w: %s", name, err,
			strings.TrimSpace(string(out)))
	}
	return nil
}

// savedSetup reads the setup that the wizard saved. No file gives an empty setup.
func savedSetup(dataDir string) (setup.State, error) {
	var saved setup.State
	path := filepath.Join(dataDir, "setup.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return saved, nil
	}
	if err != nil {
		return saved, fmt.Errorf("read the saved setup: %w", err)
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return saved, fmt.Errorf("decode %s: %w", path, err)
	}
	return saved, nil
}
