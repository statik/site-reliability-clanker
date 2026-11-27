package setup

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// bedrockModels is a choice of the step Language model for every role.
const bedrockModels = `{"triage":"us.anthropic.claude-haiku-5-5",` +
	`"explainer":"us.anthropic.claude-haiku-5-5","author":"us.anthropic.claude-sonnet-5-5",` +
	`"investigator":"us.anthropic.claude-sonnet-5-5","reviewer":"us.anthropic.claude-sonnet-5-5"}`

func TestTheWizardChecksEachBedrockModelBeforeItKeepsTheChoice(t *testing.T) {
	server, handler, cli := placedServer(t)
	cli.answers["bedrock list-inference-profiles"] = `[
		{"id":"us.moonshotai.kimi-k3.1","name":"US Moonshot AI Kimi K3.1","status":"ACTIVE"},
		{"id":"us.anthropic.claude-sonnet-5-5","name":"US Sonnet","status":"ACTIVE"},
		{"id":"us.anthropic.claude-haiku-5-5","name":"US Haiku","status":"ACTIVE"},
		{"id":"us.anthropic.claude-old","name":"Old","status":"LEGACY"}]`
	cli.answers["bedrock get-inference-profile"] = `"ACTIVE"`
	cli.answers["bedrock-runtime converse"] = `{"stopReason":"max_tokens","usage":{"outputTokens":64}}`
	code, listed := request(t, handler, server.Token(), http.MethodGet, "/api/aws/bedrock", "")
	ids := []string{}
	for _, profile := range listed["profiles"].([]any) {
		ids = append(ids, profile.(map[string]any)["id"].(string))
	}
	if code != http.StatusOK || listed["region"] != "eu-west-1" || strings.Join(ids, " ") !=
		"us.anthropic.claude-haiku-5-5 us.anthropic.claude-sonnet-5-5 us.moonshotai.kimi-k3.1" {
		t.Fatalf("status %d, region %v, profiles %v", code, listed["region"], ids)
	}
	defaults := listed["defaults"].(map[string]any)
	if defaults["author"] != "us.anthropic.claude-sonnet-5-5" ||
		defaults["triage"] != "us.anthropic.claude-haiku-5-5" ||
		len(listed["priced"].([]any)) != 2 {
		t.Errorf("defaults %v, priced %v", defaults, listed["priced"])
	}
	code, answer := post(t, server, handler, "/api/model",
		`{"choice":"bedrock","models":{"author":"us.anthropic.claude-sonnet-5-5"}}`)
	if code != http.StatusBadRequest || !strings.Contains(answer["error"].(string), "has no model") {
		t.Fatalf("a choice without every role: status %d, %v", code, answer)
	}
	code, answer = post(t, server, handler, "/api/model", `{"choice":"bedrock","models":`+
		bedrockModels+`}`)
	if code != http.StatusOK || answer["started"] != true {
		t.Fatalf("status %d, %v", code, answer)
	}
	if job := waitJob(t, server); job.Status != "done" {
		t.Fatalf("job = %+v", job)
	}
	converse := []string{}
	for _, call := range cli.calls {
		if call[0] == "bedrock-runtime" {
			converse = append(converse, argumentAfter(call, "--model-id")+" "+
				argumentAfter(call, "--region")+" "+argumentAfter(call, "--profile"))
		}
	}
	if strings.Join(converse, ", ") != "us.anthropic.claude-haiku-5-5 eu-west-1 team/operator, "+
		"us.anthropic.claude-sonnet-5-5 eu-west-1 team/operator" {
		t.Errorf("converse calls = %q, want one for each model with the sign-in", converse)
	}
	in := server.installInput()
	if server.state.KeyChoice != ModelBedrock || in.ModelProvider != ProviderBedrock ||
		in.ModelKey != "" || in.BedrockModels["reviewer"] != "us.anthropic.claude-sonnet-5-5" {
		t.Errorf("state %q, input %+v", server.state.KeyChoice, in)
	}
	plan := awsPlan(server.state)
	if !strings.Contains(plan.Model, "us.anthropic.claude-sonnet-5-5 for the Author") ||
		!strings.Contains(plan.Model, "Bedrock bills each token") ||
		!slices.ContainsFunc(plan.NotPriced, func(item string) bool {
			return strings.Contains(item, "Amazon Bedrock bills")
		}) {
		t.Errorf("plan = %q, not priced %q", plan.Model, plan.NotPriced)
	}
	if err := server.checkAWSModel(server.state); err != nil {
		t.Errorf("Create refuses the choice: %v", err)
	}
	cli.answers["ec2 describe-vpcs"] = `[]`
	post(t, server, handler, "/api/aws/discover", `{"profile":"team/operator","region":"us-east-2"}`)
	post(t, server, handler, "/api/aws/place", `{"mode":"new"}`)
	if server.state.KeyChoice != "" || server.state.BedrockModels != nil {
		t.Errorf("a new region keeps the choice: %q %v", server.state.KeyChoice,
			server.state.BedrockModels)
	}
}

func TestTheWizardKeepsNoBedrockChoiceThatAModelRefused(t *testing.T) {
	server, handler, cli := placedServer(t)
	cli.answers["bedrock get-inference-profile"] = `"ACTIVE"`
	cli.errors = map[string]error{"bedrock-runtime converse": &CLIError{
		Command: "aws bedrock-runtime converse", Err: errors.New("exit status 254"),
		Stderr: "AccessDeniedException: You don't have access to the model"}}
	post(t, server, handler, "/api/model", `{"choice":"bedrock","models":`+bedrockModels+`}`)
	job := waitJob(t, server)
	if job.Status != "failed" || !strings.Contains(job.Error, "bedrock:InvokeModel") ||
		!strings.Contains(job.Error, "access to the model") {
		t.Fatalf("job = %+v", job)
	}
	if server.state.KeyChoice != ModelHosted {
		t.Errorf("the choice is %q, want the key of before", server.state.KeyChoice)
	}
}

func TestBedrockIsForTheAWSTarget(t *testing.T) {
	server, handler := newServer(t, &fakeCLI{})
	post(t, server, handler, "/api/target", `{"target":"local","operator":"Sam Lee"}`)
	code, answer := post(t, server, handler, "/api/model", `{"choice":"bedrock","models":`+
		bedrockModels+`}`)
	if code != http.StatusBadRequest || !strings.Contains(answer["error"].(string), "AWS target") {
		t.Fatalf("status %d, %v", code, answer)
	}
}

func TestTheProposedModelsFollowTheProfilesOfTheRegion(t *testing.T) {
	profiles := func(ids ...string) []InferenceProfile {
		list := []InferenceProfile{}
		for _, id := range ids {
			list = append(list, InferenceProfile{ID: id})
		}
		return list
	}
	cases := []struct {
		name          string
		listed        []InferenceProfile
		author, light string
	}{
		{"the defaults", profiles("global.anthropic.claude-fable-5", "us.anthropic.claude-haiku-5-5",
			"us.anthropic.claude-sonnet-5-5"), "us.anthropic.claude-sonnet-5-5",
			"us.anthropic.claude-haiku-5-5"},
		{"another geography before global", profiles("global.anthropic.claude-fable-5",
			"global.anthropic.claude-haiku-5-5", "eu.anthropic.claude-haiku-5-5",
			"global.anthropic.claude-sonnet-5-5"), "global.anthropic.claude-sonnet-5-5",
			"eu.anthropic.claude-haiku-5-5"},
		{"no model of the defaults", profiles("global.anthropic.claude-fable-5",
			"apac.anthropic.claude-opus-5-5"), "", ""},
	}
	for _, tc := range cases {
		proposed := profileDefaults(tc.listed)
		if proposed["author"] != tc.author || proposed["reviewer"] != tc.author ||
			proposed["explainer"] != tc.light || proposed["triage"] != tc.light {
			t.Errorf("%s: proposed %v", tc.name, proposed)
		}
	}
}

func TestTheBudgetTextNamesTheModelsWithNoPrice(t *testing.T) {
	priced := bedrockChoice(nil, nil)
	if text := budgetText(priced); text != "S.R.C. stops the model calls of a day at 5 USD at list price." {
		t.Errorf("priced models: %q", text)
	}
	mixed := bedrockChoice(map[string]string{"author": "us.moonshotai.kimi-k3.1"}, nil)
	if text := budgetText(mixed); !strings.Contains(text, "no price for us.moonshotai.kimi-k3.1,") ||
		!strings.Contains(text, "5 million tokens") {
		t.Errorf("a model with no price: %q", text)
	}
}
