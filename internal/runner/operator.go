package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
)

// The commands of the operator, `clanker bootstrap` and `clanker destroy`, run OpenTofu
// with the credentials of the person who runs them. They use the methods in this file
// together with ApplyDirect and Destroy.

// destroyDataDir returns the directory for the working data of OpenTofu in a command of
// the operator. It is below the directory of the runner, one for each environment and
// layer.
func (r *Runner) destroyDataDir(env, layer string) string {
	return filepath.Join(r.WorkRoot, "destroy", env, layer)
}

// withOperatorDataDir gives a job a directory for the working data of OpenTofu below the
// directory of the runner when it has none. The commands of the operator can run in the
// stack directories of the sources, which all data directories share, so the working data,
// which records the backend, must stay out of the stack directory.
func (r *Runner) withOperatorDataDir(j Job) Job {
	if j.DataDir == "" {
		j.DataDir = r.destroyDataDir(j.Env, j.Layer)
	}
	return j
}

// varArgs gives each variable as a -var argument, in the order of the names.
func varArgs(vars map[string]string) []string {
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	args := make([]string, 0, len(names))
	for _, name := range names {
		args = append(args, "-var="+name+"="+vars[name])
	}
	return args
}

// Resources returns the addresses of the managed resources in the state of the layer, as
// the backend of the job holds it. A layer that was never applied has none. It reads the
// state with `tofu state pull`, which gives an empty state for a key that does not exist;
// `tofu state list` fails for that key as it fails for a state that cannot be read. The
// state can hold secrets, so it goes to no log.
func (r *Runner) Resources(ctx context.Context, j Job) ([]string, error) {
	p, done, err := r.prepare(r.withOperatorDataDir(j))
	if err != nil {
		return nil, err
	}
	defer done()
	if err := r.init(ctx, p); err != nil {
		return nil, err
	}
	var pulled bytes.Buffer
	if err := r.tofu(ctx, p, nil, &pulled, "state", "pull"); err != nil {
		return nil, err
	}
	return managedResources(pulled.Bytes(), j)
}

// managedResources returns the addresses of the managed resources with an instance in a
// state that `tofu state pull` printed. Data sources are not counted: a destroy has
// nothing to remove for them.
func managedResources(pulled []byte, j Job) ([]string, error) {
	if len(bytes.TrimSpace(pulled)) == 0 {
		return nil, nil
	}
	var state struct {
		Resources []struct {
			Module    string            `json:"module"`
			Mode      string            `json:"mode"`
			Type      string            `json:"type"`
			Name      string            `json:"name"`
			Instances []json.RawMessage `json:"instances"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(pulled, &state); err != nil {
		return nil, fmt.Errorf("read the state of %s/%s: %w", j.Env, j.Layer, err)
	}
	addresses := []string{}
	for _, resource := range state.Resources {
		if resource.Mode != "managed" || len(resource.Instances) == 0 {
			continue
		}
		address := resource.Type + "." + resource.Name
		if resource.Module != "" {
			address = resource.Module + "." + address
		}
		addresses = append(addresses, address)
	}
	return addresses, nil
}

// MoveState copies the state of the layer into the backend of the job with `tofu init
// -migrate-state -force-copy`. It copies from the backend that the working data of the job
// records, so an earlier call with the same DataDir must have used the old backend. A
// state in the new backend is replaced. The old backend keeps its copy.
func (r *Runner) MoveState(ctx context.Context, j Job) error {
	if j.DataDir == "" {
		return fmt.Errorf("move the state of %s/%s: the job has no DataDir, which records the "+
			"backend that holds the state now", j.Env, j.Layer)
	}
	p, done, err := r.prepare(j)
	if err != nil {
		return err
	}
	defer done()
	args := append([]string{"init", "-no-color", "-input=false", "-migrate-state",
		"-force-copy"}, p.backend...)
	return r.tofu(ctx, p, j.Log, nil, args...)
}

// PrintedOutputs returns what `tofu output -json` prints for the layer, without the
// sensitive outputs. Each output keeps the form that tofu prints it in, with sensitive,
// type and value. The text is compact JSON.
func (r *Runner) PrintedOutputs(ctx context.Context, j Job) ([]byte, error) {
	p, done, err := r.prepare(r.withOperatorDataDir(j))
	if err != nil {
		return nil, err
	}
	defer done()
	if err := r.init(ctx, p); err != nil {
		return nil, err
	}
	var printed bytes.Buffer
	if err := r.tofu(ctx, p, j.Log, &printed, "output", "-json"); err != nil {
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(printed.Bytes(), &all); err != nil {
		return nil, fmt.Errorf("decode the outputs of %s/%s: %w", j.Env, j.Layer, err)
	}
	kept := make(map[string]json.RawMessage, len(all))
	for name, raw := range all {
		var output struct {
			Sensitive bool `json:"sensitive"`
		}
		if err := json.Unmarshal(raw, &output); err != nil {
			return nil, fmt.Errorf("decode the output %s of %s/%s: %w", name, j.Env, j.Layer, err)
		}
		if !output.Sensitive {
			kept[name] = raw
		}
	}
	return json.Marshal(kept)
}
