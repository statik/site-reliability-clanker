package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/scrub"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC) }

// repeater is a model that repeats secrets in its answer and in a tool call.
type repeater struct{ seen []ChatMessage }

func (*repeater) Mode() string { return "test" }

func (r *repeater) Complete(_ context.Context, req Request) (Response, error) {
	r.seen = req.Messages
	call := ToolCall{ID: "call-1", Type: "function"}
	call.Function.Name = "open_pull_request"
	call.Function.Arguments = `{"title":"Rotate","body":"the cell holds complete-known-8812"}`
	return Response{Model: "test-model", Message: ChatMessage{Role: "assistant",
		Content:   "The cell holds complete-known-8812 and the password is hunter2.",
		ToolCalls: []ToolCall{call}}}, nil
}

func TestCompleteScrubsTheInputTheAnswerAndTheStoredRecord(t *testing.T) {
	scrub.Add("complete-known-8812")
	db := storetest.Open(t)
	model := &repeater{}
	a := &Agent{DB: db, Clock: fixedClock{}, LLM: model,
		Models: map[string]string{RoleExplainer: "test-model"}}
	resp, err := a.complete(context.Background(), RoleExplainer, "WI-0001", Request{
		Messages: []ChatMessage{{Role: "user", Content: "| relay | complete-known-8812 |"}}})
	if err != nil {
		t.Fatal(err)
	}
	var transcript string
	if err := db.QueryRow(`SELECT transcript FROM agent_calls WHERE work_item = 'WI-0001'`).
		Scan(&transcript); err != nil {
		t.Fatal(err)
	}
	texts := map[string]string{"input": model.seen[0].Content, "answer": resp.Message.Content,
		"tool arguments": resp.Message.ToolCalls[0].Function.Arguments,
		"stored record":  transcript}
	for name, text := range texts {
		if strings.Contains(text, "complete-known-8812") || strings.Contains(text, "hunter2") {
			t.Errorf("a secret is in the %s: %s", name, text)
		}
		if !strings.Contains(text, "[REDACTED]") {
			t.Errorf("the %s has no marker: %s", name, text)
		}
	}
}
