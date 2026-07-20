package openai

import (
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	agent "github.com/prasenjit-net/go-agent"
)

func toParams(req *agent.Request) (openai.ChatCompletionNewParams, error) {
	messages, err := toMessages(req)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	params := openai.ChatCompletionNewParams{
		Model:               shared.ChatModel(req.Model),
		Messages:            messages,
		MaxCompletionTokens: openai.Int(int64(maxTokens)),
	}
	if len(req.Tools) > 0 {
		params.Tools = toTools(req.Tools)
		params.ToolChoice = toToolChoice(req.ToolChoice)
	}
	if req.Thinking != nil {
		if effort, ok := toReasoningEffort(*req.Thinking); ok {
			params.ReasoningEffort = effort
		}
	}
	return params, nil
}

// toMessages translates the unified system blocks and message history into
// a single Chat Completions message list. System blocks concatenate into
// one leading `system` message (Cacheable is a no-op — OpenAI's prompt
// caching needs no explicit hint). A user-role Message that is entirely
// ToolResultBlocks (exactly how the Agent run loop builds a tool-result
// turn) becomes one `tool`-role message per result, since Chat Completions
// has no bundled multi-tool-result message shape.
func toMessages(req *agent.Request) ([]openai.ChatCompletionMessageParamUnion, error) {
	var out []openai.ChatCompletionMessageParamUnion

	if len(req.System) > 0 {
		var sb strings.Builder
		for i, s := range req.System {
			if i > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(s.Text)
		}
		out = append(out, openai.SystemMessage(sb.String()))
	}

	for _, m := range req.Messages {
		switch m.Role {
		case agent.RoleUser:
			msgs, err := toUserMessages(m)
			if err != nil {
				return nil, err
			}
			out = append(out, msgs...)

		case agent.RoleAssistant:
			msg, err := toAssistantMessage(m)
			if err != nil {
				return nil, err
			}
			out = append(out, msg)

		default:
			return nil, fmt.Errorf("openai: unsupported message role %q", m.Role)
		}
	}
	return out, nil
}

func toUserMessages(m agent.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	var out []openai.ChatCompletionMessageParamUnion
	var parts []openai.ChatCompletionContentPartUnionParam

	flush := func() {
		if len(parts) > 0 {
			out = append(out, openai.UserMessage(parts))
			parts = nil
		}
	}

	for _, b := range m.Content {
		if trb, ok := b.(agent.ToolResultBlock); ok {
			flush()
			out = append(out, openai.ToolMessage(flattenText(trb.Content), trb.ToolUseID))
			continue
		}
		part, err := toContentPart(b)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	flush()
	return out, nil
}

func toContentPart(b agent.ContentBlock) (openai.ChatCompletionContentPartUnionParam, error) {
	switch v := b.(type) {
	case agent.TextBlock:
		return openai.TextContentPart(v.Text), nil

	case agent.ImageBlock:
		var url string
		switch v.Source.Kind {
		case agent.SourceURL:
			url = v.Source.Data
		case agent.SourceBase64:
			url = fmt.Sprintf("data:%s;base64,%s", v.Source.MediaType, v.Source.Data)
		default:
			return openai.ChatCompletionContentPartUnionParam{}, fmt.Errorf("openai: unsupported image source kind %q", v.Source.Kind)
		}
		return openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: url}), nil

	case agent.DocumentBlock:
		if v.Source.Kind != agent.SourceBase64 {
			return openai.ChatCompletionContentPartUnionParam{}, fmt.Errorf("openai: document content requires base64-encoded data")
		}
		return openai.FileContentPart(openai.ChatCompletionContentPartFileFileParam{
			FileData: openai.String(fmt.Sprintf("data:%s;base64,%s", v.Source.MediaType, v.Source.Data)),
			Filename: openai.String(v.Title),
		}), nil

	default:
		return openai.ChatCompletionContentPartUnionParam{}, fmt.Errorf("openai: unsupported user content block type %T", b)
	}
}

func toAssistantMessage(m agent.Message) (openai.ChatCompletionMessageParamUnion, error) {
	var text strings.Builder
	var toolCalls []openai.ChatCompletionMessageToolCallUnionParam

	for _, b := range m.Content {
		switch v := b.(type) {
		case agent.TextBlock:
			text.WriteString(v.Text)
		case agent.ThinkingBlock:
			// Chat Completions has no user-suppliable "thinking" content to
			// echo back; reasoning is handled server-side per turn. Skip.
		case agent.ToolUseBlock:
			args := "{}"
			if len(v.Input) > 0 {
				args = string(v.Input)
			}
			toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: v.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      v.Name,
						Arguments: args,
					},
				},
			})
		default:
			return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("openai: unsupported assistant content block type %T", b)
		}
	}

	msg := openai.ChatCompletionAssistantMessageParam{ToolCalls: toolCalls}
	if text.Len() > 0 {
		msg.Content = openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.String(text.String())}
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &msg}, nil
}

// flattenText concatenates every TextBlock in blocks. OpenAI's ToolMessage
// helper takes a plain string; every ToolResult built via
// agent.TextResult/JSONResult/ErrorResultf produces a single TextBlock, so
// this covers the common case.
func flattenText(blocks []agent.ContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if tb, ok := b.(agent.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}

func toTools(tools []agent.RegisteredTool) []openai.ChatCompletionToolUnionParam {
	out := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		sch := t.Schema()
		params := shared.FunctionParameters{
			"type":       "object",
			"properties": sch.Properties,
		}
		if len(sch.Required) > 0 {
			params["required"] = sch.Required
		}
		out = append(out, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        t.Name(),
			Description: openai.String(t.Description()),
			Parameters:  params,
		}))
	}
	return out
}

func toToolChoice(tc agent.ToolChoice) openai.ChatCompletionToolChoiceOptionUnionParam {
	switch tc.Mode {
	case agent.ToolChoiceAny:
		return openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String("required")}
	case agent.ToolChoiceOne:
		return openai.ToolChoiceOptionFunctionToolChoice(openai.ChatCompletionNamedToolChoiceFunctionParam{Name: tc.Name})
	case agent.ToolChoiceNone:
		return openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String("none")}
	default:
		return openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String("auto")}
	}
}

// toReasoningEffort maps our provider-agnostic ThinkingConfig onto OpenAI's
// reasoning_effort parameter. There is no literal "adaptive" concept on
// OpenAI's side, so ThinkingAdaptive maps to "medium" as a reasonable
// default; ThinkingBudgeted approximates a token budget onto the nearest
// effort tier, since OpenAI has no fixed-token-budget thinking mode. ok is
// false for ThinkingOff, leaving the field unset (regular chat models
// ignore it; reasoning models fall back to their own default).
func toReasoningEffort(cfg agent.ThinkingConfig) (shared.ReasoningEffort, bool) {
	switch cfg.Mode {
	case agent.ThinkingAdaptive:
		return shared.ReasoningEffortMedium, true
	case agent.ThinkingBudgeted:
		switch {
		case cfg.Budget <= 4000:
			return shared.ReasoningEffortLow, true
		case cfg.Budget <= 16000:
			return shared.ReasoningEffortMedium, true
		default:
			return shared.ReasoningEffortHigh, true
		}
	default:
		return "", false
	}
}

func fromChatCompletion(resp *openai.ChatCompletion) (*agent.Response, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai: response contained no choices")
	}
	choice := resp.Choices[0]

	var blocks []agent.ContentBlock
	if choice.Message.Content != "" {
		blocks = append(blocks, agent.TextBlock{Text: choice.Message.Content})
	}
	for _, tc := range choice.Message.ToolCalls {
		fn := tc.AsFunction()
		var input []byte
		if fn.Function.Arguments != "" {
			input = []byte(fn.Function.Arguments)
		}
		blocks = append(blocks, agent.ToolUseBlock{ID: fn.ID, Name: fn.Function.Name, Input: input})
	}

	return &agent.Response{
		ID:         resp.ID,
		Model:      resp.Model,
		Message:    agent.Message{Role: agent.RoleAssistant, Content: blocks},
		StopReason: fromFinishReason(choice.FinishReason),
		Usage:      fromUsage(resp.Usage),
		Raw:        resp,
	}, nil
}

func fromFinishReason(reason string) agent.StopReason {
	switch reason {
	case "stop":
		return agent.StopEndTurn
	case "length":
		return agent.StopMaxTokens
	case "tool_calls", "function_call":
		return agent.StopToolUse
	case "content_filter":
		return agent.StopContentFilter
	default:
		return agent.StopUnknown
	}
}

func fromUsage(u openai.CompletionUsage) agent.Usage {
	return agent.Usage{
		InputTokens:     int(u.PromptTokens),
		OutputTokens:    int(u.CompletionTokens),
		CacheReadTokens: int(u.PromptTokensDetails.CachedTokens),
	}
}
