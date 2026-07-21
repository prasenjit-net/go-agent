package gemini

import (
	"testing"

	"google.golang.org/genai"

	agent "github.com/prasenjit-net/go-agent"
)

func TestObserve_TextQueuesDeltaAndAccumulates(t *testing.T) {
	s := &eventStream{}
	s.observe(&genai.GenerateContentResponse{
		ResponseID: "resp_1",
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{Parts: []*genai.Part{{Text: "hel"}}},
		}},
	})
	s.observe(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{Parts: []*genai.Part{{Text: "lo"}}},
		}},
	})

	if s.text.String() != "hello" {
		t.Errorf("accumulated text = %q, want hello", s.text.String())
	}
	if len(s.queued) != 2 {
		t.Fatalf("queued = %+v, want 2 text-delta events", s.queued)
	}
	for i, want := range []string{"hel", "lo"} {
		if s.queued[i].Type != agent.EventTextDelta || s.queued[i].TextDelta != want {
			t.Errorf("queued[%d] = %+v, want TextDelta %q", i, s.queued[i], want)
		}
	}
	if s.respID != "resp_1" {
		t.Errorf("respID = %q, want resp_1 (should stick from the first chunk that set it)", s.respID)
	}
}

func TestObserve_ThinkingPartQueuesThinkingDelta(t *testing.T) {
	s := &eventStream{}
	s.observe(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{Parts: []*genai.Part{{Thought: true, Text: "reasoning..."}}},
		}},
	})
	if len(s.queued) != 1 || s.queued[0].Type != agent.EventThinkingDelta || s.queued[0].ThinkingDelta != "reasoning..." {
		t.Errorf("queued = %+v", s.queued)
	}
}

func TestObserve_FunctionCallQueuesToolCallStartAndAccumulatesBlock(t *testing.T) {
	s := &eventStream{}
	s.observe(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{Parts: []*genai.Part{
				genai.NewPartFromFunctionCall("get_weather", map[string]any{"city": "Paris"}),
			}},
		}},
	})
	if len(s.queued) != 1 || s.queued[0].Type != agent.EventToolCallStart {
		t.Fatalf("queued = %+v", s.queued)
	}
	if s.queued[0].ToolCall.Name != "get_weather" {
		t.Errorf("ToolCall = %+v", s.queued[0].ToolCall)
	}
	if len(s.toolBlocks) != 1 {
		t.Fatalf("toolBlocks = %+v, want 1", s.toolBlocks)
	}
}

func TestObserve_NoCandidatesOrContentIsNoOp(t *testing.T) {
	s := &eventStream{}
	s.observe(&genai.GenerateContentResponse{ResponseID: "resp_1"})
	if len(s.queued) != 0 {
		t.Errorf("queued = %+v, want none", s.queued)
	}

	s.observe(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{}}})
	if len(s.queued) != 0 {
		t.Errorf("queued = %+v, want none (Content was nil)", s.queued)
	}
}

func TestFinalResponse_ToolBlocksForceStopToolUse(t *testing.T) {
	s := &eventStream{
		respID:       "resp_1",
		model:        "gemini-test",
		finishReason: genai.FinishReasonStop,
	}
	s.text.WriteString("checking...")
	s.toolBlocks = []agent.ContentBlock{agent.ToolUseBlock{ID: "call_1", Name: "get_weather"}}

	resp := s.finalResponse()
	if resp.StopReason != agent.StopToolUse {
		t.Errorf("StopReason = %v, want tool_use (finish_reason alone was STOP)", resp.StopReason)
	}
	if len(resp.Message.Content) != 2 {
		t.Fatalf("Content = %+v, want [text, tool_use]", resp.Message.Content)
	}
	if _, ok := resp.Message.Content[0].(agent.TextBlock); !ok {
		t.Errorf("Content[0] = %+v, want TextBlock", resp.Message.Content[0])
	}
	if _, ok := resp.Message.Content[1].(agent.ToolUseBlock); !ok {
		t.Errorf("Content[1] = %+v, want ToolUseBlock", resp.Message.Content[1])
	}
}

func TestFinalResponse_NoToolBlocksUsesFinishReason(t *testing.T) {
	s := &eventStream{finishReason: genai.FinishReasonMaxTokens}
	resp := s.finalResponse()
	if resp.StopReason != agent.StopMaxTokens {
		t.Errorf("StopReason = %v, want max_tokens", resp.StopReason)
	}
}
