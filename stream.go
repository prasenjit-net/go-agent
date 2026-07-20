package agent

import (
	"context"
	"io"
)

// EventType identifies the kind of a streamed Event.
type EventType string

const (
	EventTextDelta     EventType = "text_delta"
	EventThinkingDelta EventType = "thinking_delta"
	EventToolCallStart EventType = "tool_call_start"
	EventToolCallDelta EventType = "tool_call_delta" // streamed partial JSON input
	EventToolCallEnd   EventType = "tool_call_end"
	EventToolResult    EventType = "tool_result"  // emitted after Agent executes a tool
	EventMessageDone   EventType = "message_done" // one full assistant turn complete
	EventRunDone       EventType = "run_done"     // the whole Run/RunStream finished
)

// Event is one item in an EventStream. Only the fields relevant to Type are
// populated; the rest are zero values.
type Event struct {
	Type EventType

	TextDelta     string
	ThinkingDelta string

	ToolCall   *ToolCall   // set on tool_call_* and tool_result events
	ToolResult *ToolResult // set on tool_result

	Response *Response // set on message_done
	Result   *Result   // set on run_done
}

// EventStream is returned by StreamingProvider.Stream and Agent.RunStream.
// Next returns io.EOF once the stream is exhausted, mirroring sql.Rows /
// bufio.Scanner conventions: the caller drives iteration, and ctx
// cancellation is checked exactly where the caller expects it.
type EventStream interface {
	Next(ctx context.Context) (Event, error)
	Close() error
}

// sliceStream is a trivial EventStream backed by a pre-built slice of
// events. Used by the streaming fallback (see StreamingFallback) and useful
// in tests.
type sliceStream struct {
	events []Event
	pos    int
}

// NewSliceStream returns an EventStream that yields events in order, then
// io.EOF. Exported for use by provider adapters and tests that need to hand
// back a ready-made stream without implementing EventStream themselves.
func NewSliceStream(events ...Event) EventStream {
	return &sliceStream{events: events}
}

func (s *sliceStream) Next(ctx context.Context) (Event, error) {
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	if s.pos >= len(s.events) {
		return Event{}, io.EOF
	}
	ev := s.events[s.pos]
	s.pos++
	return ev, nil
}

func (s *sliceStream) Close() error { return nil }

// responseToEvents converts a completed Response into the event sequence a
// real stream would have produced, for providers/situations that only have
// a non-streaming Generate available.
func responseToEvents(resp *Response) []Event {
	events := make([]Event, 0, len(resp.Message.Content)+1)
	for _, block := range resp.Message.Content {
		switch b := block.(type) {
		case TextBlock:
			events = append(events, Event{Type: EventTextDelta, TextDelta: b.Text})
		case ThinkingBlock:
			events = append(events, Event{Type: EventThinkingDelta, ThinkingDelta: b.Text})
		case ToolUseBlock:
			call := ToolCall{ID: b.ID, Name: b.Name, Input: b.Input}
			events = append(events,
				Event{Type: EventToolCallStart, ToolCall: &call},
				Event{Type: EventToolCallEnd, ToolCall: &call},
			)
		}
	}
	events = append(events, Event{Type: EventMessageDone, Response: resp})
	return events
}
