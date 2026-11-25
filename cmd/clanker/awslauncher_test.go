package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"

	"github.com/statik/site-reliability-clanker/internal/awsrunner"
	"github.com/statik/site-reliability-clanker/internal/dispatch"
)

const exampleRunner = `{"cluster": "runners", "task_definition": "clanker-runner:3",
	"container": "runner", "subnets": ["subnet-0123456789abcdef0"],
	"security_groups": ["sg-0123456789abcdef0"]}`

// instance is the metadata service of an instance with a private IPv4 address, or one
// that fails.
type instance struct {
	ip    string
	err   error
	asked bool
}

func (i *instance) GetMetadata(context.Context, *imds.GetMetadataInput, ...func(*imds.Options)) (
	*imds.GetMetadataOutput, error) {
	i.asked = true
	if i.err != nil {
		return nil, i.err
	}
	return &imds.GetMetadataOutput{Content: io.NopCloser(strings.NewReader(i.ip))}, nil
}

// clients returns AWS clients that make no call. The ECS client records its region.
func clients(meta *instance, region *string) awsClients {
	return awsClients{
		ecs: func(r string) awsrunner.ECS {
			*region = r
			return nil
		},
		metadata: func() awsrunner.Metadata { return meta },
	}
}

func setRunnerSettings(t *testing.T, runner, region, coreURL string) {
	t.Helper()
	t.Setenv(awsRunnerEnv, runner)
	t.Setenv(regionEnv, region)
	t.Setenv(dispatch.CoreURLEnv, coreURL)
}

func TestWithoutTheRunnerSettingTheCoreOnAWSHasNoLauncher(t *testing.T) {
	setRunnerSettings(t, "", "eu-west-1", "")
	meta, region := &instance{}, ""
	launcher, err := awsLauncher(context.Background(), options{cloud: "aws", addr: ":7070"},
		clients(meta, &region))
	if err != nil || launcher != nil || meta.asked || region != "" {
		t.Fatalf("launcher = %v, error = %v, metadata asked = %v, region = %q", launcher, err,
			meta.asked, region)
	}
	d, err := newDispatcher(context.Background(), options{cloud: "aws", addr: ":7070"})
	if err != nil {
		t.Fatal(err)
	}
	if _, none := d.Launcher.(noLauncher); !none {
		t.Fatalf("launcher = %#v, want noLauncher", d.Launcher)
	}
}

func TestWithTheRunnerSettingRunnersStartOnFargate(t *testing.T) {
	setRunnerSettings(t, exampleRunner, "eu-west-1", "")
	meta, region := &instance{ip: "10.0.1.5\n"}, ""
	launcher, err := awsLauncher(context.Background(),
		options{cloud: "aws", env: "prod", addr: "0.0.0.0:7070"}, clients(meta, &region))
	if err != nil {
		t.Fatal(err)
	}
	fargate, isFargate := launcher.(awsrunner.Fargate)
	if !isFargate || region != "eu-west-1" || fargate.Env != "prod" ||
		fargate.CoreURL != "http://10.0.1.5:7070" || fargate.Config.Cluster != "runners" {
		t.Fatalf("launcher = %#v, ECS region = %q", launcher, region)
	}
}

func TestTheDispatcherOnAWSUsesFargateWithTheRunnerSetting(t *testing.T) {
	// With the core URL set, building the dispatcher makes no call to AWS or to IMDS.
	setRunnerSettings(t, exampleRunner, "eu-west-1", "http://10.0.1.5:7070")
	d, err := newDispatcher(context.Background(), options{cloud: "aws", env: "prod",
		addr: ":7070"})
	if err != nil {
		t.Fatal(err)
	}
	fargate, isFargate := d.Launcher.(awsrunner.Fargate)
	if !isFargate || fargate.ECS == nil || fargate.CoreURL != "http://10.0.1.5:7070" {
		t.Fatalf("launcher = %#v, want Fargate with an ECS client", d.Launcher)
	}
}

func TestTheCoreURLSettingReplacesTheAddressOfTheInstance(t *testing.T) {
	setRunnerSettings(t, exampleRunner, "eu-west-1", "http://core.internal:7070")
	meta, region := &instance{err: errors.New("no instance")}, ""
	launcher, err := awsLauncher(context.Background(), options{cloud: "aws", addr: ":7070"},
		clients(meta, &region))
	if err != nil {
		t.Fatal(err)
	}
	if fargate := launcher.(awsrunner.Fargate); fargate.CoreURL != "http://core.internal:7070" ||
		meta.asked {
		t.Fatalf("core URL = %q, metadata asked = %v", fargate.CoreURL, meta.asked)
	}
}

func TestARunnerSettingThatIsNotValidStopsTheStart(t *testing.T) {
	cases := []struct {
		runner, region, coreURL, addr string
		meta                          *instance
		want                          string
	}{
		{`{"cluster": "runners"}`, "eu-west-1", "", ":7070", &instance{ip: "10.0.1.5"},
			awsRunnerEnv + ": the runner setting has no task_definition"},
		{exampleRunner, "", "", ":7070", &instance{ip: "10.0.1.5"}, "set " + regionEnv},
		{exampleRunner, "eu-west-1", "core:7070", ":7070", &instance{ip: "10.0.1.5"},
			dispatch.CoreURLEnv + ` is "core:7070", which is not an http or https URL`},
		{exampleRunner, "eu-west-1", "", "127.0.0.1", &instance{ip: "10.0.1.5"},
			`--addr "127.0.0.1" has no port`},
		{exampleRunner, "eu-west-1", "", ":7070", &instance{err: errors.New("no token")},
			"no token. Set " + dispatch.CoreURLEnv},
	}
	for _, c := range cases {
		setRunnerSettings(t, c.runner, c.region, c.coreURL)
		region := ""
		launcher, err := awsLauncher(context.Background(), options{cloud: "aws", addr: c.addr},
			clients(c.meta, &region))
		if err == nil || !strings.Contains(err.Error(), c.want) || launcher != nil {
			t.Errorf("%+v: launcher = %v, error = %v, want %q", c, launcher, err, c.want)
		}
	}
}
