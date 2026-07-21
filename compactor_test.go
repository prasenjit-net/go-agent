package agent_test

import (
	"context"
	"testing"

	agent "github.com/prasenjit-net/go-agent"
)

func TestWindowCompactor_KeepsEverythingUnderLimit(t *testing.T) {
	msgs := []agent.Message{agent.UserMessage("hi"), agent.AssistantMessage(agent.TextBlock{Text: "hello"})}
	c := agent.NewWindowCompactor(10)

	got, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}
	if len(got) != len(msgs) {
		t.Errorf("len(got) = %d, want %d (nothing should be dropped)", len(got), len(msgs))
	}
}

func TestWindowCompactor_TruncatesToLastN(t *testing.T) {
	msgs := []agent.Message{
		agent.UserMessage("one"),
		agent.AssistantMessage(agent.TextBlock{Text: "two"}),
		agent.UserMessage("three"),
		agent.AssistantMessage(agent.TextBlock{Text: "four"}),
	}
	c := agent.NewWindowCompactor(2)

	got, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}
	if len(got) != 2 || got[0].Text() != "three" || got[1].Text() != "four" {
		t.Errorf("got = %+v, want the last 2 messages", got)
	}
}

func TestWindowCompactor_DropsOrphanedToolResultAtWindowStart(t *testing.T) {
	// A window of 2 would naively keep [tool_result(call_1), text] — but
	// call_1's originating tool_use falls just outside the window, so the
	// orphaned tool_result must be dropped too, not just truncated to.
	msgs := []agent.Message{
		agent.UserMessage("what's the weather?"),
		agent.AssistantMessage(agent.ToolUseBlock{ID: "call_1", Name: "get_weather"}),
		{Role: agent.RoleUser, Content: []agent.ContentBlock{
			agent.ToolResultBlock{ToolUseID: "call_1", Content: []agent.ContentBlock{agent.TextBlock{Text: "72F"}}},
		}},
		agent.AssistantMessage(agent.TextBlock{Text: "It's 72F."}),
	}
	c := agent.NewWindowCompactor(2)

	got, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}
	if len(got) != 1 || got[0].Text() != "It's 72F." {
		t.Errorf("got = %+v, want only the trailing text message (the orphaned tool_result dropped)", got)
	}
}

func TestWindowCompactor_KeepsPairedToolUseAndResult(t *testing.T) {
	// A window that keeps both the tool_use and its tool_result must not
	// drop either — the pairing is intact, so nothing is orphaned.
	msgs := []agent.Message{
		agent.UserMessage("what's the weather?"),
		agent.AssistantMessage(agent.ToolUseBlock{ID: "call_1", Name: "get_weather"}),
		{Role: agent.RoleUser, Content: []agent.ContentBlock{
			agent.ToolResultBlock{ToolUseID: "call_1", Content: []agent.ContentBlock{agent.TextBlock{Text: "72F"}}},
		}},
	}
	c := agent.NewWindowCompactor(2)

	got, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got = %+v, want both the tool_use and tool_result kept", got)
	}
	if _, ok := got[0].Content[0].(agent.ToolUseBlock); !ok {
		t.Errorf("got[0] = %+v, want the tool_use message", got[0])
	}
}

func TestWindowCompactor_NonPositiveLimitDisablesCompaction(t *testing.T) {
	msgs := []agent.Message{agent.UserMessage("one"), agent.UserMessage("two"), agent.UserMessage("three")}
	c := agent.NewWindowCompactor(0)

	got, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}
	if len(got) != len(msgs) {
		t.Errorf("len(got) = %d, want %d (maxMessages <= 0 should be a no-op)", len(got), len(msgs))
	}
}
