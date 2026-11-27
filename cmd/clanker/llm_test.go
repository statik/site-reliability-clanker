package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"

	"github.com/statik/site-reliability-clanker/internal/agent"
)

func environment(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestChooseLLMPicksTheClientFromTheSettings(t *testing.T) {
	cases := []struct {
		name     string
		settings map[string]string
		wantMode string
	}{
		{"no setting", map[string]string{}, "replay"},
		{"an OpenRouter key", map[string]string{"OPENROUTER_API_KEY": "sk-or-x"}, "openrouter"},
		{"an endpoint without an OpenRouter key", map[string]string{
			"CLANKER_LLM_BASE_URL": "http://127.0.0.1:11434/v1", "CLANKER_LLM_MODEL": "m"}, "local"},
		{"an endpoint wins over an OpenRouter key", map[string]string{
			"CLANKER_LLM_BASE_URL": "http://127.0.0.1:11434/v1", "CLANKER_LLM_MODEL": "m",
			"OPENROUTER_API_KEY": "sk-or-x"}, "local"},
		{"replay wins over an OpenRouter key", map[string]string{
			"CLANKER_AGENT_MODE": "replay", "OPENROUTER_API_KEY": "sk-or-x"}, "replay"},
		{"replay wins over an endpoint with no model", map[string]string{
			"CLANKER_AGENT_MODE": "replay", "CLANKER_LLM_BASE_URL": "http://127.0.0.1:1/v1"},
			"replay"},
	}
	for _, tc := range cases {
		llm, _, err := chooseLLM(environment(tc.settings), noBedrock(t))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if llm.Mode() != tc.wantMode {
			t.Errorf("%s: mode = %q, want %q", tc.name, llm.Mode(), tc.wantMode)
		}
	}
}

func TestChooseLLMKeepsTheDefaultModelsForOpenRouter(t *testing.T) {
	_, models, err := chooseLLM(environment(map[string]string{"OPENROUTER_API_KEY": "sk-or-x",
		"CLANKER_LLM_MODEL": "ignored-without-a-base-url"}), noBedrock(t))
	if err != nil {
		t.Fatal(err)
	}
	for role, want := range agent.DefaultModels() {
		if models[role] != want {
			t.Errorf("model of %s = %q, want %q", role, models[role], want)
		}
	}
}

func TestOneModelNameServesEveryRoleAndARoleCanOverrideIt(t *testing.T) {
	llm, models, err := chooseLLM(environment(map[string]string{
		"CLANKER_LLM_BASE_URL": "http://127.0.0.1:11434/v1", "CLANKER_LLM_API_KEY": "secret",
		"CLANKER_LLM_MODEL": "small", "CLANKER_LLM_MODEL_REVIEWER": "large"}), noBedrock(t))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{agent.RoleTriage: "small", agent.RoleExplainer: "small",
		agent.RoleAuthor: "small", agent.RoleInvestigator: "small", agent.RoleReviewer: "large"}
	if len(models) != len(want) {
		t.Errorf("models = %v, want %v", models, want)
	}
	for role, model := range want {
		if models[role] != model {
			t.Errorf("model of %s = %q, want %q", role, models[role], model)
		}
	}
	chat, ok := llm.(*agent.Chat)
	if !ok {
		t.Fatalf("client is %T, want *agent.Chat", llm)
	}
	if chat.BaseURL != "http://127.0.0.1:11434/v1" || chat.APIKey != "secret" {
		t.Errorf("client has base URL %q and key %q", chat.BaseURL, chat.APIKey)
	}
}

func TestAnEndpointWithoutAModelForARoleIsAnError(t *testing.T) {
	_, _, err := chooseLLM(environment(map[string]string{
		"CLANKER_LLM_BASE_URL": "http://127.0.0.1:11434/v1"}), noBedrock(t))
	if err == nil || !strings.Contains(err.Error(), "CLANKER_LLM_MODEL") {
		t.Errorf("error = %v, want one that names CLANKER_LLM_MODEL", err)
	}
	_, _, err = chooseLLM(environment(map[string]string{
		"CLANKER_LLM_BASE_URL":     "http://127.0.0.1:11434/v1",
		"CLANKER_LLM_MODEL_TRIAGE": "m", "CLANKER_LLM_MODEL_EXPLAINER": "m",
		"CLANKER_LLM_MODEL_AUTHOR": "m", "CLANKER_LLM_MODEL_INVESTIGATOR": "m"}), noBedrock(t))
	if err == nil || !strings.Contains(err.Error(), "CLANKER_LLM_MODEL_REVIEWER") {
		t.Errorf("error = %v, want one that names CLANKER_LLM_MODEL_REVIEWER", err)
	}
	_, _, err = chooseLLM(environment(map[string]string{
		"CLANKER_LLM_BASE_URL":     "http://127.0.0.1:11434/v1",
		"CLANKER_LLM_MODEL_TRIAGE": "m", "CLANKER_LLM_MODEL_EXPLAINER": "m",
		"CLANKER_LLM_MODEL_AUTHOR": "m", "CLANKER_LLM_MODEL_INVESTIGATOR": "m",
		"CLANKER_LLM_MODEL_REVIEWER": "m"}), noBedrock(t))
	if err != nil {
		t.Errorf("five role settings with no common model gave the error %v", err)
	}
}

// noBedrock is the Bedrock runtime of a test that must not reach Bedrock.
func noBedrock(t *testing.T) bedrockRuntime {
	return func(region string) (agent.ConverseAPI, error) {
		t.Errorf("the settings reached Bedrock in %q", region)
		return nil, errors.New("no Bedrock in this test")
	}
}

// standInConverse is a Bedrock runtime that the tests never call.
type standInConverse struct{ region string }

func (standInConverse) Converse(context.Context, *bedrockruntime.ConverseInput,
	...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	return nil, errors.New("not called")
}

func standInBedrock(region string) (agent.ConverseAPI, error) {
	return standInConverse{region: region}, nil
}

func TestBedrockSettings(t *testing.T) {
	llm, models, err := chooseLLM(environment(map[string]string{
		"CLANKER_LLM_PROVIDER": "bedrock", "AWS_REGION": "us-west-2",
		"OPENROUTER_API_KEY": "sk-or-x"}), standInBedrock)
	if err != nil {
		t.Fatal(err)
	}
	client, ok := llm.(*agent.Bedrock)
	if !ok || client.Mode() != "bedrock" || client.Region != "us-west-2" ||
		client.API.(standInConverse).region != "us-west-2" {
		t.Fatalf("client = %#v, want Bedrock in us-west-2 before OpenRouter", llm)
	}
	want := map[string]string{agent.RoleTriage: "us.anthropic.claude-haiku-5-5",
		agent.RoleExplainer:    "us.anthropic.claude-haiku-5-5",
		agent.RoleAuthor:       "us.anthropic.claude-sonnet-5-5",
		agent.RoleInvestigator: "us.anthropic.claude-sonnet-5-5",
		agent.RoleReviewer:     "us.anthropic.claude-sonnet-5-5"}
	for role, model := range want {
		if models[role] != model {
			t.Errorf("model of %s = %q, want %q", role, models[role], model)
		}
	}
	llm, models, err = chooseLLM(environment(map[string]string{
		"CLANKER_LLM_PROVIDER": "bedrock", "AWS_REGION": "us-west-2",
		"CLANKER_LLM_REGION": "eu-west-1", "CLANKER_LLM_MODEL": "eu.anthropic.claude-haiku-5-5",
		"CLANKER_LLM_MODEL_AUTHOR": "eu.anthropic.claude-sonnet-5-5"}), standInBedrock)
	if err != nil {
		t.Fatal(err)
	}
	if llm.(*agent.Bedrock).Region != "eu-west-1" || models[agent.RoleAuthor] !=
		"eu.anthropic.claude-sonnet-5-5" || models[agent.RoleReviewer] !=
		"eu.anthropic.claude-haiku-5-5" || len(models) != len(want) {
		t.Errorf("region %s, models %v: want eu-west-1, the Author override and one model "+
			"for the rest", llm.(*agent.Bedrock).Region, models)
	}
	replay, _, err := chooseLLM(environment(map[string]string{"CLANKER_AGENT_MODE": "replay",
		"CLANKER_LLM_PROVIDER": "bedrock"}), noBedrock(t))
	if err != nil || replay.Mode() != "replay" {
		t.Errorf("replay mode with Bedrock = %v, %v; want replay", replay, err)
	}
}

func TestBedrockSettingsThatStopTheStart(t *testing.T) {
	cases := []struct {
		name     string
		settings map[string]string
		bedrock  bedrockRuntime
		want     []string
	}{
		{"no region", map[string]string{"CLANKER_LLM_PROVIDER": "bedrock"}, noBedrock(t),
			[]string{"CLANKER_LLM_REGION", "AWS_REGION"}},
		{"an unknown provider", map[string]string{"CLANKER_LLM_PROVIDER": "openai"},
			noBedrock(t), []string{"CLANKER_LLM_PROVIDER", `"openai"`, "bedrock"}},
		{"Bedrock and an endpoint", map[string]string{"CLANKER_LLM_PROVIDER": "bedrock",
			"AWS_REGION": "us-west-2", "CLANKER_LLM_BASE_URL": "http://127.0.0.1:1/v1"},
			noBedrock(t), []string{"CLANKER_LLM_BASE_URL", "unset one"}},
		{"no credentials", map[string]string{"CLANKER_LLM_PROVIDER": "bedrock",
			"AWS_REGION": "us-west-2"}, func(string) (agent.ConverseAPI, error) {
			return nil, errors.New("no session for the profile")
		}, []string{"no session for the profile"}},
	}
	for _, tc := range cases {
		_, _, err := chooseLLM(environment(tc.settings), tc.bedrock)
		if err == nil {
			t.Errorf("%s: no error", tc.name)
			continue
		}
		for _, part := range tc.want {
			if !strings.Contains(err.Error(), part) {
				t.Errorf("%s: error %q does not contain %q", tc.name, err, part)
			}
		}
	}
}

func TestTheCoreOnAWSReachesBedrockWithTheClientsOfAWS(t *testing.T) {
	asked := ""
	o := options{cloud: "aws", aws: awsAPIs{bedrock: func(region string) agent.ConverseAPI {
		asked = region
		return standInConverse{region: region}
	}}}
	api, err := bedrockFor(context.Background(), o)("us-east-2")
	if err != nil || asked != "us-east-2" || api.(standInConverse).region != "us-east-2" {
		t.Errorf("client %v, %v, asked for %q; want the client of awsAPIs in us-east-2", api,
			err, asked)
	}
}
