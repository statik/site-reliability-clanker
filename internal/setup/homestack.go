package setup

import (
	"bytes"
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
	"syscall"
	"time"

	"github.com/statik/site-reliability-clanker/internal/setup/stack"
)

// awsHome is the directory of one environment on AWS on this machine.
type awsHome struct{ dir string }

func (h awsHome) stack() string  { return filepath.Join(h.dir, "stack") }
func (h awsHome) vars() string   { return filepath.Join(h.dir, "home.tfvars") }
func (h awsHome) state() string  { return filepath.Join(h.dir, "home.tfstate") }
func (h awsHome) data() string   { return filepath.Join(h.dir, "tfdata") }
func (h awsHome) record() string { return filepath.Join(h.dir, "install.json") }
func (h awsHome) layers() string { return filepath.Join(h.dir, "layers") }
func (h awsHome) build() string  { return filepath.Join(h.dir, "build") }

// HomeOutputs are the outputs of the home stack that are not secret.
type HomeOutputs struct {
	CoreURL          string            `json:"core_url"`
	IDPURL           string            `json:"idp_url"`
	AdminEmail       string            `json:"admin_email"`
	ApproverEmail    string            `json:"approver_email"`
	ParameterPath    string            `json:"parameter_path"`
	ParameterKey     string            `json:"parameter_key_arn"`
	CoreRole         string            `json:"core_role_arn"`
	RunnerRepository string            `json:"runner_repository_url"`
	ImageRegistry    string            `json:"image_registry"`
	Cluster          string            `json:"cluster_arn"`
	TargetGroup      string            `json:"core_target_group_arn"`
	AutoScalingGroup string            `json:"core_autoscaling_group"`
	CoreImageEnv     map[string]string `json:"core_image_env"`
}

// homeStack runs OpenTofu in the copy of the home stack, with the state of the environment
// and the session of the profile.
type homeStack struct {
	home    awsHome
	profile string
	region  string
	// out gets the output of OpenTofu, line by line and redacted. errors are redacted too.
	out    io.Writer
	redact redactor
}

// phaseVars are the inputs of phase 2: the digest of the runner image and the image of the
// core. Empty means phase 1.
type phaseVars struct {
	RunnerDigest string `json:"runner_digest,omitempty"`
	CoreImage    string `json:"core_image,omitempty"`
}

func (p phaseVars) args() []string {
	args := []string{}
	if p.RunnerDigest != "" {
		args = append(args, "-var=runner_image_digest="+p.RunnerDigest)
	}
	if p.CoreImage != "" {
		args = append(args, "-var=core_image_id="+p.CoreImage)
	}
	return args
}

// environment is the environment of OpenTofu: this one without the settings of OpenTofu
// and the keys of AWS, with the profile, the region, the directory of the working data and
// the realm of Keycloak.
func (s homeStack) environment() []string {
	env := []string{}
	for _, entry := range profileEnvironment(os.Environ(), s.profile, s.region) {
		if !strings.HasPrefix(entry, "TF_") {
			env = append(env, entry)
		}
	}
	return append(env, "TF_IN_AUTOMATION=1", "TF_INPUT=0", "TF_DATA_DIR="+s.home.data(),
		"TF_VAR_keycloak_realm="+stack.KeycloakRealm())
}

// tofuStopTime is how long OpenTofu has to stop after an interrupt. It finishes the calls
// that run and writes its state; a create of a database can take minutes.
const tofuStopTime = 30 * time.Minute

func (s homeStack) tofu(ctx context.Context, capture io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "tofu", args...)
	cmd.Dir = s.home.stack()
	cmd.Env = s.environment()
	// A stop sends one interrupt, so that OpenTofu ends the calls that run and saves its
	// state; a kill could leave a resource that the state does not know. OpenTofu has its own
	// process group: Ctrl+C in a terminal reaches only this program, and a second interrupt,
	// which makes OpenTofu stop at once, never comes from the terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = tofuStopTime
	var tail bytes.Buffer
	fmt.Fprintf(s.out, "$ tofu %s\n", strings.Join(args, " "))
	cmd.Stderr = io.MultiWriter(&tail, s.out)
	cmd.Stdout = cmd.Stderr
	if capture != nil {
		cmd.Stdout = capture
	}
	if err := cmd.Run(); err != nil {
		return s.redact.err(fmt.Errorf("tofu %s in %s: %w: %s", args[0], s.home.stack(), err,
			lastPart(tail.String())))
	}
	return nil
}

// prepare writes a new copy of the stack from the files that this binary carries, and
// initializes it with the state file of the environment.
func (s homeStack) prepare(ctx context.Context) error {
	files, err := stack.AWS()
	if err != nil {
		return fmt.Errorf("open the files of the home stack: %w", err)
	}
	if err := os.RemoveAll(s.home.stack()); err != nil {
		return fmt.Errorf("remove the copy of the home stack of an earlier run: %w", err)
	}
	err = fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(s.home.stack(), filepath.FromSlash(name))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		return os.WriteFile(target, content, mode)
	})
	if err != nil {
		return fmt.Errorf("copy the home stack to %s: %w", s.home.stack(), err)
	}
	if err := s.protect(); err != nil {
		return err
	}
	return s.tofu(ctx, nil, "init", "-no-color", "-input=false",
		"-backend-config=path="+s.home.state())
}

// protect makes the state readable only by its owner: it holds the passwords.
func (s homeStack) protect() error {
	for _, path := range []string{s.home.state(), s.home.state() + ".backup"} {
		err := os.Chmod(path, 0o600)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("make %s readable only by you: %w", path, err)
		}
	}
	return nil
}

func (s homeStack) apply(ctx context.Context, phase phaseVars) error {
	args := append([]string{"apply", "-no-color", "-input=false", "-auto-approve",
		"-var-file=" + s.home.vars()}, phase.args()...)
	err := s.tofu(ctx, nil, args...)
	if protectErr := s.protect(); protectErr != nil {
		return errors.Join(err, protectErr)
	}
	return err
}

// destroy destroys the stack and checks that its state holds no resource.
func (s homeStack) destroy(ctx context.Context, phase phaseVars) error {
	args := append([]string{"destroy", "-no-color", "-input=false", "-auto-approve",
		"-var-file=" + s.home.vars()}, phase.args()...)
	err := s.tofu(ctx, nil, args...)
	if protectErr := s.protect(); protectErr != nil {
		return errors.Join(err, protectErr)
	}
	if err != nil {
		return err
	}
	left, err := s.resources(ctx)
	if err != nil {
		return err
	}
	if len(left) > 0 {
		return fmt.Errorf("the destroy ended, but the state still holds %d resources: %s",
			len(left), strings.Join(left, ", "))
	}
	return nil
}

// resources lists the addresses of the resources in the state.
func (s homeStack) resources(ctx context.Context) ([]string, error) {
	var listed bytes.Buffer
	if err := s.tofu(ctx, &listed, "state", "list"); err != nil {
		return nil, err
	}
	return strings.Fields(listed.String()), nil
}

// outputs reads the outputs of the stack that are not secret. `tofu output -json` prints
// the secret ones too, so its output goes to no log.
func (s homeStack) outputs(ctx context.Context) (HomeOutputs, error) {
	var printed bytes.Buffer
	if err := s.tofu(ctx, &printed, "output", "-json", "-no-color"); err != nil {
		return HomeOutputs{}, err
	}
	var all map[string]struct {
		Sensitive bool            `json:"sensitive"`
		Value     json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(printed.Bytes(), &all); err != nil {
		return HomeOutputs{}, fmt.Errorf("read the outputs of the home stack: %w", err)
	}
	public := map[string]json.RawMessage{}
	for name, output := range all {
		if !output.Sensitive {
			public[name] = output.Value
		}
	}
	text, err := json.Marshal(public)
	if err != nil {
		return HomeOutputs{}, fmt.Errorf("read the outputs of the home stack: %w", err)
	}
	var outputs HomeOutputs
	if err := json.Unmarshal(text, &outputs); err != nil {
		return HomeOutputs{}, fmt.Errorf("read the outputs of the home stack: %w", err)
	}
	return outputs, nil
}
