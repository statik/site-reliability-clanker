package main

import (
	"strings"
	"testing"

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
		llm, _, err := chooseLLM(environment(tc.settings))
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
		"CLANKER_LLM_MODEL": "ignored-without-a-base-url"}))
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
		"CLANKER_LLM_MODEL": "small", "CLANKER_LLM_MODEL_REVIEWER": "large"}))
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
		"CLANKER_LLM_BASE_URL": "http://127.0.0.1:11434/v1"}))
	if err == nil || !strings.Contains(err.Error(), "CLANKER_LLM_MODEL") {
		t.Errorf("error = %v, want one that names CLANKER_LLM_MODEL", err)
	}
	_, _, err = chooseLLM(environment(map[string]string{
		"CLANKER_LLM_BASE_URL":     "http://127.0.0.1:11434/v1",
		"CLANKER_LLM_MODEL_TRIAGE": "m", "CLANKER_LLM_MODEL_EXPLAINER": "m",
		"CLANKER_LLM_MODEL_AUTHOR": "m", "CLANKER_LLM_MODEL_INVESTIGATOR": "m"}))
	if err == nil || !strings.Contains(err.Error(), "CLANKER_LLM_MODEL_REVIEWER") {
		t.Errorf("error = %v, want one that names CLANKER_LLM_MODEL_REVIEWER", err)
	}
	_, _, err = chooseLLM(environment(map[string]string{
		"CLANKER_LLM_BASE_URL":     "http://127.0.0.1:11434/v1",
		"CLANKER_LLM_MODEL_TRIAGE": "m", "CLANKER_LLM_MODEL_EXPLAINER": "m",
		"CLANKER_LLM_MODEL_AUTHOR": "m", "CLANKER_LLM_MODEL_INVESTIGATOR": "m",
		"CLANKER_LLM_MODEL_REVIEWER": "m"}))
	if err != nil {
		t.Errorf("five role settings with no common model gave the error %v", err)
	}
}
