package setup

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed controlplane.yaml
var controlPlaneTemplate []byte

// ErrNeedsLogin means that the profile has no valid session.
var ErrNeedsLogin = errors.New("the AWS profile has no valid session")

var (
	envNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,22}[a-z0-9]$`)
	regionPattern  = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d$`)
	profilePattern = regexp.MustCompile(`^[A-Za-z0-9_.@+=,][A-Za-z0-9_.@+=,/-]{0,127}$`)
)

// CLI runs the aws program and returns its standard output. Tests replace it.
type CLI func(ctx context.Context, args ...string) ([]byte, error)

func realCLI(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "aws", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(stderr.String())
		if len(text) > 600 {
			text = text[len(text)-600:]
		}
		return nil, fmt.Errorf("aws %s: %w: %s", args[0]+" "+args[1], err, text)
	}
	return stdout.Bytes(), nil
}

// AWS talks to one account through one profile of the AWS CLI. The wizard uses the
// session of the person who runs it. It stores no credential.
type AWS struct {
	Profile string
	Region  string
	Env     string
	Run     CLI
	// WorkDir holds the temporary input files of the CLI.
	WorkDir string
}

// Validate checks the three names before they go into a command line.
func (a AWS) Validate() error {
	if !profilePattern.MatchString(a.Profile) {
		return fmt.Errorf("the profile name %q is not valid", a.Profile)
	}
	if !regionPattern.MatchString(a.Region) {
		return fmt.Errorf("the region %q is not valid: use a name such as us-east-1", a.Region)
	}
	if !envNamePattern.MatchString(a.Env) {
		return fmt.Errorf("the environment name %q is not valid: use 3 to 24 lowercase "+
			"letters, digits or hyphens, and start with a letter", a.Env)
	}
	return nil
}

func (a AWS) cli(ctx context.Context, args ...string) ([]byte, error) {
	full := append(args, "--profile", a.Profile, "--region", a.Region, "--output", "json")
	return a.Run(ctx, full...)
}

// StackName is the name of the CloudFormation stack of the control plane.
func (a AWS) StackName() string { return "clanker-" + a.Env + "-control" }

// KeyParameter is the name of the Parameter Store entry of the model API key.
func (a AWS) KeyParameter() string { return "/clanker/" + a.Env + "/openrouter-api-key" }

// Profiles lists the profiles of the AWS CLI on this machine.
func Profiles(ctx context.Context, run CLI) ([]string, error) {
	out, err := run(ctx, "configure", "list-profiles")
	if err != nil {
		return nil, err
	}
	profiles := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			profiles = append(profiles, name)
		}
	}
	return profiles, nil
}

// Identity is the answer of AWS to "who am I".
type Identity struct {
	Account string `json:"Account"`
	Arn     string `json:"Arn"`
}

// WhoAmI returns the account and principal of the profile, or ErrNeedsLogin.
func (a AWS) WhoAmI(ctx context.Context) (Identity, error) {
	out, err := a.cli(ctx, "sts", "get-caller-identity")
	if err != nil {
		text := strings.ToLower(err.Error())
		for _, sign := range []string{"sso", "expired", "token", "credentials"} {
			if strings.Contains(text, sign) {
				return Identity{}, fmt.Errorf("%w: %v", ErrNeedsLogin, err)
			}
		}
		return Identity{}, err
	}
	var id Identity
	if err := json.Unmarshal(out, &id); err != nil {
		return Identity{}, fmt.Errorf("decode the answer of sts get-caller-identity: %w", err)
	}
	return id, nil
}

// Login starts the browser sign-in of the profile and waits for it.
func (a AWS) Login(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if _, err := a.Run(ctx, "sso", "login", "--profile", a.Profile); err != nil {
		return fmt.Errorf("%w. If the profile %s gets its credentials from another tool, "+
			"sign in with that tool and choose \"Sign in\" again", err, a.Profile)
	}
	return nil
}

// PutKey stores the model API key as a SecureString. The key goes to the CLI in a file
// that only this user can read, not on the command line, and the file is removed.
func (a AWS) PutKey(ctx context.Context, key string) error {
	input, err := json.Marshal(map[string]any{"Name": a.KeyParameter(), "Value": key,
		"Type": "SecureString", "Overwrite": true})
	if err != nil {
		return fmt.Errorf("encode the parameter: %w", err)
	}
	file, err := os.CreateTemp(a.WorkDir, "parameter-*.json")
	if err != nil {
		return fmt.Errorf("create the parameter file: %w", err)
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("protect the parameter file: %w", err)
	}
	if _, err := file.Write(input); err != nil {
		return fmt.Errorf("write the parameter file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close the parameter file: %w", err)
	}
	_, err = a.cli(ctx, "ssm", "put-parameter", "--cli-input-json", "file://"+file.Name())
	return err
}

// Binary is the file that the container downloads.
type Binary struct {
	URL    string
	SHA256 string
}

const changeSetName = "clanker-setup"

// Change is one resource that the review found.
type Change struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	Type   string `json:"type"`
}

// Review asks CloudFormation what the template would create. It creates a change set and
// no resource. The change set is what Deploy executes, so the review is what runs.
func (a AWS) Review(ctx context.Context, binary Binary, size Size, hasKey bool) ([]Change, error) {
	template := filepath.Join(a.WorkDir, "controlplane.yaml")
	if err := os.WriteFile(template, controlPlaneTemplate, 0o644); err != nil {
		return nil, fmt.Errorf("write the template: %w", err)
	}
	// A change set from an earlier review must go first, or the name is taken.
	_, _ = a.cli(ctx, "cloudformation", "delete-change-set", "--stack-name", a.StackName(),
		"--change-set-name", changeSetName)
	_, err := a.cli(ctx, "cloudformation", "create-change-set",
		"--stack-name", a.StackName(), "--change-set-name", changeSetName,
		"--change-set-type", "CREATE", "--template-body", "file://"+template,
		"--capabilities", "CAPABILITY_IAM",
		"--tags", "Key=clanker_env,Value="+a.Env, "Key=clanker_layer,Value=control-plane",
		"--parameters",
		"ParameterKey=EnvName,ParameterValue="+a.Env,
		"ParameterKey=BinaryUrl,ParameterValue="+binary.URL,
		"ParameterKey=BinarySha256,ParameterValue="+binary.SHA256,
		"ParameterKey=KeyParameterName,ParameterValue="+a.KeyParameter(),
		"ParameterKey=HasModelKey,ParameterValue="+strconv.FormatBool(hasKey),
		"ParameterKey=Cpu,ParameterValue="+strconv.Itoa(size.CPU),
		"ParameterKey=Memory,ParameterValue="+strconv.Itoa(size.MemoryMB))
	if err != nil {
		return nil, err
	}
	if _, err := a.cli(ctx, "cloudformation", "wait", "change-set-create-complete",
		"--stack-name", a.StackName(), "--change-set-name", changeSetName); err != nil {
		return nil, err
	}
	out, err := a.cli(ctx, "cloudformation", "describe-change-set",
		"--stack-name", a.StackName(), "--change-set-name", changeSetName)
	if err != nil {
		return nil, err
	}
	var described struct {
		Changes []struct {
			ResourceChange struct {
				Action            string
				LogicalResourceID string `json:"LogicalResourceId"`
				ResourceType      string
			}
		}
	}
	if err := json.Unmarshal(out, &described); err != nil {
		return nil, fmt.Errorf("decode the change set: %w", err)
	}
	changes := make([]Change, 0, len(described.Changes))
	for _, c := range described.Changes {
		changes = append(changes, Change{Action: c.ResourceChange.Action,
			ID: c.ResourceChange.LogicalResourceID, Type: c.ResourceChange.ResourceType})
	}
	return changes, nil
}

// Deploy executes the reviewed change set and waits until the stack exists. CloudFormation
// reports the service as created only when its container runs and passes its health check.
func (a AWS) Deploy(ctx context.Context, progress func(string)) error {
	if _, err := a.cli(ctx, "cloudformation", "execute-change-set",
		"--stack-name", a.StackName(), "--change-set-name", changeSetName); err != nil {
		return err
	}
	progress("CloudFormation creates the network, the file system, the roles and the service.")
	done := make(chan error, 1)
	go func() {
		_, err := a.cli(ctx, "cloudformation", "wait", "stack-create-complete",
			"--stack-name", a.StackName())
		done <- err
	}()
	seen := map[string]bool{}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			a.reportEvents(ctx, seen, progress)
			if err != nil {
				return fmt.Errorf("the stack %s was not created: %w", a.StackName(), err)
			}
			return nil
		case <-ticker.C:
			a.reportEvents(ctx, seen, progress)
		}
	}
}

func (a AWS) reportEvents(ctx context.Context, seen map[string]bool, progress func(string)) {
	out, err := a.cli(ctx, "cloudformation", "describe-stack-events", "--stack-name",
		a.StackName(), "--max-items", "60")
	if err != nil {
		return
	}
	var described struct {
		StackEvents []struct {
			EventID              string `json:"EventId"`
			LogicalResourceID    string `json:"LogicalResourceId"`
			ResourceStatus       string
			ResourceStatusReason string
		}
	}
	if json.Unmarshal(out, &described) != nil {
		return
	}
	for i := len(described.StackEvents) - 1; i >= 0; i-- {
		event := described.StackEvents[i]
		if seen[event.EventID] {
			continue
		}
		seen[event.EventID] = true
		line := event.LogicalResourceID + ": " + event.ResourceStatus
		if strings.HasSuffix(event.ResourceStatus, "FAILED") && event.ResourceStatusReason != "" {
			line += " (" + event.ResourceStatusReason + ")"
		}
		progress(line)
	}
}

// Size is the processor and memory of the container.
type Size struct {
	Name     string
	CPU      int
	MemoryMB int
}

// Sizes that the wizard offers. The first is the default.
var Sizes = []Size{{"small", 512, 1024}, {"medium", 1024, 2048}}

// SizeOf returns the size with that name, or the default.
func SizeOf(name string) Size {
	for _, size := range Sizes {
		if size.Name == name {
			return size
		}
	}
	return Sizes[0]
}

// Home names the cluster and the service of the control plane.
type Home struct {
	Cluster string
	Service string
}

// Home reads the cluster and the service from the stack. It returns an empty Home with no
// error when the stack does not exist.
func (a AWS) Home(ctx context.Context) (Home, error) {
	out, err := a.cli(ctx, "cloudformation", "describe-stacks", "--stack-name", a.StackName())
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return Home{}, nil
		}
		return Home{}, err
	}
	var described struct {
		Stacks []struct {
			Outputs []struct{ OutputKey, OutputValue string }
		}
	}
	if err := json.Unmarshal(out, &described); err != nil {
		return Home{}, fmt.Errorf("decode the stack: %w", err)
	}
	var home Home
	for _, stack := range described.Stacks {
		for _, o := range stack.Outputs {
			switch o.OutputKey {
			case "Cluster":
				home.Cluster = o.OutputValue
			case "Service":
				home.Service = o.OutputValue
			}
		}
	}
	return home, nil
}

// Task is the running container of the control plane.
type Task struct {
	Cluster   string
	ID        string
	RuntimeID string
}

// Target is the name of the container for a Session Manager session.
func (t Task) Target() string { return "ecs:" + t.Cluster + "_" + t.ID + "_" + t.RuntimeID }

// ErrNoTask means that no container of the control plane accepts a tunnel now.
var ErrNoTask = errors.New("no container of the control plane accepts a tunnel yet")

// RunningTask finds the container that runs now and accepts ECS Exec. A new container
// replaces a stopped one, so the wizard looks it up each time it opens a tunnel.
func (a AWS) RunningTask(ctx context.Context, home Home) (Task, error) {
	out, err := a.cli(ctx, "ecs", "list-tasks", "--cluster", home.Cluster, "--service-name",
		home.Service, "--desired-status", "RUNNING")
	if err != nil {
		return Task{}, err
	}
	var listed struct {
		TaskArns []string `json:"taskArns"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		return Task{}, fmt.Errorf("decode the task list: %w", err)
	}
	if len(listed.TaskArns) == 0 {
		return Task{}, ErrNoTask
	}
	out, err = a.cli(ctx, "ecs", "describe-tasks", "--cluster", home.Cluster, "--tasks",
		listed.TaskArns[0])
	if err != nil {
		return Task{}, err
	}
	var described struct {
		Tasks []struct {
			TaskArn    string `json:"taskArn"`
			LastStatus string `json:"lastStatus"`
			Containers []struct {
				RuntimeID     string `json:"runtimeId"`
				ManagedAgents []struct {
					Name       string `json:"name"`
					LastStatus string `json:"lastStatus"`
				} `json:"managedAgents"`
			} `json:"containers"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(out, &described); err != nil {
		return Task{}, fmt.Errorf("decode the task: %w", err)
	}
	for _, task := range described.Tasks {
		if task.LastStatus != "RUNNING" {
			continue
		}
		for _, container := range task.Containers {
			for _, agent := range container.ManagedAgents {
				if agent.Name == "ExecuteCommandAgent" && agent.LastStatus == "RUNNING" &&
					container.RuntimeID != "" {
					id := task.TaskArn[strings.LastIndex(task.TaskArn, "/")+1:]
					return Task{Cluster: home.Cluster, ID: id, RuntimeID: container.RuntimeID}, nil
				}
			}
		}
	}
	return Task{}, ErrNoTask
}

// WaitTask waits until a container runs and accepts ECS Exec.
func (a AWS) WaitTask(ctx context.Context, home Home, limit time.Duration) (Task, error) {
	deadline := time.Now().Add(limit)
	for {
		task, err := a.RunningTask(ctx, home)
		if err == nil {
			return task, nil
		}
		if !errors.Is(err, ErrNoTask) {
			return Task{}, err
		}
		if time.Now().After(deadline) {
			return Task{}, fmt.Errorf("%w after %s: look at the log group /clanker/%s/"+
				"control-plane in CloudWatch", ErrNoTask, limit, a.Env)
		}
		select {
		case <-ctx.Done():
			return Task{}, ctx.Err()
		case <-time.After(6 * time.Second):
		}
	}
}

// Tunnel forwards a local port to the dashboard port inside the container, through ECS
// Exec. The tunnel lives until the context ends.
func (a AWS) Tunnel(ctx context.Context, task Task, localPort int) *exec.Cmd {
	parameters := fmt.Sprintf(`{"portNumber":["7070"],"localPortNumber":["%d"]}`, localPort)
	return exec.CommandContext(ctx, "aws", "ssm", "start-session", "--target", task.Target(),
		"--document-name", "AWS-StartPortForwardingSession", "--parameters", parameters,
		"--profile", a.Profile, "--region", a.Region)
}

// Destroy deletes the stack and the stored key. Nothing of the control plane stays.
func (a AWS) Destroy(ctx context.Context, progress func(string)) error {
	progress("Deleting the stack " + a.StackName() + ".")
	if _, err := a.cli(ctx, "cloudformation", "delete-stack", "--stack-name",
		a.StackName()); err != nil {
		return err
	}
	if _, err := a.cli(ctx, "cloudformation", "wait", "stack-delete-complete", "--stack-name",
		a.StackName()); err != nil {
		return fmt.Errorf("the stack %s was not deleted: %w", a.StackName(), err)
	}
	progress("Deleting the stored model key.")
	if _, err := a.cli(ctx, "ssm", "delete-parameter", "--name", a.KeyParameter()); err != nil &&
		!strings.Contains(err.Error(), "ParameterNotFound") {
		return err
	}
	progress("Nothing of the control plane is left in the account.")
	return nil
}

// ReleaseBinary finds the Linux arm64 binary of a release and its checksum.
func ReleaseBinary(ctx context.Context, client *http.Client, base, version string) (Binary, error) {
	if version == "" || version == "dev" {
		return Binary{}, errors.New("this is a development build with no release: the " +
			"container has nothing to download. Use a released binary")
	}
	const name = "clanker-linux-arm64"
	root := strings.TrimSuffix(base, "/") + "/" + version + "/"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, root+"checksums.txt", nil)
	if err != nil {
		return Binary{}, fmt.Errorf("build the checksum request: %w", err)
	}
	request.Header.Set("User-Agent", "clanker-setup")
	response, err := client.Do(request)
	if err != nil {
		return Binary{}, fmt.Errorf("get the checksums of release %s: %w", version, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Binary{}, fmt.Errorf("get the checksums of release %s: HTTP %d", version,
			response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if err != nil {
		return Binary{}, fmt.Errorf("read the checksums of release %s: %w", version, err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name && len(fields[0]) == 64 {
			return Binary{URL: root + name, SHA256: fields[0]}, nil
		}
	}
	return Binary{}, fmt.Errorf("release %s has no checksum for %s", version, name)
}
