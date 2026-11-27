package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/agent"
)

// liveBedrockEnv turns on TestLiveBedrock. The test calls Amazon Bedrock and costs a few
// cents, so no other run calls it.
const liveBedrockEnv = "CLANKER_LIVE_BEDROCK"

// TestLiveBedrock calls Amazon Bedrock with the settings of the model and the AWS
// credentials of this machine, as `clanker up` on this machine does: one call of the
// Explainer, and one round trip of the Author with a tool call and its result. The prompts
// are the system prompts of the roles and an example application, with no data of a
// person.
//
// Run it with CLANKER_LIVE_BEDROCK=1 CLANKER_LLM_PROVIDER=bedrock AWS_REGION=<region> and
// AWS_PROFILE=<profile>.
func TestLiveBedrock(t *testing.T) {
	if os.Getenv(liveBedrockEnv) != "1" {
		t.Skipf("set %s=1 to call Amazon Bedrock", liveBedrockEnv)
	}
	ctx := context.Background()
	llm, models, err := chooseLLM(os.Getenv, bedrockFor(ctx, options{cloud: "local"}))
	if err != nil {
		t.Fatal(err)
	}
	if llm.Mode() != agent.ModeBedrock {
		t.Fatalf("mode = %s: set CLANKER_LLM_PROVIDER=bedrock", llm.Mode())
	}
	spent := liveExplainer(ctx, t, llm, models[agent.RoleExplainer])
	spent += liveAuthor(ctx, t, llm, models[agent.RoleAuthor])
	t.Logf("the two checks cost %.4f USD at list price", spent)
}

func livePrompt(t *testing.T, role string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "agent", "prompts",
		role+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReplacer("{{releases_repository}}", "paperboat-releases",
		"{{releases_directory}}", "releases").Replace(string(raw))
}

func liveData(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return "<data>" + string(raw) + "</data>"
}

func liveExplainer(ctx context.Context, t *testing.T, llm agent.LLM, model string) float64 {
	t.Helper()
	input := map[string]any{"title": "Upgrade paperboat from 1.4.0 to 1.5.0",
		"kind": "upgrade", "env": "staging", "layers": []string{"50-app"}, "tier": "T2",
		"tier_rule": "an application upgrade", "trigger": "release 1.5.0",
		"plans": []map[string]any{{"layer": "50-app", "add": 0, "change": 1, "destroy": 0,
			"details": []string{"helm_release.app: image.tag: 1.4.0 → 1.5.0"}}},
		"deferred_layers": []string{}}
	resp, err := llm.Complete(ctx, agent.Request{Model: model, Messages: []agent.ChatMessage{
		{Role: "system", Content: livePrompt(t, "explainer")},
		{Role: "user", Content: liveData(t, input)}}})
	if err != nil {
		t.Fatalf("Explainer: %v", err)
	}
	start, end := strings.Index(resp.Message.Content, "{"),
		strings.LastIndex(resp.Message.Content, "}")
	var explanation struct{ Sentence string }
	if start < 0 || end < start || json.Unmarshal([]byte(resp.Message.Content[start:end+1]),
		&explanation) != nil || explanation.Sentence == "" {
		t.Fatalf("the Explainer did not return the JSON object with a sentence: %s",
			resp.Message.Content)
	}
	t.Logf("Explainer %s: %d + %d tokens, %.5f USD: %s", resp.Model, resp.PromptTokens,
		resp.CompletionTokens, resp.CostUSD, explanation.Sentence)
	return resp.CostUSD
}

func liveTool(name, description string, properties ...string) agent.Tool {
	var tool agent.Tool
	tool.Type = "function"
	tool.Function.Name, tool.Function.Description = name, description
	fields := map[string]any{}
	for _, property := range properties {
		fields[property] = map[string]any{"type": "string"}
	}
	tool.Function.Parameters = map[string]any{"type": "object", "properties": fields,
		"required": properties}
	return tool
}

func liveAuthor(ctx context.Context, t *testing.T, llm agent.LLM, model string) float64 {
	t.Helper()
	tools := []agent.Tool{
		liveTool("read_file", "Read a file from a repository on the main branch.", "repo",
			"path"),
		liveTool("set_tfvar", "Set one variable in the terraform.tfvars file of a layer.",
			"layer", "name", "value"),
		liveTool("open_pull_request", "Open the pull request for the change. Call it once, "+
			"last.", "title", "body"),
	}
	finding := map[string]any{"key": "release:paperboat:1.5.0", "env": "staging",
		"title": "paperboat 1.5.0 is released",
		"detail": map[string]any{"kind": "upgrade", "layer": "50-app", "variable": "image_tag",
			"version": "1.5.0"}}
	messages := []agent.ChatMessage{
		{Role: "system", Content: livePrompt(t, "author")},
		{Role: "user", Content: liveData(t, map[string]any{"finding": finding, "cloud": "local",
			"tfvars_path": "platform/stacks/local/<layer>/terraform.tfvars"})}}
	first, err := llm.Complete(ctx, agent.Request{Model: model, Messages: messages,
		Tools: tools})
	if err != nil {
		t.Fatalf("Author, first request: %v", err)
	}
	if len(first.Message.ToolCalls) == 0 {
		t.Fatalf("the Author called no tool: %s", first.Message.Content)
	}
	messages = append(messages, first.Message)
	for _, call := range first.Message.ToolCalls {
		t.Logf("Author called %s with %s", call.Function.Name, call.Function.Arguments)
		result := "ok"
		if call.Function.Name == "read_file" {
			result = "image_tag = \"1.4.0\"\nreplicas  = 2\n"
		}
		messages = append(messages, agent.ChatMessage{Role: "tool", ToolCallID: call.ID,
			Content: result})
	}
	second, err := llm.Complete(ctx, agent.Request{Model: model, Messages: messages,
		Tools: tools})
	if err != nil {
		t.Fatalf("Author, request with the tool results: %v", err)
	}
	next := []string{}
	for _, call := range second.Message.ToolCalls {
		next = append(next, call.Function.Name)
	}
	t.Logf("Author %s: %d + %d and %d + %d tokens, %.5f USD; then it calls %v", first.Model,
		first.PromptTokens, first.CompletionTokens, second.PromptTokens,
		second.CompletionTokens, first.CostUSD+second.CostUSD, next)
	return first.CostUSD + second.CostUSD
}
