package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
)

// taskArnPrefix starts the ARN of each task. The account is the example account of the AWS
// documentation.
const taskArnPrefix = "arn:aws:ecs:us-east-1:111122223333:task/"

// task is one runner task: a container of the runner image.
type task struct {
	arn       string
	container string
	name      string
	status    string
	exitCode  *int
	stopCode  string
	reason    string
}

// ecsTasks serves the calls of ECS. Each task is a container that `docker run` starts.
type ecsTasks struct {
	opts   options
	record *recorder
	tokens *os.File

	mu    sync.Mutex
	next  int
	tasks map[string]*task
}

type keyValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type runTaskInput struct {
	Cluster              string `json:"cluster"`
	TaskDefinition       string `json:"taskDefinition"`
	LaunchType           string `json:"launchType"`
	Count                int    `json:"count"`
	StartedBy            string `json:"startedBy"`
	NetworkConfiguration struct {
		AwsvpcConfiguration struct {
			Subnets        []string `json:"subnets"`
			SecurityGroups []string `json:"securityGroups"`
			AssignPublicIP string   `json:"assignPublicIp"`
		} `json:"awsvpcConfiguration"`
	} `json:"networkConfiguration"`
	Overrides struct {
		ContainerOverrides []struct {
			Name        string     `json:"name"`
			Environment []keyValue `json:"environment"`
		} `json:"containerOverrides"`
	} `json:"overrides"`
	Tags []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"tags"`
}

func (e *ecsTasks) serve(w http.ResponseWriter, r *http.Request, action string) {
	var err error
	switch action {
	case "RunTask":
		err = e.runTask(w, r)
	case "DescribeTasks":
		err = e.describeTasks(w, r)
	case "StopTask":
		err = e.stopTask(w, r)
	default:
		err = fmt.Errorf("the stand-in does not serve ECS %s", action)
	}
	if err != nil {
		e.record.add("ecs", action, r, map[string]any{"error": err.Error()})
		awsError(w, "InvalidParameterException", err)
	}
}

// runTask starts the runner container of one task. The values of the environment go to
// docker through its own environment, so no token is on a command line.
func (e *ecsTasks) runTask(w http.ResponseWriter, r *http.Request) error {
	var in runTaskInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return err
	}
	if len(in.Overrides.ContainerOverrides) != 1 {
		return errors.New("want one container override, for the runner")
	}
	override := in.Overrides.ContainerOverrides[0]
	e.mu.Lock()
	e.next++
	t := &task{arn: fmt.Sprintf("%s%s/%032d", taskArnPrefix, in.Cluster, e.next),
		container: override.Name, name: fmt.Sprintf("fakeaws-task-%d-%d", os.Getpid(), e.next),
		status: "PROVISIONING"}
	e.tasks[t.arn] = t
	e.mu.Unlock()
	names, coreURL := []string{}, ""
	args := []string{"run", "--rm", "--name", t.name, "--platform", "linux/arm64",
		"--network", e.opts.network}
	environ := os.Environ()
	for _, pair := range override.Environment {
		names = append(names, pair.Name)
		args = append(args, "--env", pair.Name)
		environ = append(environ, pair.Name+"="+pair.Value)
		switch pair.Name {
		case "CLANKER_CORE_URL":
			coreURL = pair.Value
		case "CLANKER_JOB_TOKEN":
			if _, err := fmt.Fprintln(e.tokens, pair.Value); err != nil {
				return fmt.Errorf("write the token file: %w", err)
			}
		}
	}
	tags := map[string]string{}
	for _, tag := range in.Tags {
		tags[tag.Key] = tag.Value
	}
	network := in.NetworkConfiguration.AwsvpcConfiguration
	e.record.add("ecs", "RunTask", r, map[string]any{"task": t.arn, "cluster": in.Cluster,
		"task_definition": in.TaskDefinition, "launch_type": in.LaunchType, "count": in.Count,
		"started_by": in.StartedBy, "subnets": network.Subnets,
		"security_groups": network.SecurityGroups, "assign_public_ip": network.AssignPublicIP,
		"container": override.Name, "environment_names": names, "core_url": coreURL,
		"tags": tags})
	go e.runContainer(t, append(args, e.opts.image), environ)
	writeJSON(w, map[string]any{"tasks": []any{e.describe(t)}, "failures": []any{}})
	return nil
}

// runContainer runs the container of a task and records how it ended.
func (e *ecsTasks) runContainer(t *task, args, environ []string) {
	e.setStatus(t, "RUNNING")
	output, err := os.Create(filepath.Join(e.opts.logs, filepath.Base(t.name)+".log"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeaws: task log:", err)
	}
	cmd := exec.Command("docker", args...)
	cmd.Env = environ
	if output != nil {
		cmd.Stdout, cmd.Stderr = output, output
		defer output.Close()
	}
	runErr := cmd.Run()
	code := 0
	var exit *exec.ExitError
	switch {
	case errors.As(runErr, &exit):
		code = exit.ExitCode()
	case runErr != nil:
		code = 255
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if t.status == "STOPPED" {
		return
	}
	t.status, t.exitCode = "STOPPED", &code
	t.stopCode, t.reason = "EssentialContainerExited", "Essential container in task exited"
}

func (e *ecsTasks) setStatus(t *task, status string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t.status != "STOPPED" {
		t.status = status
	}
}

// describe returns a task as ECS describes it. The caller holds no lock.
func (e *ecsTasks) describe(t *task) map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	container := map[string]any{"name": t.container, "lastStatus": t.status}
	if t.exitCode != nil {
		container["exitCode"] = *t.exitCode
	}
	described := map[string]any{"taskArn": t.arn, "lastStatus": t.status,
		"launchType": "FARGATE", "containers": []any{container}}
	if t.stopCode != "" {
		described["stopCode"], described["stoppedReason"] = t.stopCode, t.reason
	}
	return described
}

func (e *ecsTasks) find(arn string) *task {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tasks[arn]
}

func (e *ecsTasks) describeTasks(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Cluster string   `json:"cluster"`
		Tasks   []string `json:"tasks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return err
	}
	found, failures := []any{}, []any{}
	for _, arn := range in.Tasks {
		if t := e.find(arn); t != nil {
			found = append(found, e.describe(t))
		} else {
			failures = append(failures, map[string]any{"arn": arn, "reason": "MISSING"})
		}
	}
	e.record.add("ecs", "DescribeTasks", r, map[string]any{"cluster": in.Cluster,
		"tasks": in.Tasks})
	writeJSON(w, map[string]any{"tasks": found, "failures": failures})
	return nil
}

func (e *ecsTasks) stopTask(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Cluster string `json:"cluster"`
		Task    string `json:"task"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return err
	}
	t := e.find(in.Task)
	if t == nil {
		return fmt.Errorf("the task %s does not exist", in.Task)
	}
	e.record.add("ecs", "StopTask", r, map[string]any{"cluster": in.Cluster,
		"task": in.Task, "reason": in.Reason})
	e.remove(t, "UserInitiated", in.Reason)
	writeJSON(w, map[string]any{"task": e.describe(t)})
	return nil
}

// remove removes the container of a task and marks the task stopped.
func (e *ecsTasks) remove(t *task, stopCode, reason string) {
	if out, err := exec.Command("docker", "rm", "-f", t.name).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "fakeaws: remove %s: %v: %s\n", t.name, err, out)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if t.status != "STOPPED" {
		t.status, t.stopCode, t.reason = "STOPPED", stopCode, reason
	}
}

// stopAll removes the containers of the tasks that still run.
func (e *ecsTasks) stopAll() {
	e.mu.Lock()
	running := []*task{}
	for _, arn := range slices.Sorted(maps.Keys(e.tasks)) {
		if t := e.tasks[arn]; t.status != "STOPPED" {
			running = append(running, t)
		}
	}
	e.mu.Unlock()
	for _, t := range running {
		e.remove(t, "TaskFailedToStart", "the stand-in for AWS stopped")
	}
}
