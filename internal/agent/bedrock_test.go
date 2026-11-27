package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go"
)

// fakeConverse records each request and gives the next answer, or the error.
type fakeConverse struct {
	requests []*bedrockruntime.ConverseInput
	answers  []*bedrockruntime.ConverseOutput
	err      error
}

func (f *fakeConverse) Converse(_ context.Context, in *bedrockruntime.ConverseInput,
	_ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	f.requests = append(f.requests, in)
	if f.err != nil {
		return nil, f.err
	}
	answer := f.answers[0]
	f.answers = f.answers[1:]
	return answer, nil
}

func answer(stop types.StopReason, input, output int32,
	blocks ...types.ContentBlock) *bedrockruntime.ConverseOutput {
	return &bedrockruntime.ConverseOutput{StopReason: stop,
		Output: &types.ConverseOutputMemberMessage{Value: types.Message{
			Role: types.ConversationRoleAssistant, Content: blocks}},
		Usage: &types.TokenUsage{InputTokens: aws.Int32(input), OutputTokens: aws.Int32(output)}}
}

func text(value string) types.ContentBlock { return &types.ContentBlockMemberText{Value: value} }

func thinking(signature string) types.ContentBlock {
	return &types.ContentBlockMemberReasoningContent{
		Value: &types.ReasoningContentBlockMemberReasoningText{
			Value: types.ReasoningTextBlock{Text: aws.String(""), Signature: aws.String(signature)}}}
}

func toolUse(id, name string, input map[string]any) types.ContentBlock {
	return &types.ContentBlockMemberToolUse{Value: types.ToolUseBlock{ToolUseId: aws.String(id),
		Name: aws.String(name), Input: document.NewLazyDocument(input)}}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func TestBedrockSendsTheSystemPromptAndReadsTheAnswerAndItsCost(t *testing.T) {
	api := &fakeConverse{answers: []*bedrockruntime.ConverseOutput{
		answer(types.StopReasonEndTurn, 1000, 200, thinking("sig-1"), text(`{"sentence":"ok"}`))}}
	client := &Bedrock{API: api, Region: "us-west-2"}
	resp, err := client.Complete(context.Background(), Request{
		Model: "us.anthropic.claude-sonnet-5-5", Messages: []ChatMessage{
			{Role: "system", Content: "You explain changes."},
			{Role: "user", Content: "<data>{}</data>"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	in := api.requests[0]
	if aws.ToString(in.ModelId) != "us.anthropic.claude-sonnet-5-5" ||
		aws.ToInt32(in.InferenceConfig.MaxTokens) != bedrockMaxTokens || in.ToolConfig != nil {
		t.Errorf("request = model %s, max tokens %d, tools %v", aws.ToString(in.ModelId),
			aws.ToInt32(in.InferenceConfig.MaxTokens), in.ToolConfig)
	}
	system, ok := in.System[0].(*types.SystemContentBlockMemberText)
	if len(in.System) != 1 || !ok || system.Value != "You explain changes." {
		t.Errorf("system = %#v, want the system message", in.System)
	}
	if len(in.Messages) != 1 || in.Messages[0].Role != types.ConversationRoleUser {
		t.Errorf("messages = %#v, want one user message", in.Messages)
	}
	if resp.Message.Content != `{"sentence":"ok"}` || resp.Message.Role != "assistant" ||
		resp.Model != "us.anthropic.claude-sonnet-5-5" {
		t.Errorf("answer = %q from %q", resp.Message.Content, resp.Model)
	}
	// 1000 tokens at 2.20 USD and 200 tokens at 11.00 USD for each million.
	if resp.PromptTokens != 1000 || resp.CompletionTokens != 200 || !near(resp.CostUSD, 0.0044) {
		t.Errorf("usage = %d, %d tokens, %g USD; want 1000, 200, 0.0044", resp.PromptTokens,
			resp.CompletionTokens, resp.CostUSD)
	}
	if client.Mode() != "bedrock" {
		t.Errorf("mode = %q, want bedrock", client.Mode())
	}
	record, err := json.Marshal(resp.Message)
	if err != nil || strings.Contains(string(record), "sig-1") {
		t.Errorf("the record of the answer holds the thinking block: %s, %v", record, err)
	}
}

func TestBedrockToolCallRoundTripSendsTheTurnBackUnchanged(t *testing.T) {
	turn := []types.ContentBlock{thinking("sig-2"), text("I read the file first."),
		toolUse("tool-1", "read_file", map[string]any{"repo": "platform", "path": "a.tf"}),
		toolUse("tool-2", "read_file", map[string]any{"repo": "platform", "path": "b.tf"})}
	api := &fakeConverse{answers: []*bedrockruntime.ConverseOutput{
		answer(types.StopReasonToolUse, 50, 20, turn...),
		answer(types.StopReasonEndTurn, 90, 10, text("done"))}}
	client := &Bedrock{API: api, Region: "us-west-2"}
	tools := []Tool{tool("read_file", "Read a file.", map[string]any{
		"repo": stringProp("repository"), "path": stringProp("path")}, "repo", "path")}
	messages := []ChatMessage{{Role: "system", Content: "You are the Author."},
		{Role: "user", Content: "<data>{}</data>"}}
	first, err := client.Complete(context.Background(), Request{Model: "m", Messages: messages,
		Tools: tools})
	if err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	calls := first.Message.ToolCalls
	if len(calls) != 2 || calls[0].ID != "tool-1" || calls[0].Function.Name != "read_file" ||
		calls[0].Type != "function" || first.Message.Content != "I read the file first." {
		t.Fatalf("first answer = %+v, want two read_file calls and the text", first.Message)
	}
	var arguments map[string]string
	if err := json.Unmarshal([]byte(calls[1].Function.Arguments), &arguments); err != nil ||
		arguments["path"] != "b.tf" {
		t.Errorf("arguments = %s, %v; want the path b.tf", calls[1].Function.Arguments, err)
	}
	// The agent scrubs the answer before the loop reads it. The turn goes back as Bedrock
	// wrote it all the same.
	first.Message.Content = "[REDACTED]"
	messages = append(messages, first.Message,
		ChatMessage{Role: "tool", ToolCallID: "tool-1", Content: "content of a"},
		ChatMessage{Role: "tool", ToolCallID: "tool-2", Content: ""})
	if _, err := client.Complete(context.Background(), Request{Model: "m", Messages: messages,
		Tools: tools}); err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	in := api.requests[1]
	if len(in.Messages) != 3 {
		t.Fatalf("messages = %d, want the user, the turn and one message of results",
			len(in.Messages))
	}
	sent := in.Messages[1]
	if sent.Role != types.ConversationRoleAssistant || len(sent.Content) != len(turn) {
		t.Fatalf("turn = %#v, want the four blocks of Bedrock", sent)
	}
	for index := range turn {
		if sent.Content[index] != turn[index] {
			t.Errorf("block %d of the turn changed: %#v", index, sent.Content[index])
		}
	}
	results := in.Messages[2]
	if results.Role != types.ConversationRoleUser || len(results.Content) != 2 {
		t.Fatalf("results = %#v, want one user message with two results", results)
	}
	for index, want := range []string{"content of a", "(no output)"} {
		result, ok := results.Content[index].(*types.ContentBlockMemberToolResult)
		if !ok {
			t.Fatalf("block %d of the results is %T", index, results.Content[index])
		}
		got := result.Value.Content[0].(*types.ToolResultContentBlockMemberText).Value
		if aws.ToString(result.Value.ToolUseId) != calls[index].ID || got != want {
			t.Errorf("result %d = %s %q, want %s %q", index,
				aws.ToString(result.Value.ToolUseId), got, calls[index].ID, want)
		}
	}
	spec := in.ToolConfig.Tools[0].(*types.ToolMemberToolSpec).Value
	if aws.ToString(spec.Name) != "read_file" || aws.ToString(spec.Description) != "Read a file." {
		t.Errorf("tool = %s: %s", aws.ToString(spec.Name), aws.ToString(spec.Description))
	}
}

func TestBedrockBuildsATurnThatItDidNotWrite(t *testing.T) {
	call := ToolCall{ID: "call-1", Type: "function"}
	call.Function.Name, call.Function.Arguments = "set_tfvar", `{"layer":"50-app"}`
	api := &fakeConverse{answers: []*bedrockruntime.ConverseOutput{
		answer(types.StopReasonEndTurn, 1, 1, text("ok"))}}
	client := &Bedrock{API: api, Region: "us-west-2"}
	_, err := client.Complete(context.Background(), Request{Model: "m", Messages: []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "  ", ToolCalls: []ToolCall{call}},
		{Role: "tool", ToolCallID: "call-1", Content: "set"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	turn := api.requests[0].Messages[1].Content
	use, ok := turn[0].(*types.ContentBlockMemberToolUse)
	if len(turn) != 1 || !ok || aws.ToString(use.Value.Name) != "set_tfvar" {
		t.Fatalf("turn = %#v, want only the tool call: Converse refuses an empty text", turn)
	}
	input, err := use.Value.Input.MarshalSmithyDocument()
	if err != nil || string(input) != `{"layer":"50-app"}` {
		t.Errorf("input = %s, %v", input, err)
	}
	call.Function.Arguments = "not json"
	_, err = client.Complete(context.Background(), Request{Model: "m", Messages: []ChatMessage{
		{Role: "user", Content: "hello"}, {Role: "assistant", ToolCalls: []ToolCall{call}}}})
	if err == nil || !strings.Contains(err.Error(), "call-1") {
		t.Errorf("error = %v, want one that names the tool call", err)
	}
}

func TestBedrockErrorsNameTheModelTheRegionAndTheFix(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want []string
	}{
		{"access", &types.AccessDeniedException{Message: aws.String("not authorized")},
			[]string{"refused", "bedrock:InvokeModel", "not authorized"}},
		{"throttling", &types.ThrottlingException{Message: aws.String("too many tokens")},
			[]string{"throttled", "Service Quotas", "too many tokens"}},
		{"quota", &types.ServiceQuotaExceededException{Message: aws.String("quota")},
			[]string{"throttled", "Service Quotas"}},
		{"unknown model", &types.ValidationException{Message: aws.String("invalid model")},
			[]string{"did not take", "inference profile", "invalid model"}},
		{"network", errors.New("connection reset"), []string{"connection reset"}},
	}
	for _, tc := range cases {
		client := &Bedrock{API: &fakeConverse{err: tc.err}, Region: "eu-west-1"}
		_, err := client.Complete(context.Background(), oneMessage("eu.anthropic.claude-haiku-5-5"))
		if err == nil {
			t.Errorf("%s: no error", tc.name)
			continue
		}
		for _, part := range append(tc.want, "eu-west-1", "eu.anthropic.claude-haiku-5-5") {
			if !strings.Contains(err.Error(), part) {
				t.Errorf("%s: error %q does not contain %q", tc.name, err, part)
			}
		}
		if !errors.Is(err, tc.err) {
			t.Errorf("%s: the error does not wrap the error of the SDK", tc.name)
		}
	}
}

func TestBedrockRefusesAnAnswerThatStoppedEarly(t *testing.T) {
	cases := map[types.StopReason]string{
		types.StopReasonMaxTokens:       "limit of 16000 tokens",
		types.StopReasonContentFiltered: "content_filtered",
	}
	for stop, want := range cases {
		api := &fakeConverse{answers: []*bedrockruntime.ConverseOutput{
			answer(stop, 5, 16000, text(`{"sent`))}}
		_, err := (&Bedrock{API: api, Region: "us-west-2"}).Complete(context.Background(),
			oneMessage("m"))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %v, want one with %q", stop, err, want)
		}
	}
	empty := &fakeConverse{answers: []*bedrockruntime.ConverseOutput{
		{StopReason: types.StopReasonEndTurn}}}
	_, err := (&Bedrock{API: empty, Region: "us-west-2"}).Complete(context.Background(),
		oneMessage("m"))
	if err == nil || !strings.Contains(err.Error(), "no message") {
		t.Errorf("error = %v, want one that says no message", err)
	}
}

func TestBedrockCost(t *testing.T) {
	cases := []struct {
		model          string
		prompt, answer int
		want           float64
	}{
		{"us.anthropic.claude-haiku-5-5", 100_000, 1_000_000, 0.011 + 0.55},
		{"global.anthropic.claude-haiku-5-5", 100_000, 1_000_000, 0.010 + 0.50},
		{"us.anthropic.claude-haiku-5-5", 100_001, 1_000_000, 100_001*0.55/1e6 + 2.75},
		{"global.anthropic.claude-haiku-5-5", 200_000, 0, 0.10},
		{"global.anthropic.claude-sonnet-5-5", 1_000_000, 1_000_000, 12},
		{"us.moonshotai.kimi-k3.1", 1_000_000, 1_000_000, 0},
	}
	for _, tc := range cases {
		if got := bedrockCost(tc.model, tc.prompt, tc.answer); !near(got, tc.want) {
			t.Errorf("cost of %s with %d and %d tokens = %g, want %g", tc.model, tc.prompt,
				tc.answer, got, tc.want)
		}
	}
}

// TestBedrockWireFormat runs the round trip through the SDK against a stand-in on this
// machine, so that the blocks of a turn are sent back in the shape that Converse reads.
func TestBedrockWireFormat(t *testing.T) {
	answers := []string{
		`{"stopReason":"tool_use","usage":{"inputTokens":40,"outputTokens":12},
		"output":{"message":{"role":"assistant","content":[
		{"reasoningContent":{"reasoningText":{"text":"","signature":"sig-wire"}}},
		{"toolUse":{"toolUseId":"tu-1","name":"read_file","input":{"path":"a.tf"}}}]}}}`,
		`{"stopReason":"end_turn","usage":{"inputTokens":60,"outputTokens":3},
		"output":{"message":{"role":"assistant","content":[{"text":"done"}]}}}`,
	}
	var bodies, paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read the request: %v", err)
		}
		bodies, paths = append(bodies, string(raw)), append(paths, r.URL.EscapedPath())
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, answers[len(bodies)-1]); err != nil {
			t.Errorf("write the answer: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	api := bedrockruntime.New(bedrockruntime.Options{Region: "us-west-2",
		BaseEndpoint: aws.String(server.URL),
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", "")})
	client := &Bedrock{API: api, Region: "us-west-2"}
	tools := []Tool{tool("read_file", "Read a file.", map[string]any{
		"path": stringProp("path")}, "path")}
	messages := []ChatMessage{{Role: "system", Content: "sys"}, {Role: "user", Content: "go"}}
	first, err := client.Complete(context.Background(), Request{Model: "us.anthropic.claude-sonnet-5-5",
		Messages: messages, Tools: tools})
	if err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	if len(first.Message.ToolCalls) != 1 || first.Message.ToolCalls[0].Function.Arguments !=
		`{"path":"a.tf"}` {
		t.Fatalf("tool calls = %+v", first.Message.ToolCalls)
	}
	messages = append(messages, first.Message,
		ChatMessage{Role: "tool", ToolCallID: "tu-1", Content: "content"})
	second, err := client.Complete(context.Background(), Request{
		Model: "us.anthropic.claude-sonnet-5-5", Messages: messages, Tools: tools})
	if err != nil || second.Message.Content != "done" {
		t.Fatalf("second Complete = %q, %v", second.Message.Content, err)
	}
	if paths[0] != "/model/us.anthropic.claude-sonnet-5-5/converse" {
		t.Errorf("path = %s", paths[0])
	}
	var body struct {
		Messages []struct {
			Role    string           `json:"role"`
			Content []map[string]any `json:"content"`
		} `json:"messages"`
		System     []map[string]string `json:"system"`
		ToolConfig struct {
			Tools []map[string]any `json:"tools"`
		} `json:"toolConfig"`
		InferenceConfig map[string]int `json:"inferenceConfig"`
	}
	if err := json.Unmarshal([]byte(bodies[1]), &body); err != nil {
		t.Fatalf("decode %s: %v", bodies[1], err)
	}
	turn, err := json.Marshal(body.Messages[1])
	if err != nil {
		t.Fatal(err)
	}
	want := `{"role":"assistant","content":[{"reasoningContent":{"reasoningText":` +
		`{"signature":"sig-wire","text":""}}},{"toolUse":{"input":{"path":"a.tf"},"name":` +
		`"read_file","toolUseId":"tu-1"}}]}`
	if string(turn) != want {
		t.Errorf("turn sent back = %s\nwant %s", turn, want)
	}
	result, err := json.Marshal(body.Messages[2])
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"role":"user","content":[{"toolResult":{"content":[{"text":"content"}],` +
		`"toolUseId":"tu-1"}}]}`; string(result) != want {
		t.Errorf("results = %s\nwant %s", result, want)
	}
	if len(body.System) != 1 || body.System[0]["text"] != "sys" ||
		body.InferenceConfig["maxTokens"] != bedrockMaxTokens || len(body.ToolConfig.Tools) != 1 {
		t.Errorf("request = %s", bodies[1])
	}
}

// TestBedrockThrottlingFromTheService checks that the error of the service reaches the
// caller as a ThrottlingException after the retries of the SDK.
func TestBedrockThrottlingFromTheService(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Amzn-ErrorType", "ThrottlingException")
		w.WriteHeader(http.StatusTooManyRequests)
		if _, err := io.WriteString(w, `{"message":"Too many tokens, please wait."}`); err != nil {
			t.Errorf("write the answer: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	api := bedrockruntime.New(bedrockruntime.Options{Region: "us-west-2",
		BaseEndpoint: aws.String(server.URL), RetryMaxAttempts: 2,
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", "")})
	_, err := (&Bedrock{API: api, Region: "us-west-2"}).Complete(context.Background(),
		oneMessage("us.anthropic.claude-haiku-5-5"))
	var throttled *types.ThrottlingException
	var apiErr smithy.APIError
	if !errors.As(err, &throttled) || !errors.As(err, &apiErr) ||
		!strings.Contains(err.Error(), "Service Quotas") {
		t.Errorf("error = %v, want a ThrottlingException with the fix", err)
	}
	if calls != 2 {
		t.Errorf("the SDK called %d times, want 2: it retries a throttled call", calls)
	}
}
