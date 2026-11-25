package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/statik/site-reliability-clanker/internal/awsrunner"
	"github.com/statik/site-reliability-clanker/internal/dispatch"
)

// awsRunnerEnv says where the runners run on AWS, as JSON: the ECS cluster, the task
// definition and container of the runner, and the private subnets and security groups of
// the tasks. None of it is a secret. Parameter Store can give it like the other settings.
const awsRunnerEnv = "CLANKER_AWS_RUNNER"

// awsClients make the AWS clients of the launcher. Tests replace them.
type awsClients struct {
	ecs      func(region string) awsrunner.ECS
	metadata func() awsrunner.Metadata
}

// awsLauncher returns the launcher of the runners on AWS. Without the setting it returns
// no launcher, and the core then starts no runner. A setting that is not valid stops the
// start.
func awsLauncher(ctx context.Context, o options, clients awsClients) (dispatch.Launcher,
	error) {
	text := os.Getenv(awsRunnerEnv)
	if text == "" {
		slog.Warn("the control plane starts no runner on AWS: set " + awsRunnerEnv +
			" to the ECS cluster, task definition, container, subnets and security groups " +
			"of the runners")
		return nil, nil
	}
	config, err := awsrunner.ParseConfig(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", awsRunnerEnv, err)
	}
	region := os.Getenv(regionEnv)
	if region == "" {
		return nil, fmt.Errorf("%s is set, so the control plane starts runners in ECS: set %s "+
			"to the region of the cluster", awsRunnerEnv, regionEnv)
	}
	coreURL, err := runnerCoreURL(ctx, o.addr, clients.metadata)
	if err != nil {
		return nil, err
	}
	slog.Info("runners start as ECS tasks on Fargate", "region", region,
		"cluster", config.Cluster, "task_definition", config.TaskDefinition,
		"core_url", coreURL)
	return awsrunner.Fargate{ECS: clients.ecs(region), Config: config, Env: o.env,
		CoreURL: coreURL}, nil
}

// runnerCoreURL returns the address of the control plane as a runner task reaches it: the
// setting CLANKER_CORE_URL, or else the private IPv4 address of this instance with the port
// of --addr.
func runnerCoreURL(ctx context.Context, addr string, metadata func() awsrunner.Metadata) (
	string, error) {
	if text := os.Getenv(dispatch.CoreURLEnv); text != "" {
		address, err := url.Parse(text)
		if err != nil || (address.Scheme != "http" && address.Scheme != "https") ||
			address.Host == "" {
			return "", fmt.Errorf("%s is %q, which is not an http or https URL with a host",
				dispatch.CoreURLEnv, text)
		}
		return text, nil
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "", fmt.Errorf("--addr %q has no port for the runner tasks: use host:port, "+
			"or set %s", addr, dispatch.CoreURLEnv)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ip, err := awsrunner.PrivateIPv4(ctx, metadata())
	if err != nil {
		return "", fmt.Errorf("%w. Set %s to the address of the control plane that the "+
			"runner tasks reach, such as http://10.0.1.5:7070", err, dispatch.CoreURLEnv)
	}
	return "http://" + net.JoinHostPort(ip, port), nil
}
