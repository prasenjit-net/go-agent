// Package agenttest provides a scriptable agent.Provider for testing
// application code built on go-agent — agent wiring, tool registration,
// hooks, and run-loop behavior — without any network calls or API cost.
package agenttest

import (
	"context"
	"sync"

	agent "github.com/prasenjit-net/go-agent"
)

// MockProvider is a scriptable agent.Provider. Set Responses for a fixed
// script, or OnGenerate for responses computed from the request (e.g. to
// assert on tool results from a previous turn before deciding what to
// return next).
type MockProvider struct {
	// Responses is consumed in order, one per Generate call; the last
	// entry repeats if Generate is called more times than there are
	// entries. Ignored when OnGenerate is set.
	Responses []*agent.Response

	// OnGenerate, if set, is called instead of consuming Responses.
	OnGenerate func(req *agent.Request) (*agent.Response, error)

	// NameValue overrides the provider name returned by Name(). Defaults
	// to "mock".
	NameValue string

	// CapabilitiesValue is returned by Capabilities(), so tests can
	// exercise capability-gated code paths (e.g. WithThinking validation).
	CapabilitiesValue agent.Capabilities

	mu    sync.Mutex
	calls []*agent.Request
	idx   int
}

func (m *MockProvider) Name() string {
	if m.NameValue != "" {
		return m.NameValue
	}
	return "mock"
}

// Generate implements agent.Provider.
func (m *MockProvider) Generate(_ context.Context, req *agent.Request) (*agent.Response, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req)
	m.mu.Unlock()

	if m.OnGenerate != nil {
		return m.OnGenerate(req)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Responses) == 0 {
		return nil, &agent.Error{Provider: m.Name(), Code: agent.ErrInvalidRequest, Message: "agenttest: MockProvider has no scripted responses"}
	}
	idx := m.idx
	if idx >= len(m.Responses) {
		idx = len(m.Responses) - 1
	} else {
		m.idx++
	}
	return m.Responses[idx], nil
}

// Capabilities implements agent.Capable.
func (m *MockProvider) Capabilities() agent.Capabilities { return m.CapabilitiesValue }

// Calls returns every request Generate has received so far, in order.
func (m *MockProvider) Calls() []*agent.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*agent.Request, len(m.calls))
	copy(out, m.calls)
	return out
}

var (
	_ agent.Provider = (*MockProvider)(nil)
	_ agent.Capable  = (*MockProvider)(nil)
)

// MockStreamingProvider extends MockProvider with a scriptable Stream
// method, for testing streaming call sites (Agent.RunStream) without a real
// StreamingProvider. It is a distinct type from MockProvider — rather than
// MockProvider always implementing Stream — so tests can also exercise
// Agent's non-streaming-provider fallback path (see
// agent.WithStreamingFallback) with a provider that genuinely has no Stream
// method, matching how a real minimal Provider looks.
type MockStreamingProvider struct {
	MockProvider

	// StreamFunc builds the event stream for a Stream call. If nil, Stream
	// synthesizes one from Generate (via Responses/OnGenerate): a single
	// text_delta (if the response has text) and a tool_call_start per tool
	// use, followed by message_done — enough to exercise RunStream call
	// sites without hand-writing an EventStream.
	StreamFunc func(req *agent.Request) (agent.EventStream, error)
}

// Stream implements agent.StreamingProvider.
func (m *MockStreamingProvider) Stream(ctx context.Context, req *agent.Request) (agent.EventStream, error) {
	if m.StreamFunc != nil {
		return m.StreamFunc(req)
	}
	resp, err := m.Generate(ctx, req)
	if err != nil {
		return nil, err
	}

	var events []agent.Event
	if text := resp.Message.Text(); text != "" {
		events = append(events, agent.Event{Type: agent.EventTextDelta, TextDelta: text})
	}
	for _, tu := range resp.Message.ToolUses() {
		call := agent.ToolCall{ID: tu.ID, Name: tu.Name, Input: tu.Input}
		events = append(events, agent.Event{Type: agent.EventToolCallStart, ToolCall: &call})
	}
	events = append(events, agent.Event{Type: agent.EventMessageDone, Response: resp})
	return agent.NewSliceStream(events...), nil
}

var _ agent.StreamingProvider = (*MockStreamingProvider)(nil)
