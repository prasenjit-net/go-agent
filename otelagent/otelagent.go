// Package otelagent provides an OpenTelemetry tracing agent.Hooks
// implementation. NewHooks starts one span per Agent.Generate call and one
// child span per tool call, tags them with provider/model/tool-name and
// (for Generate spans) token usage, and ends them from the matching
// After*/OnError hook.
//
// Shipped as a separate subpackage — not a core dependency — so the root
// module stays free of an OpenTelemetry dependency for callers who don't
// want tracing; import this package only if you do. Attribute names
// loosely follow OpenTelemetry's GenAI semantic conventions, which are
// still marked experimental upstream — treat them as unstable until that
// convention itself stabilizes.
package otelagent

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	agent "github.com/prasenjit-net/go-agent"
)

// NewHooks returns agent.Hooks that trace every Generate and tool call made
// through an Agent configured with agent.WithHooks(NewHooks(tracer, ...)).
// provider identifies the backend for the gen_ai.system attribute (e.g.
// "claude") — agent.Request/Response carry no provider name themselves, so
// the caller, who already chose the provider, supplies it once here.
//
// agent.Hooks has no way to thread a per-call correlation token through its
// Before/After pairs beyond the ctx and ToolCall values it's given, so
// spans are correlated by those: Generate spans by ctx identity (safe,
// since BeforeGenerate/AfterGenerate for one Agent.RunMessages call are
// always sequential for a given ctx, never concurrent), tool-call spans by
// (ctx, ToolCall.ID) (safe, since concurrent tool calls within one turn
// always carry distinct IDs). Two unrelated Agent.RunMessages calls that
// share the literal same ctx object (e.g. both passed context.Background()
// directly, with nothing derived from it) could see span cross-talk; giving
// each call its own derived context avoids that, and is standard practice
// regardless of tracing.
//
// If the caller also needs their own Hooks (e.g. tool-approval logic),
// compose them manually — agent.Hooks holds one function per event, not a
// chain, so only one BeforeGenerate/etc. can be installed at a time.
func NewHooks(tracer trace.Tracer, provider string) agent.Hooks {
	h := &tracingHooks{
		tracer:        tracer,
		provider:      provider,
		generateSpans: map[context.Context]trace.Span{},
		toolSpans:     map[toolKey]trace.Span{},
	}
	return agent.Hooks{
		BeforeGenerate: h.beforeGenerate,
		AfterGenerate:  h.afterGenerate,
		BeforeToolCall: h.beforeToolCall,
		AfterToolCall:  h.afterToolCall,
		OnError:        h.onError,
	}
}

type toolKey struct {
	ctx context.Context
	id  string
}

type tracingHooks struct {
	tracer   trace.Tracer
	provider string

	mu            sync.Mutex
	generateSpans map[context.Context]trace.Span
	toolSpans     map[toolKey]trace.Span
}

func (h *tracingHooks) beforeGenerate(ctx context.Context, req *agent.Request) error {
	_, span := h.tracer.Start(ctx, "agent.generate", trace.WithAttributes(
		attribute.String("gen_ai.system", h.provider),
		attribute.String("gen_ai.request.model", req.Model),
	))
	h.mu.Lock()
	h.generateSpans[ctx] = span
	h.mu.Unlock()
	return nil
}

func (h *tracingHooks) afterGenerate(ctx context.Context, resp *agent.Response) {
	span, ok := h.takeGenerateSpan(ctx)
	if !ok {
		return
	}
	span.SetAttributes(
		attribute.String("gen_ai.response.finish_reason", string(resp.StopReason)),
		attribute.Int("gen_ai.usage.input_tokens", resp.Usage.InputTokens),
		attribute.Int("gen_ai.usage.output_tokens", resp.Usage.OutputTokens),
	)
	span.End()
}

// onError ends the still-open Generate span for ctx, if any, recording err
// on it first. Reached instead of afterGenerate whenever RunMessages
// returns before a matching AfterGenerate fires — a BeforeGenerate error
// from a composed outer hook, or Generate itself failing after retries are
// exhausted — so this is this span's only chance to be closed. A max-
// iterations error has no open span by the time it fires (the last
// iteration's AfterGenerate already closed it) and a tool handler's error
// belongs to a tool span, not a Generate span (see afterToolCall) — both
// cases correctly no-op here since takeGenerateSpan finds nothing.
func (h *tracingHooks) onError(ctx context.Context, err error) {
	span, ok := h.takeGenerateSpan(ctx)
	if !ok {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	span.End()
}

func (h *tracingHooks) takeGenerateSpan(ctx context.Context) (trace.Span, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	span, ok := h.generateSpans[ctx]
	if ok {
		delete(h.generateSpans, ctx)
	}
	return span, ok
}

// beforeToolCall always allows the call through (allow=true, override=nil)
// — a tracing hook must never itself change application behavior.
func (h *tracingHooks) beforeToolCall(ctx context.Context, call agent.ToolCall) (bool, *agent.ToolResult) {
	_, span := h.tracer.Start(ctx, "agent.tool_call", trace.WithAttributes(
		attribute.String("gen_ai.tool.name", call.Name),
	))
	key := toolKey{ctx: ctx, id: call.ID}
	h.mu.Lock()
	h.toolSpans[key] = span
	h.mu.Unlock()
	return true, nil
}

func (h *tracingHooks) afterToolCall(ctx context.Context, call agent.ToolCall, result agent.ToolResult) {
	key := toolKey{ctx: ctx, id: call.ID}
	h.mu.Lock()
	span, ok := h.toolSpans[key]
	if ok {
		delete(h.toolSpans, key)
	}
	h.mu.Unlock()
	if !ok {
		return
	}
	if result.IsError {
		span.SetStatus(codes.Error, "tool call returned an error result")
	}
	span.End()
}
