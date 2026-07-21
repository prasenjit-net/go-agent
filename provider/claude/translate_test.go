package claude

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	agent "github.com/prasenjit-net/go-agent"
)

func TestToMessages_RoundTripsToolUseAndResult(t *testing.T) {
	msgs := []agent.Message{
		agent.UserMessage("what's the weather in Paris?"),
		agent.AssistantMessage(agent.ToolUseBlock{ID: "call_1", Name: "get_weather", Input: json.RawMessage(`{"city":"Paris"}`)}),
		{Role: agent.RoleUser, Content: []agent.ContentBlock{
			agent.ToolResultBlock{ToolUseID: "call_1", Content: []agent.ContentBlock{agent.TextBlock{Text: "72F and sunny"}}},
		}},
	}

	out, err := toMessages(msgs)
	if err != nil {
		t.Fatalf("toMessages returned error: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3", len(out))
	}
	if out[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("out[0].Role = %v, want user", out[0].Role)
	}
	if out[1].Role != anthropic.MessageParamRoleAssistant {
		t.Errorf("out[1].Role = %v, want assistant", out[1].Role)
	}
	toolUse := out[1].Content[0].OfToolUse
	if toolUse == nil || toolUse.ID != "call_1" || toolUse.Name != "get_weather" {
		t.Errorf("assistant tool_use block = %+v", toolUse)
	}
	toolResult := out[2].Content[0].OfToolResult
	if toolResult == nil || toolResult.ToolUseID != "call_1" {
		t.Errorf("tool_result block = %+v", toolResult)
	}
}

func TestFromContentBlock_TextAndToolUse(t *testing.T) {
	resp, err := unmarshalMessage(t, `{
		"id": "msg_1", "model": "claude-opus-4-8", "role": "assistant", "type": "message",
		"stop_reason": "tool_use",
		"content": [
			{"type": "text", "text": "Let me check."},
			{"type": "tool_use", "id": "call_1", "name": "get_weather", "input": {"city": "Paris"}}
		],
		"usage": {"input_tokens": 10, "output_tokens": 5, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
	}`)
	if err != nil {
		t.Fatal(err)
	}

	got := fromMessage(resp)
	if got.StopReason != agent.StopToolUse {
		t.Errorf("StopReason = %v, want tool_use", got.StopReason)
	}
	if got.Usage.InputTokens != 10 || got.Usage.OutputTokens != 5 {
		t.Errorf("Usage = %+v", got.Usage)
	}
	if len(got.Message.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(got.Message.Content))
	}
	tb, ok := got.Message.Content[0].(agent.TextBlock)
	if !ok || tb.Text != "Let me check." {
		t.Errorf("Content[0] = %+v", got.Message.Content[0])
	}
	tu, ok := got.Message.Content[1].(agent.ToolUseBlock)
	if !ok || tu.ID != "call_1" || tu.Name != "get_weather" {
		t.Errorf("Content[1] = %+v", got.Message.Content[1])
	}
}

func unmarshalMessage(t *testing.T, raw string) (*anthropic.Message, error) {
	t.Helper()
	var m anthropic.Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func TestFromStopReason(t *testing.T) {
	cases := map[anthropic.StopReason]agent.StopReason{
		anthropic.StopReasonEndTurn:      agent.StopEndTurn,
		anthropic.StopReasonMaxTokens:    agent.StopMaxTokens,
		anthropic.StopReasonToolUse:      agent.StopToolUse,
		anthropic.StopReasonRefusal:      agent.StopRefusal,
		anthropic.StopReasonPauseTurn:    agent.StopEndTurn,
		anthropic.StopReasonStopSequence: agent.StopEndTurn,
	}
	for in, want := range cases {
		if got := fromStopReason(in); got != want {
			t.Errorf("fromStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToMessageNewParams_DefaultsMaxTokensWhenUnset(t *testing.T) {
	req := &agent.Request{Model: "claude-opus-4-8", Messages: []agent.Message{agent.UserMessage("hi")}}
	params, err := toMessageNewParams(req)
	if err != nil {
		t.Fatalf("toMessageNewParams returned error: %v", err)
	}
	if params.MaxTokens != defaultMaxTokens {
		t.Errorf("MaxTokens = %d, want default %d", params.MaxTokens, defaultMaxTokens)
	}
	if string(params.Model) != "claude-opus-4-8" {
		t.Errorf("Model = %q", params.Model)
	}
}

func TestToMessageNewParams_WiresSystemToolsThinking(t *testing.T) {
	type input struct {
		City string `json:"city" jsonschema:"required"`
	}
	tool := agent.NewTool("get_weather", "Get current weather.",
		func(ctx context.Context, in input) (agent.ToolResult, error) { return agent.TextResult(""), nil })

	req := &agent.Request{
		Model:      "claude-opus-4-8",
		MaxTokens:  512,
		System:     []agent.SystemBlock{{Text: "be terse", Cacheable: true}},
		Messages:   []agent.Message{agent.UserMessage("hi")},
		Tools:      []agent.RegisteredTool{tool},
		ToolChoice: agent.ToolChoice{Mode: agent.ToolChoiceAny},
		Thinking:   &agent.ThinkingConfig{Mode: agent.ThinkingAdaptive},
	}

	params, err := toMessageNewParams(req)
	if err != nil {
		t.Fatalf("toMessageNewParams returned error: %v", err)
	}
	if params.MaxTokens != 512 {
		t.Errorf("MaxTokens = %d, want 512", params.MaxTokens)
	}
	if len(params.System) != 1 || params.System[0].Text != "be terse" {
		t.Errorf("System = %+v", params.System)
	}
	if params.System[0].CacheControl != anthropic.NewCacheControlEphemeralParam() {
		t.Errorf("System[0].CacheControl = %+v, want an ephemeral cache control breakpoint", params.System[0].CacheControl)
	}
	if len(params.Tools) != 1 {
		t.Errorf("Tools = %+v, want 1", params.Tools)
	}
	if params.ToolChoice.OfAny == nil {
		t.Errorf("ToolChoice = %+v, want OfAny set", params.ToolChoice)
	}
	if params.Thinking.OfAdaptive == nil {
		t.Errorf("Thinking = %+v, want OfAdaptive set", params.Thinking)
	}
}

func TestToMessageNewParams_PropagatesToMessagesError(t *testing.T) {
	req := &agent.Request{
		Model:    "claude-opus-4-8",
		Messages: []agent.Message{{Role: "system", Content: []agent.ContentBlock{agent.TextBlock{Text: "x"}}}},
	}
	if _, err := toMessageNewParams(req); err == nil {
		t.Error("expected an error for an unsupported message role, got nil")
	}
}

func TestToMessageCountTokensParams_WiresSystemAndTools(t *testing.T) {
	type input struct {
		City string `json:"city" jsonschema:"required"`
	}
	tool := agent.NewTool("get_weather", "Get current weather.",
		func(ctx context.Context, in input) (agent.ToolResult, error) { return agent.TextResult(""), nil })

	req := &agent.Request{
		Model:    "claude-opus-4-8",
		System:   []agent.SystemBlock{{Text: "be terse"}},
		Messages: []agent.Message{agent.UserMessage("hi")},
		Tools:    []agent.RegisteredTool{tool},
	}
	params, err := toMessageCountTokensParams(req)
	if err != nil {
		t.Fatalf("toMessageCountTokensParams returned error: %v", err)
	}
	if len(params.System.OfTextBlockArray) != 1 || params.System.OfTextBlockArray[0].Text != "be terse" {
		t.Errorf("System = %+v", params.System)
	}
	if len(params.Tools) != 1 || params.Tools[0].OfTool.Name != "get_weather" {
		t.Errorf("Tools = %+v", params.Tools)
	}
}

func TestToContentBlock(t *testing.T) {
	cases := []struct {
		name    string
		block   agent.ContentBlock
		wantErr bool
	}{
		{"image base64", agent.ImageBlock{Source: agent.ImageSource{Kind: agent.SourceBase64, MediaType: "image/png", Data: "abc"}}, false},
		{"image url", agent.ImageBlock{Source: agent.ImageSource{Kind: agent.SourceURL, Data: "https://example.com/x.png"}}, false},
		{"image unsupported kind", agent.ImageBlock{Source: agent.ImageSource{Kind: "ftp"}}, true},
		{"document base64", agent.DocumentBlock{Source: agent.ImageSource{Kind: agent.SourceBase64, Data: "abc"}, Title: "doc"}, false},
		{"document url", agent.DocumentBlock{Source: agent.ImageSource{Kind: agent.SourceURL, Data: "https://example.com/x.pdf"}}, false},
		{"document unsupported kind", agent.DocumentBlock{Source: agent.ImageSource{Kind: "ftp"}}, true},
		{"thinking", agent.ThinkingBlock{Text: "reasoning", Signature: "sig"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := toContentBlock(tc.block)
			if (err != nil) != tc.wantErr {
				t.Errorf("toContentBlock(%s) error = %v, wantErr %v", tc.name, err, tc.wantErr)
			}
		})
	}
}

func TestToContentBlock_DocumentTitleSet(t *testing.T) {
	block, err := toContentBlock(agent.DocumentBlock{Source: agent.ImageSource{Kind: agent.SourceBase64, Data: "abc"}, Title: "my doc"})
	if err != nil {
		t.Fatalf("toContentBlock returned error: %v", err)
	}
	if block.OfDocument == nil || !block.OfDocument.Title.Valid() || block.OfDocument.Title.Value != "my doc" {
		t.Errorf("OfDocument = %+v", block.OfDocument)
	}
}

func TestToToolChoice(t *testing.T) {
	cases := []struct {
		name string
		mode agent.ToolChoiceMode
		want func(anthropic.ToolChoiceUnionParam) bool
	}{
		{"any", agent.ToolChoiceAny, func(tc anthropic.ToolChoiceUnionParam) bool { return tc.OfAny != nil }},
		{"one", agent.ToolChoiceOne, func(tc anthropic.ToolChoiceUnionParam) bool { return tc.OfTool != nil }},
		{"none", agent.ToolChoiceNone, func(tc anthropic.ToolChoiceUnionParam) bool { return tc.OfNone != nil }},
		{"auto (default)", agent.ToolChoiceAuto, func(tc anthropic.ToolChoiceUnionParam) bool { return tc.OfAuto != nil }},
		{"unrecognized falls back to auto", "bogus", func(tc anthropic.ToolChoiceUnionParam) bool { return tc.OfAuto != nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toToolChoice(agent.ToolChoice{Mode: tc.mode, Name: "get_weather"})
			if !tc.want(got) {
				t.Errorf("toToolChoice(%q) = %+v, unexpected shape", tc.mode, got)
			}
		})
	}
}

func TestToThinking(t *testing.T) {
	cases := []struct {
		name string
		cfg  agent.ThinkingConfig
		want func(anthropic.ThinkingConfigParamUnion) bool
	}{
		{"budgeted", agent.ThinkingConfig{Mode: agent.ThinkingBudgeted, Budget: 2048}, func(tc anthropic.ThinkingConfigParamUnion) bool {
			return tc.OfEnabled != nil && tc.OfEnabled.BudgetTokens == 2048
		}},
		{"adaptive", agent.ThinkingConfig{Mode: agent.ThinkingAdaptive}, func(tc anthropic.ThinkingConfigParamUnion) bool {
			return tc.OfAdaptive != nil
		}},
		{"off (default)", agent.ThinkingConfig{Mode: agent.ThinkingOff}, func(tc anthropic.ThinkingConfigParamUnion) bool {
			return tc.OfDisabled != nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toThinking(tc.cfg)
			if !tc.want(got) {
				t.Errorf("toThinking(%+v) = %+v, unexpected shape", tc.cfg, got)
			}
		})
	}
}

func TestFromContentBlock(t *testing.T) {
	resp, err := unmarshalMessage(t, `{
		"id": "msg_1", "model": "claude-opus-4-8", "role": "assistant", "type": "message",
		"stop_reason": "end_turn",
		"content": [
			{"type": "thinking", "thinking": "let me think", "signature": "sig123"}
		],
		"usage": {"input_tokens": 1, "output_tokens": 1, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	got := fromMessage(resp)
	if len(got.Message.Content) != 1 {
		t.Fatalf("Content = %+v", got.Message.Content)
	}
	th, ok := got.Message.Content[0].(agent.ThinkingBlock)
	if !ok || th.Text != "let me think" || th.Signature != "sig123" {
		t.Errorf("Content[0] = %+v", got.Message.Content[0])
	}
}

func TestFromContentBlock_UnknownTypeSkipped(t *testing.T) {
	_, ok := fromContentBlock(anthropic.ContentBlockUnion{Type: "container_upload"})
	if ok {
		t.Error("expected ok=false for a block type with no unified equivalent")
	}
}

func TestFromUsage(t *testing.T) {
	got := fromUsage(anthropic.Usage{InputTokens: 10, OutputTokens: 20, CacheReadInputTokens: 5, CacheCreationInputTokens: 3})
	want := agent.Usage{InputTokens: 10, OutputTokens: 20, CacheReadTokens: 5, CacheCreationTokens: 3}
	if got != want {
		t.Errorf("fromUsage = %+v, want %+v", got, want)
	}
}

func TestToTools_SetsNameDescriptionAndSchema(t *testing.T) {
	type input struct {
		City string `json:"city" jsonschema:"required"`
	}
	tool := agent.NewTool("get_weather", "Get current weather.",
		func(ctx context.Context, in input) (agent.ToolResult, error) { return agent.TextResult(""), nil })

	out := toTools([]agent.RegisteredTool{tool})
	if len(out) != 1 || out[0].OfTool == nil {
		t.Fatalf("toTools output = %+v", out)
	}
	tp := out[0].OfTool
	if tp.Name != "get_weather" {
		t.Errorf("Name = %q, want get_weather", tp.Name)
	}
	if !tp.Description.Valid() || tp.Description.Value != "Get current weather." {
		t.Errorf("Description = %+v", tp.Description)
	}
	if len(tp.InputSchema.Required) != 1 || tp.InputSchema.Required[0] != "city" {
		t.Errorf("InputSchema.Required = %v", tp.InputSchema.Required)
	}
}
