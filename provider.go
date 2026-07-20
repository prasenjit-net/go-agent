package agent

import "context"

// Provider is the only interface a backend must implement to be usable by
// Agent. Everything else in this file is additive: a minimal provider that
// implements just this interface already gets a working Run loop, tool
// execution, hooks, and retries. Streaming, capability negotiation, and
// token counting are unlocked by implementing the optional interfaces below
// as needed — see StreamingProvider, Capable, and TokenCounter.
type Provider interface {
	// Name identifies the provider, e.g. "claude", "openai", "gemini". Used
	// in error messages and observability hooks.
	Name() string

	// Generate performs a single, non-streaming inference call.
	Generate(ctx context.Context, req *Request) (*Response, error)
}

// StreamingProvider is implemented by providers that can stream a response
// incrementally. If a Provider does not implement it, Agent.RunStream still
// works via a documented fallback (see WithStreamingFallback).
type StreamingProvider interface {
	Provider
	Stream(ctx context.Context, req *Request) (EventStream, error)
}

// TokenCounter is implemented by providers with a token-counting endpoint.
// Agent uses it, when present, for pre-flight context-window checks and
// cost-estimation helpers; it is never required.
type TokenCounter interface {
	CountTokens(ctx context.Context, req *Request) (int, error)
}

// SystemUpdater is implemented by providers that support injecting an
// operator instruction mid-conversation without rebuilding the system
// prompt (and, on providers with prompt caching, without invalidating the
// cached prefix). Agent.Note uses it when available and falls back to a
// synthetic reminder block otherwise, so the same call works everywhere.
type SystemUpdater interface {
	// SystemUpdateMessage returns the Message to append to history in order
	// to deliver note using this provider's native mechanism.
	SystemUpdateMessage(note string) (Message, error)
}

// Capable is implemented by providers that want to declare what they
// support, so Agent can validate configuration early — e.g. reject
// WithThinking() against a provider with no thinking support at
// construction time, instead of surfacing a confusing error from the wire
// on the first call.
type Capable interface {
	Capabilities() Capabilities
}

// Capabilities describes what a Provider supports. Zero values are always
// conservative ("not supported" / "unknown"), matching the Capabilities{}
// returned by CapabilitiesOf for a Provider that doesn't implement Capable.
type Capabilities struct {
	Streaming             bool
	Tools                 bool
	ParallelToolCalls     bool
	Vision                bool
	Documents             bool
	Thinking              bool
	SystemCaching         bool
	MidConversationSystem bool
	MaxContextTokens      int // 0 = unknown/unbounded
	MaxOutputTokens       int // 0 = unknown
}

// CapabilitiesOf returns p's declared capabilities, or a zero-value
// Capabilities{} if p does not implement Capable. Callers should always go
// through this function rather than a raw type assertion, so behavior stays
// consistent for minimal providers that skip Capable entirely.
func CapabilitiesOf(p Provider) Capabilities {
	if c, ok := p.(Capable); ok {
		return c.Capabilities()
	}
	return Capabilities{}
}
