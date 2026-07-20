package agent

import (
	"context"
	"encoding/json"
)

// ToolCall describes a single tool invocation, passed to Hooks.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// Hooks are optional callbacks the Agent run loop invokes at well-defined
// points, covering the practical needs of a production agent: structured
// logging/metrics, tracing, human-in-the-loop tool approval, and error
// observation. Every field is optional; a nil hook is simply skipped.
type Hooks struct {
	// BeforeGenerate runs before every provider call. Returning a non-nil
	// error aborts the run without calling the provider.
	BeforeGenerate func(ctx context.Context, req *Request) error

	// AfterGenerate runs after every successful provider call.
	AfterGenerate func(ctx context.Context, resp *Response)

	// BeforeToolCall runs before a tool is invoked. Returning allow=false
	// skips the tool entirely; if override is non-nil, its ToolResult is
	// used as the tool's result (e.g. to synthesize a "denied by policy"
	// response the model can react to). If override is nil and allow is
	// false, a generic denial result is used.
	BeforeToolCall func(ctx context.Context, call ToolCall) (allow bool, override *ToolResult)

	// AfterToolCall runs after a tool has been invoked (whether it
	// succeeded, returned a model-recoverable error, or was denied by
	// BeforeToolCall).
	AfterToolCall func(ctx context.Context, call ToolCall, result ToolResult)

	// OnError runs whenever the run loop is about to return a fatal error
	// (a provider error that exhausted retries, a tool handler's Go error,
	// max iterations exceeded, etc.).
	OnError func(ctx context.Context, err error)
}
