package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/setup"
)

// installFlags are the inputs of `clanker install --cloud aws`. An input that is not given
// takes its value from ~/.clanker/aws/<env>/home.tfvars, when that file exists.
type installFlags struct {
	network       string
	subnets       string
	routerGroup   string
	zoneID        string
	nanos         string
	modelKeyFile  string
	modelProvider string
	bedrockModels roleModels
	tags          tagFlags
}

// roleModels collects --bedrock-model ROLE=ID.
type roleModels map[string]string

func (m *roleModels) String() string { return fmt.Sprintf("%d models", len(*m)) }

func (m *roleModels) Set(text string) error {
	role, id, found := strings.Cut(text, "=")
	if !found || strings.TrimSpace(role) == "" || id == "" {
		return errors.New("give a model as ROLE=ID, for example " +
			"author=us.anthropic.claude-sonnet-5-5")
	}
	if *m == nil {
		*m = roleModels{}
	}
	(*m)[role] = id
	return nil
}

// tagFlags collects --tag KEY=VALUE. The values are never printed.
type tagFlags map[string]string

func (t *tagFlags) String() string { return fmt.Sprintf("%d tags", len(*t)) }

func (t *tagFlags) Set(text string) error {
	key, value, found := strings.Cut(text, "=")
	if !found || strings.TrimSpace(key) == "" || value == "" {
		return errors.New("give a tag as KEY=VALUE")
	}
	if *t == nil {
		*t = tagFlags{}
	}
	(*t)[key] = value
	return nil
}

// awsLayers runs `clanker bootstrap --cloud aws` and `clanker destroy --cloud aws` in this
// process for the installer.
type awsLayers struct {
	o   options
	run setup.CLI
}

func (l awsLayers) options(request setup.LayerRequest) options {
	o := l.o
	o.env, o.cloud, o.profile, o.region = request.Env, setup.TargetAWS, request.Profile,
		request.Region
	o.brokerPrincipal, o.parameterPath = request.BrokerPrincipal, request.ParameterPath
	o.kmsKey, o.dataDir, o.layerVariables = request.KMSKey, request.DataDir, request.Variables
	return o
}

func (l awsLayers) Bootstrap(ctx context.Context, request setup.LayerRequest) error {
	return bootstrapAWS(ctx, l.options(request), l.run, request.Log)
}

func (l awsLayers) Destroy(ctx context.Context, request setup.LayerRequest) error {
	return destroyAWS(ctx, l.options(request), l.run, request.Log)
}

func installer(o options) *setup.Installer {
	return &setup.Installer{Layers: awsLayers{o: o, run: runAWS}, Version: version}
}

// installInput turns the flags into the input of the installer.
func installInput(o options) (setup.InstallInput, error) {
	if o.cloud != setup.TargetAWS {
		return setup.InstallInput{}, fmt.Errorf("the installer sets up AWS only: give "+
			"--cloud aws, not %q", o.cloud)
	}
	if o.env == "local" || o.profile == "" {
		return setup.InstallInput{}, errors.New("name the environment and the AWS CLI " +
			"profile: --env NAME --profile NAME")
	}
	in := setup.InstallInput{Env: o.env, Profile: o.profile, Region: o.region,
		Network: o.install.network, RouterGroup: o.install.routerGroup,
		ZoneID: o.install.zoneID, Nanos: o.install.nanos, Source: o.sourceDir}
	if o.install.subnets != "" {
		in.Subnets = strings.Split(o.install.subnets, ",")
	}
	if o.install.tags != nil {
		in.Tags = o.install.tags
	}
	if err := modelInput(o.install, &in); err != nil {
		return setup.InstallInput{}, err
	}
	if o.install.modelKeyFile != "" {
		key, err := os.ReadFile(o.install.modelKeyFile)
		if err != nil {
			return setup.InstallInput{}, fmt.Errorf("read the model key: %w", err)
		}
		in.ModelKey = strings.TrimSpace(string(key))
		if len(in.ModelKey) < 20 || strings.ContainsAny(in.ModelKey, " \t\r\n\"'") {
			return setup.InstallInput{}, fmt.Errorf("%s does not hold an API key alone",
				o.install.modelKeyFile)
		}
	}
	return in, nil
}

// modelInput reads the choice of the model. A --bedrock-model alone chooses Bedrock, and a
// key file alone chooses OpenRouter.
func modelInput(flags installFlags, in *setup.InstallInput) error {
	in.ModelProvider = flags.modelProvider
	if in.ModelProvider == "" && len(flags.bedrockModels) > 0 {
		in.ModelProvider = setup.ProviderBedrock
	}
	switch in.ModelProvider {
	case "", setup.ProviderOpenRouter:
		if len(flags.bedrockModels) > 0 {
			return errors.New("--bedrock-model is for --model-provider bedrock")
		}
	case setup.ProviderBedrock:
		if flags.modelKeyFile != "" {
			return errors.New("--model-key-file is for OpenRouter: the core calls Amazon " +
				"Bedrock with the role of its instance and needs no key")
		}
		in.BedrockModels = flags.bedrockModels
	default:
		return fmt.Errorf("--model-provider %q does not exist: use %s or %s",
			in.ModelProvider, setup.ProviderBedrock, setup.ProviderOpenRouter)
	}
	return nil
}

// printReport prints the progress of the installer.
func printReport() setup.Report {
	return setup.Report{Step: func(int, string) {}, Line: func(text string) {
		fmt.Println(text)
	}}
}

func runInstall(ctx context.Context, o options) error {
	in, err := installInput(o)
	if err != nil {
		return err
	}
	_, err = installer(o).Install(ctx, in, printReport())
	return err
}

func runUninstall(ctx context.Context, o options) error {
	in, err := installInput(o)
	if err != nil {
		return err
	}
	return installer(o).Uninstall(ctx, in, printReport())
}
