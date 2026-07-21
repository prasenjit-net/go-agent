package openai

import (
	"testing"

	"github.com/openai/openai-go/v3"

	agent "github.com/prasenjit-net/go-agent"
)

func newTestEventStream() *eventStream {
	return &eventStream{toolCalls: map[int64]*accumulatingToolCall{}}
}

func TestObserveDelta_TextAccumulates(t *testing.T) {
	s := newTestEventStream()
	ev, ok := s.observeDelta(openai.ChatCompletionChunkChoiceDelta{Content: "hel"})
	if !ok || ev.Type != agent.EventTextDelta || ev.TextDelta != "hel" {
		t.Fatalf("first delta = %+v, %v", ev, ok)
	}
	ev, ok = s.observeDelta(openai.ChatCompletionChunkChoiceDelta{Content: "lo"})
	if !ok || ev.TextDelta != "lo" {
		t.Fatalf("second delta = %+v, %v", ev, ok)
	}
	if s.text.String() != "hello" {
		t.Errorf("accumulated text = %q, want hello", s.text.String())
	}
}

func TestObserveDelta_ToolCallStartsOnlyOnceIDAndNameKnown(t *testing.T) {
	s := newTestEventStream()

	// First chunk: only the ID arrives — no unified event yet.
	_, ok := s.observeDelta(openai.ChatCompletionChunkChoiceDelta{
		ToolCalls: []openai.ChatCompletionChunkChoiceDeltaToolCall{{Index: 0, ID: "call_1"}},
	})
	if ok {
		t.Fatal("expected no event when only the tool call ID has arrived")
	}

	// Second chunk: the name arrives — now EventToolCallStart should fire.
	ev, ok := s.observeDelta(openai.ChatCompletionChunkChoiceDelta{
		ToolCalls: []openai.ChatCompletionChunkChoiceDeltaToolCall{{
			Index:    0,
			Function: openai.ChatCompletionChunkChoiceDeltaToolCallFunction{Name: "get_weather"},
		}},
	})
	if !ok || ev.Type != agent.EventToolCallStart || ev.ToolCall.ID != "call_1" || ev.ToolCall.Name != "get_weather" {
		t.Fatalf("expected EventToolCallStart once name arrives, got %+v, %v", ev, ok)
	}

	// Third chunk: argument text arrives — already started, no further event.
	_, ok = s.observeDelta(openai.ChatCompletionChunkChoiceDelta{
		ToolCalls: []openai.ChatCompletionChunkChoiceDeltaToolCall{{
			Index:    0,
			Function: openai.ChatCompletionChunkChoiceDeltaToolCallFunction{Arguments: `{"city":"Paris"}`},
		}},
	})
	if ok {
		t.Error("expected no further event once the tool call has already started")
	}
	if s.toolCalls[0].arguments.String() != `{"city":"Paris"}` {
		t.Errorf("accumulated arguments = %q", s.toolCalls[0].arguments.String())
	}
}

func TestFinalResponse_SortsToolCallsByIndexAndSetsFinishReasonAndUsage(t *testing.T) {
	s := newTestEventStream()
	s.text.WriteString("here you go")
	s.respID = "resp_1"
	s.model = "gpt-test"
	s.finishReason = "tool_calls"
	s.usage = openai.CompletionUsage{PromptTokens: 10, CompletionTokens: 5}

	// Populate out of order to prove finalResponse sorts by index, not
	// insertion order.
	s.toolCalls[1] = &accumulatingToolCall{id: "call_2", name: "second"}
	s.toolCalls[1].arguments.WriteString(`{"b":2}`)
	s.toolCalls[0] = &accumulatingToolCall{id: "call_1", name: "first"}
	s.toolCalls[0].arguments.WriteString(`{"a":1}`)

	resp := s.finalResponse()
	if resp.ID != "resp_1" || resp.Model != "gpt-test" {
		t.Errorf("ID/Model = %q/%q", resp.ID, resp.Model)
	}
	if resp.StopReason != agent.StopToolUse {
		t.Errorf("StopReason = %v, want tool_use", resp.StopReason)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
	// blocks[0] is the text block; tool calls follow in index order.
	if len(resp.Message.Content) != 3 {
		t.Fatalf("Content = %+v, want 3 blocks", resp.Message.Content)
	}
	first, ok := resp.Message.Content[1].(agent.ToolUseBlock)
	if !ok || first.ID != "call_1" {
		t.Errorf("Content[1] = %+v, want call_1 (index 0) first", resp.Message.Content[1])
	}
	second, ok := resp.Message.Content[2].(agent.ToolUseBlock)
	if !ok || second.ID != "call_2" {
		t.Errorf("Content[2] = %+v, want call_2 (index 1) second", resp.Message.Content[2])
	}
}

func TestFinalResponse_NoTextOrToolCallsProducesNoBlocks(t *testing.T) {
	s := newTestEventStream()
	s.finishReason = "stop"
	resp := s.finalResponse()
	if len(resp.Message.Content) != 0 {
		t.Errorf("Content = %+v, want empty", resp.Message.Content)
	}
	if resp.StopReason != agent.StopEndTurn {
		t.Errorf("StopReason = %v, want end_turn", resp.StopReason)
	}
}
