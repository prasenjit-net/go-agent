package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// StreamingFallbackMode controls Agent.RunStream's behavior against a
// Provider that does not implement StreamingProvider.
type StreamingFallbackMode string

const (
	// FallbackSingleShot (the default) performs one blocking Generate call
	// per turn and emits its content as a single burst of events, so
	// RunStream still returns a working, if non-incremental, stream rather
	// than an error.
	FallbackSingleShot StreamingFallbackMode = "single_shot"
	// FallbackError makes RunStream return an ErrStreamUnsupported error
	// immediately against a non-streaming provider, for callers that need
	// to know streaming is unavailable rather than silently degrading.
	FallbackError StreamingFallbackMode = "error"
)

// RunStream is the streaming counterpart to Run: it starts from a single
// user turn and returns one logical EventStream for the entire run,
// including any tool round-trips — the caller sees a single stream from
// first token to final answer no matter how many provider calls or tool
// executions happen underneath.
func (a *Agent) RunStream(ctx context.Context, input string) (EventStream, error) {
	return a.RunMessagesStream(ctx, UserMessage(input))
}

// RunMessagesStream is the streaming counterpart to RunMessages.
func (a *Agent) RunMessagesStream(ctx context.Context, msgs ...Message) (EventStream, error) {
	if a.provider == nil {
		return nil, &Error{Code: ErrInvalidRequest, Message: "agent: no Provider configured (use agent.WithProvider)"}
	}
	if err := a.validateConfig(); err != nil {
		return nil, err
	}
	history := make([]Message, len(msgs))
	copy(history, msgs)
	return &runStream{agent: a, history: history}, nil
}

// runStream drives the multi-turn tool-use loop while presenting a single
// EventStream to the caller. Each turn's provider-level events are passed
// through to the caller as they arrive — true incremental streaming when
// the provider implements StreamingProvider natively. Tool execution
// happens between turns (after a turn's stream is exhausted) and is
// reported via EventToolResult.
type runStream struct {
	agent      *Agent
	history    []Message
	usage      Usage
	iterations int

	inner EventStream
	accum turnAccumulator

	queued []Event // synthetic events (tool results, run_done) awaiting drain
	done   bool
}

func (s *runStream) Next(ctx context.Context) (Event, error) {
	for {
		if len(s.queued) > 0 {
			ev := s.queued[0]
			s.queued = s.queued[1:]
			return ev, nil
		}
		if s.done {
			return Event{}, io.EOF
		}
		if s.inner == nil {
			if err := s.startTurn(ctx); err != nil {
				s.done = true
				return Event{}, err
			}
			continue
		}

		ev, err := s.inner.Next(ctx)
		switch {
		case errors.Is(err, io.EOF):
			if err := s.finishTurn(ctx); err != nil {
				s.done = true
				return Event{}, err
			}
			continue
		case err != nil:
			s.done = true
			return Event{}, err
		}
		s.accum.observe(ev)
		return ev, nil
	}
}

func (s *runStream) Close() error {
	if s.inner != nil {
		return s.inner.Close()
	}
	return nil
}

func (s *runStream) startTurn(ctx context.Context) error {
	if s.iterations >= s.agent.maxIterations {
		return &Error{Code: ErrMaxIterations, Provider: s.agent.provider.Name(),
			Message: fmt.Sprintf("exceeded max iterations (%d) without a final answer", s.agent.maxIterations)}
	}

	req, err := s.agent.buildRequest(ctx, s.history)
	if err != nil {
		return err
	}
	if s.agent.hooks.BeforeGenerate != nil {
		if err := s.agent.hooks.BeforeGenerate(ctx, req); err != nil {
			s.agent.reportError(ctx, err)
			return err
		}
	}

	stream, err := s.agent.openStream(ctx, req)
	if err != nil {
		s.agent.reportError(ctx, err)
		return err
	}
	s.inner = stream
	s.accum = turnAccumulator{}
	return nil
}

func (s *runStream) finishTurn(ctx context.Context) error {
	_ = s.inner.Close()
	s.inner = nil
	s.iterations++

	resp := s.accum.response()
	s.usage.Add(resp.Usage)
	if s.agent.hooks.AfterGenerate != nil {
		s.agent.hooks.AfterGenerate(ctx, resp)
	}
	s.history = append(s.history, resp.Message)

	if resp.StopReason != StopToolUse {
		result := &Result{FinalResponse: resp, Messages: s.history, Usage: s.usage, Iterations: s.iterations}
		s.queued = append(s.queued, Event{Type: EventRunDone, Result: result})
		s.done = true
		return nil
	}

	toolResultBlocks := s.agent.executeTools(ctx, resp.Message.Content)
	idx := 0
	for _, b := range resp.Message.Content {
		tu, ok := b.(ToolUseBlock)
		if !ok || idx >= len(toolResultBlocks) {
			continue
		}
		trb, _ := toolResultBlocks[idx].(ToolResultBlock)
		call := ToolCall{ID: tu.ID, Name: tu.Name, Input: tu.Input}
		result := ToolResult{Content: trb.Content, IsError: trb.IsError}
		s.queued = append(s.queued, Event{Type: EventToolResult, ToolCall: &call, ToolResult: &result})
		idx++
	}
	s.history = append(s.history, Message{Role: RoleUser, Content: toolResultBlocks})
	return nil
}

// openStream returns a provider-level stream for req: a real one if the
// provider implements StreamingProvider, or a synthesized one built from a
// single Generate call otherwise (per a.streamFallback).
func (a *Agent) openStream(ctx context.Context, req *Request) (EventStream, error) {
	sp, ok := a.provider.(StreamingProvider)
	if !ok {
		if a.streamFallback == FallbackError {
			return nil, &Error{Code: ErrStreamUnsupported, Provider: a.provider.Name(),
				Message: "provider does not implement StreamingProvider"}
		}
		resp, err := a.generateWithRetry(ctx, req)
		if err != nil {
			return nil, err
		}
		return NewSliceStream(responseToEvents(resp)...), nil
	}

	var lastErr error
	for attempt := 0; attempt <= a.retry.MaxRetries; attempt++ {
		stream, err := sp.Stream(ctx, req)
		if err == nil {
			return stream, nil
		}
		lastErr = err
		if attempt == a.retry.MaxRetries || !IsRetryable(err) {
			return nil, err
		}
		delay := a.retry.delay(attempt)
		var agentErr *Error
		if errors.As(err, &agentErr) && agentErr.RetryAfter > 0 {
			delay = agentErr.RetryAfter
		}
		if err := sleepCtx(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

// turnAccumulator watches the events of a single turn's stream go by and
// captures the terminal EventMessageDone's Response, which every
// StreamingProvider.Stream implementation is required to emit exactly once,
// as the last event of a turn, fully populated (final content blocks,
// StopReason, Usage, ID, Model) — the same contract real vendor SDKs
// provide via their own "accumulate to final message" helpers. Intermediate
// delta events are passed through to the caller as-is and are not otherwise
// interpreted here.
type turnAccumulator struct {
	resp *Response
}

func (t *turnAccumulator) observe(ev Event) {
	if ev.Type == EventMessageDone && ev.Response != nil {
		t.resp = ev.Response
	}
}

func (t *turnAccumulator) response() *Response {
	if t.resp != nil {
		return t.resp
	}
	// Defensive: a provider stream that never emitted message_done (an
	// adapter bug) still needs something to avoid a nil deref downstream.
	return &Response{StopReason: StopUnknown}
}
