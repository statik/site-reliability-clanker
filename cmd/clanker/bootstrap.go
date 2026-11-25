package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/runner"
)

// bootstrapLayer is the layer that `clanker bootstrap` applies with the credentials of the
// person who runs it.
const bootstrapLayer = "00-bootstrap"

// localBackendOverride gives the copy of layer 00-bootstrap on a cloud a local backend,
// while the bucket that the layer creates does not hold its state.
const localBackendOverride = "terraform {\n  backend \"local\" {}\n}\n"

// workCopy is a copy of the platform files below the data directory, with a directory for
// the working data of OpenTofu. Layer 00-bootstrap runs there and not in the source files,
// which all data directories share: OpenTofu writes its working data and lock files next
// to the stack (#140). Each command makes both again, so no backend that an earlier run
// recorded is left.
type workCopy struct {
	// source holds the directory platform, as a checkout does.
	source  string
	dataDir string
}

func newWorkCopy(r *runner.Runner, source fs.FS, env string) (workCopy, error) {
	root := filepath.Join(r.WorkRoot, "bootstrap", env)
	w := workCopy{source: filepath.Join(root, "source"), dataDir: filepath.Join(root, "tofu")}
	for _, dir := range []string{w.source, w.dataDir} {
		if err := os.RemoveAll(dir); err != nil {
			return workCopy{}, fmt.Errorf("remove the copy of an earlier run %s: %w", dir, err)
		}
	}
	if err := copyPlatform(source, w.source); err != nil {
		return workCopy{}, err
	}
	if err := os.MkdirAll(w.dataDir, 0o755); err != nil {
		return workCopy{}, fmt.Errorf("create the directory %s: %w", w.dataDir, err)
	}
	return w, nil
}

// copyPlatform copies the directory platform of the source files into dir. It leaves out
// the working data and the state files that a checkout has after a run of tofu in it.
func copyPlatform(source fs.FS, dir string) error {
	return fs.WalkDir(source, "platform", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("copy the platform files: %w", err)
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if entry.IsDir() {
			if entry.Name() == ".terraform" {
				return fs.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		if strings.Contains(entry.Name(), ".tfstate") {
			return nil
		}
		content, err := fs.ReadFile(source, name)
		if err != nil {
			return fmt.Errorf("copy the platform files: %w", err)
		}
		mode := os.FileMode(0o644)
		if info, err := entry.Info(); err == nil && info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		return os.WriteFile(target, content, mode)
	})
}

// stack returns the directory of layer 00-bootstrap of the cloud in the copy.
func (w workCopy) stack(cloud string) string {
	return filepath.Join(w.source, "platform", "stacks", cloud, bootstrapLayer)
}

// setLocalBackend gives the copy of layer 00-bootstrap a local backend, or takes it away so
// that the stack has its own backend again.
func (w workCopy) setLocalBackend(cloud string, local bool) error {
	path := filepath.Join(w.stack(cloud), "backend_override.tf")
	if local {
		if err := os.WriteFile(path, []byte(localBackendOverride), 0o644); err != nil {
			return fmt.Errorf("give the copy of %s a local backend: %w", bootstrapLayer, err)
		}
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("give the copy of %s its own backend again: %w", bootstrapLayer, err)
	}
	return nil
}

func bootstrap(ctx context.Context, o options) error {
	switch o.cloud {
	case "local":
		return bootstrapLocal(ctx, o)
	case "aws":
		return bootstrapAWS(ctx, o, runAWS)
	default:
		return fmt.Errorf("bootstrap for cloud %q is not enabled: the %s modules are validated "+
			"but not applied. Approve the expected cost first; see docs/HANDOFF.md", o.cloud,
			o.cloud)
	}
}

func bootstrapLocal(ctx context.Context, o options) error {
	r, err := runner.New(o.dataDir)
	if err != nil {
		return err
	}
	w, err := newWorkCopy(r, o.source, o.env)
	if err != nil {
		return err
	}
	fmt.Printf("Applying layer 00-bootstrap of %s with your own credentials.\n", o.env)
	job := runner.Job{Env: o.env, Cloud: o.cloud, Layer: bootstrapLayer,
		Checkout: runner.Checkout{Dir: w.source}, DataDir: w.dataDir, Log: os.Stdout}
	outputs, err := r.ApplyDirect(ctx, job)
	if err != nil {
		return err
	}
	fmt.Printf("\nBootstrap of %s is complete. It created:\n", o.env)
	fmt.Printf("  state backend:   %v\n", outputs["state_backend"])
	fmt.Printf("  evidence bucket: %v\n", outputs["evidence_bucket"])
	fmt.Printf("  identities:      %v\n", outputs["identities"])
	fmt.Println("No credential of yours is stored. Next: clanker up")
	return nil
}
