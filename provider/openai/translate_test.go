package openai

import (
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
