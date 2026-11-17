// Package agent is the agent runtime. The model proposes and explains. It has no tool that
// applies a change, approves a change or reads a credential.
package agent

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChatMessage is one message of an OpenAI-compatible chat completion.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a request of the model to run a typed tool.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Tool describes a typed tool to the model.
type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// Request is one completion request.
type Request struct {
	Model    string
	Messages []ChatMessage
	Tools    []Tool
	// ReplayKey selects the recording in replay mode.
	ReplayKey string
	// Step is the index of this request inside one agent loop.
	Step int
}

// Response is one completion.
type Response struct {
	Message          ChatMessage
	Model            string
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
}

// LLM produces completions.
type LLM interface {
	Mode() string
	Complete(ctx context.Context, req Request) (Response, error)
}

// OpenRouter calls the OpenRouter chat completions endpoint.
type OpenRouter struct {
	APIKey  string
	BaseURL string
	client  http.Client
}

// NewOpenRouter returns a client for the OpenRouter API.
func NewOpenRouter(apiKey string) *OpenRouter {
	return &OpenRouter{APIKey: apiKey, BaseURL: "https://openrouter.ai/api/v1",
		client: http.Client{Timeout: 120 * time.Second}}
}

// Mode implements LLM.
func (*OpenRouter) Mode() string { return "openrouter" }

// Complete implements LLM.
func (o *OpenRouter) Complete(ctx context.Context, req Request) (Response, error) {
	payload := map[string]any{"model": req.Model, "messages": req.Messages,
		"usage": map[string]any{"include": true}}
	if len(req.Tools) > 0 {
		payload["tools"] = req.Tools
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("encode OpenRouter request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("build OpenRouter request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Title", "site-reliability-clanker")
	resp, err := o.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("call OpenRouter with model %s: %w", req.Model, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Response{}, fmt.Errorf("read OpenRouter response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("OpenRouter answered %d for model %s: %s. Check the key "+
			"and the model name", resp.StatusCode, req.Model, truncate(string(raw), 300))
	}
	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message ChatMessage `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int     `json:"prompt_tokens"`
			CompletionTokens int     `json:"completion_tokens"`
			Cost             float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Response{}, fmt.Errorf("decode OpenRouter response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("OpenRouter gave no choices for model %s", req.Model)
	}
	return Response{Message: parsed.Choices[0].Message, Model: parsed.Model,
		PromptTokens: parsed.Usage.PromptTokens, CompletionTokens: parsed.Usage.CompletionTokens,
		CostUSD: parsed.Usage.Cost}, nil
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

//go:embed replay/*.json
var recordings embed.FS

// Replay answers from recorded model responses, so the demo runs without a key or network.
type Replay struct{}

// Mode implements LLM.
func (Replay) Mode() string { return "replay" }

// ErrNoRecording means that replay mode has no recorded response for a request.
var ErrNoRecording = fmt.Errorf("no recorded response")

// Complete implements LLM. The recording for a key is a list of assistant messages, one for
// each step of the loop.
func (Replay) Complete(_ context.Context, req Request) (Response, error) {
	name := "replay/" + strings.NewReplacer("/", "_", ":", "_", " ", "-").Replace(req.ReplayKey) +
		".json"
	raw, err := recordings.ReadFile(name)
	if err != nil {
		return Response{}, fmt.Errorf("%w for key %q", ErrNoRecording, req.ReplayKey)
	}
	var steps []ChatMessage
	if err := json.Unmarshal(raw, &steps); err != nil {
		return Response{}, fmt.Errorf("decode recording %s: %w", name, err)
	}
	if req.Step >= len(steps) {
		return Response{}, fmt.Errorf("%w for key %q step %d", ErrNoRecording, req.ReplayKey,
			req.Step)
	}
	message := steps[req.Step]
	message.Role = "assistant"
	return Response{Message: message, Model: "replay"}, nil
}
