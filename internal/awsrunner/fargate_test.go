package awsrunner_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/statik/site-reliability-clanker/internal/awsrunner"
	"github.com/statik/site-reliability-clanker/internal/dispatch"
)

const (
	token   = "job-token-5f0c9a7e1b2d4c6a"
	taskARN = "arn:aws:ecs:eu-west-1:111122223333:task/runners/0123456789abcdef"
)

// fakeECS answers RunTask with runOut or runErr, and DescribeTasks with the replies in
// order; the last reply repeats. It records each request.
type fakeECS struct {
	runOut     *ecs.RunTaskOutput
	runErr     error
	replies    []reply
	stopErr    error
	onDescribe func()

	mu         sync.Mutex
	runIn      []*ecs.RunTaskInput
	describeIn []*ecs.DescribeTasksInput
	stopIn     []*ecs.StopTaskInput
}

type reply struct {
	out *ecs.DescribeTasksOutput
	err error
}

func (f *fakeECS) RunTask(_ context.Context, in *ecs.RunTaskInput, _ ...func(*ecs.Options)) (
	*ecs.RunTaskOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runIn = append(f.runIn, in)
	if f.runErr != nil {
		return nil, f.runErr
	}
	if f.runOut != nil {
		return f.runOut, nil
	}
	return &ecs.RunTaskOutput{Tasks: []types.Task{{TaskArn: aws.String(taskARN)}}}, nil
}

func (f *fakeECS) DescribeTasks(_ context.Context, in *ecs.DescribeTasksInput,
	_ ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error) {
	f.mu.Lock()
	f.describeIn = append(f.describeIn, in)
	next := f.replies[min(len(f.describeIn), len(f.replies))-1]
	f.mu.Unlock()
	if f.onDescribe != nil {
		f.onDescribe()
	}
	return next.out, next.err
}

func (f *fakeECS) StopTask(ctx context.Context, in *ecs.StopTaskInput, _ ...func(*ecs.Options)) (
	*ecs.StopTaskOutput, error) {
	if ctx.Err() != nil {
		return nil, errors.New("the stop request had a context that was over")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopIn = append(f.stopIn, in)
	return &ecs.StopTaskOutput{}, f.stopErr
}

func status(last string) reply {
	return reply{out: &ecs.DescribeTasksOutput{Tasks: []types.Task{{TaskArn: aws.String(taskARN),
		LastStatus: aws.String(last)}}}}
}

func stoppedTask(exitCode *int32, reason string, code types.TaskStopCode) reply {
	return reply{out: &ecs.DescribeTasksOutput{Tasks: []types.Task{{
		TaskArn: aws.String(taskARN), LastStatus: aws.String("STOPPED"),
		StoppedReason: aws.String(reason), StopCode: code,
		Containers: []types.Container{
			{Name: aws.String("log-router"), ExitCode: aws.Int32(0)},
			{Name: aws.String("runner"), ExitCode: exitCode},
		}}}}}
}

func exampleConfig() awsrunner.Config {
	return awsrunner.Config{Cluster: "runners", TaskDefinition: "clanker-runner:3",
		Container: "runner", Subnets: []string{"subnet-0123456789abcdef0"},
		SecurityGroups: []string{"sg-0123456789abcdef0"}}
}

func launcher(fake *fakeECS) awsrunner.Fargate {
	return awsrunner.Fargate{ECS: fake, Config: exampleConfig(), Env: "test",
		CoreURL: "http://10.0.1.5:7070", Poll: time.Millisecond}
}

// logs sends the log lines of the test to a buffer.
func logs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buffer, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buffer
}

func TestLaunchRunsOneFargateTaskInThePrivateSubnets(t *testing.T) {
	fake := &fakeECS{replies: []reply{stoppedTask(aws.Int32(0), "Essential container in task "+
		"exited", types.TaskStopCodeEssentialContainerExited)}}
	if err := launcher(fake).Launch(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	want := &ecs.RunTaskInput{
		Cluster: aws.String("runners"), TaskDefinition: aws.String("clanker-runner:3"),
		LaunchType: types.LaunchTypeFargate, Count: aws.Int32(1),
		StartedBy: aws.String("clanker"),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        []string{"subnet-0123456789abcdef0"},
				SecurityGroups: []string{"sg-0123456789abcdef0"},
				AssignPublicIp: types.AssignPublicIpDisabled}},
		Overrides: &types.TaskOverride{ContainerOverrides: []types.ContainerOverride{{
			Name: aws.String("runner"),
			Environment: []types.KeyValuePair{
				{Name: aws.String(dispatch.CoreURLEnv), Value: aws.String("http://10.0.1.5:7070")},
				{Name: aws.String(dispatch.TokenEnv), Value: aws.String(token)}}}}},
		Tags: []types.Tag{{Key: aws.String("clanker_env"), Value: aws.String("test")}},
	}
	if len(fake.runIn) != 1 || !reflect.DeepEqual(fake.runIn[0], want) {
		t.Fatalf("RunTask requests = %#v, want one %#v", fake.runIn, want)
	}
}

func TestLaunchWaitsUntilTheTaskStoppedAndReturnsNilOnExitCodeZero(t *testing.T) {
	fake := &fakeECS{replies: []reply{status("PROVISIONING"), status("RUNNING"),
		stoppedTask(aws.Int32(0), "Essential container in task exited",
			types.TaskStopCodeEssentialContainerExited)}}
	if err := launcher(fake).Launch(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if len(fake.describeIn) != 3 || len(fake.stopIn) != 0 {
		t.Fatalf("%d DescribeTasks and %d StopTask requests, want 3 and 0",
			len(fake.describeIn), len(fake.stopIn))
	}
	in := fake.describeIn[0]
	if aws.ToString(in.Cluster) != "runners" || !reflect.DeepEqual(in.Tasks, []string{taskARN}) {
		t.Fatalf("DescribeTasks request = cluster %q, tasks %v", aws.ToString(in.Cluster),
			in.Tasks)
	}
}

func TestANonZeroExitCodeIsAnErrorWithTheReason(t *testing.T) {
	fake := &fakeECS{replies: []reply{stoppedTask(aws.Int32(3), "Essential container in task "+
		"exited", types.TaskStopCodeEssentialContainerExited)}}
	err := launcher(fake).Launch(context.Background(), token)
	for _, want := range []string{taskARN, "container runner ended with exit code 3",
		"Essential container in task exited", "EssentialContainerExited"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q in it", err, want)
		}
	}
}

func TestATaskThatStoppedWithoutAnExitCodeIsAnError(t *testing.T) {
	fake := &fakeECS{replies: []reply{stoppedTask(nil, "CannotPullContainerError: pull "+
		"access denied", types.TaskStopCodeTaskFailedToStart)}}
	err := launcher(fake).Launch(context.Background(), token)
	for _, want := range []string{"stopped before its container runner ended",
		"CannotPullContainerError", "TaskFailedToStart"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q in it", err, want)
		}
	}
}

func TestATaskWithoutTheRunnerContainerIsAnError(t *testing.T) {
	config := exampleConfig()
	config.Container = "clanker"
	fake := &fakeECS{replies: []reply{stoppedTask(aws.Int32(0), "done", "")}}
	f := launcher(fake)
	f.Config = config
	err := f.Launch(context.Background(), token)
	if err == nil || !strings.Contains(err.Error(), "has no container clanker") {
		t.Fatalf("error = %v, want the missing container", err)
	}
}

func TestATaskThatECSDidNotStartIsAnError(t *testing.T) {
	fake := &fakeECS{runOut: &ecs.RunTaskOutput{Failures: []types.Failure{{
		Reason: aws.String("RESOURCE:ENI"), Detail: aws.String("no free address")}}}}
	err := launcher(fake).Launch(context.Background(), token)
	if err == nil || !strings.Contains(err.Error(), "RESOURCE:ENI (no free address)") {
		t.Fatalf("error = %v, want the failure of RunTask", err)
	}
	if len(fake.describeIn) != 0 {
		t.Fatal("the launcher described a task that did not start")
	}
}

func TestAShortGapInDescribeTasksIsTolerated(t *testing.T) {
	missing := reply{out: &ecs.DescribeTasksOutput{Failures: []types.Failure{{
		Arn: aws.String(taskARN), Reason: aws.String("MISSING")}}}}
	fake := &fakeECS{replies: []reply{missing, {err: errors.New("throttled")}, status("RUNNING"),
		missing, stoppedTask(aws.Int32(0), "done", "")}}
	if err := launcher(fake).Launch(context.Background(), token); err != nil {
		t.Fatal(err)
	}
}

func TestATaskThatECSCannotDescribeIsStopped(t *testing.T) {
	fake := &fakeECS{replies: []reply{{err: errors.New("AccessDeniedException")}}}
	err := launcher(fake).Launch(context.Background(), token)
	if err == nil || !strings.Contains(err.Error(), "AccessDeniedException") {
		t.Fatalf("error = %v, want the error of DescribeTasks", err)
	}
	if len(fake.describeIn) != 3 || len(fake.stopIn) != 1 {
		t.Fatalf("%d DescribeTasks and %d StopTask requests, want 3 and 1",
			len(fake.describeIn), len(fake.stopIn))
	}
}

func TestACancelledJobStopsItsTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &fakeECS{replies: []reply{status("RUNNING")}, onDescribe: cancel}
	err := launcher(fake).Launch(ctx, token)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(fake.stopIn) != 1 {
		t.Fatalf("%d StopTask requests, want 1", len(fake.stopIn))
	}
	in := fake.stopIn[0]
	if aws.ToString(in.Cluster) != "runners" || aws.ToString(in.Task) != taskARN ||
		aws.ToString(in.Reason) == "" {
		t.Fatalf("StopTask request = cluster %q, task %q, reason %q", aws.ToString(in.Cluster),
			aws.ToString(in.Task), aws.ToString(in.Reason))
	}
}

func TestADescribeErrorAtTheCancelIsNotAFailureOfECS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	fake := &fakeECS{replies: []reply{{err: errors.New("throttled")}}, onDescribe: func() {
		if calls++; calls == 3 {
			cancel()
		}
	}}
	err := launcher(fake).Launch(ctx, token)
	if !errors.Is(err, context.Canceled) || len(fake.stopIn) != 1 {
		t.Fatalf("error = %v, %d StopTask requests, want context.Canceled and 1", err,
			len(fake.stopIn))
	}
}

func TestNoErrorAndNoLogLineHoldsTheToken(t *testing.T) {
	buffer := logs(t)
	echo := errors.New("InvalidParameterException: environment value " + token)
	tokenFailure := []types.Failure{{Reason: aws.String("INVALID"), Detail: aws.String(token)}}
	cases := map[string]*fakeECS{
		"RunTask error":   {runErr: echo},
		"RunTask failure": {runOut: &ecs.RunTaskOutput{Failures: tokenFailure}},
		"stopped reason": {replies: []reply{stoppedTask(aws.Int32(1), "exited with "+token,
			"")}},
		"no exit code": {replies: []reply{stoppedTask(nil, token, "")}},
		"DescribeTasks error and StopTask error": {replies: []reply{{err: echo}},
			stopErr: echo},
		"DescribeTasks failure": {replies: []reply{{out: &ecs.DescribeTasksOutput{
			Failures: tokenFailure}}}},
	}
	for name, fake := range cases {
		err := launcher(fake).Launch(context.Background(), token)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		if strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[job token]") {
			t.Errorf("%s: error = %v, want the token hidden", name, err)
		}
	}
	if strings.Contains(buffer.String(), token) {
		t.Fatalf("a log line holds the token:\n%s", buffer)
	}
	if !strings.Contains(buffer.String(), "[job token]") {
		t.Fatalf("the test logged no error of DescribeTasks:\n%s", buffer)
	}
}
