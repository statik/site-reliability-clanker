package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DefaultNanos is the Nanos release whose kernel the image of the core boots. 0.1.55 is the
// fallback when it does not boot on EC2 (#133).
const DefaultNanos = "0.1.56"

// InstallSteps are the steps of the installation, in order. The wizard lists them.
var InstallSteps = []string{
	"Check this machine and the AWS session",
	"Check the inputs",
	"Create the home of the control plane (phase 1)",
	"Build and push the runner image",
	"Build and push the images of the application",
	"Build the machine image of the core",
	"Apply layer 00-bootstrap",
	"Start the control plane (phase 2)",
	"Check the dashboard through the load balancer",
}

// LayerRequest names an environment on AWS for the commands of its layers.
type LayerRequest struct {
	Env     string
	Profile string
	Region  string
	// BrokerPrincipal, ParameterPath and KMSKey come from the outputs of the home stack.
	BrokerPrincipal string
	ParameterPath   string
	KMSKey          string
	// DataDir holds the copies of the layers and the local state of 00-bootstrap.
	DataDir string
	// Variables go to each layer as TF_VAR_<name>, as the core gives them from
	// CLANKER_AWS_LAYER_VARIABLES. A list or a map is JSON.
	Variables map[string]string
	Log       io.Writer
}

// Layers applies and destroys the layers of an environment on AWS with the session of a
// profile: `clanker bootstrap --cloud aws` and `clanker destroy --cloud aws`.
type Layers interface {
	Bootstrap(ctx context.Context, request LayerRequest) error
	Destroy(ctx context.Context, request LayerRequest) error
}

// Installer creates and removes the home of the control plane on AWS. The wizard and
// `clanker install --cloud aws` run the same steps.
type Installer struct {
	// CLI runs the aws program. Nil means the real program.
	CLI CLI
	// Layers applies 00-bootstrap and destroys the layers.
	Layers Layers
	// Root holds a directory for each environment. Empty means ~/.clanker/aws.
	Root string
	// Version is the release of this binary, or "dev".
	Version string
	// HTTP checks the dashboard. Nil means a client with a timeout.
	HTTP *http.Client
	// Wait is the pause between two checks. Zero means 10 seconds.
	Wait time.Duration
}

// InstallInput is what the person chooses: in the wizard or with the flags of the command.
// An empty field takes the value of home.tfvars, when the file has one.
type InstallInput struct {
	Env     string
	Profile string
	Region  string
	// Network, Subnets and RouterGroup are the VPC, its private subnets and the security
	// group of its Tailscale subnet router.
	Network     string
	Subnets     []string
	RouterGroup string
	ZoneID      string
	// Tags are the tags that the organization requires. A key with an empty value keeps
	// the value of home.tfvars. No log, error or record holds a value.
	Tags map[string]string
	// ModelKey is the OpenRouter key of the core. Empty keeps the key of home.tfvars.
	ModelKey string
	Nanos    string
	// Source is a checkout of this repository. The runner image, the binary of the core
	// and the images of the application are built from it.
	Source string
}

// Report receives the progress: the start of each step and each line of output. The lines
// are redacted.
type Report struct {
	Step func(index int, title string)
	Line func(text string)
}

// InstallResult is what the person needs after the installation.
type InstallResult struct {
	DashboardURL  string  `json:"dashboard_url"`
	KeycloakURL   string  `json:"keycloak_url"`
	AdminEmail    string  `json:"admin_email"`
	ApproverEmail string  `json:"approver_email"`
	Passwords     string  `json:"passwords"`
	Directory     string  `json:"directory"`
	WeeklyUSD     float64 `json:"weekly_usd"`
}

// recordedImage is a machine image of the core with the release and the kernel it has.
type recordedImage struct {
	Image
	Release string `json:"release"`
	Nanos   string `json:"nanos"`
}

// installRecord is what the installer did, in install.json. It holds no secret and no
// value of a tag, so the next run can resume and the removal finds what is outside the
// state of the home stack.
type installRecord struct {
	Env     string      `json:"env"`
	Outputs HomeOutputs `json:"outputs"`
	// Runner is the runner image of a release in its repository.
	Runner struct {
		Release string `json:"release"`
		Digest  string `json:"digest"`
	} `json:"runner"`
	// Application is the release whose application images are in their repository.
	Application string `json:"application_release"`
	// Images are the machine images that the installer built and did not remove.
	Images []recordedImage `json:"images"`
	// Applied are the inputs of phase 2 of the last apply of the home stack.
	Applied   phaseVars `json:"applied"`
	Installed bool      `json:"installed"`
}

func readRecord(path string) (installRecord, error) {
	var record installRecord
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return record, nil
	}
	if err != nil {
		return record, fmt.Errorf("read the record of the installer: %w", err)
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, fmt.Errorf("read %s: %w; it is the record of the installer, and a "+
			"removal needs it", path, err)
	}
	return record, nil
}

func writeRecord(path string, record installRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the record of the installer: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write the record of the installer: %w", err)
	}
	return nil
}

func (i *Installer) home(env string) (awsHome, error) {
	root := i.Root
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return awsHome{}, fmt.Errorf("find the home directory: %w", err)
		}
		root = filepath.Join(home, ".clanker", "aws")
	}
	if !envNamePattern.MatchString(env) {
		return awsHome{}, fmt.Errorf("the environment name %q is not valid: use 3 to 24 "+
			"lowercase letters, digits or hyphens, and start with a letter", env)
	}
	return awsHome{dir: filepath.Join(root, env)}, nil
}

// Defaults reads home.tfvars of an environment: the values that an empty input takes.
func (i *Installer) Defaults(env string) (HomeVars, bool, error) {
	home, err := i.home(env)
	if err != nil {
		return HomeVars{}, false, err
	}
	return ReadHomeVars(home.vars())
}

// Directory returns the directory of an environment on this machine.
func (i *Installer) Directory(env string) (string, error) {
	home, err := i.home(env)
	return home.dir, err
}

func (i *Installer) wait() time.Duration {
	if i.Wait > 0 {
		return i.Wait
	}
	return 10 * time.Second
}

// poll calls check until it reports done, at most tries times, with a pause between.
func (i *Installer) poll(ctx context.Context, tries int, check func() (bool, error)) (bool,
	error) {
	for try := 0; try < tries; try++ {
		done, err := check()
		if err != nil || done {
			return done, err
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(i.wait()):
		}
	}
	return false, nil
}

// merge fills the empty fields of the input with the values of home.tfvars.
func merge(in InstallInput, saved HomeVars) HomeVars {
	vars := saved
	vars.Env = in.Env
	pick := func(value, fallback string) string {
		if value != "" {
			return value
		}
		return fallback
	}
	vars.Region = pick(in.Region, saved.Region)
	vars.Network = pick(in.Network, saved.Network)
	vars.RouterGroup = pick(in.RouterGroup, saved.RouterGroup)
	vars.ZoneID = pick(in.ZoneID, saved.ZoneID)
	vars.ModelKey = pick(in.ModelKey, saved.ModelKey)
	if len(in.Subnets) > 0 {
		vars.Subnets = in.Subnets
	}
	if in.Tags != nil {
		vars.Tags = map[string]string{}
		for key, value := range in.Tags {
			vars.Tags[key] = pick(value, saved.Tags[key])
		}
	}
	if vars.Tags == nil {
		vars.Tags = map[string]string{}
	}
	return vars
}

// secretsOf returns the values that no output may show.
func secretsOf(vars HomeVars) redactor {
	values := []string{vars.ModelKey}
	for _, value := range vars.Tags {
		values = append(values, value)
	}
	return newRedactor(values...)
}

// program runs a program with its output in a log, and fails with the end of its output.
type program struct {
	out    io.Writer
	redact redactor
}

func (p program) run(ctx context.Context, dir string, env []string, stdin io.Reader,
	name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = dir, env, stdin
	var tail bytes.Buffer
	fmt.Fprintf(p.out, "$ %s %s\n", name, strings.Join(args, " "))
	cmd.Stdout = io.MultiWriter(&tail, p.out)
	cmd.Stderr = cmd.Stdout
	if err := cmd.Run(); err != nil {
		return p.redact.err(fmt.Errorf("%s %s: %w: %s", name, args[0], err,
			lastPart(tail.String())))
	}
	return nil
}

// output runs a program and returns what it printed on standard output.
func (p program) output(ctx context.Context, dir string, name string, args ...string) (string,
	error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", p.redact.err(fmt.Errorf("%s %s: %w: %s", name, args[0], err,
			lastPart(stderr.String())))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// layerVariables are the inputs of the layers that the home stack writes to
// CLANKER_AWS_LAYER_VARIABLES, as TF_VAR_ values: a destroy of the layers needs them.
func layerVariables(vars HomeVars, outputs HomeOutputs, operator string) (map[string]string,
	error) {
	subnets, err := json.Marshal(vars.Subnets)
	if err != nil {
		return nil, err
	}
	tags, err := json.Marshal(vars.Tags)
	if err != nil {
		return nil, err
	}
	return map[string]string{"existing_network_id": vars.Network,
		"existing_private_subnet_ids": string(subnets), "image_registry": outputs.ImageRegistry,
		"instance_tags": string(tags), "operator_principal_arn": operator}, nil
}

// weeklyPrice is the price of one week of what runs all the time.
func weeklyPrice() float64 {
	total := 0.0
	for _, line := range awsPrices.Lines {
		total += line.Hourly * hoursEachWeek
	}
	return total
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
