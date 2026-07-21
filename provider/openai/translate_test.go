package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	agent "github.com/prasenjit-net/go-agent"
)

func TestToMessages_ToolResultBecomesToolMessage(t *testing.T) {
	req := &agent.Request{
		System: []agent.SystemBlock{{Text: "be terse"}},
		Messages: []agent.Message{
			agent.UserMessage("what's the weather?"),
			agent.AssistantMessage(agent.ToolUseBlock{ID: "call_1", Name: "get_weather", Input: json.RawMessage(`{"city":"Paris"}`)}),
			{Role: agent.RoleUser, Content: []agent.ContentBlock{
				agent.ToolResultBlock{ToolUseID: "call_1", Content: []agent.ContentBlock{agent.TextBlock{Text: "72F"}}},
			}},
		},
	}

	out, err := toMessages(req)
	if err != nil {
		t.Fatalf("toMessages returned error: %v", err)
	}
	// system, user, assistant(tool_calls), tool
	if len(out) != 4 {
		t.Fatalf("len(out) = %d, want 4: %+v", len(out), out)
	}
	if out[0].OfSystem == nil {
		t.Errorf("out[0] should be a system message, got %+v", out[0])
	}
	if out[2].OfAssistant == nil || len(out[2].OfAssistant.ToolCalls) != 1 {
		t.Fatalf("out[2] should be an assistant message with one tool call, got %+v", out[2])
	}
	if out[2].OfAssistant.ToolCalls[0].OfFunction.ID != "call_1" {
		t.Errorf("tool call ID = %q, want call_1", out[2].OfAssistant.ToolCalls[0].OfFunction.ID)
	}
	if out[3].OfTool == nil || out[3].OfTool.ToolCallID != "call_1" {
		t.Fatalf("out[3] should be a tool message for call_1, got %+v", out[3])
	}
}

func TestToAssistantMessage_TextAndToolCallTogether(t *testing.T) {
	msg := agent.AssistantMessage(
		agent.TextBlock{Text: "Let me check that."},
		agent.ToolUseBlock{ID: "call_1", Name: "get_weather", Input: json.RawMessage(`{"city":"Paris"}`)},
	)
	out, err := toAssistantMessage(msg)
	if err != nil {
		t.Fatalf("toAssistantMessage returned error: %v", err)
	}
	if out.OfAssistant == nil {
		t.Fatal("expected OfAssistant to be set")
	}
	if !out.OfAssistant.Content.OfString.Valid() || out.OfAssistant.Content.OfString.Value != "Let me check that." {
		t.Errorf("Content = %+v", out.OfAssistant.Content)
	}
	if len(out.OfAssistant.ToolCalls) != 1 || out.OfAssistant.ToolCalls[0].OfFunction.Function.Name != "get_weather" {
		t.Errorf("ToolCalls = %+v", out.OfAssistant.ToolCalls)
	}
}

func TestFromChatCompletion_ToolCallsSetStopToolUse(t *testing.T) {
	// Union types in this SDK carry internal raw-JSON state that its .As*()
	// accessor methods rely on; build the fixture via JSON unmarshaling
	// (as a real response would arrive) rather than a Go struct literal.
	raw := `{
		"id": "chatcmpl_1",
		"model": "gpt-test",
		"created": 0,
		"object": "chat.completion",
		"choices": [{
			"index": 0,
			"finish_reason": "tool_calls",
			"message": {
				"role": "assistant",
				"content": "",
				"refusal": "",
				"tool_calls": [{
					"id": "call_1",
					"type": "function",
					"function": {"name": "get_weather", "arguments": "{\"city\":\"Paris\"}"}
				}]
			}
		}]
	}`
	var resp openai.ChatCompletion
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("failed to build fixture: %v", err)
	}

	got, err := fromChatCompletion(&resp)
	if err != nil {
		t.Fatalf("fromChatCompletion returned error: %v", err)
	}
	if got.StopReason != agent.StopToolUse {
		t.Errorf("StopReason = %v, want tool_use", got.StopReason)
	}
	found := false
	for _, b := range got.Message.Content {
		if tu, ok := b.(agent.ToolUseBlock); ok {
			found = true
			if tu.Name != "get_weather" || tu.ID != "call_1" {
				t.Errorf("tool use = %+v", tu)
			}
		}
	}
	if !found {
		t.Error("expected a ToolUseBlock in the translated response")
	}
}

func TestToReasoningEffort(t *testing.T) {
	if _, ok := toReasoningEffort(agent.ThinkingConfig{Mode: agent.ThinkingOff}); ok {
		t.Error("ThinkingOff should leave reasoning_effort unset")
	}
	effort, ok := toReasoningEffort(agent.ThinkingConfig{Mode: agent.ThinkingAdaptive})
	if !ok || effort != shared.ReasoningEffortMedium {
		t.Errorf("ThinkingAdaptive -> %v, %v, want %v, true", effort, ok, shared.ReasoningEffortMedium)
	}
	effort, ok = toReasoningEffort(agent.ThinkingConfig{Mode: agent.ThinkingBudgeted, Budget: 20000})
	if !ok || effort != shared.ReasoningEffortHigh {
		t.Errorf("large budget -> %v, %v, want high, true", effort, ok)
	}
}

func TestToReasoningEffort_BudgetedTiers(t *testing.T) {
	cases := []struct {
		budget int
		want   shared.ReasoningEffort
	}{
		{4000, shared.ReasoningEffortLow},
		{4001, shared.ReasoningEffortMedium},
		{16000, shared.ReasoningEffortMedium},
		{16001, shared.ReasoningEffortHigh},
	}
	for _, tc := range cases {
		effort, ok := toReasoningEffort(agent.ThinkingConfig{Mode: agent.ThinkingBudgeted, Budget: tc.budget})
		if !ok || effort != tc.want {
			t.Errorf("budget %d -> %v, %v, want %v, true", tc.budget, effort, ok, tc.want)
		}
	}
}

func TestToParams_DefaultsMaxTokensWhenUnset(t *testing.T) {
	req := &agent.Request{Model: "gpt-test", Messages: []agent.Message{agent.UserMessage("hi")}}
	params, err := toParams(req)
	if err != nil {
		t.Fatalf("toParams returned error: %v", err)
	}
	if !params.MaxCompletionTokens.Valid() || params.MaxCompletionTokens.Value != int64(defaultMaxTokens) {
		t.Errorf("MaxCompletionTokens = %+v, want default %d", params.MaxCompletionTokens, defaultMaxTokens)
	}
}

func TestToParams_WiresToolsAndReasoningEffort(t *testing.T) {
	type input struct {
		City string `json:"city" jsonschema:"required"`
	}
	tool := agent.NewTool("get_weather", "Get current weather.",
		func(ctx context.Context, in input) (agent.ToolResult, error) { return agent.TextResult(""), nil })

	req := &agent.Request{
		Model:      "gpt-test",
		Messages:   []agent.Message{agent.UserMessage("hi")},
		Tools:      []agent.RegisteredTool{tool},
		ToolChoice: agent.ToolChoice{Mode: agent.ToolChoiceAny},
		Thinking:   &agent.ThinkingConfig{Mode: agent.ThinkingAdaptive},
	}
	params, err := toParams(req)
	if err != nil {
		t.Fatalf("toParams returned error: %v", err)
	}
	if len(params.Tools) != 1 {
		t.Errorf("Tools = %+v, want 1", params.Tools)
	}
	if !params.ToolChoice.OfAuto.Valid() || params.ToolChoice.OfAuto.Value != "required" {
		t.Errorf("ToolChoice = %+v, want OfAuto=required", params.ToolChoice)
	}
	if params.ReasoningEffort != shared.ReasoningEffortMedium {
		t.Errorf("ReasoningEffort = %v, want medium", params.ReasoningEffort)
	}
}

func TestToMessages_UnsupportedRole(t *testing.T) {
	req := &agent.Request{Messages: []agent.Message{{Role: "system", Content: []agent.ContentBlock{agent.TextBlock{Text: "x"}}}}}
	if _, err := toMessages(req); err == nil {
		t.Error("expected an error for an unsupported message role, got nil")
	}
}

func TestToContentPart(t *testing.T) {
	cases := []struct {
		name    string
		block   agent.ContentBlock
		wantErr bool
	}{
		{"text", agent.TextBlock{Text: "hi"}, false},
		{"image url", agent.ImageBlock{Source: agent.ImageSource{Kind: agent.SourceURL, Data: "https://example.com/x.png"}}, false},
		{"image base64", agent.ImageBlock{Source: agent.ImageSource{Kind: agent.SourceBase64, MediaType: "image/png", Data: "abc"}}, false},
		{"image unsupported kind", agent.ImageBlock{Source: agent.ImageSource{Kind: "ftp"}}, true},
		{"document base64", agent.DocumentBlock{Source: agent.ImageSource{Kind: agent.SourceBase64, MediaType: "application/pdf", Data: "abc"}, Title: "doc"}, false},
		{"document non-base64", agent.DocumentBlock{Source: agent.ImageSource{Kind: agent.SourceURL, Data: "https://example.com/x.pdf"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := toContentPart(tc.block)
			if (err != nil) != tc.wantErr {
				t.Errorf("toContentPart(%s) error = %v, wantErr %v", tc.name, err, tc.wantErr)
			}
		})
	}
}

func TestToUserMessages_FlushesBeforeAndAfterToolResult(t *testing.T) {
	msg := agent.Message{Role: agent.RoleUser, Content: []agent.ContentBlock{
		agent.TextBlock{Text: "before"},
		agent.ToolResultBlock{ToolUseID: "call_1", Content: []agent.ContentBlock{agent.TextBlock{Text: "result"}}},
		agent.TextBlock{Text: "after"},
	}}
	out, err := toUserMessages(msg)
	if err != nil {
		t.Fatalf("toUserMessages returned error: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3 (user, tool, user): %+v", len(out), out)
	}
	if out[0].OfUser == nil {
		t.Errorf("out[0] should be a user message with the 'before' text")
	}
	if out[1].OfTool == nil || out[1].OfTool.ToolCallID != "call_1" {
		t.Errorf("out[1] should be the tool result message, got %+v", out[1])
	}
	if out[2].OfUser == nil {
		t.Errorf("out[2] should be a user message with the 'after' text")
	}
}

func TestToToolChoice(t *testing.T) {
	cases := []struct {
		name string
		mode agent.ToolChoiceMode
		want string
	}{
		{"any", agent.ToolChoiceAny, "required"},
		{"none", agent.ToolChoiceNone, "none"},
		{"auto (default)", agent.ToolChoiceAuto, "auto"},
		{"unrecognized falls back to auto", "bogus", "auto"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toToolChoice(agent.ToolChoice{Mode: tc.mode})
			if !got.OfAuto.Valid() || got.OfAuto.Value != tc.want {
				t.Errorf("toToolChoice(%q) = %+v, want OfAuto=%q", tc.mode, got, tc.want)
			}
		})
	}
}

func TestToToolChoice_OneNamesTheFunction(t *testing.T) {
	got := toToolChoice(agent.ToolChoice{Mode: agent.ToolChoiceOne, Name: "get_weather"})
	if got.OfFunctionToolChoice == nil || got.OfFunctionToolChoice.Function.Name != "get_weather" {
		t.Errorf("toToolChoice(one) = %+v", got)
	}
}

func TestFromChatCompletion_NoChoicesErrors(t *testing.T) {
	if _, err := fromChatCompletion(&openai.ChatCompletion{}); err == nil {
		t.Error("expected an error for a response with no choices, got nil")
	}
}

func TestFromFinishReason(t *testing.T) {
	cases := map[string]agent.StopReason{
		"stop":           agent.StopEndTurn,
		"length":         agent.StopMaxTokens,
		"tool_calls":     agent.StopToolUse,
		"function_call":  agent.StopToolUse,
		"content_filter": agent.StopContentFilter,
		"bogus":          agent.StopUnknown,
	}
	for in, want := range cases {
		if got := fromFinishReason(in); got != want {
			t.Errorf("fromFinishReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFromUsage(t *testing.T) {
	raw := `{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15, "prompt_tokens_details": {"cached_tokens": 3}}`
	var u openai.CompletionUsage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("failed to build fixture: %v", err)
	}
	got := fromUsage(u)
	want := agent.Usage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 3}
	if got != want {
		t.Errorf("fromUsage = %+v, want %+v", got, want)
	}
}
