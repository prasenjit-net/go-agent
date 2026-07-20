package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/prasenjit-net/go-agent/schema"
)

// ToolResult is what a tool handler returns. It becomes a ToolResultBlock
// in the next request sent to the model.
type ToolResult struct {
	Content []ContentBlock
	IsError bool
}

// TextResult builds a successful, plain-text ToolResult.
func TextResult(text string) ToolResult {
	return ToolResult{Content: []ContentBlock{TextBlock{Text: text}}}
}

// JSONResult marshals v and returns it as a successful ToolResult. If
// marshaling fails, an error ToolResult is returned instead (never a Go
// error — a tool that can't format its own output should be recoverable by
// the model, not fatal to the run).
func JSONResult(v any) ToolResult {
	b, err := json.Marshal(v)
	if err != nil {
		return ErrorResultf("failed to marshal tool result: %v", err)
	}
	return ToolResult{Content: []ContentBlock{TextBlock{Text: string(b)}}}
}

// ErrorResultf builds an error ToolResult with a formatted message. This is
// a model-recoverable error (the model sees it and can try something else,
// or explain the failure to the user) — distinct from a Go error returned
// from a tool handler, which the Agent run loop treats as fatal.
func ErrorResultf(format string, args ...any) ToolResult {
	return ToolResult{Content: []ContentBlock{TextBlock{Text: fmt.Sprintf(format, args...)}}, IsError: true}
}

// RegisteredTool is the type-erased interface the Agent run loop and every
// provider adapter operate on. Tool[TIn] implements it; so can any other
// type — e.g. a bridge to a tool whose schema is discovered at runtime
// rather than known at compile time (an MCP server, a plugin registry).
type RegisteredTool interface {
	Name() string
	Description() string
	Schema() *schema.Schema
	Invoke(ctx context.Context, input json.RawMessage) (ToolResult, error)
}

// Tool[TIn] is the primary, strongly-typed way to define a tool. TIn is a
// plain Go struct describing the tool's input; its JSON Schema is derived
// once via reflection (see the schema package) from `json` and `jsonschema`
// struct tags and cached. The handler receives a fully-typed, already
// unmarshalled TIn — there is no map[string]any and no manual type
// assertion anywhere in a tool's implementation.
type Tool[TIn any] struct {
	name        string
	description string
	handler     func(ctx context.Context, in TIn) (ToolResult, error)

	schemaOnce sync.Once
	schemaVal  *schema.Schema
}

// NewTool defines a tool named name, described by description (which the
// model uses to decide when to call it — be specific about *when*, not just
// what the tool does), backed by handler.
func NewTool[TIn any](name, description string, handler func(ctx context.Context, in TIn) (ToolResult, error)) *Tool[TIn] {
	return &Tool[TIn]{name: name, description: description, handler: handler}
}

func (t *Tool[TIn]) Name() string        { return t.name }
func (t *Tool[TIn]) Description() string { return t.description }

// Schema returns the JSON Schema for TIn, computed once via reflection and
// cached for the lifetime of the Tool.
func (t *Tool[TIn]) Schema() *schema.Schema {
	t.schemaOnce.Do(func() {
		t.schemaVal = schema.FromStruct[TIn]()
	})
	return t.schemaVal
}

// Invoke unmarshals raw into TIn and calls the handler. Malformed input
// (the model sent something that doesn't match the schema) never panics and
// never returns a Go error — it becomes a model-recoverable error
// ToolResult, since a Go error return is reserved for handler-level
// failures that should abort the run (see Hooks and the Agent run loop).
func (t *Tool[TIn]) Invoke(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var in TIn
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return ErrorResultf("invalid input for tool %q: %v", t.name, err), nil
		}
	}
	return t.handler(ctx, in)
}

// ToolSet is a small ordered collection of RegisteredTool, primarily useful
// when an application assembles its tool list from more than a couple of
// literals (e.g. built up conditionally, or shared across multiple agents).
type ToolSet struct {
	tools map[string]RegisteredTool
	order []string
}

// NewToolSet builds a ToolSet from the given tools, in order. Duplicate
// names overwrite earlier entries but keep their original position.
func NewToolSet(tools ...RegisteredTool) *ToolSet {
	ts := &ToolSet{tools: make(map[string]RegisteredTool, len(tools))}
	for _, t := range tools {
		ts.Add(t)
	}
	return ts
}

// Add registers t, replacing any existing tool with the same name.
func (ts *ToolSet) Add(t RegisteredTool) *ToolSet {
	if ts.tools == nil {
		ts.tools = make(map[string]RegisteredTool)
	}
	if _, exists := ts.tools[t.Name()]; !exists {
		ts.order = append(ts.order, t.Name())
	}
	ts.tools[t.Name()] = t
	return ts
}

// Get returns the tool registered under name, if any.
func (ts *ToolSet) Get(name string) (RegisteredTool, bool) {
	if ts == nil {
		return nil, false
	}
	t, ok := ts.tools[name]
	return t, ok
}

// List returns every registered tool, in registration order.
func (ts *ToolSet) List() []RegisteredTool {
	if ts == nil {
		return nil
	}
	out := make([]RegisteredTool, 0, len(ts.order))
	for _, name := range ts.order {
		out = append(out, ts.tools[name])
	}
	return out
}
