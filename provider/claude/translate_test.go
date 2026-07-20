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
