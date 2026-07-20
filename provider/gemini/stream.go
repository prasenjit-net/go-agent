package gemini

import (
	"context"
	"io"
	"strings"

	"google.golang.org/genai"

	agent "github.com/prasenjit-net/go-agent"
)

// Stream implements agent.StreamingProvider.
//
// The genai SDK exposes streaming as an iter.Seq2 push-iterator
// (GenerateContentStream), whereas agent.EventStream is pull-based. This
// method bridges the two by driving the push-iterator on a goroutine and
// forwarding each item over a channel that Next reads from — the standard
// pattern for adapting a range-over-func iterator to a pull interface.
func (c *Client) Stream(ctx context.Context, req *agent.Request) (agent.EventStream, error) {
	contents, err := toContents(req.Messages)
	if err != nil {
		return nil, err
	}
	cfg, err := toConfig(req)
	if err != nil {
		return nil, err
	}

	streamCtx, cancel := context.WithCancel(ctx)
	items := make(chan streamItem)
	go func() {
		defer close(items)
		for resp, err := range c.sdk.Models.GenerateContentStream(streamCtx, req.Model, contents, cfg) {
			select {
			case items <- streamItem{resp: resp, err: err}:
			case <-streamCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	return &eventStream{cancel: cancel, items: items}, nil
}

type streamItem struct {
	resp *genai.GenerateContentResponse
	err  error
}

// eventStream accumulates text, tool calls, and usage across the chunks it
// observes and emits a single terminal EventMessageDone carrying the fully
// reconstructed Response — the contract every agent.StreamingProvider.Stream
// implementation must uphold (see turnAccumulator in the root package).
type eventStream struct {
	cancel context.CancelFunc
	items  chan streamItem
	queued []agent.Event

	text         strings.Builder
	finishReason genai.FinishReason
	usage        *genai.GenerateContentResponseUsageMetadata
	respID       string
	model        string
	toolBlocks   []agent.ContentBlock

	doneEmitted bool
}

func (s *eventStream) Next(ctx context.Context) (agent.Event, error) {
	for {
		if len(s.queued) > 0 {
			ev := s.queued[0]
			s.queued = s.queued[1:]
			return ev, nil
		}
		select {
		case <-ctx.Done():
			return agent.Event{}, ctx.Err()
		case item, ok := <-s.items:
			if !ok {
				if !s.doneEmitted {
					s.doneEmitted = true
					return agent.Event{Type: agent.EventMessageDone, Response: s.finalResponse()}, nil
				}
				return agent.Event{}, io.EOF
			}
			if item.err != nil {
				return agent.Event{}, translateError(item.err)
			}
			s.observe(item.resp)
		}
	}
}

func (s *eventStream) observe(resp *genai.GenerateContentResponse) {
	if resp.ResponseID != "" {
		s.respID = resp.ResponseID
	}
	if resp.ModelVersion != "" {
		s.model = resp.ModelVersion
	}
	if resp.UsageMetadata != nil {
		s.usage = resp.UsageMetadata
	}
	if len(resp.Candidates) == 0 {
		return
	}
	cand := resp.Candidates[0]
	if cand.FinishReason != "" {
		s.finishReason = cand.FinishReason
	}
	if cand.Content == nil {
		return
	}

	for _, p := range cand.Content.Parts {
		block, ok := fromPart(p, len(s.toolBlocks))
		if !ok {
			continue
		}
		switch b := block.(type) {
		case agent.TextBlock:
			s.text.WriteString(b.Text)
			s.queued = append(s.queued, agent.Event{Type: agent.EventTextDelta, TextDelta: b.Text})
		case agent.ThinkingBlock:
			s.queued = append(s.queued, agent.Event{Type: agent.EventThinkingDelta, ThinkingDelta: b.Text})
		case agent.ToolUseBlock:
			s.toolBlocks = append(s.toolBlocks, b)
			call := agent.ToolCall{ID: b.ID, Name: b.Name, Input: b.Input}
			s.queued = append(s.queued, agent.Event{Type: agent.EventToolCallStart, ToolCall: &call})
		}
	}
}

func (s *eventStream) finalResponse() *agent.Response {
	var blocks []agent.ContentBlock
	if s.text.Len() > 0 {
		blocks = append(blocks, agent.TextBlock{Text: s.text.String()})
	}
	blocks = append(blocks, s.toolBlocks...)

	stopReason := fromFinishReason(s.finishReason)
	if len(s.toolBlocks) > 0 {
		stopReason = agent.StopToolUse
	}

	return &agent.Response{
		ID:         s.respID,
		Model:      s.model,
		Message:    agent.Message{Role: agent.RoleAssistant, Content: blocks},
		StopReason: stopReason,
		Usage:      fromUsage(s.usage),
	}
}

func (s *eventStream) Close() error {
	s.cancel()
	return nil
}
