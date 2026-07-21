package agent

// Option configures an Agent. Adding a new knob to Agent never breaks an
// existing call site, since options are applied by name, not position.
type Option func(*Agent)

// WithProvider sets the inference backend. Effectively required — an Agent
// built without one returns an error from every Run/RunStream call.
func WithProvider(p Provider) Option {
	return func(a *Agent) { a.provider = p }
}

// WithModel sets the model identifier passed to the provider on every
// request (e.g. "claude-opus-4-8", "gpt-4.1", "gemini-2.5-pro").
func WithModel(model string) Option {
	return func(a *Agent) { a.model = model }
}

// WithSystemPrompt sets the agent's system instructions. See SystemPrompt
// for composing static, cacheable, and dynamic sections.
func WithSystemPrompt(sp *SystemPrompt) Option {
	return func(a *Agent) {
		if sp == nil {
			sp = NewSystemPrompt()
		}
		a.system = sp
	}
}

// WithTools registers the tools available to the model. Calling WithTools
// more than once appends to, rather than replaces, the existing tool set.
func WithTools(tools ...RegisteredTool) Option {
	return func(a *Agent) {
		if a.tools == nil {
			a.tools = NewToolSet()
		}
		for _, t := range tools {
			a.tools.Add(t)
		}
	}
}

// WithToolChoice controls whether/how the model must use a tool. Defaults
// to ToolChoiceAuto.
func WithToolChoice(tc ToolChoice) Option {
	return func(a *Agent) { a.toolChoice = tc }
}

// WithMaxTokens sets the maximum tokens the model may generate per turn.
func WithMaxTokens(n int) Option {
	return func(a *Agent) { a.maxTokens = n }
}

// WithThinking enables extended reasoning per cfg. Leave unset (the
// default) to run without extended thinking.
func WithThinking(cfg ThinkingConfig) Option {
	return func(a *Agent) { a.thinking = &cfg }
}

// WithMaxIterations bounds how many model round-trips a single Run/RunStream
// call may take before it gives up with ErrMaxIterations. Defaults to
// DefaultMaxIterations. This is the hard backstop against a runaway tool
// loop; combine with a context deadline for a wall-clock bound as well.
func WithMaxIterations(n int) Option {
	return func(a *Agent) { a.maxIterations = n }
}

// WithMaxParallelTools bounds how many tool calls from a single model turn
// run concurrently. Zero (the default) means unbounded — every tool call in
// a turn runs at once, which is fine for typical single-digit tool-call
// counts but worth bounding when individual tools are resource-heavy (e.g.
// each spawns a subprocess).
func WithMaxParallelTools(n int) Option {
	return func(a *Agent) { a.maxParallel = n }
}

// WithHooks attaches observability/approval callbacks. See Hooks.
func WithHooks(h Hooks) Option {
	return func(a *Agent) { a.hooks = h }
}

// WithConversationStore sets the backing store used by Agent.NewSession.
// Defaults to an in-memory store.
func WithConversationStore(s ConversationStore) Option {
	return func(a *Agent) {
		if s != nil {
			a.store = s
		}
	}
}

// WithRetryPolicy overrides the default retry/backoff policy applied to
// retryable provider errors. See RetryPolicy and IsRetryable.
func WithRetryPolicy(rp RetryPolicy) Option {
	return func(a *Agent) { a.retry = rp }
}

// WithStreamingFallback controls RunStream's behavior against a provider
// that does not implement StreamingProvider. Defaults to FallbackSingleShot.
func WithStreamingFallback(mode StreamingFallbackMode) Option {
	return func(a *Agent) { a.streamFallback = mode }
}

// WithCompactor enables automatic history compaction on Session.Send: when
// the provider implements TokenCounter and a session's estimated token
// count is at or above thresholdTokens, c is invoked to shrink history
// before the turn runs, and the compacted history is what gets persisted.
// Unset by default — compaction is lossy, so it's an explicit opt-in rather
// than an automatic behavior change. See Compactor and NewWindowCompactor.
func WithCompactor(c Compactor, thresholdTokens int) Option {
	return func(a *Agent) {
		a.compactor = c
		a.compactTokens = thresholdTokens
	}
}
