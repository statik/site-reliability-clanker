// Package runner executes the jobs of the control plane: OpenTofu for one layer of one
// environment, a runbook script, or a read-only kubectl command. It starts the same
// programs that an operator uses. It never decides whether a change is permitted: the
// orchestrator gives it a credential from the access broker and a saved plan to apply.
//
// The runner owns every file of a job. A job comes in as values (the archive of a commit,
// the bytes of a plan, the text of a credential) and its results go back as values, so the
// runner can run in another process or on another machine than the orchestrator.
package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
	"sort"
	"strings"
	"sync"

	"github.com/statik/site-reliability-clanker/internal/jobspec"
)

// ErrStalePlan means that the state changed after the plan was saved.
var ErrStalePlan = jobspec.ErrStalePlan

// Runner runs the jobs.
type Runner struct {
	// StateRoot holds one directory of state for each environment.
	StateRoot string
	// PluginCache is shared by all jobs so that each provider downloads once.
	PluginCache string
	// WorkRoot holds the unpacked checkouts and the temporary files of the jobs.
	WorkRoot string
	// ExtraEnv is added to the environment of every job, for example TF_VAR_admin_token.
	ExtraEnv []string

	digestOnce sync.Once
	digest     string
	dockerHost string
	unpack     sync.Mutex
}

// New returns a runner that keeps its files below dir.
func New(dir string) (*Runner, error) {
	r := &Runner{
		StateRoot:   filepath.Join(dir, "state"),
		PluginCache: filepath.Join(dir, "plugin-cache"),
		WorkRoot:    filepath.Join(dir, "work"),
	}
	for _, sub := range []string{r.StateRoot, r.PluginCache, r.WorkRoot} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return nil, fmt.Errorf("create runner directory %s: %w", sub, err)
		}
	}
	out, err := exec.Command("docker", "context", "inspect", "--format",
		"{{.Endpoints.docker.Host}}").Output()
	if err == nil {
		r.dockerHost = strings.TrimSpace(string(out))
	}
	return r, nil
}

// Checkout is the tree of one commit that a job works on.
type Checkout struct {
	// ID names the checkout. Two jobs with the same ID share one unpacked directory, so a
	// plan and its apply run in the same place.
	ID string
	// Archive is the tree as a gzip-compressed tar file.
	Archive []byte
	// Dir is a directory that holds the tree already. The command line uses it for a
	// checkout of the repository. When it is set, ID and Archive are not used.
	Dir string
}

// Job is one OpenTofu run.
type Job struct {
	Env      string
	Cloud    string
	Layer    string
	Checkout Checkout
	// KubeConfig is the text of the credential file from the access broker. It is empty
	// when the layer needs no cluster credential.
	KubeConfig string
	// AWS is the AWS credential from the access broker. It is nil when the job has none.
	AWS *jobspec.AWSSession
	// Backend holds the settings of a remote state backend, as `tofu init -backend-config`
	// takes them. When it is empty the state is a file in the state directory.
	Backend map[string]string
	// LocalState keeps the state of a job on a cloud target in the state directory, with
	// the local backend. The stack must then have a local backend: the commands of the
	// operator give layer 00-bootstrap one with an override file in a copy of the stack,
	// while the bucket that the layer creates does not hold its state.
	LocalState bool
	// DataDir is the directory for the working data of OpenTofu (TF_DATA_DIR), which
	// records the backend. Empty: the directory .terraform of the stack, or for the
	// commands of the operator a directory below the work directory of the runner.
	DataDir string
	// Vars are given with -var to an apply or a destroy without a saved plan, so they
	// replace the values of terraform.tfvars. Only the commands of the operator set them.
	Vars map[string]string
	Log  io.Writer
}

// StateDir returns the state directory of an environment.
func (r *Runner) StateDir(env string) string { return filepath.Join(r.StateRoot, env) }

// StatePath returns the state file of a layer in the state directory: the state of every
// layer on the local target, and of layer 00-bootstrap on a cloud while it is local.
func (r *Runner) StatePath(env, layer string) string {
	return filepath.Join(r.StateDir(env), layer+".tfstate")
}

// checkoutDir returns the directory with the tree of the checkout. It unpacks the archive
// on the first use of an ID.
func (r *Runner) checkoutDir(c Checkout) (string, error) {
	if c.Dir != "" {
		return c.Dir, nil
	}
	if c.ID == "" || strings.Contains(c.ID, "..") || filepath.IsAbs(c.ID) {
		return "", fmt.Errorf("the checkout ID %q is not valid", c.ID)
	}
	r.unpack.Lock()
	defer r.unpack.Unlock()
	dir := filepath.Join(r.WorkRoot, "checkouts", filepath.FromSlash(c.ID))
	marker := filepath.Join(dir, ".complete")
	if _, err := os.Stat(marker); err == nil {
		return dir, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("remove incomplete checkout %s: %w", dir, err)
	}
	if err := untar(c.Archive, dir); err != nil {
		return "", fmt.Errorf("unpack checkout %s: %w", c.ID, err)
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		return "", fmt.Errorf("mark checkout %s as complete: %w", c.ID, err)
	}
	return dir, nil
}

func untar(archive []byte, dir string) error {
	unzipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("open the archive: %w", err)
	}
	reader := tar.NewReader(unzipped)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return os.MkdirAll(dir, 0o755)
		}
		if err != nil {
			return fmt.Errorf("read the archive: %w", err)
		}
		name := filepath.Clean(header.Name)
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("the archive has an entry outside its directory: %q", header.Name)
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
		}
		switch header.Typeflag {
		case tar.TypeReg:
			content, err := io.ReadAll(reader)
			if err != nil {
				return fmt.Errorf("read %s from the archive: %w", header.Name, err)
			}
			mode := os.FileMode(0o644)
			if header.Mode&0o111 != 0 {
				mode = 0o755
			}
			if err := os.WriteFile(path, content, mode); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
		case tar.TypeSymlink:
			if err := os.Symlink(header.Linkname, path); err != nil {
				return fmt.Errorf("link %s: %w", path, err)
			}
		}
	}
}

// credentialFile writes the text of a credential to a file that only this user can read.
// The returned function removes it. An empty text gives an empty path.
func (r *Runner) credentialFile(text string) (string, func(), error) {
	if text == "" {
		return "", func() {}, nil
	}
	dir := filepath.Join(r.WorkRoot, "credentials")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, fmt.Errorf("create credential directory %s: %w", dir, err)
	}
	file, err := os.CreateTemp(dir, "*.kubeconfig")
	if err != nil {
		return "", nil, fmt.Errorf("create credential file: %w", err)
	}
	remove := func() { os.Remove(file.Name()) }
	if _, err := file.WriteString(text); err != nil {
		file.Close()
		remove()
		return "", nil, fmt.Errorf("write credential file: %w", err)
	}
	if err := file.Close(); err != nil {
		remove()
		return "", nil, fmt.Errorf("close credential file: %w", err)
	}
	return file.Name(), remove, nil
}

// run is one prepared job: where its stack is, its environment, and the arguments of
// `tofu init` that say where its state is.
type run struct {
	job     Job
	stack   string
	environ []string
	backend []string
}

func (r *Runner) prepare(j Job) (run, func(), error) {
	checkout, err := r.checkoutDir(j.Checkout)
	if err != nil {
		return run{}, nil, err
	}
	stack := filepath.Join(checkout, "platform", "stacks", j.Cloud, j.Layer)
	if _, err := os.Stat(stack); err != nil {
		return run{}, nil, fmt.Errorf("find stack for %s/%s in the checkout: %w", j.Env, j.Layer, err)
	}
	backend, err := r.backendConfig(j)
	if err != nil {
		return run{}, nil, err
	}
	stateLocation, err := r.stateLocation(j)
	if err != nil {
		return run{}, nil, err
	}
	kubeconfig, remove, err := r.credentialFile(j.KubeConfig)
	if err != nil {
		return run{}, nil, err
	}
	env := append(os.Environ(),
		"TF_IN_AUTOMATION=1",
		"TF_INPUT=0",
		"TF_PLUGIN_CACHE_DIR="+r.PluginCache,
	)
	if stateLocation != "" {
		env = append(env, "TF_VAR_state_location="+stateLocation)
	}
	if j.Cloud == "local" {
		env = append(env, "KIND_EXPERIMENTAL_DOCKER_NETWORK=clanker-"+j.Env)
	}
	// The variables of the job come after the values above, so they replace them.
	env = append(env, r.ExtraEnv...)
	if r.dockerHost != "" {
		env = append(env, "DOCKER_HOST="+r.dockerHost)
	}
	if j.DataDir != "" {
		env = append(env, "TF_DATA_DIR="+j.DataDir)
	}
	if kubeconfig != "" {
		env = append(env, "KUBE_CONFIG_PATH="+kubeconfig, "KUBECONFIG="+kubeconfig)
	} else {
		env = append(env, "KUBE_CONFIG_PATH=", "KUBECONFIG=/dev/null")
	}
	env = append(env, awsEnv(j.AWS)...)
	return run{job: j, stack: stack, environ: env, backend: backend}, remove, nil
}

// awsEnv returns the keys of the AWS session of a job as environment variables, or none
// for a job without one. The keys exist only in the environment of the programs of the
// job, which `aws eks get-token` in a kubeconfig also reads. They come last, so they replace
// keys that the runner itself has. No file holds them.
func awsEnv(session *jobspec.AWSSession) []string {
	if session == nil {
		return nil
	}
	return []string{"AWS_ACCESS_KEY_ID=" + session.AccessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + session.SecretAccessKey,
		"AWS_SESSION_TOKEN=" + session.SessionToken}
}

// stateLocation returns the value of the input state_location of the stacks. With a
// backend it is the bucket of the backend. A backend with no bucket gives an empty value,
// and the variables of the job must then give it. Without a backend it is the state
// directory of the environment, which it creates. On a cloud target a local state is not
// the state location: the job gives the bucket in its variables.
func (r *Runner) stateLocation(j Job) (string, error) {
	if len(j.Backend) > 0 {
		return j.Backend["bucket"], nil
	}
	dir := r.StateDir(j.Env)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create state directory for %s: %w", j.Env, err)
	}
	if j.Cloud != "local" {
		return "", nil
	}
	return dir, nil
}

func (r *Runner) tofu(ctx context.Context, p run, log, capture io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "tofu", args...)
	cmd.Dir = p.stack
	cmd.Env = p.environ
	var tail bytes.Buffer
	writers := []io.Writer{&tail}
	if log != nil {
		fmt.Fprintf(log, "$ tofu %s\n", strings.Join(args, " "))
		writers = append(writers, log)
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
			return fmt.Errorf("%w: tofu %s for %s/%s", ErrStalePlan, args[0], p.job.Env, p.job.Layer)
		}
		if len(text) > 1500 {
			text = text[len(text)-1500:]
		}
		return fmt.Errorf("tofu %s for %s/%s: %w: %s", args[0], p.job.Env, p.job.Layer, err,
			strings.TrimSpace(text))
	}
	return nil
}

// backendConfig returns the arguments of `tofu init` that say where the state of the job is.
// The stacks of the local target, and a stack with LocalState, have the local backend,
// which takes a path. A job on another target must bring the settings of its backend. On
// AWS they are checked, so that an s3 backend gets complete settings and no local one.
func (r *Runner) backendConfig(j Job) ([]string, error) {
	if j.LocalState && len(j.Backend) > 0 {
		return nil, fmt.Errorf("the job for %s/%s has backend settings and keeps its state in "+
			"the state directory: give one of them", j.Env, j.Layer)
	}
	if len(j.Backend) == 0 {
		if j.Cloud != "local" && !j.LocalState {
			return nil, fmt.Errorf("the job for %s/%s on %s has no backend settings: only the "+
				"stacks of the local target keep their state in the state directory", j.Env,
				j.Layer, j.Cloud)
		}
		return []string{"-backend-config=path=" + r.StatePath(j.Env, j.Layer)}, nil
	}
	if j.Cloud == "aws" {
		if err := checkS3Backend(j.Backend); err != nil {
			return nil, fmt.Errorf("the job for %s/%s: %w", j.Env, j.Layer, err)
		}
	}
	names := make([]string, 0, len(j.Backend))
	for name := range j.Backend {
		names = append(names, name)
	}
	sort.Strings(names)
	config := make([]string, 0, len(names))
	for _, name := range names {
		config = append(config, "-backend-config="+name+"="+j.Backend[name])
	}
	return config, nil
}

func (r *Runner) init(ctx context.Context, p run) error {
	args := append([]string{"init", "-no-color", "-input=false"}, p.backend...)
	return r.tofu(ctx, p, p.job.Log, nil, args...)
}

// PlanResult is a saved plan.
type PlanResult struct {
	// Plan is the plan file of OpenTofu. Apply needs exactly these bytes.
	Plan []byte
	// JSON is the same plan in the JSON form.
	JSON []byte
	// SHA256 is the hash of Plan. It identifies exactly what an approver agrees to.
	SHA256 string
}

// planFile makes an empty temporary directory for one plan file.
func (r *Runner) planFile(layer string) (string, func(), error) {
	dir, err := os.MkdirTemp(r.WorkRoot, "plan-")
	if err != nil {
		return "", nil, fmt.Errorf("create plan directory: %w", err)
	}
	return filepath.Join(dir, layer+".tfplan"), func() { os.RemoveAll(dir) }, nil
}

// Plan runs init and plan for the layer and returns the saved plan.
func (r *Runner) Plan(ctx context.Context, j Job) (PlanResult, error) {
	p, done, err := r.prepare(j)
	if err != nil {
		return PlanResult{}, err
	}
	defer done()
	path, remove, err := r.planFile(j.Layer)
	if err != nil {
		return PlanResult{}, err
	}
	defer remove()
	if err := r.init(ctx, p); err != nil {
		return PlanResult{}, err
	}
	if err := r.tofu(ctx, p, j.Log, nil, "plan", "-no-color", "-input=false",
		"-lock-timeout=60s", "-out="+path); err != nil {
		return PlanResult{}, err
	}
	var planJSON bytes.Buffer
	if err := r.tofu(ctx, p, nil, &planJSON, "show", "-json", path); err != nil {
		return PlanResult{}, err
	}
	plan, err := os.ReadFile(path)
	if err != nil {
		return PlanResult{}, fmt.Errorf("read the saved plan of %s/%s: %w", j.Env, j.Layer, err)
	}
	return PlanResult{Plan: plan, JSON: planJSON.Bytes(), SHA256: jobspec.SHA256(plan)}, nil
}

// Apply applies a saved plan and returns the outputs of the layer. It checks first that
// the plan has the approved hash. It never makes a new plan.
func (r *Runner) Apply(ctx context.Context, j Job, plan []byte, wantSHA256 string) (map[string]any, error) {
	if sum := jobspec.SHA256(plan); sum != wantSHA256 {
		return nil, fmt.Errorf("apply %s/%s: plan hash %s is not the approved hash %s",
			j.Env, j.Layer, sum, wantSHA256)
	}
	p, done, err := r.prepare(j)
	if err != nil {
		return nil, err
	}
	defer done()
	path, remove, err := r.planFile(j.Layer)
	if err != nil {
		return nil, err
	}
	defer remove()
	if err := os.WriteFile(path, plan, 0o600); err != nil {
		return nil, fmt.Errorf("write the plan of %s/%s: %w", j.Env, j.Layer, err)
	}
	if err := r.init(ctx, p); err != nil {
		return nil, err
	}
	if err := r.tofu(ctx, p, j.Log, nil, "apply", "-no-color", "-input=false",
		"-lock-timeout=60s", path); err != nil {
		return nil, err
	}
	return r.outputs(ctx, p)
}

// outputs returns the outputs of the layer that are not sensitive. `tofu output -json`
// reads them from the backend of the job, so a bucket gives them as a local file does. It
// prints the sensitive values too, so what it prints goes to no log.
func (r *Runner) outputs(ctx context.Context, p run) (map[string]any, error) {
	var printed bytes.Buffer
	if err := r.tofu(ctx, p, p.job.Log, &printed, "output", "-json"); err != nil {
		return nil, err
	}
	var all map[string]struct {
		Value     any  `json:"value"`
		Sensitive bool `json:"sensitive"`
	}
	if err := json.Unmarshal(printed.Bytes(), &all); err != nil {
		return nil, fmt.Errorf("decode the outputs of %s/%s: %w", p.job.Env, p.job.Layer, err)
	}
	outputs := make(map[string]any, len(all))
	for name, output := range all {
		if !output.Sensitive {
			outputs[name] = output.Value
		}
	}
	return outputs, nil
}

// Destroy removes every resource of a layer. Only the CLI teardown calls it. The teardown
// gives the stack directories of the sources, which all data directories share. Thus the
// working data of OpenTofu, which records the backend, stays out of the stack directory.
func (r *Runner) Destroy(ctx context.Context, j Job) error {
	p, done, err := r.prepare(r.withOperatorDataDir(j))
	if err != nil {
		return err
	}
	defer done()
	if err := r.init(ctx, p); err != nil {
		return err
	}
	args := append([]string{"destroy", "-no-color", "-input=false", "-auto-approve"},
		varArgs(j.Vars)...)
	return r.tofu(ctx, p, j.Log, nil, args...)
}

// ApplyDirect plans and applies a layer in one step with the credentials of the operator,
// and returns the outputs of the layer. Only `clanker bootstrap` calls it, for layer
// 00-bootstrap.
func (r *Runner) ApplyDirect(ctx context.Context, j Job) (map[string]any, error) {
	p, done, err := r.prepare(j)
	if err != nil {
		return nil, err
	}
	defer done()
	if err := r.init(ctx, p); err != nil {
		return nil, err
	}
	args := append([]string{"apply", "-no-color", "-input=false", "-auto-approve"},
		varArgs(j.Vars)...)
	if err := r.tofu(ctx, p, j.Log, nil, args...); err != nil {
		return nil, err
	}
	return r.outputs(ctx, p)
}

// RunbookJob is one run of a script from the runbooks repository.
type RunbookJob struct {
	Env string
	// Checkout is the runbooks repository at the commit of the work item.
	Checkout Checkout
	// Name is the name of the script in the jobs directory, without ".sh".
	Name       string
	Params     map[string]string
	KubeConfig string
	// AWS is the AWS session of the grant of the run. It is nil when the run has none. On
	// AWS the kubeconfig gets its tokens with it.
	AWS *jobspec.AWSSession
	// Namespace and DataNamespace are the targets of the environment: the namespace of the
	// application, and the namespace of its data services or "" when there is none.
	Namespace     string
	DataNamespace string
	Log           io.Writer
}

// Runbook runs the script with the credential, the AWS session, the targets and the
// parameters in its environment. It returns the outputs that the script wrote to the file
// that OUTPUT_FILE names, one "name=value" on each line. The core checks them against the
// definition of the runbook.
func (r *Runner) Runbook(ctx context.Context, j RunbookJob) (map[string]any, error) {
	checkout, err := r.checkoutDir(j.Checkout)
	if err != nil {
		return nil, err
	}
	script := filepath.Join(checkout, "jobs", j.Name+".sh")
	if _, err := os.Stat(script); err != nil {
		return nil, fmt.Errorf("runbook %s is not in the checkout %s", j.Name, j.Checkout.ID)
	}
	kubeconfig, remove, err := r.credentialFile(j.KubeConfig)
	if err != nil {
		return nil, err
	}
	defer remove()
	outputFile, removeOutputs, err := r.outputFile()
	if err != nil {
		return nil, fmt.Errorf("runbook %s: %w", j.Name, err)
	}
	defer removeOutputs()
	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfig, "STATE_DIR="+r.StateDir(j.Env),
		"OUTPUT_FILE="+outputFile, "TARGET_APPLICATION="+j.Namespace,
		"TARGET_DATA="+j.DataNamespace)
	cmd.Env = append(cmd.Env, paramEnv(j.Params)...)
	cmd.Env = append(cmd.Env, awsEnv(j.AWS)...)
	cmd.Stdout, cmd.Stderr = j.Log, j.Log
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("runbook %s: %w", j.Name, err)
	}
	raw, err := os.ReadFile(outputFile)
	if err != nil {
		return nil, fmt.Errorf("read the outputs of runbook %s: %w", j.Name, err)
	}
	outputs, err := parseOutputs(string(raw))
	if err != nil {
		return nil, fmt.Errorf("runbook %s: %w", j.Name, err)
	}
	return outputs, nil
}

// outputFile makes the empty file that a script writes its outputs to. The returned
// function removes it.
func (r *Runner) outputFile() (string, func(), error) {
	file, err := os.CreateTemp(r.WorkRoot, "outputs-*")
	if err != nil {
		return "", nil, fmt.Errorf("create the output file: %w", err)
	}
	remove := func() { os.Remove(file.Name()) }
	if err := file.Close(); err != nil {
		remove()
		return "", nil, fmt.Errorf("close the output file: %w", err)
	}
	return file.Name(), remove, nil
}

// paramEnv gives each parameter as PARAM_<NAME> in upper case, in the order of the names.
func paramEnv(params map[string]string) []string {
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	env := make([]string, 0, len(names))
	for _, name := range names {
		env = append(env, "PARAM_"+strings.ToUpper(name)+"="+params[name])
	}
	return env
}

// parseOutputs reads the lines "name=value" of an output file. An empty line is skipped.
func parseOutputs(text string) (map[string]any, error) {
	outputs := map[string]any{}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found || name == "" {
			return nil, fmt.Errorf("the output line %q is not name=value", line)
		}
		if _, twice := outputs[name]; twice {
			return nil, fmt.Errorf("the output %q is written twice", name)
		}
		outputs[name] = value
	}
	return outputs, nil
}

// Kubectl runs one kubectl command with the credential and returns what it printed. The
// output comes back also when the command fails. session is the AWS session of the
// grant, or nil: on AWS the kubeconfig gets its tokens with it.
func (r *Runner) Kubectl(ctx context.Context, kubeConfig string, session *jobspec.AWSSession,
	args ...string) ([]byte, error) {
	path, remove, err := r.credentialFile(kubeConfig)
	if err != nil {
		return nil, err
	}
	defer remove()
	cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", path}, args...)...)
	cmd.Env = append(os.Environ(), awsEnv(session)...)
	return cmd.CombinedOutput()
}

// ToolDigest identifies the exact tool versions that run jobs. It stands in for the digest of
// the Nix-built runner image until that image runs the jobs.
func (r *Runner) ToolDigest() string {
	r.digestOnce.Do(func() {
		h := sha256.New()
		for _, args := range [][]string{
			{"tofu", "version"}, {"helm", "version", "--short"},
			{"kubectl", "version", "--client"},
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
