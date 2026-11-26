// Command runnerharness runs the jobs of scripts/verify/runner-image-check.sh. It serves the
// runner routes of the dispatcher, as the core does, and starts each runner in a new
// container of the runner image, as a launcher on a cloud does. Thus a plan and its apply
// share no file. They share only the state in the bucket and what the dispatcher holds.
//
// It runs a plan, an apply with a changed plan file, which the runner must refuse, and an
// apply of the plan. Then it stops a runner container in the middle of an apply, as ECS
// stops a task. It prints one line for each check and fails when a check fails.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/internal/dispatch"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
	"github.com/statik/site-reliability-clanker/internal/runner"
)

//go:embed fixture/main.tf
var fixture []byte

// stopFixture is the stack of the stop case. Its create runs until the runner is stopped.
//
//go:embed fixture/stop/main.tf
var stopFixture []byte

// The place of the fixtures in the checkout, as the runner finds a stack.
const (
	env       = "check"
	cloud     = "fixture"
	layer     = "10-state"
	stopLayer = "20-stop"
)

type options struct {
	image     string
	network   string
	digest    string
	message   string
	bucket    string
	accessKey string
	secretKey string
}

func parseOptions() (options, error) {
	var o options
	flag.StringVar(&o.image, "image", "", "the runner image")
	flag.StringVar(&o.network, "network", "", "the Docker network with the MinIO container")
	flag.StringVar(&o.digest, "digest", "", "the tool digest that each job must report")
	flag.StringVar(&o.message, "message", "", "the value of the variable message")
	flag.StringVar(&o.bucket, "bucket", "", "the bucket of the state")
	flag.StringVar(&o.accessKey, "s3-access-key", "", "the access key of MinIO")
	flag.StringVar(&o.secretKey, "s3-secret-key", "", "the secret key of MinIO")
	flag.Parse()
	for name, value := range map[string]string{"image": o.image, "network": o.network,
		"digest": o.digest, "message": o.message, "bucket": o.bucket,
		"s3-access-key": o.accessKey, "s3-secret-key": o.secretKey} {
		if value == "" {
			return o, fmt.Errorf("the flag --%s is required", name)
		}
	}
	return o, nil
}

// checkoutArchive returns the checkout of the jobs: the two fixtures as the only stacks.
func checkoutArchive() ([]byte, error) {
	var archive bytes.Buffer
	zipped := gzip.NewWriter(&archive)
	writer := tar.NewWriter(zipped)
	for stack, content := range map[string][]byte{layer: fixture, stopLayer: stopFixture} {
		header := &tar.Header{Name: "platform/stacks/" + cloud + "/" + stack + "/main.tf",
			Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := writer.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("write the header of the fixture %s: %w", stack, err)
		}
		if _, err := writer.Write(content); err != nil {
			return nil, fmt.Errorf("write the fixture %s: %w", stack, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close the tar archive: %w", err)
	}
	if err := zipped.Close(); err != nil {
		return nil, fmt.Errorf("close the gzip stream: %w", err)
	}
	return archive.Bytes(), nil
}

// containerLauncher starts each runner with `docker run` in a new container that is
// removed when it ends. The token goes to the container through the environment of the
// docker command, so it is not on a command line.
type containerLauncher struct {
	image   string
	network string
	coreURL string
	// stops gets the end of each container that was stopped.
	stops chan stoppedContainer

	mu   sync.Mutex
	next int
}

// stoppedContainer is how a stopped runner container ended: when, and with which exit code.
type stoppedContainer struct {
	ended    time.Time
	exitCode int
	output   string
}

func (l *containerLauncher) Launch(ctx context.Context, token string) error {
	l.mu.Lock()
	l.next++
	name := fmt.Sprintf("clanker-runner-check-%d-%d", os.Getpid(), l.next)
	l.mu.Unlock()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--name", name, "--platform",
		"linux/arm64", "--network", l.network, "--env", dispatch.CoreURLEnv+"="+l.coreURL,
		"--env", dispatch.TokenEnv, l.image)
	// A stop of the job stops the container as ECS stops a task: SIGTERM, and SIGKILL after
	// the stop timeout of the runner task definition.
	cmd.Cancel = func() error {
		return exec.Command("docker", "stop", "--time",
			strconv.Itoa(int(runner.TaskStopTime.Seconds())), name).Run()
	}
	cmd.WaitDelay = 10 * time.Second
	cmd.Env = append(os.Environ(), dispatch.TokenEnv+"="+token)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if ctx.Err() != nil {
		l.stops <- stoppedContainer{ended: time.Now(), exitCode: cmd.ProcessState.ExitCode(),
			output: strings.TrimSpace(output.String())}
	}
	if err != nil {
		return fmt.Errorf("the runner container ended with an error: %w: %s", err,
			strings.TrimSpace(output.String()))
	}
	return nil
}

// jobLog keeps what a job prints. The dispatcher writes to it from its HTTP handlers.
type jobLog struct {
	mu   sync.Mutex
	text bytes.Buffer
}

func (l *jobLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.Write(p)
}

func (l *jobLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.String()
}

type harness struct {
	opts       options
	launcher   *containerLauncher
	dispatcher *dispatch.Dispatcher
	archive    []byte
	failures   int
}

func (h *harness) check(ok bool, format string, args ...any) {
	if ok {
		fmt.Printf("ok: "+format+"\n", args...)
		return
	}
	h.failures++
	fmt.Printf("FAILED: "+format+"\n", args...)
}

// spec returns a job on the fixture with the S3 backend and the MinIO keys as its AWS
// credential, as the core sends a job with an S3 backend and a broker credential.
func (h *harness) spec(kind, stack string) jobspec.Spec {
	return jobspec.Spec{Kind: kind, Env: env, Cloud: cloud, Layer: stack,
		CheckoutID: "runner-image-check",
		AWS: &jobspec.AWSSession{AccessKeyID: h.opts.accessKey,
			SecretAccessKey: h.opts.secretKey, Expires: time.Now().Add(time.Hour)},
		Backend: map[string]string{"bucket": h.opts.bucket, "key": env + "/" + stack + ".tfstate",
			"region": "us-east-1", "use_lockfile": "true"},
		Variables: map[string]string{"TF_VAR_message": h.opts.message}}
}

// run gives a job to a new runner container. It prints the log of a job that failed.
func (h *harness) run(ctx context.Context, spec jobspec.Spec, plan []byte) (jobspec.Result,
	error) {
	log := &jobLog{}
	job := jobspec.Job{Spec: spec, Checkout: h.archive, Plan: plan, Log: log}
	started := time.Now()
	result, err := h.dispatcher.Run(ctx, job)
	fmt.Printf("the %s job ended in %s with the status %q\n", spec.Kind,
		time.Since(started).Round(100*time.Millisecond), result.Status)
	if text := log.String(); err != nil && text != "" {
		fmt.Printf("log of the %s job:\n%s\n", spec.Kind, text)
	}
	return result, err
}

// plannedActions returns the actions of the resource at address in the JSON of a plan.
func plannedActions(planJSON []byte, address string) ([]string, error) {
	var plan struct {
		ResourceChanges []struct {
			Address string `json:"address"`
			Change  struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		return nil, fmt.Errorf("decode the JSON of the plan: %w", err)
	}
	for _, change := range plan.ResourceChanges {
		if change.Address == address {
			return change.Change.Actions, nil
		}
	}
	return nil, fmt.Errorf("the plan has no change of %s", address)
}

func (h *harness) planJob(ctx context.Context) (jobspec.Result, error) {
	result, err := h.run(ctx, h.spec(jobspec.KindPlan, layer), nil)
	if err != nil {
		return result, fmt.Errorf("the plan job failed: %w", err)
	}
	if len(result.Plan) == 0 {
		return result, errors.New("the plan job uploaded no plan file")
	}
	h.check(result.PlanSHA256 == jobspec.SHA256(result.Plan),
		"the plan hash %s is the SHA-256 of the %d bytes of the uploaded plan",
		result.PlanSHA256, len(result.Plan))
	actions, err := plannedActions(result.PlanJSON, "terraform_data.marker")
	h.check(err == nil && reflect.DeepEqual(actions, []string{"create"}),
		"the plan creates terraform_data.marker: actions %v, error %v", actions, err)
	h.check(result.ToolDigest == h.opts.digest, "the plan job reports the tool digest %s",
		result.ToolDigest)
	return result, nil
}

// changedApply sends the plan with one byte changed and the approved hash. The runner must
// refuse it before OpenTofu runs.
func (h *harness) changedApply(ctx context.Context, plan jobspec.Result) {
	changed := bytes.Clone(plan.Plan)
	changed[len(changed)-1] ^= 0xff
	spec := h.spec(jobspec.KindApply, layer)
	spec.PlanSHA256 = plan.PlanSHA256
	result, err := h.run(ctx, spec, changed)
	h.check(err != nil && result.Status == jobspec.StatusFailed &&
		strings.Contains(result.Error, "is not the approved hash"),
		"the runner refuses a plan file that is not the approved one: status %q, error %v",
		result.Status, err)
	h.check(result.ToolDigest == h.opts.digest,
		"the refused apply job reports the tool digest %s", result.ToolDigest)
}

func (h *harness) applyJob(ctx context.Context, plan jobspec.Result) {
	spec := h.spec(jobspec.KindApply, layer)
	spec.PlanSHA256 = plan.PlanSHA256
	result, err := h.run(ctx, spec, plan.Plan)
	h.check(err == nil && result.Status == jobspec.StatusOK,
		"the apply job applies the approved plan in a new container: error %v", err)
	h.check(result.ToolDigest == h.opts.digest, "the apply job reports the tool digest %s",
		result.ToolDigest)
	// The runner reads the outputs from the state in the bucket. The container of the
	// apply has no state file of its own.
	h.check(result.Outputs["message"] == h.opts.message,
		"the apply job reports the output message %v", result.Outputs["message"])
	h.check(result.Outputs["state_location"] == h.opts.bucket,
		"the apply job reports the output state_location %v, the bucket of the backend",
		result.Outputs["state_location"])
}

// stoppedApply stops the runner container in the middle of an apply, as ECS stops a task.
// The runner must interrupt OpenTofu, which ends the create, saves the state and releases
// the lock, and the runner must end before ECS would kill it. A plan after the stop then
// gets the lock and sees the resource of the interrupted create in the state.
func (h *harness) stoppedApply(ctx context.Context) {
	plan, err := h.run(ctx, h.spec(jobspec.KindPlan, stopLayer), nil)
	if err != nil {
		h.check(false, "the plan of the stack %s: %v", stopLayer, err)
		return
	}
	spec := h.spec(jobspec.KindApply, stopLayer)
	spec.PlanSHA256 = plan.PlanSHA256
	log := &jobLog{}
	applyCtx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() {
		_, err := h.dispatcher.Run(applyCtx, jobspec.Job{Spec: spec, Checkout: h.archive,
			Plan: plan.Plan, Log: log})
		done <- err
	}()
	if err := waitForLog(log, done, "the slow create started", 3*time.Minute); err != nil {
		h.check(false, "the apply of %s reaches the slow create: %v\n%s", stopLayer, err, log)
		return
	}
	stopped := time.Now()
	stop()
	<-done
	var end stoppedContainer
	select {
	case end = <-h.launcher.stops:
	case <-time.After(runner.TaskStopTime + time.Minute):
		h.check(false, "the stopped runner container of the apply of %s ended", stopLayer)
		return
	}
	took := end.ended.Sub(stopped)
	h.check(end.exitCode != 137 && took < runner.TaskStopGrace,
		"the stopped runner container ended on its own %s after the stop, with exit code %d, "+
			"before ECS would kill it: %s", took.Round(100*time.Millisecond), end.exitCode,
		end.output)
	after, err := h.run(ctx, h.spec(jobspec.KindPlan, stopLayer), nil)
	h.check(err == nil, "a plan after the stop gets the lock of the state: error %v", err)
	if err == nil {
		actions, err := plannedActions(after.PlanJSON, "terraform_data.slow")
		h.check(err == nil && reflect.DeepEqual(actions, []string{"delete", "create"}),
			"the state has the interrupted create as tainted, so the plan replaces it: "+
				"actions %v, error %v", actions, err)
	}
}

// waitForLog waits until the log of a job has text. It fails when the job ended first.
func waitForLog(log *jobLog, done <-chan error, text string, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for !strings.Contains(log.String(), text) {
		select {
		case err := <-done:
			return fmt.Errorf("the job ended first: %v", err)
		case <-time.After(500 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the log has no %q after %s", text, limit)
		}
	}
	return nil
}

func run() error {
	opts, err := parseOptions()
	if err != nil {
		return err
	}
	archive, err := checkoutArchive()
	if err != nil {
		return err
	}
	// Docker Desktop sends host.docker.internal from a container to the loopback of the host.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for the runners: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	launcher := &containerLauncher{image: opts.image, network: opts.network,
		coreURL: fmt.Sprintf("http://host.docker.internal:%d", port),
		stops:   make(chan stoppedContainer, 4)}
	h := &harness{opts: opts, launcher: launcher, dispatcher: dispatch.New(launcher),
		archive: archive}
	mux := http.NewServeMux()
	mux.Handle("/runner/v1/", h.dispatcher.Handler())
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "runnerharness: serve:", err)
		}
	}()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	plan, err := h.planJob(ctx)
	if err != nil {
		return err
	}
	h.changedApply(ctx, plan)
	h.applyJob(ctx, plan)
	h.stoppedApply(ctx)
	if h.failures > 0 {
		return fmt.Errorf("%d checks failed", h.failures)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "runnerharness:", err)
		os.Exit(1)
	}
}
