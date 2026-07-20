package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	agent "github.com/prasenjit-net/go-agent"
)

func toMessageNewParams(req *agent.Request) (anthropic.MessageNewParams, error) {
	messages, err := toMessages(req.Messages)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		Messages:  messages,
		MaxTokens: int64(maxTokens),
	}
	if len(req.System) > 0 {
		params.System = toSystemBlocks(req.System)
	}
	if len(req.Tools) > 0 {
		params.Tools = toTools(req.Tools)
		params.ToolChoice = toToolChoice(req.ToolChoice)
	}
	if req.Thinking != nil {
		params.Thinking = toThinking(*req.Thinking)
	}
	return params, nil
}

func toMessageCountTokensParams(req *agent.Request) (anthropic.MessageCountTokensParams, error) {
	messages, err := toMessages(req.Messages)
	if err != nil {
		return anthropic.MessageCountTokensParams{}, err
	}
	params := anthropic.MessageCountTokensParams{
		Model:    anthropic.Model(req.Model),
		Messages: messages,
	}
	if len(req.System) > 0 {
		params.System = anthropic.MessageCountTokensParamsSystemUnion{OfTextBlockArray: toSystemBlocks(req.System)}
	}
	if len(req.Tools) > 0 {
		tools := make([]anthropic.MessageCountTokensToolUnionParam, 0, len(req.Tools))
		for _, t := range req.Tools {
			sch := t.Schema()
			inputSchema := anthropic.ToolInputSchemaParam{Properties: sch.Properties, Required: sch.Required}
			tu := anthropic.MessageCountTokensToolParamOfTool(inputSchema, t.Name())
			if tu.OfTool != nil {
				tu.OfTool.Description = anthropic.String(t.Description())
			}
			tools = append(tools, tu)
		}
		params.Tools = tools
	}
	return params, nil
}

func toSystemBlocks(blocks []agent.SystemBlock) []anthropic.TextBlockParam {
	out := make([]anthropic.TextBlockParam, 0, len(blocks))
	for _, b := range blocks {
		tb := anthropic.TextBlockParam{Text: b.Text}
		if b.Cacheable {
			tb.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		out = append(out, tb)
	}
	return out
}

func toMessages(msgs []agent.Message) ([]anthropic.MessageParam, error) {
	out := make([]anthropic.MessageParam, 0, len(msgs))
	for _, m := range msgs {
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(m.Content))
		for _, b := range m.Content {
			cb, err := toContentBlock(b)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, cb)
		}
		switch m.Role {
		case agent.RoleUser:
			out = append(out, anthropic.NewUserMessage(blocks...))
		case agent.RoleAssistant:
			out = append(out, anthropic.NewAssistantMessage(blocks...))
		default:
			return nil, fmt.Errorf("claude: unsupported message role %q", m.Role)
		}
	}
	return out, nil
}

func toContentBlock(b agent.ContentBlock) (anthropic.ContentBlockParamUnion, error) {
	switch v := b.(type) {
	case agent.TextBlock:
		return anthropic.NewTextBlock(v.Text), nil

	case agent.ImageBlock:
		switch v.Source.Kind {
		case agent.SourceBase64:
			return anthropic.NewImageBlockBase64(v.Source.MediaType, v.Source.Data), nil
		case agent.SourceURL:
			return anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: v.Source.Data}), nil
		default:
			return anthropic.ContentBlockParamUnion{}, fmt.Errorf("claude: unsupported image source kind %q", v.Source.Kind)
		}

	case agent.DocumentBlock:
		var block anthropic.ContentBlockParamUnion
		switch v.Source.Kind {
		case agent.SourceBase64:
			block = anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{Data: v.Source.Data})
		case agent.SourceURL:
			block = anthropic.NewDocumentBlock(anthropic.URLPDFSourceParam{URL: v.Source.Data})
		default:
			return anthropic.ContentBlockParamUnion{}, fmt.Errorf("claude: unsupported document source kind %q", v.Source.Kind)
		}
		if v.Title != "" && block.OfDocument != nil {
			block.OfDocument.Title = anthropic.String(v.Title)
		}
		return block, nil

	case agent.ToolUseBlock:
		var input any
		if len(v.Input) > 0 {
			if err := json.Unmarshal(v.Input, &input); err != nil {
				return anthropic.ContentBlockParamUnion{}, fmt.Errorf("claude: unmarshalling tool_use input for echo: %w", err)
			}
		}
		return anthropic.NewToolUseBlock(v.ID, input, v.Name), nil

	case agent.ToolResultBlock:
		block := anthropic.NewToolResultBlock(v.ToolUseID, flattenText(v.Content), v.IsError)
		return block, nil

	case agent.ThinkingBlock:
		return anthropic.NewThinkingBlock(v.Signature, v.Text), nil

	default:
		return anthropic.ContentBlockParamUnion{}, fmt.Errorf("claude: unsupported content block type %T", b)
	}
}

// flattenText concatenates every TextBlock in blocks. Anthropic's
// NewToolResultBlock helper takes a plain string; every ToolResult built via
// agent.TextResult/JSONResult/ErrorResultf produces a single TextBlock, so
// this covers the common case. Non-text content in a tool result (e.g. an
// image) is not yet supported by this adapter.
func flattenText(blocks []agent.ContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if tb, ok := b.(agent.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}

func toTools(tools []agent.RegisteredTool) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		sch := t.Schema()
		inputSchema := anthropic.ToolInputSchemaParam{
			Properties: sch.Properties,
			Required:   sch.Required,
		}
		tu := anthropic.ToolUnionParamOfTool(inputSchema, t.Name())
		if tu.OfTool != nil {
			tu.OfTool.Description = anthropic.String(t.Description())
		}
		out = append(out, tu)
	}
	return out
}

func toToolChoice(tc agent.ToolChoice) anthropic.ToolChoiceUnionParam {
	switch tc.Mode {
	case agent.ToolChoiceAny:
		return anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{}}
	case agent.ToolChoiceOne:
		return anthropic.ToolChoiceParamOfTool(tc.Name)
	case agent.ToolChoiceNone:
		none := anthropic.NewToolChoiceNoneParam()
		return anthropic.ToolChoiceUnionParam{OfNone: &none}
	default:
		return anthropic.ToolChoiceUnionParam{OfAuto: &anthropic.ToolChoiceAutoParam{}}
	}
}

func toThinking(cfg agent.ThinkingConfig) anthropic.ThinkingConfigParamUnion {
	switch cfg.Mode {
	case agent.ThinkingBudgeted:
		return anthropic.ThinkingConfigParamOfEnabled(int64(cfg.Budget))
	case agent.ThinkingAdaptive:
		return anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}
	default:
		disabled := anthropic.NewThinkingConfigDisabledParam()
		return anthropic.ThinkingConfigParamUnion{OfDisabled: &disabled}
	}
}

func fromMessage(msg *anthropic.Message) *agent.Response {
	blocks := make([]agent.ContentBlock, 0, len(msg.Content))
	for _, b := range msg.Content {
		if cb, ok := fromContentBlock(b); ok {
			blocks = append(blocks, cb)
		}
	}
	return &agent.Response{
		ID:         msg.ID,
		Model:      string(msg.Model),
		Message:    agent.Message{Role: agent.RoleAssistant, Content: blocks},
		StopReason: fromStopReason(msg.StopReason),
		Usage:      fromUsage(msg.Usage),
		Raw:        msg,
	}
}

// fromContentBlock translates a response content block. Block types with no
// unified equivalent yet (server-side tool results, redacted thinking,
// container uploads) are skipped rather than causing an error, so a
// response that used one of those features still returns whatever
// text/tool_use content it has; ok reports whether a block was produced.
func fromContentBlock(b anthropic.ContentBlockUnion) (block agent.ContentBlock, ok bool) {
	switch b.Type {
	case "text":
		tb := b.AsText()
		return agent.TextBlock{Text: tb.Text}, true
	case "thinking":
		th := b.AsThinking()
		return agent.ThinkingBlock{Text: th.Thinking, Signature: th.Signature}, true
	case "tool_use":
		tu := b.AsToolUse()
		return agent.ToolUseBlock{ID: tu.ID, Name: tu.Name, Input: []byte(tu.Input)}, true
	default:
		return nil, false
	}
}

func fromStopReason(sr anthropic.StopReason) agent.StopReason {
	switch sr {
	case anthropic.StopReasonEndTurn, anthropic.StopReasonStopSequence, anthropic.StopReasonPauseTurn:
		return agent.StopEndTurn
	case anthropic.StopReasonMaxTokens:
		return agent.StopMaxTokens
	case anthropic.StopReasonToolUse:
		return agent.StopToolUse
	case anthropic.StopReasonRefusal:
		return agent.StopRefusal
	default:
		return agent.StopUnknown
	}
}

func fromUsage(u anthropic.Usage) agent.Usage {
	return agent.Usage{
		InputTokens:         int(u.InputTokens),
		OutputTokens:        int(u.OutputTokens),
		CacheReadTokens:     int(u.CacheReadInputTokens),
		CacheCreationTokens: int(u.CacheCreationInputTokens),
	}
}
