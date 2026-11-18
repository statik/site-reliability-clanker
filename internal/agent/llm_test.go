package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type capturedRequest struct {
	path   string
	header http.Header
	body   string
}

// endpoint starts a chat completions server that records the request and gives the answer.
func endpoint(t *testing.T, status int, answer string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	got := &capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		got.path, got.header, got.body = r.URL.Path, r.Header.Clone(), string(raw)
		w.WriteHeader(status)
		if _, err := io.WriteString(w, answer); err != nil {
			t.Errorf("write answer: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server, got
}

const answerWithUsage = `{"model":"served-model","choices":[{"message":{"role":"assistant",
	"content":"{\"sentence\":\"ok\"}"}}],
	"usage":{"prompt_tokens":120,"completion_tokens":30,"cost":0.0042}}`

func oneMessage(model string) Request {
	return Request{Model: model, Messages: []ChatMessage{{Role: "user", Content: "hello"}}}
}

func TestOpenRouterRequestKeepsItsShape(t *testing.T) {
	server, got := endpoint(t, http.StatusOK, answerWithUsage)
	client := NewOpenRouter("sk-or-test")
	client.BaseURL = server.URL
	resp, err := client.Complete(context.Background(), oneMessage("anthropic/claude-haiku-5.5"))
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	wantBody := `{"messages":[{"role":"user","content":"hello"}],` +
		`"model":"anthropic/claude-haiku-5.5","usage":{"include":true}}`
	if got.body != wantBody {
		t.Errorf("body = %s, want %s", got.body, wantBody)
	}
	if got.path != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", got.path)
	}
	wantHeaders := map[string]string{"Authorization": "Bearer sk-or-test",
		"Content-Type": "application/json", "X-Title": "site-reliability-clanker"}
	for name, want := range wantHeaders {
		if value := got.header.Get(name); value != want {
			t.Errorf("header %s = %q, want %q", name, value, want)
		}
	}
	if client.Mode() != "openrouter" {
		t.Errorf("mode = %q, want openrouter", client.Mode())
	}
	if client.client.Timeout != 120*time.Second {
		t.Errorf("timeout = %s, want 2m0s", client.client.Timeout)
	}
	if resp.CostUSD != 0.0042 || resp.PromptTokens != 120 || resp.CompletionTokens != 30 {
		t.Errorf("usage = %+v, want 120 prompt tokens, 30 completion tokens, cost 0.0042", resp)
	}
}

func TestOpenRouterErrorNamesTheEndpointAndTheModel(t *testing.T) {
	server, _ := endpoint(t, http.StatusUnauthorized, `{"error":"no such key"}`)
	client := NewOpenRouter("sk-or-test")
	client.BaseURL = server.URL
	_, err := client.Complete(context.Background(), oneMessage("some/model"))
	if err == nil {
		t.Fatal("Complete gave no error for status 401")
	}
	for _, part := range []string{"OpenRouter answered 401", "some/model", "no such key"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not contain %q", err, part)
		}
	}
}

func TestLocalRequestHasNoOpenRouterFields(t *testing.T) {
	server, got := endpoint(t, http.StatusOK, `{"model":"qwen","choices":[{"message":
		{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":7,"completion_tokens":3}}`)
	client := NewLocal(server.URL+"/v1/", "")
	resp, err := client.Complete(context.Background(), oneMessage("qwen"))
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	wantBody := `{"max_tokens":8192,"messages":[{"role":"user","content":"hello"}],` +
		`"model":"qwen"}`
	if got.body != wantBody {
		t.Errorf("body = %s, want %s", got.body, wantBody)
	}
	if got.path != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", got.path)
	}
	for _, name := range []string{"Authorization", "X-Title"} {
		if _, present := got.header[name]; present {
			t.Errorf("the request has the header %s = %q", name, got.header.Get(name))
		}
	}
	if client.Mode() != "local" {
		t.Errorf("mode = %q, want local", client.Mode())
	}
	if client.client.Timeout != 10*time.Minute {
		t.Errorf("timeout = %s, want 10m0s", client.client.Timeout)
	}
	if resp.PromptTokens != 7 || resp.CompletionTokens != 3 || resp.CostUSD != 0 {
		t.Errorf("usage = %+v, want 7 prompt tokens, 3 completion tokens, no cost", resp)
	}
	if resp.Message.Content != "hi" || resp.Model != "qwen" {
		t.Errorf("answer = %q from %q, want hi from qwen", resp.Message.Content, resp.Model)
	}
}

func TestLocalRequestSendsTheKeyWhenOneIsSet(t *testing.T) {
	server, got := endpoint(t, http.StatusOK, answerWithUsage)
	client := NewLocal(server.URL, "local-key")
	if _, err := client.Complete(context.Background(), oneMessage("qwen")); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if value := got.header.Get("Authorization"); value != "Bearer local-key" {
		t.Errorf("header Authorization = %q, want Bearer local-key", value)
	}
}

func TestAnAnswerWithoutAModelNameGetsTheRequestedModel(t *testing.T) {
	server, _ := endpoint(t, http.StatusOK,
		`{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
	resp, err := NewLocal(server.URL, "").Complete(context.Background(), oneMessage("qwen"))
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Model != "qwen" {
		t.Errorf("model = %q, want qwen", resp.Model)
	}
}

func TestLocalErrorNamesTheEndpoint(t *testing.T) {
	server, _ := endpoint(t, http.StatusNotFound, `{"error":"model not found"}`)
	_, err := NewLocal(server.URL, "").Complete(context.Background(), oneMessage("absent"))
	if err == nil {
		t.Fatal("Complete gave no error for status 404")
	}
	for _, part := range []string{server.URL, "answered 404", "absent", "model not found"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not contain %q", err, part)
		}
	}
}

func TestAnAnswerWithNoChoicesIsAnError(t *testing.T) {
	server, _ := endpoint(t, http.StatusOK, `{"choices":[]}`)
	_, err := NewLocal(server.URL, "").Complete(context.Background(), oneMessage("qwen"))
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Errorf("error = %v, want one that says no choices", err)
	}
}

func TestBudgetReached(t *testing.T) {
	a := &Agent{DailyBudgetUSD: 5, DailyBudgetTokens: 1000}
	cases := []struct {
		name string
		used usage
		want string
	}{
		{"below both limits", usage{spentUSD: 4.99, unpricedTokens: 999}, ""},
		{"at the limit in USD", usage{spentUSD: 5}, "5.00 USD"},
		{"at the limit in tokens", usage{unpricedTokens: 1000}, "1000 tokens"},
		{"above the limit in tokens", usage{spentUSD: 1, unpricedTokens: 4000}, "1000 tokens"},
	}
	for _, tc := range cases {
		summary, err := a.budgetReached(tc.used)
		if tc.want == "" {
			if err != nil || summary != "" {
				t.Errorf("%s: stopped with %q, %v", tc.name, summary, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) || summary == "" {
			t.Errorf("%s: summary %q, error %v, want an error with %q", tc.name, summary, err,
				tc.want)
		}
	}
	unlimited := &Agent{}
	if _, err := unlimited.budgetReached(usage{spentUSD: 99, unpricedTokens: 1 << 40}); err != nil {
		t.Errorf("an agent with no limits stopped: %v", err)
	}
}
