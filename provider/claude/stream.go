package claude

import (
	"context"
	"fmt"
	"io"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	agent "github.com/prasenjit-net/go-agent"
)

// Stream implements agent.StreamingProvider.
func (c *Client) Stream(ctx context.Context, req *agent.Request) (agent.EventStream, error) {
	params, err := toMessageNewParams(req)
	if err != nil {
		return nil, err
	}
	sdkStream := c.sdk.Messages.NewStreaming(ctx, params)
	return &eventStream{sdk: sdkStream}, nil
}

// eventStream adapts the Anthropic SDK's ssestream.Stream into
// agent.EventStream, passing text/thinking deltas and tool-call starts
// through as they arrive and emitting a single terminal EventMessageDone
// carrying the fully accumulated Response — the contract every
// agent.StreamingProvider.Stream implementation must uphold (see
// turnAccumulator in the root package).
type eventStream struct {
	sdk *ssestream.Stream[anthropic.MessageStreamEventUnion]
	acc anthropic.Message

	doneEmitted bool
}

func (s *eventStream) Next(ctx context.Context) (agent.Event, error) {
	for {
		if err := ctx.Err(); err != nil {
			return agent.Event{}, err
		}
		if !s.sdk.Next() {
			if err := s.sdk.Err(); err != nil {
				return agent.Event{}, translateError(err)
			}
			if !s.doneEmitted {
				s.doneEmitted = true
				return agent.Event{Type: agent.EventMessageDone, Response: fromMessage(&s.acc)}, nil
			}
			return agent.Event{}, io.EOF
		}

		raw := s.sdk.Current()
		if err := s.acc.Accumulate(raw); err != nil {
			return agent.Event{}, fmt.Errorf("claude: accumulating stream event: %w", err)
		}

		if ev, ok := translateStreamEvent(raw); ok {
			return ev, nil
		}
		// Events with no unified single-delta equivalent (message_start,
		// content_block_start/stop for non-tool_use blocks, message_stop,
		// ...) are consumed for accumulation and the loop continues to the
		// next SSE event.
	}
}

func (s *eventStream) Close() error {
	return s.sdk.Close()
}

func translateStreamEvent(raw anthropic.MessageStreamEventUnion) (agent.Event, bool) {
	switch raw.Type {
	case "content_block_delta":
		switch raw.Delta.Type {
		case "text_delta":
			return agent.Event{Type: agent.EventTextDelta, TextDelta: raw.Delta.Text}, true
		case "thinking_delta":
			return agent.Event{Type: agent.EventThinkingDelta, ThinkingDelta: raw.Delta.Thinking}, true
		}
		return agent.Event{}, false

	case "content_block_start":
		if raw.ContentBlock.Type == "tool_use" {
			call := agent.ToolCall{ID: raw.ContentBlock.ID, Name: raw.ContentBlock.Name}
			return agent.Event{Type: agent.EventToolCallStart, ToolCall: &call}, true
		}
		return agent.Event{}, false

	default:
		return agent.Event{}, false
	}
}
