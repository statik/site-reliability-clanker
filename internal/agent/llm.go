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

// localMaxTokens is the answer limit for an endpoint that the operator runs. The default
// limit of such an endpoint can cut an answer in the middle of its JSON object.
const localMaxTokens = 8192

// Chat calls the chat completions route of an OpenAI-compatible endpoint.
type Chat struct {
	APIKey  string
	BaseURL string
	// name is the name of the endpoint in an error message.
	name string
	mode string
	// openRouter adds the request fields that only OpenRouter reads.
	openRouter bool
	// maxTokens is the answer limit in the request. Zero sends no limit.
	maxTokens int
	client    http.Client
}

// NewOpenRouter returns a client for the OpenRouter API.
func NewOpenRouter(apiKey string) *Chat {
	return &Chat{APIKey: apiKey, BaseURL: "https://openrouter.ai/api/v1", name: "OpenRouter",
		mode: "openrouter", openRouter: true, client: http.Client{Timeout: 120 * time.Second}}
}

// NewLocal returns a client for an OpenAI-compatible endpoint at baseURL, for example a
// model server on the same host. The key is optional. A local model is slow, so the
// timeout is 10 minutes.
func NewLocal(baseURL, apiKey string) *Chat {
	return &Chat{APIKey: apiKey, BaseURL: strings.TrimRight(baseURL, "/"),
		name: "The model endpoint " + baseURL, mode: "local", maxTokens: localMaxTokens,
		client: http.Client{Timeout: 10 * time.Minute}}
}

// Mode implements LLM.
func (c *Chat) Mode() string { return c.mode }

func (c *Chat) newRequest(ctx context.Context, req Request) (*http.Request, error) {
	payload := map[string]any{"model": req.Model, "messages": req.Messages}
	if c.openRouter {
		payload["usage"] = map[string]any{"include": true}
	}
	if c.maxTokens > 0 {
		payload["max_tokens"] = c.maxTokens
	}
	if len(req.Tools) > 0 {
		payload["tools"] = req.Tools
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode the model request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build the model request: %w", err)
	}
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.openRouter {
		httpReq.Header.Set("X-Title", "site-reliability-clanker")
	}
	return httpReq, nil
}

// Complete implements LLM.
func (c *Chat) Complete(ctx context.Context, req Request) (Response, error) {
	httpReq, err := c.newRequest(ctx, req)
	if err != nil {
		return Response{}, err
	}
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("call %s with model %s: %w", c.name, req.Model, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Response{}, fmt.Errorf("read the response of %s: %w", c.name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("%s answered %d for model %s: %s. Check the key "+
			"and the model name", c.name, resp.StatusCode, req.Model, truncate(string(raw), 300))
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
		return Response{}, fmt.Errorf("decode the response of %s: %w", c.name, err)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("%s gave no choices for model %s", c.name, req.Model)
	}
	// The approval page shows the model name as the source of the text.
	if parsed.Model == "" {
		parsed.Model = req.Model
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
