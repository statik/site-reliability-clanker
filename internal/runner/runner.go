// Package runner executes OpenTofu for one layer of one environment. It shells out to the
// same tofu CLI that an operator uses. It never decides whether a change is permitted: the
// orchestrator gives it a credential from the access broker and a saved plan to apply.
package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrStalePlan means that the state changed after the plan was saved.
var ErrStalePlan = errors.New("the saved plan is stale")

// Runner runs OpenTofu commands.
type Runner struct {
	// StateRoot holds one directory of state for each environment.
	StateRoot string
	// PluginCache is shared by all jobs so that each provider downloads once.
	PluginCache string
	// ExtraEnv is added to the environment of every job, for example TF_VAR_admin_token.
	ExtraEnv []string

	digestOnce sync.Once
	digest     string
	dockerHost string
}

// New returns a runner. dataDir is the data directory of the control plane.
func New(dataDir string) (*Runner, error) {
	r := &Runner{
		StateRoot:   filepath.Join(dataDir, "state"),
		PluginCache: filepath.Join(dataDir, "plugin-cache"),
	}
	for _, dir := range []string{r.StateRoot, r.PluginCache} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create runner directory %s: %w", dir, err)
		}
	}
	out, err := exec.Command("docker", "context", "inspect", "--format",
		"{{.Endpoints.docker.Host}}").Output()
	if err == nil {
		r.dockerHost = strings.TrimSpace(string(out))
	}
	return r, nil
}

// Job identifies the layer to run and where its code and credential are.
type Job struct {
	Env   string
	Cloud string
	Layer string
	// Checkout is the root of the exported platform-live commit.
	Checkout string
	// KubeConfig is the credential file from the access broker. Empty when the layer
	// needs no cluster credential.
	KubeConfig string
	Log        io.Writer
}

// StateDir returns the state directory of an environment.
func (r *Runner) StateDir(env string) string { return filepath.Join(r.StateRoot, env) }

func (r *Runner) stackDir(j Job) string {
	return filepath.Join(j.Checkout, "platform", "stacks", j.Cloud, j.Layer)
}

func (r *Runner) environ(j Job) []string {
	env := append(os.Environ(),
		"TF_IN_AUTOMATION=1",
		"TF_INPUT=0",
		"TF_PLUGIN_CACHE_DIR="+r.PluginCache,
		"TF_VAR_state_location="+r.StateDir(j.Env),
		"KIND_EXPERIMENTAL_DOCKER_NETWORK=clanker-"+j.Env,
	)
	env = append(env, r.ExtraEnv...)
	if r.dockerHost != "" {
		env = append(env, "DOCKER_HOST="+r.dockerHost)
	}
	if j.KubeConfig != "" {
		env = append(env, "KUBE_CONFIG_PATH="+j.KubeConfig, "KUBECONFIG="+j.KubeConfig)
	} else {
		env = append(env, "KUBE_CONFIG_PATH=", "KUBECONFIG=/dev/null")
	}
	return env
}

func (r *Runner) tofu(ctx context.Context, j Job, capture io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "tofu", args...)
	cmd.Dir = r.stackDir(j)
	cmd.Env = r.environ(j)
	var tail bytes.Buffer
	writers := []io.Writer{&tail}
	if j.Log != nil {
		fmt.Fprintf(j.Log, "$ tofu %s\n", strings.Join(args, " "))
		writers = append(writers, j.Log)
	}
	cmd.Stderr = io.MultiWriter(writers...)
	if capture != nil {
		cmd.Stdout = capture
	} else {
		cmd.Stdout = cmd.Stderr
	}
	if err := cmd.Run(); err != nil {
		text := tail.String()
		if strings.Contains(text, "Saved plan is stale") {
			return fmt.Errorf("%w: tofu %s for %s/%s", ErrStalePlan, args[0], j.Env, j.Layer)
		}
		if len(text) > 1500 {
			text = text[len(text)-1500:]
		}
		return fmt.Errorf("tofu %s for %s/%s: %w: %s", args[0], j.Env, j.Layer, err,
			strings.TrimSpace(text))
	}
	return nil
}

func (r *Runner) init(ctx context.Context, j Job) error {
	if err := os.MkdirAll(r.StateDir(j.Env), 0o755); err != nil {
		return fmt.Errorf("create state directory for %s: %w", j.Env, err)
	}
	if _, err := os.Stat(r.stackDir(j)); err != nil {
		return fmt.Errorf("find stack for %s/%s in the checkout: %w", j.Env, j.Layer, err)
	}
	statePath := filepath.Join(r.StateDir(j.Env), j.Layer+".tfstate")
	return r.tofu(ctx, j, nil, "init", "-no-color", "-input=false",
		"-backend-config=path="+statePath)
}

// PlanResult describes a saved plan.
type PlanResult struct {
	PlanPath string
	JSONPath string
	SHA256   string
}

// Plan runs init and plan for the layer. It saves the plan file and its JSON form in outDir.
// The SHA-256 of the plan file identifies exactly what an approver agrees to.
func (r *Runner) Plan(ctx context.Context, j Job, outDir string) (PlanResult, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return PlanResult{}, fmt.Errorf("create plan directory %s: %w", outDir, err)
	}
	if err := r.init(ctx, j); err != nil {
		return PlanResult{}, err
	}
	result := PlanResult{
		PlanPath: filepath.Join(outDir, j.Layer+".tfplan"),
		JSONPath: filepath.Join(outDir, j.Layer+".plan.json"),
	}
	err := r.tofu(ctx, j, nil, "plan", "-no-color", "-input=false", "-lock-timeout=60s",
		"-out="+result.PlanPath)
	if err != nil {
		return PlanResult{}, err
	}
	var planJSON bytes.Buffer
	quiet := j
	quiet.Log = nil
	if err := r.tofu(ctx, quiet, &planJSON, "show", "-json", result.PlanPath); err != nil {
		return PlanResult{}, err
	}
	if err := os.WriteFile(result.JSONPath, planJSON.Bytes(), 0o600); err != nil {
		return PlanResult{}, fmt.Errorf("write plan JSON %s: %w", result.JSONPath, err)
	}
	sum, err := FileSHA256(result.PlanPath)
	if err != nil {
		return PlanResult{}, err
	}
	result.SHA256 = sum
	return result, nil
}

// Apply applies a saved plan. It checks first that the plan file still has the approved hash.
func (r *Runner) Apply(ctx context.Context, j Job, planPath, wantSHA256 string) error {
	sum, err := FileSHA256(planPath)
	if err != nil {
		return err
	}
	if sum != wantSHA256 {
		return fmt.Errorf("apply %s/%s: plan file hash %s is not the approved hash %s",
			j.Env, j.Layer, sum, wantSHA256)
	}
	if err := r.init(ctx, j); err != nil {
		return err
	}
	return r.tofu(ctx, j, nil, "apply", "-no-color", "-input=false", "-lock-timeout=60s",
		planPath)
}

// Destroy removes every resource of a layer. Only the CLI teardown calls it.
func (r *Runner) Destroy(ctx context.Context, j Job) error {
	if err := r.init(ctx, j); err != nil {
		return err
	}
	return r.tofu(ctx, j, nil, "destroy", "-no-color", "-input=false", "-auto-approve")
}

// ApplyDirect plans and applies a layer in one step with the credentials of the operator.
// Only `clanker bootstrap` calls it, for layer 00-bootstrap.
func (r *Runner) ApplyDirect(ctx context.Context, j Job) error {
	if err := r.init(ctx, j); err != nil {
		return err
	}
	return r.tofu(ctx, j, nil, "apply", "-no-color", "-input=false", "-auto-approve")
}

// Outputs reads the outputs of a layer from its state file. A layer that was never applied
// gives an empty map.
func (r *Runner) Outputs(env, layer string) (map[string]any, error) {
	path := filepath.Join(r.StateDir(env), layer+".tfstate")
	var state struct {
		Outputs map[string]struct {
			Value     any  `json:"value"`
			Sensitive bool `json:"sensitive"`
		} `json:"outputs"`
	}
	// OpenTofu rewrites the state file during an apply. A read at that moment can see a
	// partial file, so a decode failure is tried again before it is an error.
	var decodeErr error
	for attempt := 0; attempt < 5; attempt++ {
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read state of %s/%s: %w", env, layer, err)
		}
		if decodeErr = json.Unmarshal(raw, &state); decodeErr == nil {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("decode state of %s/%s: %w", env, layer, decodeErr)
	}
	outputs := map[string]any{}
	for name, o := range state.Outputs {
		if o.Sensitive {
			continue
		}
		outputs[name] = o.Value
	}
	return outputs, nil
}

// FileSHA256 returns the hex SHA-256 of a file.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s to hash it: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ToolDigest identifies the exact tool versions that run jobs. It stands in for the digest of
// the Nix-built runner image until that image runs the jobs.
func (r *Runner) ToolDigest() string {
	r.digestOnce.Do(func() {
		h := sha256.New()
		for _, args := range [][]string{
			{"tofu", "version"}, {"helm", "version", "--short"},
			{"kubectl", "version", "--client"}, {"opa", "version"}, {"git", "--version"},
		} {
			out, err := exec.Command(args[0], args[1:]...).Output()
			if err != nil {
				fmt.Fprintf(h, "%s: unavailable\n", args[0])
				continue
			}
			h.Write(out)
		}
		r.digest = "tools-sha256:" + hex.EncodeToString(h.Sum(nil))[:32]
	})
	return r.digest
}
