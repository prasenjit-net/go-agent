package otelagent_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	agent "github.com/prasenjit-net/go-agent"
	"github.com/prasenjit-net/go-agent/agenttest"
	"github.com/prasenjit-net/go-agent/otelagent"
)

// recordingSpan captures what was done to it via the trace.Span API, for
// assertions. Embedding noop.Span satisfies every method this test doesn't
// care about (SpanContext, IsRecording, AddEvent, ...).
type recordingSpan struct {
	noop.Span
	name string

	mu         sync.Mutex
	ended      bool
	attrs      map[attribute.Key]attribute.Value
	errors     []error
	statusCode codes.Code
	statusDesc string
}

func (s *recordingSpan) SetAttributes(kv ...attribute.KeyValue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attrs == nil {
		s.attrs = map[attribute.Key]attribute.Value{}
	}
	for _, kv := range kv {
		s.attrs[kv.Key] = kv.Value
	}
}

func (s *recordingSpan) RecordError(err error, _ ...trace.EventOption) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errors = append(s.errors, err)
}

func (s *recordingSpan) SetStatus(code codes.Code, desc string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusCode = code
	s.statusDesc = desc
}

func (s *recordingSpan) End(...trace.SpanEndOption) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ended = true
}

func (s *recordingSpan) snapshot() (ended bool, attrs map[attribute.Key]attribute.Value, errs []error, code codes.Code, desc string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended, s.attrs, s.errors, s.statusCode, s.statusDesc
}

// recordingTracer creates recordingSpans and keeps every one it created, in
// creation order, so tests can inspect them after a run completes.
type recordingTracer struct {
	noop.Tracer

	mu    sync.Mutex
	spans []*recordingSpan
}

func (t *recordingTracer) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	span := &recordingSpan{name: name}
	cfg := trace.NewSpanStartConfig(opts...)
	span.SetAttributes(cfg.Attributes()...)

	t.mu.Lock()
	t.spans = append(t.spans, span)
	t.mu.Unlock()
	return ctx, span
}

func (t *recordingTracer) allSpans() []*recordingSpan {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*recordingSpan, len(t.spans))
	copy(out, t.spans)
	return out
}

var _ trace.Tracer = (*recordingTracer)(nil)

func TestNewHooks_TracesSimpleTextResponse(t *testing.T) {
	tracer := &recordingTracer{}
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{
			{
				Message:    agent.AssistantMessage(agent.TextBlock{Text: "hello"}),
				StopReason: agent.StopEndTurn,
				Usage:      agent.Usage{InputTokens: 10, OutputTokens: 5},
			},
		},
	}
	a := agent.New(
		agent.WithProvider(mock),
		agent.WithModel("mock-model"),
		agent.WithHooks(otelagent.NewHooks(tracer, "mock")),
	)

	if _, err := a.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	spans := tracer.allSpans()
	if len(spans) != 1 {
		t.Fatalf("len(spans) = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.name != "agent.generate" {
		t.Errorf("span name = %q, want agent.generate", span.name)
	}
	ended, attrs, errs, _, _ := span.snapshot()
	if !ended {
		t.Error("span was never ended")
	}
	if len(errs) != 0 {
		t.Errorf("errors = %v, want none", errs)
	}
	if attrs[attribute.Key("gen_ai.system")].AsString() != "mock" {
		t.Errorf("gen_ai.system = %q, want mock", attrs[attribute.Key("gen_ai.system")].AsString())
	}
	if attrs[attribute.Key("gen_ai.request.model")].AsString() != "mock-model" {
		t.Errorf("gen_ai.request.model = %q, want mock-model", attrs[attribute.Key("gen_ai.request.model")].AsString())
	}
	if attrs[attribute.Key("gen_ai.usage.input_tokens")].AsInt64() != 10 {
		t.Errorf("gen_ai.usage.input_tokens = %v, want 10", attrs[attribute.Key("gen_ai.usage.input_tokens")].AsInt64())
	}
	if attrs[attribute.Key("gen_ai.usage.output_tokens")].AsInt64() != 5 {
		t.Errorf("gen_ai.usage.output_tokens = %v, want 5", attrs[attribute.Key("gen_ai.usage.output_tokens")].AsInt64())
	}
}

func TestNewHooks_TracesToolCallAsChildSpan(t *testing.T) {
	tracer := &recordingTracer{}
	weatherTool := agent.NewTool("get_weather", "Get current weather.",
		func(_ context.Context, in struct {
			City string `json:"city" jsonschema:"required"`
		}) (agent.ToolResult, error) {
			return agent.TextResult("72F"), nil
		})
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{
			{
				Message: agent.AssistantMessage(agent.ToolUseBlock{
					ID: "call_1", Name: "get_weather", Input: json.RawMessage(`{"city":"Paris"}`),
				}),
				StopReason: agent.StopToolUse,
			},
			{Message: agent.AssistantMessage(agent.TextBlock{Text: "72F"}), StopReason: agent.StopEndTurn},
		},
	}
	a := agent.New(
		agent.WithProvider(mock),
		agent.WithModel("mock-model"),
		agent.WithTools(weatherTool),
		agent.WithHooks(otelagent.NewHooks(tracer, "mock")),
	)

	if _, err := a.Run(context.Background(), "weather in Paris?"); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	spans := tracer.allSpans()
	if len(spans) != 3 {
		t.Fatalf("len(spans) = %d, want 3 (generate, tool_call, generate)", len(spans))
	}
	toolSpan := spans[1]
	if toolSpan.name != "agent.tool_call" {
		t.Fatalf("spans[1].name = %q, want agent.tool_call", toolSpan.name)
	}
	ended, attrs, _, code, _ := toolSpan.snapshot()
	if !ended {
		t.Error("tool span was never ended")
	}
	if attrs[attribute.Key("gen_ai.tool.name")].AsString() != "get_weather" {
		t.Errorf("gen_ai.tool.name = %q, want get_weather", attrs[attribute.Key("gen_ai.tool.name")].AsString())
	}
	if code == codes.Error {
		t.Error("a successful tool call should not set status Error")
	}
}

func TestNewHooks_RecordsToolErrorOnToolSpan(t *testing.T) {
	tracer := &recordingTracer{}
	failingTool := agent.NewTool("risky", "Always fails.",
		func(_ context.Context, in struct{}) (agent.ToolResult, error) {
			return agent.ErrorResultf("boom"), nil
		})
	mock := &agenttest.MockProvider{
		Responses: []*agent.Response{
			{
				Message:    agent.AssistantMessage(agent.ToolUseBlock{ID: "call_1", Name: "risky", Input: json.RawMessage(`{}`)}),
				StopReason: agent.StopToolUse,
			},
			{Message: agent.AssistantMessage(agent.TextBlock{Text: "sorry"}), StopReason: agent.StopEndTurn},
		},
	}
	a := agent.New(
		agent.WithProvider(mock),
		agent.WithModel("mock-model"),
		agent.WithTools(failingTool),
		agent.WithHooks(otelagent.NewHooks(tracer, "mock")),
	)

	if _, err := a.Run(context.Background(), "try the risky tool"); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	spans := tracer.allSpans()
	toolSpan := spans[1]
	_, _, _, code, _ := toolSpan.snapshot()
	if code != codes.Error {
		t.Errorf("status code = %v, want codes.Error for a tool result with IsError=true", code)
	}
}

func TestNewHooks_RecordsGenerateErrorAndEndsSpan(t *testing.T) {
	tracer := &recordingTracer{}
	wantErr := &agent.Error{Provider: "mock", Code: agent.ErrInvalidRequest, Message: "boom", Retryable: false}
	mock := &agenttest.MockProvider{
		OnGenerate: func(_ *agent.Request) (*agent.Response, error) { return nil, wantErr },
	}
	a := agent.New(
		agent.WithProvider(mock),
		agent.WithModel("mock-model"),
		agent.WithHooks(otelagent.NewHooks(tracer, "mock")),
	)

	if _, err := a.Run(context.Background(), "hi"); err == nil {
		t.Fatal("expected Run to return an error")
	}

	spans := tracer.allSpans()
	if len(spans) != 1 {
		t.Fatalf("len(spans) = %d, want 1", len(spans))
	}
	ended, _, errs, code, _ := spans[0].snapshot()
	if !ended {
		t.Error("the generate span should be ended via onError, not leaked")
	}
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want exactly 1 recorded", errs)
	}
	if code != codes.Error {
		t.Errorf("status code = %v, want codes.Error", code)
	}
}
