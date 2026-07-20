package openai

import (
	"context"
	"io"
	"sort"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/ssestream"

	agent "github.com/prasenjit-net/go-agent"
)

// Stream implements agent.StreamingProvider.
func (c *Client) Stream(ctx context.Context, req *agent.Request) (agent.EventStream, error) {
	params, err := toParams(req)
	if err != nil {
		return nil, err
	}
	sdkStream := c.sdk.Chat.Completions.NewStreaming(ctx, params)
	return &eventStream{sdk: sdkStream, id: req.Model, toolCalls: map[int64]*accumulatingToolCall{}}, nil
}

type accumulatingToolCall struct {
	id        string
	name      string
	arguments strings.Builder
	started   bool
}

// eventStream adapts the OpenAI SDK's chunked ssestream.Stream into
// agent.EventStream. Unlike the Anthropic SDK, openai-go has no built-in
// "accumulate chunks into a final message" helper, so this type does that
// accumulation by hand (content text, tool-call arguments keyed by index,
// and the terminal usage/finish_reason) and emits a single terminal
// EventMessageDone carrying the fully reconstructed Response — the same
// contract every agent.StreamingProvider.Stream implementation must uphold.
type eventStream struct {
	sdk *ssestream.Stream[openai.ChatCompletionChunk]
	id  string

	text         strings.Builder
	toolCalls    map[int64]*accumulatingToolCall
	finishReason string
	usage        openai.CompletionUsage
	model        string
	respID       string

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
				return agent.Event{Type: agent.EventMessageDone, Response: s.finalResponse()}, nil
			}
			return agent.Event{}, io.EOF
		}

		chunk := s.sdk.Current()
		if s.respID == "" {
			s.respID = chunk.ID
		}
		if chunk.Model != "" {
			s.model = chunk.Model
		}
		if chunk.Usage.TotalTokens > 0 {
			s.usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != "" {
			s.finishReason = choice.FinishReason
		}

		if ev, ok := s.observeDelta(choice.Delta); ok {
			return ev, nil
		}
		// A delta that only carried tool-call name/ID (the first chunk for
		// a given tool call, before any argument text arrives) or a role
		// marker has no unified event to surface yet; keep pulling.
	}
}

// observeDelta updates accumulator state from one chunk's delta and returns
// the single most relevant event to surface for it, if any.
func (s *eventStream) observeDelta(delta openai.ChatCompletionChunkChoiceDelta) (agent.Event, bool) {
	if delta.Content != "" {
		s.text.WriteString(delta.Content)
		return agent.Event{Type: agent.EventTextDelta, TextDelta: delta.Content}, true
	}

	for _, tc := range delta.ToolCalls {
		acc, exists := s.toolCalls[tc.Index]
		if !exists {
			acc = &accumulatingToolCall{}
			s.toolCalls[tc.Index] = acc
		}
		if tc.ID != "" {
			acc.id = tc.ID
		}
		if tc.Function.Name != "" {
			acc.name = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			acc.arguments.WriteString(tc.Function.Arguments)
		}
		if !acc.started && acc.id != "" && acc.name != "" {
			acc.started = true
			call := agent.ToolCall{ID: acc.id, Name: acc.name}
			return agent.Event{Type: agent.EventToolCallStart, ToolCall: &call}, true
		}
	}
	return agent.Event{}, false
}

func (s *eventStream) finalResponse() *agent.Response {
	var blocks []agent.ContentBlock
	if s.text.Len() > 0 {
		blocks = append(blocks, agent.TextBlock{Text: s.text.String()})
	}

	indices := make([]int64, 0, len(s.toolCalls))
	for idx := range s.toolCalls {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	for _, idx := range indices {
		tc := s.toolCalls[idx]
		var input []byte
		if tc.arguments.Len() > 0 {
			input = []byte(tc.arguments.String())
		}
		blocks = append(blocks, agent.ToolUseBlock{ID: tc.id, Name: tc.name, Input: input})
	}

	return &agent.Response{
		ID:         s.respID,
		Model:      s.model,
		Message:    agent.Message{Role: agent.RoleAssistant, Content: blocks},
		StopReason: fromFinishReason(s.finishReason),
		Usage:      fromUsage(s.usage),
	}
}

func (s *eventStream) Close() error {
	return s.sdk.Close()
}
