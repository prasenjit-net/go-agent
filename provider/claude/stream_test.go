package claude

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	agent "github.com/prasenjit-net/go-agent"
)

func TestTranslateStreamEvent(t *testing.T) {
	cases := []struct {
		name   string
		raw    anthropic.MessageStreamEventUnion
		wantOK bool
		check  func(t *testing.T, ev agent.Event)
	}{
		{
			name: "text delta",
			raw: anthropic.MessageStreamEventUnion{
				Type:  "content_block_delta",
				Delta: anthropic.MessageStreamEventUnionDelta{Type: "text_delta", Text: "hello"},
			},
			wantOK: true,
			check: func(t *testing.T, ev agent.Event) {
				if ev.Type != agent.EventTextDelta || ev.TextDelta != "hello" {
					t.Errorf("event = %+v", ev)
				}
			},
		},
		{
			name: "thinking delta",
			raw: anthropic.MessageStreamEventUnion{
				Type:  "content_block_delta",
				Delta: anthropic.MessageStreamEventUnionDelta{Type: "thinking_delta", Thinking: "reasoning..."},
			},
			wantOK: true,
			check: func(t *testing.T, ev agent.Event) {
				if ev.Type != agent.EventThinkingDelta || ev.ThinkingDelta != "reasoning..." {
					t.Errorf("event = %+v", ev)
				}
			},
		},
		{
			name: "unrecognized delta type",
			raw: anthropic.MessageStreamEventUnion{
				Type:  "content_block_delta",
				Delta: anthropic.MessageStreamEventUnionDelta{Type: "citations_delta"},
			},
			wantOK: false,
		},
		{
			name: "tool_use content_block_start",
			raw: anthropic.MessageStreamEventUnion{
				Type:         "content_block_start",
				ContentBlock: anthropic.ContentBlockStartEventContentBlockUnion{Type: "tool_use", ID: "call_1", Name: "get_weather"},
			},
			wantOK: true,
			check: func(t *testing.T, ev agent.Event) {
				if ev.Type != agent.EventToolCallStart || ev.ToolCall == nil || ev.ToolCall.ID != "call_1" || ev.ToolCall.Name != "get_weather" {
					t.Errorf("event = %+v", ev)
				}
			},
		},
		{
			name: "text content_block_start has no unified event",
			raw: anthropic.MessageStreamEventUnion{
				Type:         "content_block_start",
				ContentBlock: anthropic.ContentBlockStartEventContentBlockUnion{Type: "text"},
			},
			wantOK: false,
		},
		{
			name:   "message_start has no unified event",
			raw:    anthropic.MessageStreamEventUnion{Type: "message_start"},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, ok := translateStreamEvent(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("translateStreamEvent() ok = %v, want %v", ok, tc.wantOK)
			}
			if tc.check != nil {
				tc.check(t, ev)
			}
		})
	}
}
