package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"

	"github.com/statik/site-reliability-clanker/internal/agent"
)

// The settings of the provider of the model.
const (
	providerEnv  = "CLANKER_LLM_PROVIDER"
	llmRegionEnv = "CLANKER_LLM_REGION"
	// providerBedrock is the value of CLANKER_LLM_PROVIDER for Amazon Bedrock.
	providerBedrock = "bedrock"
)

// bedrockRuntime makes the client of the Bedrock runtime in a region.
type bedrockRuntime func(region string) (agent.ConverseAPI, error)

// chooseLLM reads the settings of the model from the environment. It returns the client and
// the model for each role. The order is: replay mode, then Amazon Bedrock, then an
// OpenAI-compatible endpoint, then OpenRouter. Without a setting it returns the replay
// client.
func chooseLLM(getenv func(string) string, bedrock bedrockRuntime) (agent.LLM,
	map[string]string, error) {
	// CLANKER_AGENT_MODE=replay forces recorded responses, for a repeatable demo.
	if getenv("CLANKER_AGENT_MODE") == "replay" {
		return agent.Replay{}, agent.DefaultModels(), nil
	}
	switch provider := getenv(providerEnv); provider {
	case "":
	case providerBedrock:
		return chooseBedrock(getenv, bedrock)
	default:
		return nil, nil, fmt.Errorf("%s is %q: the one value is %s. Unset it to use "+
			"OpenRouter or the endpoint of CLANKER_LLM_BASE_URL", providerEnv, provider,
			providerBedrock)
	}
	if baseURL := getenv("CLANKER_LLM_BASE_URL"); baseURL != "" {
		models, err := localModels(getenv)
		if err != nil {
			return nil, nil, err
		}
		return agent.NewLocal(baseURL, getenv("CLANKER_LLM_API_KEY")), models, nil
	}
	if key := getenv("OPENROUTER_API_KEY"); key != "" {
		return agent.NewOpenRouter(key), agent.DefaultModels(), nil
	}
	return agent.Replay{}, agent.DefaultModels(), nil
}

// chooseBedrock returns the client of Amazon Bedrock in the region of CLANKER_LLM_REGION or
// AWS_REGION. A role without a model setting gets its default inference profile.
func chooseBedrock(getenv func(string) string, bedrock bedrockRuntime) (agent.LLM,
	map[string]string, error) {
	if getenv("CLANKER_LLM_BASE_URL") != "" {
		return nil, nil, fmt.Errorf("%s=%s and CLANKER_LLM_BASE_URL are both set: unset one "+
			"of them", providerEnv, providerBedrock)
	}
	region := getenv(llmRegionEnv)
	if region == "" {
		region = getenv(regionEnv)
	}
	if region == "" {
		return nil, nil, fmt.Errorf("%s=%s needs a region: set %s or %s", providerEnv,
			providerBedrock, llmRegionEnv, regionEnv)
	}
	api, err := bedrock(region)
	if err != nil {
		return nil, nil, err
	}
	return &agent.Bedrock{API: api, Region: region}, bedrockModels(getenv), nil
}

// localModels gives the model of each role on the endpoint of the operator. The default
// models are OpenRouter names, which such an endpoint does not serve, so each role needs a
// name from the environment.
func localModels(getenv func(string) string) (map[string]string, error) {
	models := map[string]string{}
	for role := range agent.DefaultModels() {
		override := "CLANKER_LLM_MODEL_" + strings.ToUpper(role)
		model := getenv(override)
		if model == "" {
			model = getenv("CLANKER_LLM_MODEL")
		}
		if model == "" {
			return nil, fmt.Errorf("CLANKER_LLM_BASE_URL is set, but the role %s has no model: "+
				"set CLANKER_LLM_MODEL to a model that the endpoint serves, or set %s", role,
				override)
		}
		models[role] = model
	}
	return models, nil
}

// bedrockModels gives the inference profile of each role: CLANKER_LLM_MODEL_<ROLE>, then
// CLANKER_LLM_MODEL, then the default of the role.
func bedrockModels(getenv func(string) string) map[string]string {
	models := agent.DefaultBedrockModels()
	for role := range models {
		if model := getenv("CLANKER_LLM_MODEL_" + strings.ToUpper(role)); model != "" {
			models[role] = model
		} else if model := getenv("CLANKER_LLM_MODEL"); model != "" {
			models[role] = model
		}
	}
	return models
}

// bedrockFor returns how the core reaches Amazon Bedrock. On AWS it uses the client of
// awsAPIs: the role of the instance, or the stand-in of the checks. Elsewhere it uses the
// credentials of this machine.
func bedrockFor(ctx context.Context, o options) bedrockRuntime {
	if o.cloud == "aws" {
		return func(region string) (agent.ConverseAPI, error) { return o.aws.bedrock(region), nil }
	}
	return func(region string) (agent.ConverseAPI, error) { return machineBedrock(ctx, region) }
}

// machineBedrock returns the Bedrock runtime with the credentials of this machine: the keys
// of the environment, or the profile of AWS_PROFILE, also a profile with a single sign-on
// session. It reads the credentials once, so that a missing or expired session stops the
// start and not the first model call. Only this command reads the configuration of the AWS
// CLI: the core packages do not, because a profile can name a program that gives the
// credentials, and the core starts no program.
func machineBedrock(ctx context.Context, region string) (agent.ConverseAPI, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("read the AWS configuration of this machine for Amazon "+
			"Bedrock: %w", err)
	}
	if _, err := cfg.Credentials.Retrieve(ctx); err != nil {
		return nil, fmt.Errorf("%s=%s needs AWS credentials on this machine: %w. Set "+
			"AWS_PROFILE to a profile of the AWS CLI and sign in to it (aws sso login), or "+
			"set the keys of the environment", providerEnv, providerBedrock, err)
	}
	return bedrockruntime.NewFromConfig(cfg), nil
}
