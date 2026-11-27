package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// converseRequest is the part of a Converse request that the stand-in reads.
type converseRequest struct {
	System []struct {
		Text string `json:"text"`
	} `json:"system"`
	Messages        []json.RawMessage `json:"messages"`
	InferenceConfig struct {
		MaxTokens int `json:"maxTokens"`
	} `json:"inferenceConfig"`
}

// roleAnswers are the answers of the stand-in for Bedrock, by the first words of the
// system prompt of a role. The Explainer gets an approval summary, and the Reviewer a
// review that keeps the tier of a first deployment.
var roleAnswers = map[string]string{
	"You are the Explainer role": `{"sentence": "The stand-in for Bedrock explains the ` +
		`first deployment.", "why": "aws-core-check", "downtime": "none", "user_effect": ` +
		`"none", "cost_change": "none", "duration": "minutes", "pre_checks": [], "backup": ` +
		`"none", "rollback": "destroy", "verification": "the check", "if_deferred": "none"}`,
	"You are the Reviewer role": `{"tier": "T3", "concerns": []}`,
}

// converse serves Bedrock Converse at /model/<model ID>/converse. It records the model and
// the role of each call, and answers with fixed usage: 1000 input tokens and 100 output
// tokens.
func (s *server) converse(w http.ResponseWriter, r *http.Request) {
	model, err := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(r.URL.EscapedPath(),
		"/model/"), "/converse"))
	if err != nil {
		bedrockError(w, err)
		return
	}
	var in converseRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		bedrockError(w, err)
		return
	}
	role, text := "", ""
	for prefix, answer := range roleAnswers {
		if len(in.System) > 0 && strings.HasPrefix(in.System[0].Text, prefix) {
			role, text = strings.TrimPrefix(prefix, "You are the "), answer
		}
	}
	s.record.add("bedrock", "Converse", r, map[string]any{"model": model, "role": role,
		"max_tokens": in.InferenceConfig.MaxTokens, "messages": len(in.Messages)})
	if role == "" {
		bedrockError(w, fmt.Errorf("the stand-in has no answer for the system prompt of "+
			"this call"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	answer := map[string]any{"stopReason": "end_turn",
		"usage":   map[string]int{"inputTokens": 1000, "outputTokens": 100, "totalTokens": 1100},
		"metrics": map[string]int{"latencyMs": 1},
		"output": map[string]any{"message": map[string]any{"role": "assistant",
			"content": []map[string]string{{"text": text}}}}}
	if err := json.NewEncoder(w).Encode(answer); err != nil {
		fmt.Fprintln(os.Stderr, "fakeaws: answer Converse:", err)
	}
}

// bedrockError answers a call of the REST protocol of Bedrock with a ValidationException.
func bedrockError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Amzn-ErrorType", "ValidationException")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
}
