// Package awsrunner starts the runners of jobs on AWS: each runner is one task of Amazon
// Elastic Container Service (ECS) on AWS Fargate. The package starts no program, so the
// core of the control plane can use it.
package awsrunner

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/statik/site-reliability-clanker/internal/dispatch"
)

const (
	// startedBy marks the tasks of the control plane in ECS.
	startedBy = "clanker"
	// tagEnv is the tag that names the environment of a task.
	tagEnv = "clanker_env"
	// defaultPoll is the time between two looks at a task when Fargate.Poll is zero.
	defaultPoll = 10 * time.Second
	// maxMisses is how many looks in a row can fail before the launcher gives up on a task.
	// Just after RunTask, ECS can still say that the task is missing.
	maxMisses = 3
	// stopTimeout bounds the request that stops a task after its job ended.
	stopTimeout = 30 * time.Second
	// hiddenToken stands for the job token in the text that ECS returns.
	hiddenToken = "[job token]"
	stopped     = "STOPPED"
)

// ECS is the part of Amazon ECS that the launcher uses.
type ECS interface {
	RunTask(ctx context.Context, in *ecs.RunTaskInput, options ...func(*ecs.Options)) (
		*ecs.RunTaskOutput, error)
	DescribeTasks(ctx context.Context, in *ecs.DescribeTasksInput,
		options ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error)
	StopTask(ctx context.Context, in *ecs.StopTaskInput, options ...func(*ecs.Options)) (
		*ecs.StopTaskOutput, error)
}

// NewECS returns Amazon ECS in the region, with the credentials of the role of the
// instance. It makes no call.
func NewECS(region string) ECS {
	return ecs.New(ecs.Options{Region: region,
		Credentials: aws.NewCredentialsCache(ec2rolecreds.New())})
}

// Fargate starts each runner as a task on Fargate, in private subnets and with no public IP
// address. It is the launcher of the AWS target.
type Fargate struct {
	ECS    ECS
	Config Config
	// Env is the name of the environment. Each task carries it in the tag clanker_env.
	Env string
	// CoreURL is the address of the control plane as a task reaches it.
	CoreURL string
	// Poll is the time between two looks at a running task. Zero means 10 seconds.
	Poll time.Duration
}

var _ dispatch.Launcher = Fargate{}

// Launch runs one runner task and returns when it stopped. It returns nil when the runner
// container ended with exit code 0. When ctx ends first, Launch asks ECS to stop the task.
//
// The token goes only into the environment of the task. No error and no log line holds
// it.
func (f Fargate) Launch(ctx context.Context, token string) error {
	hide := func(text string) string {
		if token == "" {
			return text
		}
		return strings.ReplaceAll(text, token, hiddenToken)
	}
	arn, err := f.run(ctx, token, hide)
	if err != nil {
		return err
	}
	slog.Info("a runner task started", "cluster", f.Config.Cluster, "task", arn)
	return f.wait(ctx, arn, hide)
}

// run starts the task and returns its ARN.
func (f Fargate) run(ctx context.Context, token string, hide func(string) string) (
	string, error) {
	out, err := f.ECS.RunTask(ctx, f.runInput(token))
	if err != nil {
		return "", fmt.Errorf("start a runner task in the ECS cluster %s: %s. The role of the "+
			"instance needs ecs:RunTask and ecs:TagResource on the task definition %s, and "+
			"iam:PassRole on its execution role", f.Config.Cluster, hide(err.Error()),
			f.Config.TaskDefinition)
	}
	if len(out.Tasks) != 1 || out.Tasks[0].TaskArn == nil {
		return "", fmt.Errorf("ECS started no runner task in the cluster %s: %s",
			f.Config.Cluster, hide(failures(out.Failures)))
	}
	return aws.ToString(out.Tasks[0].TaskArn), nil
}

func (f Fargate) runInput(token string) *ecs.RunTaskInput {
	return &ecs.RunTaskInput{
		Cluster:        aws.String(f.Config.Cluster),
		TaskDefinition: aws.String(f.Config.TaskDefinition),
		LaunchType:     types.LaunchTypeFargate,
		Count:          aws.Int32(1),
		StartedBy:      aws.String(startedBy),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        f.Config.Subnets,
				SecurityGroups: f.Config.SecurityGroups,
				AssignPublicIp: types.AssignPublicIpDisabled,
			},
		},
		Overrides: &types.TaskOverride{ContainerOverrides: []types.ContainerOverride{{
			Name: aws.String(f.Config.Container),
			Environment: []types.KeyValuePair{
				{Name: aws.String(dispatch.CoreURLEnv), Value: aws.String(f.CoreURL)},
				{Name: aws.String(dispatch.TokenEnv), Value: aws.String(token)},
			},
		}}},
		Tags: []types.Tag{{Key: aws.String(tagEnv), Value: aws.String(f.Env)}},
	}
}

// wait looks at the task until it stopped, and returns its outcome. When ctx ends, or ECS
// cannot describe the task several times in a row, it stops the task.
func (f Fargate) wait(ctx context.Context, arn string, hide func(string) string) error {
	poll := f.Poll
	if poll <= 0 {
		poll = defaultPoll
	}
	tick := time.NewTicker(poll)
	defer tick.Stop()
	misses := 0
	for {
		select {
		case <-ctx.Done():
			return f.stop(ctx, arn, hide, fmt.Errorf("the job ended before its runner task "+
				"%s stopped: %w", arn, ctx.Err()))
		case <-tick.C:
		}
		task, err := f.describe(ctx, arn, hide)
		switch {
		case err != nil && ctx.Err() != nil:
			continue
		case err != nil:
			misses++
			if misses < maxMisses {
				slog.Warn("cannot describe the runner task", "task", arn, "error", err)
				continue
			}
			return f.stop(ctx, arn, hide, err)
		case aws.ToString(task.LastStatus) == stopped:
			return f.outcome(task, arn, hide)
		}
		misses = 0
	}
}

func (f Fargate) describe(ctx context.Context, arn string, hide func(string) string) (
	types.Task, error) {
	out, err := f.ECS.DescribeTasks(ctx, &ecs.DescribeTasksInput{
		Cluster: aws.String(f.Config.Cluster), Tasks: []string{arn}})
	if err != nil {
		return types.Task{}, fmt.Errorf("describe the runner task %s: %s. The role of the "+
			"instance needs ecs:DescribeTasks", arn, hide(err.Error()))
	}
	for _, task := range out.Tasks {
		if aws.ToString(task.TaskArn) == arn {
			return task, nil
		}
	}
	return types.Task{}, fmt.Errorf("ECS does not describe the runner task %s: %s", arn,
		hide(failures(out.Failures)))
}

// outcome returns nil when the runner container of a stopped task ended with exit code 0.
func (f Fargate) outcome(task types.Task, arn string, hide func(string) string) error {
	name := f.Config.Container
	var container *types.Container
	for index := range task.Containers {
		if aws.ToString(task.Containers[index].Name) == name {
			container = &task.Containers[index]
		}
	}
	detail := hide(stopDetail(task, container))
	switch {
	case container == nil:
		return fmt.Errorf("the runner task %s stopped and has no container %s: %s", arn, name,
			detail)
	case container.ExitCode == nil:
		return fmt.Errorf("the runner task %s stopped before its container %s ended: %s", arn,
			name, detail)
	case *container.ExitCode != 0:
		return fmt.Errorf("the runner task %s stopped: its container %s ended with exit code "+
			"%d: %s", arn, name, *container.ExitCode, detail)
	}
	return nil
}

// stop asks ECS to stop the task of a job that ends, and returns cause. The context of the
// job can be over already, so the request has a context of its own.
func (f Fargate) stop(ctx context.Context, arn string, hide func(string) string,
	cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	_, err := f.ECS.StopTask(ctx, &ecs.StopTaskInput{Cluster: aws.String(f.Config.Cluster),
		Task: aws.String(arn), Reason: aws.String("the control plane stopped the job")})
	if err != nil {
		return fmt.Errorf("%w. The control plane could not stop the runner task %s: %s. The "+
			"role of the instance needs ecs:StopTask", cause, arn, hide(err.Error()))
	}
	return fmt.Errorf("%w. The control plane told ECS to stop the runner task %s", cause, arn)
}

// stopDetail says why a task stopped: its stop code, its reason and the reason of the
// runner container.
func stopDetail(task types.Task, container *types.Container) string {
	parts := []string{}
	if task.StopCode != "" {
		parts = append(parts, "stop code "+string(task.StopCode))
	}
	if reason := aws.ToString(task.StoppedReason); reason != "" {
		parts = append(parts, reason)
	}
	if container != nil && aws.ToString(container.Reason) != "" {
		parts = append(parts, "container: "+aws.ToString(container.Reason))
	}
	if len(parts) == 0 {
		return "ECS gave no reason"
	}
	return strings.Join(parts, "; ")
}

// failures says why ECS did not run or describe a task.
func failures(list []types.Failure) string {
	parts := []string{}
	for _, failure := range list {
		part := aws.ToString(failure.Reason)
		if detail := aws.ToString(failure.Detail); detail != "" {
			part += " (" + detail + ")"
		}
		if arn := aws.ToString(failure.Arn); arn != "" {
			part += " for " + arn
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "ECS gave no reason"
	}
	return strings.Join(parts, "; ")
}
