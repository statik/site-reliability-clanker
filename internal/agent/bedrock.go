package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// ModeBedrock is the mode of the calls to Amazon Bedrock.
const ModeBedrock = "bedrock"

const (
	// bedrockMaxTokens is the answer limit of a Bedrock call. The models think before they
	// answer, and the thinking counts against the same limit.
	bedrockMaxTokens = 16000
	// bedrockTimeout limits one call, with the retries of the SDK.
	bedrockTimeout = 5 * time.Minute
)

// ConverseAPI is the one call of the Bedrock runtime that the client uses.
type ConverseAPI interface {
	Converse(ctx context.Context, in *bedrockruntime.ConverseInput,
		options ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

// NewBedrockRuntime returns the Bedrock runtime in the region, with the credentials of the
// role of the instance. It makes no call.
func NewBedrockRuntime(region string) ConverseAPI {
	return bedrockruntime.New(bedrockruntime.Options{Region: region,
		Credentials: aws.NewCredentialsCache(ec2rolecreds.New())})
}

// Bedrock calls the Converse API of Amazon Bedrock in one region. A model of a request is
// the ID of an inference profile, for example us.anthropic.claude-sonnet-5-5.
type Bedrock struct {
	API    ConverseAPI
	Region string
}

// Mode implements LLM.
func (b *Bedrock) Mode() string { return ModeBedrock }

// Complete implements LLM.
func (b *Bedrock) Complete(ctx context.Context, req Request) (Response, error) {
	in, err := converseInput(req)
	if err != nil {
		return Response{}, fmt.Errorf("build the Bedrock request for model %s: %w", req.Model, err)
	}
	ctx, cancel := context.WithTimeout(ctx, bedrockTimeout)
	defer cancel()
	out, err := b.API.Converse(ctx, in)
	if err != nil {
		return Response{}, b.callError(req.Model, err)
	}
	reply, ok := out.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return Response{}, fmt.Errorf("Amazon Bedrock in %s gave no message for model %s",
			b.Region, req.Model)
	}
	switch out.StopReason {
	case types.StopReasonEndTurn, types.StopReasonToolUse, types.StopReasonStopSequence:
	case types.StopReasonMaxTokens:
		return Response{}, fmt.Errorf("the answer of model %s reached the limit of %d tokens, "+
			"so it is cut", req.Model, bedrockMaxTokens)
	default:
		return Response{}, fmt.Errorf("Amazon Bedrock stopped the answer of model %s: %s",
			req.Model, out.StopReason)
	}
	message, err := chatMessage(reply.Value)
	if err != nil {
		return Response{}, fmt.Errorf("read the answer of model %s: %w", req.Model, err)
	}
	resp := Response{Message: message, Model: req.Model}
	if out.Usage != nil {
		resp.PromptTokens = int(aws.ToInt32(out.Usage.InputTokens))
		resp.CompletionTokens = int(aws.ToInt32(out.Usage.OutputTokens))
	}
	resp.CostUSD = bedrockCost(req.Model, resp.PromptTokens, resp.CompletionTokens)
	return resp, nil
}

// callError names the model, the region and what to do for the errors of Bedrock that a
// setting or an account can cause. The SDK has retried a throttled call before this.
func (b *Bedrock) callError(model string, err error) error {
	where := fmt.Sprintf("Amazon Bedrock in %s with model %s", b.Region, model)
	var (
		denied    *types.AccessDeniedException
		throttled *types.ThrottlingException
		quota     *types.ServiceQuotaExceededException
		notFound  *types.ResourceNotFoundException
		invalid   *types.ValidationException
	)
	switch {
	case errors.As(err, &denied):
		return fmt.Errorf("%s refused the call: %w. The role needs bedrock:InvokeModel on the "+
			"inference profile and on the foundation models that it routes to, and the "+
			"account needs access to the model", where, err)
	case errors.As(err, &throttled), errors.As(err, &quota):
		return fmt.Errorf("%s throttled the call after the retries: %w. Wait, or ask for a "+
			"higher quota of tokens for the model in Service Quotas", where, err)
	case errors.As(err, &notFound), errors.As(err, &invalid):
		return fmt.Errorf("%s did not take the request: %w. Check that the model is the ID "+
			"of an inference profile in %s", where, err, b.Region)
	}
	return fmt.Errorf("call %s: %w", where, err)
}

// converseInput turns a request into a Converse request: the system messages become the
// system prompt, and the tools the tool configuration. Converse wants the roles to
// alternate, so consecutive messages of one role become one message, and the results of
// the tools go back in one user message.
func converseInput(req Request) (*bedrockruntime.ConverseInput, error) {
	in := &bedrockruntime.ConverseInput{ModelId: aws.String(req.Model),
		InferenceConfig: &types.InferenceConfiguration{MaxTokens: aws.Int32(bedrockMaxTokens)}}
	for _, message := range req.Messages {
		if message.Role == "system" {
			in.System = append(in.System, &types.SystemContentBlockMemberText{
				Value: message.Content})
			continue
		}
		role, blocks, err := converseBlocks(message)
		if err != nil {
			return nil, err
		}
		if len(blocks) == 0 {
			continue
		}
		last := len(in.Messages) - 1
		if last >= 0 && in.Messages[last].Role == role {
			in.Messages[last].Content = append(in.Messages[last].Content, blocks...)
			continue
		}
		in.Messages = append(in.Messages, types.Message{Role: role, Content: blocks})
	}
	if len(req.Tools) > 0 {
		in.ToolConfig = &types.ToolConfiguration{}
		for _, tool := range req.Tools {
			in.ToolConfig.Tools = append(in.ToolConfig.Tools, &types.ToolMemberToolSpec{
				Value: types.ToolSpecification{Name: aws.String(tool.Function.Name),
					Description: aws.String(tool.Function.Description),
					InputSchema: &types.ToolInputSchemaMemberJson{
						Value: document.NewLazyDocument(tool.Function.Parameters)}}})
		}
	}
	return in, nil
}

// converseBlocks returns the role and the content of one message. Converse refuses an
// empty text, so an empty text is left out.
func converseBlocks(message ChatMessage) (types.ConversationRole, []types.ContentBlock, error) {
	switch message.Role {
	case "user":
		return types.ConversationRoleUser, textBlocks(message.Content), nil
	case "tool":
		content := message.Content
		if content == "" {
			content = "(no output)"
		}
		return types.ConversationRoleUser, []types.ContentBlock{&types.ContentBlockMemberToolResult{
			Value: types.ToolResultBlock{ToolUseId: aws.String(message.ToolCallID),
				Content: []types.ToolResultContentBlock{
					&types.ToolResultContentBlockMemberText{Value: content}}}}}, nil
	case "assistant":
		// A turn of Bedrock goes back as Bedrock wrote it. Its thinking blocks are bound
		// to the conversation, and Bedrock refuses a turn whose text or tool calls
		// changed. The model gets its own words back, which it wrote before the scrubber
		// read them.
		if message.converse != nil {
			return types.ConversationRoleAssistant, message.converse, nil
		}
		blocks := textBlocks(message.Content)
		for _, call := range message.ToolCalls {
			var input map[string]any
			if err := json.Unmarshal([]byte(call.Function.Arguments), &input); err != nil {
				return "", nil, fmt.Errorf("the arguments of the tool call %s are not a JSON "+
					"object: %w", call.ID, err)
			}
			blocks = append(blocks, &types.ContentBlockMemberToolUse{Value: types.ToolUseBlock{
				ToolUseId: aws.String(call.ID), Name: aws.String(call.Function.Name),
				Input: document.NewLazyDocument(input)}})
		}
		return types.ConversationRoleAssistant, blocks, nil
	}
	return "", nil, fmt.Errorf("a message has the role %q, which Converse does not take",
		message.Role)
}

func textBlocks(text string) []types.ContentBlock {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return []types.ContentBlock{&types.ContentBlockMemberText{Value: text}}
}

// chatMessage reads the answer of Converse: its text and its tool calls. Other blocks,
// such as thinking, stay only in the copy of the turn for the next request.
func chatMessage(reply types.Message) (ChatMessage, error) {
	message := ChatMessage{Role: "assistant", converse: reply.Content}
	var texts []string
	for _, block := range reply.Content {
		switch content := block.(type) {
		case *types.ContentBlockMemberText:
			texts = append(texts, content.Value)
		case *types.ContentBlockMemberToolUse:
			arguments := []byte("{}")
			if content.Value.Input != nil {
				var err error
				if arguments, err = content.Value.Input.MarshalSmithyDocument(); err != nil {
					return ChatMessage{}, fmt.Errorf("encode the input of the tool call %s: %w",
						aws.ToString(content.Value.ToolUseId), err)
				}
			}
			call := ToolCall{ID: aws.ToString(content.Value.ToolUseId), Type: "function"}
			call.Function.Name, call.Function.Arguments = aws.ToString(content.Value.Name),
				string(arguments)
			message.ToolCalls = append(message.ToolCalls, call)
		}
	}
	message.Content = strings.Join(texts, "\n")
	return message, nil
}

// tokenPrices are list prices in USD for one million tokens.
type tokenPrices struct{ input, output float64 }

// modelPrices are the prices of one inference profile. A prompt longer than longPrompt
// tokens costs the long prices for all its tokens. Zero longPrompt means one price.
type modelPrices struct {
	standard   tokenPrices
	longPrompt int
	long       tokenPrices
}

// bedrockPrices are the on-demand list prices of the inference profiles of the default
// models, from the AWS Price List (offer AmazonBedrockFoundationModels, US West (Oregon),
// published 2026-11-23). A geographic profile such as us. costs 10% more than the global
// one. A model that is not here records no cost, so the daily token limit applies to it.
var bedrockPrices = map[string]modelPrices{
	"us.anthropic.claude-haiku-5-5": {standard: tokenPrices{0.11, 0.55}, longPrompt: 100_000,
		long: tokenPrices{0.55, 2.75}},
	"global.anthropic.claude-haiku-5-5": {standard: tokenPrices{0.10, 0.50},
		longPrompt: 100_000, long: tokenPrices{0.50, 2.50}},
	"us.anthropic.claude-sonnet-5-5":     {standard: tokenPrices{2.20, 11.00}},
	"global.anthropic.claude-sonnet-5-5": {standard: tokenPrices{2.00, 10.00}},
}

// HasBedrockPrice reports whether the agent knows the list price of an inference profile.
// The calls of another profile record no cost, so the daily token limit stops them.
func HasBedrockPrice(model string) bool {
	_, found := bedrockPrices[model]
	return found
}

// bedrockCost returns the list price of one call, or zero for a model with no price.
func bedrockCost(model string, promptTokens, completionTokens int) float64 {
	prices, found := bedrockPrices[model]
	if !found {
		return 0
	}
	rate := prices.standard
	if prices.longPrompt > 0 && promptTokens > prices.longPrompt {
		rate = prices.long
	}
	return (float64(promptTokens)*rate.input + float64(completionTokens)*rate.output) / 1e6
}
