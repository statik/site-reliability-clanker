package main

import (
	"fmt"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/agent"
)

// chooseLLM reads the settings of the model from the environment. It returns the client and
// the model for each role. The order is: replay mode, then an OpenAI-compatible endpoint,
// then OpenRouter. Without a setting it returns the replay client.
func chooseLLM(getenv func(string) string) (agent.LLM, map[string]string, error) {
	// CLANKER_AGENT_MODE=replay forces recorded responses, for a repeatable demo.
	if getenv("CLANKER_AGENT_MODE") == "replay" {
		return agent.Replay{}, agent.DefaultModels(), nil
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
