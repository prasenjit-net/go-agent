# go-agent — Design Document

**Status:** Draft v0.1
**Module:** `github.com/<you>/go-agent` (replace `<you>` with the actual org/user before init)
**Root package name:** `agent`
**Target Go version:** 1.22+ (generics, `slices`/`maps` stdlib packages, `context` throughout)

---

## Table of Contents

1. [Goals & Non-Goals](#1-goals--non-goals)
2. [Design Principles](#2-design-principles)
3. [Prior Art & Positioning](#3-prior-art--positioning)
4. [Module & Package Layout](#4-module--package-layout)
5. [Core Domain Model](#5-core-domain-model)
6. [The Provider Interface](#6-the-provider-interface)
7. [First-Class Providers: Claude, OpenAI, Gemini](#7-first-class-providers-claude-openai-gemini)
8. [Adding a New Provider (Minimum Implementation)](#8-adding-a-new-provider-minimum-implementation)
9. [Strongly-Typed Tool Registration](#9-strongly-typed-tool-registration)
10. [System Instructions](#10-system-instructions)
11. [The Agent Type & Run Loop](#11-the-agent-type--run-loop)
12. [Streaming](#12-streaming)
13. [Conversation State, Sessions & Memory](#13-conversation-state-sessions--memory)
14. [Error Handling & Retries](#14-error-handling--retries)
15. [Concurrency, Context & Cancellation](#15-concurrency-context--cancellation)
16. [Observability: Hooks, Tracing, Usage](#16-observability-hooks-tracing-usage)
17. [Configuration Conventions](#17-configuration-conventions)
18. [Testing Strategy](#18-testing-strategy)
19. [Security Considerations](#19-security-considerations)
20. [Distribution, Versioning & Installation](#20-distribution-versioning--installation)
21. [End-to-End Examples](#21-end-to-end-examples)
22. [Roadmap](#22-roadmap)
23. [Open Questions](#23-open-questions)

---

## 1. Goals & Non-Goals

### Goals

- **One standard interface, many inference backends.** A single `Provider` interface that Claude, OpenAI, and Gemini all implement as first-class citizens, with identical `Agent`/tool/streaming code working unchanged across all three.
- **Minimum-effort extensibility.** A developer wiring up a fourth provider (Bedrock, Ollama, Mistral, a company-internal gateway, a local model server) should be able to do it by implementing **one method** (`Generate`), with everything else (streaming, capability flags, token counting) optional and additive.
- **Strongly typed tool registration.** Tools are defined as plain Go structs with typed handler functions — no hand-written JSON Schema, no `map[string]any` input, no runtime type assertions in tool bodies.
- **First-class system instructions.** Composable, cacheable, template-capable system prompts that translate correctly to each provider's native mechanism (including providers that support mid-conversation system updates).
- **Easy to embed.** A Go project should be able to `go get` the root module, wire a provider, and make its first agent call in under 15 lines. No code generation step, no YAML-driven bootstrapping required (though optional declarative config is a nice-to-have, see Roadmap).
- **Idiomatic Go.** Small interfaces, functional options, explicit `context.Context` propagation, no reflection magic beyond tool-schema generation, no panics across API boundaries.
- **Production-grade robustness.** Typed error taxonomy, retry/backoff, cancellation, streaming back-pressure, and hooks for observability from day one — not bolted on later.

### Non-Goals

- Not a RAG framework. No vector store abstraction, no embedding pipeline. (An embeddings-capable `Provider` extension is a reasonable future addition, but out of scope for v1.)
- Not a prompt-management SaaS client. System prompt composition is local and code-driven.
- Not a full multi-agent orchestration platform in v1. Single-agent tool-use loops are the core primitive; multi-agent delegation is a documented Phase 2+ extension built on the same primitives (see [Roadmap](#22-roadmap)).
- Not trying to match the surface area of Python's LangChain. The bias is toward a small, well-typed core over a sprawling plugin ecosystem.

---

## 2. Design Principles

1. **Small core interface, optional capability interfaces.** Mirrors `io.Reader` / `io.ReaderAt` / `io.Closer`: the mandatory `Provider` interface has exactly one method. `StreamingProvider`, `TokenCounter`, `Capable`, `SystemUpdater` etc. are separate interfaces a provider can additionally implement. The `Agent` type type-asserts and degrades gracefully when a capability is absent.
2. **Unified wire-neutral domain model.** `Message`, `ContentBlock`, `Request`, `Response` are provider-agnostic. All translation to/from a specific vendor's JSON shape happens inside that vendor's adapter package and nowhere else. Application code never imports a provider SDK's types directly.
3. **Compile-time safety for the things developers get wrong most often.** Tool input parsing is the single biggest source of runtime bugs in hand-rolled agent code (wrong JSON path, missed field, wrong type). Go generics remove that class of bug entirely: `Tool[TIn]`'s handler receives a fully-typed, already-unmarshalled `TIn`.
4. **No hidden global state.** No package-level client, no `init()`-registered providers required for the common path. A `Provider` is a value you construct and pass in. (A registry *is* provided for config-driven/dynamic use cases — see §8 — but it's opt-in.)
5. **Functional options everywhere configuration grows.** `agent.New(opts ...Option)`, `claude.New(apiKey string, opts ...claude.Option)`. Adding a new knob never breaks existing call sites.
6. **Escape hatches, not walls.** `Response.Raw` carries the provider-native response object. `RegisteredTool` is a plain interface so advanced users can bypass `Tool[TIn]` entirely (e.g., to bridge in MCP tools or dynamically-discovered schemas). Nothing in the design prevents dropping to the underlying SDK for a one-off feature that hasn't been abstracted yet.
7. **Pay for what you use.** The root module has effectively zero third-party runtime dependencies. Provider adapters (which pull in `anthropic-sdk-go`, `openai-go`, `google.golang.org/genai`) live in importable subpackages so a consumer who only wants Claude never needs OpenAI's or Gemini's SDK in their build.

---

## 3. Prior Art & Positioning

| Project | Language | Takeaway applied here |
|---|---|---|
| Anthropic Go SDK (`anthropic-sdk-go`) + `toolrunner` | Go | Struct-tag-driven schema generation (`jsonschema:"required,description=..."`) is proven ergonomic in Go; we adopt a compatible tag convention. |
| Claude Agent SDK / Claude Code harness | TS/Python | Hooks around tool execution (approve/deny/modify), system-prompt composition, and a bounded agent loop are the right shape for a harness, independent of language. |
| LangChainGo, Eino (ByteDance), Genkit-Go | Go | Validate that a unified `Message`/`ContentBlock` model plus a `Provider`-style interface works well in Go; we aim for a smaller core surface and stronger typing on the tool path than these provide today. |
| `io` package idioms | Go stdlib | The optional-capability-interface pattern (`io.Reader` vs `io.ReaderAt`) is the template for `Provider` vs `StreamingProvider`/`TokenCounter`/`Capable`. |

**Positioning:** go-agent is closer to "a well-typed, multi-vendor Messages API client with a built-in tool-use loop" than to a general orchestration framework. That scope is intentional — it's the 80% case, and it composes cleanly with anything else (queues, web frameworks, workflow engines) a Go service already uses.

---

## 4. Module & Package Layout

**Decision: start as a single Go module with subpackages**, not a multi-module workspace. Go only compiles packages that are actually imported, so a consumer who imports `github.com/<you>/go-agent/provider/claude` and never imports `.../provider/openai` never builds or ships OpenAI's SDK — the "pay for what you use" goal is satisfied without the operational overhead of independently-versioned nested modules (separate `go.mod` files, separate release tags like `provider/claude/v1.2.0`, `go.work` for local dev). Revisit this only if a specific provider SDK's dependency churn or version constraints start forcing unrelated releases of the root module — that's an explicit, revisitable trade-off, not a permanent one.

```
go-agent/                          module: github.com/<you>/go-agent
├── go.mod                         # near-zero third-party deps at the root
├── agent.go                       # Agent type, Run/RunStream, the loop
├── options.go                     # functional Option type + With* constructors
├── message.go                     # Role, ContentBlock, Message, constructors
├── request.go                     # Request, ToolChoice, ThinkingConfig
├── response.go                    # Response, StopReason, Usage
├── provider.go                    # Provider, StreamingProvider, TokenCounter,
│                                   #   Capable, Capabilities, CapabilitiesOf()
├── tool.go                        # Tool[TIn], RegisteredTool, ToolSet, ToolResult
├── system.go                      # SystemPrompt builder, SystemBlock
├── stream.go                      # Event, EventType, EventStream
├── session.go                     # Session, ConversationStore, InMemoryStore
├── errors.go                      # Error, ErrorCode, IsRetryable, RetryPolicy
├── hooks.go                       # Hooks, ToolCall
├── registry.go                    # optional: RegisterProvider/NewFromConfig
│
├── schema/                        # public Schema type + reflection generator
│   ├── schema.go                  #   (used by Tool[TIn]; also usable standalone
│   └── reflect.go                 #    for bridging externally-described tools, e.g. MCP)
│
├── provider/
│   ├── claude/                    # first-class: wraps anthropic-sdk-go
│   │   ├── claude.go
│   │   ├── translate.go           # Request/Response <-> Anthropic wire types
│   │   └── stream.go
│   ├── openai/                    # first-class: wraps openai-go
│   │   ├── openai.go
│   │   ├── translate.go
│   │   └── stream.go
│   └── gemini/                    # first-class: wraps google.golang.org/genai
│       ├── gemini.go
│       ├── translate.go
│       └── stream.go
│
├── providertest/                  # conformance suite for provider authors
│   └── conformance.go
├── agenttest/                     # mock.Provider for testing agent code
│   └── mock.go
│
├── examples/
│   ├── quickstart/
│   ├── tools/
│   ├── streaming/
│   ├── multiprovider/
│   └── custom-provider/
│
└── docs/
    └── DESIGN.md                  # this file
```

Import surface for a consumer who only wants Claude:

```go
import (
    "github.com/<you>/go-agent"
    "github.com/<you>/go-agent/provider/claude"
)
```

`go.mod` for that consumer picks up `anthropic-sdk-go` transitively but never `openai-go` or `google.golang.org/genai`, because those packages are never imported.

---

## 5. Core Domain Model

Everything downstream of this section — the agent loop, tool execution, streaming, all three provider adapters — operates purely on these types. No provider-specific type ever crosses this boundary except via the intentional `Response.Raw` escape hatch.

```go
// message.go

type Role string

const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
)

// ContentBlock is a closed sum type (sealed via an unexported method) covering
// every block shape the three first-class providers can send or receive.
type ContentBlock interface {
    contentBlock()
}

type TextBlock struct {
    Text string
}

type ImageBlock struct {
    Source ImageSource
}

type ImageSource struct {
    Kind      SourceKind // SourceBase64 | SourceURL
    MediaType string     // e.g. "image/png"
    Data      string     // base64 payload, or the URL, depending on Kind
}

type DocumentBlock struct {
    Source ImageSource // same shape; PDFs/text docs reuse SourceKind+Data
    Title  string
}

// ToolUseBlock is emitted by the model when it wants a tool invoked.
type ToolUseBlock struct {
    ID    string
    Name  string
    Input json.RawMessage
}

// ToolResultBlock carries a tool's output back to the model.
type ToolResultBlock struct {
    ToolUseID string
    Content   []ContentBlock
    IsError   bool
}

// ThinkingBlock carries extended-reasoning content. Signature is opaque and
// provider-specific; if a provider requires it echoed back unmodified on the
// next turn, the Agent loop does so automatically — application code never
// touches Signature directly.
type ThinkingBlock struct {
    Text      string
    Signature string
}

func (TextBlock) contentBlock()        {}
func (ImageBlock) contentBlock()       {}
func (DocumentBlock) contentBlock()    {}
func (ToolUseBlock) contentBlock()     {}
func (ToolResultBlock) contentBlock()  {}
func (ThinkingBlock) contentBlock()    {}

type Message struct {
    Role    Role
    Content []ContentBlock
}

func UserMessage(text string) Message {
    return Message{Role: RoleUser, Content: []ContentBlock{TextBlock{Text: text}}}
}

func UserMessageBlocks(blocks ...ContentBlock) Message {
    return Message{Role: RoleUser, Content: blocks}
}

func AssistantMessage(blocks ...ContentBlock) Message {
    return Message{Role: RoleAssistant, Content: blocks}
}
```

```go
// request.go

type Request struct {
    Model      string
    System     []SystemBlock
    Messages   []Message
    Tools      []RegisteredTool
    ToolChoice ToolChoice
    MaxTokens  int
    Thinking   *ThinkingConfig
    Metadata   map[string]string // free-form, provider adapters may ignore
}

type ThinkingMode string

const (
    ThinkingOff      ThinkingMode = "off"
    ThinkingAdaptive ThinkingMode = "adaptive" // provider decides depth
    ThinkingBudgeted ThinkingMode = "budgeted" // fixed token budget, legacy models
)

type ThinkingConfig struct {
    Mode   ThinkingMode
    Budget int // only consulted when Mode == ThinkingBudgeted
}

type ToolChoiceMode string

const (
    ToolChoiceAuto ToolChoiceMode = "auto"
    ToolChoiceAny  ToolChoiceMode = "any"
    ToolChoiceOne  ToolChoiceMode = "tool"
    ToolChoiceNone ToolChoiceMode = "none"
)

type ToolChoice struct {
    Mode ToolChoiceMode
    Name string // required when Mode == ToolChoiceOne
}
```

```go
// response.go

type StopReason string

const (
    StopEndTurn       StopReason = "end_turn"
    StopMaxTokens      StopReason = "max_tokens"
    StopToolUse        StopReason = "tool_use"
    StopRefusal        StopReason = "refusal"
    StopContentFilter  StopReason = "content_filter"
)

type Usage struct {
    InputTokens        int
    OutputTokens       int
    CacheReadTokens    int
    CacheCreationTokens int
}

type Response struct {
    ID         string
    Model      string
    Message    Message // Role is always RoleAssistant
    StopReason StopReason
    Usage      Usage

    // Raw is the provider-native response object (e.g. *anthropic.Message,
    // *openai.ChatCompletion, *genai.GenerateContentResponse). Escape hatch
    // for provider-specific fields not yet promoted into the unified model.
    // Application code that reads Raw is coupled to that provider by
    // definition — used sparingly, and never inside the core Agent loop.
    Raw any
}
```

A capability matrix (below, §7) tracks which of these fields each first-class provider actually populates.

---

## 6. The Provider Interface

```go
// provider.go

// Provider is the only interface a backend must implement to be usable by
// Agent. Everything else is additive.
type Provider interface {
    Name() string
    Generate(ctx context.Context, req *Request) (*Response, error)
}

// StreamingProvider is implemented by providers that can stream. If absent,
// Agent.RunStream still works: it falls back to a single Generate call and
// synthesizes a stream with one EventMessageDone event (configurable — see
// WithStreamingFallback in §12).
type StreamingProvider interface {
    Provider
    Stream(ctx context.Context, req *Request) (EventStream, error)
}

// TokenCounter is implemented by providers with a token-counting endpoint.
// Agent uses it (when present) for pre-flight context-window checks and for
// cost estimation helpers; it is never required.
type TokenCounter interface {
    CountTokens(ctx context.Context, req *Request) (int, error)
}

// SystemUpdater is implemented by providers that support injecting an
// operator instruction mid-conversation without rebuilding the whole system
// prompt (and, on providers with prompt caching, without invalidating the
// cached prefix). See §10.
type SystemUpdater interface {
    // Returns a Message to append (provider-specific role/shape) rather than
    // mutating Request.System, so the Agent loop can append it to history
    // like any other turn.
    SystemUpdateMessage(note string) (Message, error)
}

// Capable is implemented by providers that want to declare what they
// support, so Agent can validate configuration early (fail fast on
// WithThinking() against a provider with no thinking support, for example)
// instead of surfacing a confusing 400 from the wire.
type Capable interface {
    Capabilities() Capabilities
}

type Capabilities struct {
    Streaming         bool
    Tools             bool
    ParallelToolCalls bool
    Vision            bool
    Documents         bool
    Thinking          bool
    SystemCaching     bool
    MidConversationSystem bool
    MaxContextTokens  int  // 0 = unknown/unbounded
    MaxOutputTokens   int  // 0 = unknown
}

// CapabilitiesOf returns p's declared capabilities, or a conservative
// zero-value Capabilities{} if p does not implement Capable. Agent always
// goes through this function rather than a raw type assertion.
func CapabilitiesOf(p Provider) Capabilities {
    if c, ok := p.(Capable); ok {
        return c.Capabilities()
    }
    return Capabilities{}
}
```

**Why this shape:** a developer wrapping, say, a local Ollama server needs to write exactly one method (`Generate`) to get a working `Agent` — no streaming, no tool-choice nuance, no capability declaration required. As they invest more, they add `Stream`, then `Capabilities()`, then `CountTokens`, each one unlocking more of the framework's behavior, without ever having to revisit the first method they wrote.

---

## 7. First-Class Providers: Claude, OpenAI, Gemini

Each lives in its own subpackage, wraps the vendor's official Go SDK, and implements `Provider`, `StreamingProvider`, `TokenCounter`, and `Capable` in full. Construction follows the same functional-options shape across all three so switching providers is a one-line change in application code (see §21, "Provider swap example").

```go
// provider/claude
func New(apiKey string, opts ...Option) *Client
// Option: WithBaseURL, WithHTTPClient, WithBetaHeaders, WithDefaultModel

// provider/openai
func New(apiKey string, opts ...Option) *Client
// Option: WithBaseURL, WithHTTPClient, WithOrganization, WithDefaultModel

// provider/gemini
func New(apiKey string, opts ...Option) *Client
// Option: WithHTTPClient, WithProject/WithLocation (Vertex mode), WithDefaultModel
```

Each adapter's `translate.go` owns 100% of the mapping between the unified model and that vendor's wire format, including:

- `ContentBlock` ↔ vendor content-block union (e.g. Claude's `ContentBlockParamUnion`, OpenAI's `ChatCompletionMessageParam` content parts, Gemini's `Part`).
- `RegisteredTool.Schema()` → vendor tool/function-declaration JSON shape.
- `ToolChoice` → vendor `tool_choice` / `tool_config` shape (including the providers where "force one specific tool" is expressed differently).
- `ThinkingConfig` → vendor extended-thinking / reasoning-effort parameter (Claude's `thinking`, OpenAI's `reasoning_effort`, Gemini's `thinkingConfig`), including no-op when a provider doesn't support it and `Capabilities().Thinking == false`.
- Vendor error types → the unified `*Error` (see §14) — rate limits, auth failures, overloaded, context-length-exceeded, and safety refusals all map to a common `ErrorCode` regardless of vendor-specific spelling.
- Vendor SSE/stream event shapes → the unified `Event` stream (see §12).

### Capability matrix (initial target — adjust as each adapter lands)

| Capability | Claude | OpenAI | Gemini |
|---|---|---|---|
| Streaming | ✅ | ✅ | ✅ |
| Tools / function calling | ✅ | ✅ | ✅ |
| Parallel tool calls | ✅ | ✅ | ✅ |
| Vision (images) | ✅ | ✅ | ✅ |
| Documents (PDF) | ✅ | ✅ (via file input) | ✅ |
| Extended thinking / reasoning | ✅ (adaptive) | ✅ (`reasoning_effort`) | ✅ (`thinkingConfig`) |
| Prompt caching hints | ✅ (`cache_control`) | ➖ (no-op, ignored) | ➖ (implicit caching, no explicit hint) |
| Mid-conversation system update | ✅ (`role: system` message, newest models) | ➖ (fallback: synthetic reminder) | ➖ (fallback: synthetic reminder) |
| Token counting endpoint | ✅ | ✅ (tiktoken-equivalent local estimate, flagged as approximate) | ✅ |

Where a vendor doesn't support a feature natively, the adapter degrades to the documented fallback (e.g., `SystemBlock.Cacheable` is simply dropped; a mid-conversation system note becomes a synthetic `<system-reminder>` user-turn block) rather than erroring — the same `Agent` code runs unmodified against any of the three, with best-effort behavior on the gaps. `Capabilities()` lets an application query this instead of guessing.

---

## 8. Adding a New Provider (Minimum Implementation)

This is the concrete recipe a developer follows to bring up provider #4 (Bedrock, Mistral, a local llama.cpp server, an internal gateway, etc.).

**Step 1 — required.** Implement `Provider`:

```go
type MyProvider struct{ /* http client, base URL, api key, whatever */ }

func (p *MyProvider) Name() string { return "myprovider" }

func (p *MyProvider) Generate(ctx context.Context, req *agent.Request) (*agent.Response, error) {
    wireReq := toWireFormat(req)          // your translation
    wireResp, err := p.call(ctx, wireReq)
    if err != nil {
        return nil, translateError(err)   // wrap into *agent.Error where you can
    }
    return fromWireFormat(wireResp), nil
}
```

That's it — `agent.New(agent.WithProvider(&MyProvider{...}), ...)` now works: tool loop, hooks, retries, and non-streaming `Run` all function. `RunStream` also works via the automatic fallback (single `Generate` call, synthesized as a one-shot stream).

**Step 2 — optional, unlocks streaming.** Implement `Stream`:

```go
func (p *MyProvider) Stream(ctx context.Context, req *agent.Request) (agent.EventStream, error) {
    // open your SSE/websocket connection, return an agent.EventStream
    // implementation that translates your wire events to agent.Event
}
```

**Step 3 — optional, unlocks capability-aware behavior and fail-fast validation.** Implement `Capable`:

```go
func (p *MyProvider) Capabilities() agent.Capabilities {
    return agent.Capabilities{Streaming: true, Tools: true, Vision: false}
}
```

**Step 4 — optional.** Implement `TokenCounter` and/or `SystemUpdater` if applicable.

**Step 5 — validate.** Run the shared conformance suite against your implementation:

```go
func TestMyProviderConformance(t *testing.T) {
    providertest.Run(t, NewFromEnv(t), providertest.Options{
        SkipStreaming: false, // or true if you skipped Step 2
        SkipTools:     false,
        Model:         "my-default-model",
    })
}
```

`providertest.Run` exercises: a plain text round-trip, a single tool-call round-trip, a forced tool-choice round-trip, a streaming round-trip (if not skipped), an error-mapping check against a deliberately invalid request, and a context-cancellation check. It's the same suite the three first-class adapters are held to, published specifically so third-party/community providers have an objective bar.

**Optional: config-driven construction.** For applications that select a provider at runtime from config rather than code (e.g., a multi-tenant service where the tenant's plan determines the model vendor), an opt-in registry is provided:

```go
// registry.go
func RegisterProvider(name string, factory func(cfg map[string]string) (Provider, error))
func NewProviderFromConfig(name string, cfg map[string]string) (Provider, error)
```

The three first-class packages self-register under `init()` *only if imported* (`provider/claude`, `provider/openai`, `provider/gemini` each call `agent.RegisterProvider("claude", ...)` etc. in their own `init()`), so this stays consistent with the "pay for what you import" principle — the registry is never populated with a provider whose package wasn't linked in.

---

## 9. Strongly-Typed Tool Registration

This is one of the two areas (with system instructions) the requirements called out explicitly, so the design leans hard into Go generics + struct tags to eliminate stringly-typed/`map[string]any` tool code entirely.

```go
// tool.go

type ToolResult struct {
    Content []ContentBlock
    IsError bool
}

func TextResult(s string) ToolResult {
    return ToolResult{Content: []ContentBlock{TextBlock{Text: s}}}
}

func JSONResult(v any) ToolResult {
    b, err := json.Marshal(v)
    if err != nil {
        return ErrorResultf("failed to marshal result: %v", err)
    }
    return ToolResult{Content: []ContentBlock{TextBlock{Text: string(b)}}}
}

func ErrorResultf(format string, a ...any) ToolResult {
    return ToolResult{Content: []ContentBlock{TextBlock{Text: fmt.Sprintf(format, a...)}}, IsError: true}
}

// RegisteredTool is the type-erased interface the Agent loop and provider
// adapters actually operate on. Tool[TIn] implements it; so can anything
// else (e.g. a bridge to externally-described MCP tools).
type RegisteredTool interface {
    Name() string
    Description() string
    Schema() schema.Schema
    Invoke(ctx context.Context, raw json.RawMessage) (ToolResult, error)
}

// Tool[TIn] is the primary, strongly-typed way to define a tool. TIn is a
// plain Go struct; its JSON schema is derived once via reflection from
// struct tags and cached.
type Tool[TIn any] struct {
    name        string
    description string
    handler     func(ctx context.Context, in TIn) (ToolResult, error)
    schemaOnce  sync.Once
    schemaVal   schema.Schema
}

func NewTool[TIn any](
    name, description string,
    handler func(ctx context.Context, in TIn) (ToolResult, error),
) *Tool[TIn] {
    return &Tool[TIn]{name: name, description: description, handler: handler}
}

func (t *Tool[TIn]) Name() string        { return t.name }
func (t *Tool[TIn]) Description() string { return t.description }

func (t *Tool[TIn]) Schema() schema.Schema {
    t.schemaOnce.Do(func() {
        t.schemaVal = schema.FromStruct[TIn]()
    })
    return t.schemaVal
}

func (t *Tool[TIn]) Invoke(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
    var in TIn
    if err := json.Unmarshal(raw, &in); err != nil {
        return ErrorResultf("invalid tool input: %v", err), nil // not a Go error: a
                                                                  // model-recoverable
                                                                  // tool_result error
    }
    return t.handler(ctx, in)
}
```

**Defining a tool** — the whole developer-facing surface:

```go
type WeatherInput struct {
    City  string `json:"city" jsonschema:"required,description=City name, e.g. Paris"`
    Units string `json:"units,omitempty" jsonschema:"enum=celsius;fahrenheit,description=Temperature unit"`
}

var GetWeather = agent.NewTool(
    "get_weather",
    "Get the current weather for a city. Call this when the user asks about current conditions.",
    func(ctx context.Context, in WeatherInput) (agent.ToolResult, error) {
        report, err := weatherClient.Lookup(ctx, in.City, in.Units)
        if err != nil {
            return agent.ErrorResultf("lookup failed: %v", err), nil
        }
        return agent.JSONResult(report), nil
    },
)
```

The struct-tag convention deliberately mirrors `anthropic-sdk-go`'s existing `jsonschema:"required,description=..."` syntax (see §3) rather than inventing a new one, so it's already-familiar to anyone who has used Anthropic's Go tooling. The `schema` package (§ below) supports:

| Tag key | Effect |
|---|---|
| `required` | marks the field required in the generated schema |
| `description=...` | tool-parameter description shown to the model |
| `enum=a;b;c` | restricts to an enum (semicolon-separated; comma is reserved for the outer tag list) |
| (nested struct) | recurses into a nested `object` schema |
| `[]T` / `[N]T` | maps to a JSON `array` schema with `items` |
| `*T` | optional field unless explicitly marked `required` |

`schema.FromStruct[TIn]()` is exported from `github.com/<you>/go-agent/schema` so it's independently usable — e.g. to build an MCP tool bridge (a `RegisteredTool` wrapping a schema fetched at runtime from an MCP server rather than reflected from a Go type), or to validate a hand-written schema against a struct in tests.

**Registering tools on an agent:**

```go
tools := agent.NewToolSet(GetWeather, SearchDocs, SendEmail)

a := agent.New(
    agent.WithProvider(claudeProvider),
    agent.WithModel("claude-opus-4-8"),
    agent.WithTools(tools.List()...),
)
```

`ToolSet` is a small convenience wrapper (`Add`, `Get(name)`, `List()`) — `WithTools` also accepts bare `RegisteredTool` values directly for the common case of two or three tools:

```go
agent.WithTools(GetWeather, SearchDocs)
```

**Design rationale:** compare this to the typical hand-rolled Go tool-use loop, where the tool body starts with `var in map[string]any; json.Unmarshal(raw, &in); city, ok := in["city"].(string)` — every one of those steps is a latent bug (wrong key, wrong type assertion, missing nil check). `Tool[TIn]` removes all of it: if the struct compiles, the field access in the handler is guaranteed type-correct, and the schema Claude/OpenAI/Gemini see is generated from the exact same struct the handler consumes, so schema and implementation can never drift apart.

---

## 10. System Instructions

Called out explicitly in the requirements as needing "very good support." The design goals here: composability (build a system prompt out of independently-testable pieces), dynamic content (inject values at request time without string-concatenation soup), first-class support for prompt-caching hints on providers that have them, and a clean story for updating instructions mid-conversation.

```go
// system.go

type SystemBlock struct {
    Text      string
    Cacheable bool // hint only; providers without caching support ignore it
}

type SystemPrompt struct {
    parts []systemPart
}

type systemPart interface {
    render(ctx context.Context) (SystemBlock, error)
}

func NewSystemPrompt() *SystemPrompt { return &SystemPrompt{} }

// Add appends a static, non-cacheable instruction block.
func (s *SystemPrompt) Add(text string) *SystemPrompt

// AddCacheable appends a static instruction block hinted as cacheable — use
// this for large, stable content (few-shot examples, a knowledge base dump,
// tool-usage policy) that doesn't change between requests.
func (s *SystemPrompt) AddCacheable(text string) *SystemPrompt

// AddFunc appends a block computed at render time — e.g. current date, the
// authenticated user's name, feature flags. Evaluated fresh on every Run.
func (s *SystemPrompt) AddFunc(fn func(ctx context.Context) (string, error)) *SystemPrompt

// AddTemplate renders a text/template with data at render time. Convenience
// wrapper over AddFunc for the common "instructions with placeholders" case.
func (s *SystemPrompt) AddTemplate(tmpl string, data any) *SystemPrompt

// Render evaluates all parts (in order) into the final ordered list of
// SystemBlock the Request carries. Called once per Agent.Run/RunStream
// invocation, not memoized, so AddFunc/AddTemplate content is always fresh.
func (s *SystemPrompt) Render(ctx context.Context) ([]SystemBlock, error)
```

**Composition example** — a support-bot system prompt built from independently testable sections:

```go
sp := agent.NewSystemPrompt().
    Add("You are a customer support agent for Acme Corp. Be concise and factual.").
    AddCacheable(knowledgeBaseDump).                 // large, stable -> cached by Claude
    AddFunc(func(ctx context.Context) (string, error) {
        user := userFromContext(ctx)
        return fmt.Sprintf("The current user is %s (plan: %s).", user.Name, user.Plan), nil
    }).
    AddTemplate("Today's date is {{.Date}}.", map[string]string{"Date": time.Now().Format("2006-01-02")})
```

Each `Add*` call returns `*SystemPrompt`, so sections chain, but each section is also just a closure/value that can be unit-tested independently (e.g. assert `AddFunc`'s function produces the right string for a fixture `context.Context`, without touching the network).

**Ordering matters for prompt caching** (see the underlying provider guidance this design follows: caching is a prefix match, so stable content must precede volatile content). `SystemPrompt.Render` preserves call order, so the convention is: static/cacheable sections first, dynamic (`AddFunc`/`AddTemplate`) sections last. The design doc calls this out in the package doc comment on `SystemPrompt` rather than enforcing it structurally, since enforcing an ordering constraint would fight legitimate cases (e.g., a cacheable block that must appear after a short dynamic preamble).

**Per-provider translation:**

- Claude adapter: `SystemBlock{Cacheable: true}` → a system content block with `cache_control: {type: "ephemeral"}`; `Cacheable: false` → a plain text block.
- OpenAI adapter: all `SystemBlock`s concatenate into the leading `system` role message; `Cacheable` is a no-op (OpenAI's automatic caching needs no explicit hint).
- Gemini adapter: `SystemBlock`s become `systemInstruction`; `Cacheable` is a no-op (implicit caching).

**Mid-conversation updates.** For providers implementing `SystemUpdater` (§6), `Agent.Note` uses the provider's native mechanism; otherwise it falls back to a synthetic reminder block so the same call always works:

```go
func (a *Agent) Note(ctx context.Context, text string) error {
    if su, ok := a.provider.(agent.SystemUpdater); ok {
        msg, err := su.SystemUpdateMessage(text)
        if err != nil {
            return err
        }
        a.pending = append(a.pending, msg)
        return nil
    }
    // Fallback: inject as a clearly-delimited synthetic reminder in the next
    // user turn, so behavior is consistent (if lower-fidelity) across every
    // provider rather than erroring on providers without native support.
    a.pendingNote = &text
    return nil
}
```

This mirrors the requirement's emphasis on "very good support for system instructions" by making the feature work everywhere, with the best available fidelity per provider, rather than only on the one vendor that has a native primitive for it.

---

## 11. The Agent Type & Run Loop

```go
// agent.go

type Agent struct {
    provider      Provider
    model         string
    system        *SystemPrompt
    tools         *ToolSet
    toolChoice    ToolChoice
    maxTokens     int
    thinking      *ThinkingConfig
    maxIterations int
    hooks         Hooks
    store         ConversationStore
    retry         RetryPolicy
}

type Option func(*Agent)

func WithProvider(p Provider) Option
func WithModel(model string) Option
func WithSystemPrompt(sp *SystemPrompt) Option
func WithTools(tools ...RegisteredTool) Option
func WithToolChoice(tc ToolChoice) Option
func WithMaxTokens(n int) Option
func WithThinking(cfg ThinkingConfig) Option
func WithMaxIterations(n int) Option        // default: 25; hard cap against runaway loops
func WithHooks(h Hooks) Option
func WithConversationStore(s ConversationStore) Option
func WithRetryPolicy(rp RetryPolicy) Option

func New(opts ...Option) *Agent {
    a := &Agent{
        maxIterations: 25,
        toolChoice:    ToolChoice{Mode: ToolChoiceAuto},
        store:         NewInMemoryStore(),
        retry:         DefaultRetryPolicy(),
    }
    for _, opt := range opts {
        opt(a)
    }
    return a
}
```

`New` validates eagerly where it can: if `WithThinking` is set and `CapabilitiesOf(provider).Thinking == false`, `New` doesn't panic (constructors shouldn't), but the first `Run` call returns a clear `*Error{Code: ErrInvalidRequest}` explaining the mismatch, rather than a raw provider 400.

**Result and the run methods:**

```go
type Result struct {
    FinalResponse *Response
    Messages      []Message // the full transcript appended during this Run
    Usage         Usage     // summed across every iteration (incl. tool round-trips)
    Iterations    int
}

func (a *Agent) Run(ctx context.Context, input string) (*Result, error)
func (a *Agent) RunMessages(ctx context.Context, msgs ...Message) (*Result, error)
func (a *Agent) RunStream(ctx context.Context, input string) (EventStream, error)
```

**The loop** (pseudocode — this is the same shape described conceptually in §"how an AI agent works" earlier in this conversation, now as concrete control flow):

```
func (a *Agent) run(ctx, history []Message) (*Result, error) {
    usage := Usage{}
    for iter := 0; iter < a.maxIterations; iter++ {
        systemBlocks, err := a.system.Render(ctx)          // §10
        req := &Request{
            Model: a.model, System: systemBlocks, Messages: history,
            Tools: a.tools.List(), ToolChoice: a.toolChoice,
            MaxTokens: a.maxTokens, Thinking: a.thinking,
        }

        if a.hooks.BeforeGenerate != nil {
            if err := a.hooks.BeforeGenerate(ctx, req); err != nil { return nil, err }
        }

        resp, err := a.generateWithRetry(ctx, req)          // §14
        if err != nil {
            if a.hooks.OnError != nil { a.hooks.OnError(ctx, err) }
            return nil, err
        }
        usage.Add(resp.Usage)
        if a.hooks.AfterGenerate != nil { a.hooks.AfterGenerate(ctx, resp) }

        history = append(history, resp.Message)

        if resp.StopReason != StopToolUse {
            return &Result{FinalResponse: resp, Messages: history, Usage: usage, Iterations: iter+1}, nil
        }

        toolResults := a.executeTools(ctx, resp.Message.Content) // parallel, see below
        history = append(history, Message{Role: RoleUser, Content: toolResults})
    }
    return nil, &Error{Code: ErrMaxIterations, ...}
}
```

**Tool execution** runs every `ToolUseBlock` in a response concurrently (matching how every first-class provider expects *all* `tool_result`s for a turn to come back in a single next message — see the tool-use conventions referenced earlier in this conversation), gated by `Hooks.BeforeToolCall`/`AfterToolCall`:

```go
func (a *Agent) executeTools(ctx context.Context, blocks []ContentBlock) []ContentBlock {
    var toolUses []ToolUseBlock
    for _, b := range blocks {
        if tu, ok := b.(ToolUseBlock); ok {
            toolUses = append(toolUses, tu)
        }
    }
    results := make([]ContentBlock, len(toolUses))
    var wg sync.WaitGroup
    for i, tu := range toolUses {
        wg.Add(1)
        go func(i int, tu ToolUseBlock) {
            defer wg.Done()
            results[i] = a.invokeOne(ctx, tu)
        }(i, tu)
    }
    wg.Wait()
    return results
}

func (a *Agent) invokeOne(ctx context.Context, tu ToolUseBlock) ContentBlock {
    call := ToolCall{ID: tu.ID, Name: tu.Name, Input: tu.Input}

    if a.hooks.BeforeToolCall != nil {
        allow, override := a.hooks.BeforeToolCall(ctx, call)
        if !allow {
            r := ToolResult{Content: []ContentBlock{TextBlock{Text: "denied by policy"}}, IsError: true}
            if override != nil { r = *override }
            return ToolResultBlock{ToolUseID: tu.ID, Content: r.Content, IsError: r.IsError}
        }
    }

    tool, ok := a.tools.Get(tu.Name)
    var result ToolResult
    if !ok {
        result = ErrorResultf("unknown tool %q", tu.Name)
    } else {
        var err error
        result, err = tool.Invoke(ctx, tu.Input)
        if err != nil {
            // A Go error from a tool handler (vs a model-recoverable
            // ToolResult{IsError:true}) is treated as fatal to the run —
            // it indicates a programming error, not something the model
            // can route around. Surfaced via Hooks.OnError and aborts.
            result = ErrorResultf("tool %q failed: %v", tu.Name, err)
        }
    }

    if a.hooks.AfterToolCall != nil { a.hooks.AfterToolCall(ctx, call, result) }
    return ToolResultBlock{ToolUseID: tu.ID, Content: result.Content, IsError: result.IsError}
}
```

`WithMaxIterations` is the hard backstop against a runaway tool loop; combined with `context.WithTimeout` at the call site, this gives two independent safety limits (iteration count and wall-clock time), matching the requirement for a "robust" API — a caller can't accidentally build an agent that spins forever.

---

## 12. Streaming

A unified, pull-based event model — chosen over a channel-based or range-over-func API for maximum compatibility and the simplest possible cancellation story (the caller drives iteration, so `ctx` cancellation is checked exactly where the caller expects it, same shape as `sql.Rows`).

```go
// stream.go

type EventType string

const (
    EventTextDelta     EventType = "text_delta"
    EventThinkingDelta EventType = "thinking_delta"
    EventToolCallStart EventType = "tool_call_start"
    EventToolCallDelta EventType = "tool_call_delta" // streamed partial JSON input
    EventToolCallEnd   EventType = "tool_call_end"
    EventToolResult    EventType = "tool_result"     // emitted after Agent executes a tool
    EventMessageDone   EventType = "message_done"     // one full turn complete
    EventRunDone       EventType = "run_done"         // whole Run/RunStream finished
)

type Event struct {
    Type      EventType
    TextDelta string
    ToolCall  *ToolCall   // set on tool_call_* events
    ToolResult *ToolResult // set on tool_result
    Response  *Response   // set on message_done
    Result    *Result     // set on run_done
}

// EventStream is returned by StreamingProvider.Stream and Agent.RunStream.
// Next returns io.EOF when the stream is exhausted (mirrors sql.Rows /
// bufio.Scanner conventions rather than inventing a new idiom).
type EventStream interface {
    Next(ctx context.Context) (Event, error)
    Close() error
}
```

`Agent.RunStream` wraps the provider-level stream with the same tool-execution loop described in §11: when a provider stream reports the assistant turn ended with `tool_use`, the `Agent` executes the tool(s) (emitting `EventToolCallStart`/`EventToolResult` as it goes), opens a *new* provider stream for the next turn, and keeps yielding a single logical `EventStream` to the caller — from the caller's perspective there is one stream for the whole run, tool round-trips included.

```go
stream, err := a.RunStream(ctx, "Summarize this repo's README, using the read_file tool.")
if err != nil { ... }
defer stream.Close()

for {
    ev, err := stream.Next(ctx)
    if errors.Is(err, io.EOF) {
        break
    }
    if err != nil { ... }

    switch ev.Type {
    case agent.EventTextDelta:
        fmt.Print(ev.TextDelta)
    case agent.EventToolCallStart:
        fmt.Printf("\n[calling %s]\n", ev.ToolCall.Name)
    case agent.EventRunDone:
        fmt.Printf("\n\n(used %d tokens across %d turns)\n", ev.Result.Usage.OutputTokens, ev.Result.Iterations)
    }
}
```

**Providers without native streaming** (only `Generate` implemented): `agent.WithStreamingFallback(mode)` controls behavior —

- `FallbackSingleShot` (default): `RunStream` performs one blocking `Generate` per turn and emits its entire text as one `EventTextDelta`, followed by `EventMessageDone` — callers get a working, if non-incremental, stream rather than an error.
- `FallbackError`: `RunStream` returns `ErrStreamingUnsupported` immediately — for applications that need to know streaming is unavailable rather than silently degrading.

---

## 13. Conversation State, Sessions & Memory

`Agent` itself is stateless across calls (safe for concurrent use by multiple goroutines/requests, matching how a `*sql.DB` or `*http.Client` is typically shared). State lives in a `Session`, backed by a pluggable `ConversationStore`.

```go
// session.go

type ConversationStore interface {
    Load(ctx context.Context, sessionID string) ([]Message, error)
    Save(ctx context.Context, sessionID string, msgs []Message) error
}

// NewInMemoryStore is the zero-config default — fine for single-process
// use, CLIs, and tests. Backing it with Redis/Postgres/etc. is a matter of
// implementing the two-method interface above.
func NewInMemoryStore() ConversationStore

type Session struct {
    agent     *Agent
    id        string
    history   []Message
}

func (a *Agent) NewSession(id string) *Session
func (s *Session) Send(ctx context.Context, input string) (*Result, error)
func (s *Session) SendStream(ctx context.Context, input string) (EventStream, error)
func (s *Session) History() []Message
func (s *Session) Reset()
```

`Session.Send` loads history via the store, appends the new user turn, delegates to `Agent.RunMessages`, appends the result back to history, and persists via `Save` — giving multi-turn conversations without the caller manually threading `[]Message` through every call.

**Long-conversation management** is a pluggable strategy, not baked into the core loop:

```go
type Compactor interface {
    // Called before a turn if the estimated token count (via TokenCounter,
    // when available) crosses a threshold. Returns a shorter equivalent
    // history — summarization is the caller's/implementation's choice.
    Compact(ctx context.Context, msgs []Message) ([]Message, error)
}

func WithCompactor(c Compactor, thresholdTokens int) Option
```

Left pluggable deliberately: some providers have native server-side compaction (out of scope to reimplement), others need a client-side summarization pass — the interface accommodates either without the core loop knowing which.

---

## 14. Error Handling & Retries

```go
// errors.go

type ErrorCode string

const (
    ErrAuthentication   ErrorCode = "authentication_error"
    ErrPermission       ErrorCode = "permission_error"
    ErrInvalidRequest   ErrorCode = "invalid_request_error"
    ErrRateLimited      ErrorCode = "rate_limit_error"
    ErrOverloaded       ErrorCode = "overloaded_error"
    ErrContextExceeded  ErrorCode = "context_length_exceeded"
    ErrRefusal          ErrorCode = "refusal"
    ErrProviderInternal ErrorCode = "provider_error"
    ErrMaxIterations    ErrorCode = "max_iterations_exceeded"
    ErrStreamUnsupported ErrorCode = "streaming_unsupported"
)

type Error struct {
    Code       ErrorCode
    Provider   string        // which Provider.Name() produced this
    Retryable  bool
    RetryAfter time.Duration // honored when the provider sends one (e.g. 429 Retry-After)
    Cause      error         // wrapped original error, incl. the provider SDK's own type
}

func (e *Error) Error() string { ... }
func (e *Error) Unwrap() error { return e.Cause }

func IsRetryable(err error) bool {
    var e *Error
    return errors.As(err, &e) && e.Retryable
}
```

Every adapter's error-translation code maps that vendor's specific error taxonomy onto this common set — so application code writes one `errors.As(err, &agentErr)` / `switch agentErr.Code` regardless of which provider is behind the `Agent`.

**Retries:**

```go
type RetryPolicy struct {
    MaxRetries int
    BaseDelay  time.Duration
    MaxDelay   time.Duration
    Jitter     bool
}

func DefaultRetryPolicy() RetryPolicy // MaxRetries: 2, exponential backoff, jitter on

func WithRetryPolicy(rp RetryPolicy) Option
```

`Agent.generateWithRetry` retries only when `IsRetryable(err)` is true (rate limit, overloaded, transient network/5xx) — never on `ErrInvalidRequest`, `ErrAuthentication`, or `ErrRefusal`, since retrying those wastes a call and can't succeed. `RetryAfter`, when the provider supplies it, takes precedence over the computed exponential-backoff delay.

---

## 15. Concurrency, Context & Cancellation

- Every public method that can make a network call takes `context.Context` as its first argument and honors cancellation/deadlines — no exceptions, including inside `executeTools`'s goroutines (each tool handler receives the same `ctx` and is expected to respect it; this is documented as a contract in `RegisteredTool.Invoke`'s doc comment, not enforced by the framework, since Go cannot force a handler to check `ctx.Done()`).
- `Agent` and `Provider` implementations are safe for concurrent use across goroutines once constructed (no mutation after `New`/`New` return) — the common case of "one `*Agent` shared by an HTTP server handling many requests" works without external locking.
- `Session` is **not** safe for concurrent `Send` calls on the same session ID without external synchronization (conversation history has an inherent sequential dependency); this is documented explicitly rather than silently serialized, so callers make an informed choice (e.g., a per-session mutex or a single-writer queue at the application layer).
- Parallel tool execution (§11) uses one goroutine per `ToolUseBlock`, bounded implicitly by however many tool calls the model requested in a single turn (typically small, single digits) — no unbounded goroutine growth risk under normal model behavior. A documented option (`WithMaxParallelTools(n)`, using a semaphore) is available for tool sets where individual tools are resource-heavy (e.g. each spawning a subprocess).

---

## 16. Observability: Hooks, Tracing, Usage

```go
// hooks.go

type ToolCall struct {
    ID    string
    Name  string
    Input json.RawMessage
}

type Hooks struct {
    BeforeGenerate func(ctx context.Context, req *Request) error
    AfterGenerate  func(ctx context.Context, resp *Response)
    BeforeToolCall func(ctx context.Context, call ToolCall) (allow bool, override *ToolResult)
    AfterToolCall  func(ctx context.Context, call ToolCall, result ToolResult)
    OnError        func(ctx context.Context, err error)
}
```

These four points cover the practical needs called out for a "robust" agent library:

- **Structured logging / metrics**: `AfterGenerate` and `AfterToolCall` are natural places to emit `slog` records or increment counters (token usage, latency, tool-call frequency).
- **OpenTelemetry tracing**: a `WithTracing()` convenience option (built on the same `Hooks`, shipped as a small helper rather than a hard dependency) starts a span in `BeforeGenerate` / `BeforeToolCall` and ends it in the matching `After*` hook, tagged with provider name, model, tool name, and token counts. Kept as an optional helper (not a core dependency) so the root module doesn't force an OTel dependency on consumers who don't want it.
- **Human-in-the-loop approval**: `BeforeToolCall`'s `(allow bool, override *ToolResult)` return lets an application pause on a sensitive tool (e.g. "send_email", "delete_record"), route to an approval UI out-of-band, and either allow or substitute a denial result — the same shape as the confirm/deny pattern used by hosted agent platforms, implemented here as a plain synchronous callback (an application wanting async approval blocks inside the hook on its own channel/queue).
- **Cost tracking**: `Result.Usage` (aggregate) and per-call `Response.Usage` (via `AfterGenerate`) are both available; a small `usage.Tracker` helper (Roadmap) can multiply by a configurable per-model price table.

---

## 17. Configuration Conventions

- **Functional options** (`Option`, `claude.Option`, etc.) are the only configuration mechanism for constructors — no config structs passed positionally, so field additions are always backward compatible.
- **Credentials** are never read implicitly from the environment by the root `agent` package. Each provider adapter *may* offer a `NewFromEnv()` convenience constructor (`claude.NewFromEnv()` reading `ANTHROPIC_API_KEY`, `openai.NewFromEnv()` reading `OPENAI_API_KEY`, `gemini.NewFromEnv()` reading `GEMINI_API_KEY`/`GOOGLE_API_KEY`) as an explicit opt-in convenience, alongside the explicit `New(apiKey string, ...)` — never a silent global default.
- **No package-level mutable state** other than the opt-in provider registry (§8), which is only ever populated by packages the consumer explicitly imported.
- **Defaults are conservative and documented**: `MaxIterations = 25`, `RetryPolicy = 2 retries with jittered exponential backoff`, `ToolChoice = auto`, `Thinking = nil` (off unless explicitly requested).

---

## 18. Testing Strategy

**For applications building on go-agent** — `agenttest`:

```go
// agenttest/mock.go
type MockProvider struct {
    // Responses is consumed in order, one per Generate call; the last entry
    // repeats if the loop calls Generate more times than there are entries.
    Responses []*agent.Response
    OnGenerate func(req *agent.Request) (*agent.Response, error) // for dynamic scripting
}
```

This lets an application unit-test its own agent wiring (system prompt content, tool registration, hook behavior, loop termination) deterministically, with zero network calls and zero API cost:

```go
mock := &agenttest.MockProvider{
    Responses: []*agent.Response{
        {Message: agent.AssistantMessage(agent.ToolUseBlock{ID: "1", Name: "get_weather", Input: []byte(`{"city":"Paris"}`)}), StopReason: agent.StopToolUse},
        {Message: agent.AssistantMessage(agent.TextBlock{Text: "It's sunny in Paris."}), StopReason: agent.StopEndTurn},
    },
}
a := agent.New(agent.WithProvider(mock), agent.WithTools(GetWeather))
result, err := a.Run(context.Background(), "weather in paris?")
// assert result.Iterations == 2, tool was actually invoked, etc.
```

**For the library itself:**

- **Unit tests per adapter's `translate.go`**, table-driven, against fixed input/output fixtures — no live API calls in the default `go test ./...` run.
- **`providertest.Run`** (§8) as an opt-in integration suite, gated behind a build tag or an environment variable (e.g. `GOAGENT_INTEGRATION=1`) so it only runs in CI jobs with real API keys, never in a contributor's default local `go test`.
- **Golden tests for `schema.FromStruct[T]()`** — a table of representative structs (nested, slices, enums, optional fields) with expected JSON Schema output, checked byte-for-byte, since this is the one place reflection-based "magic" needs the tightest test coverage.
- **Fuzz testing** on `schema.FromStruct` and the JSON-unmarshal path in `Tool[TIn].Invoke` (Go's built-in fuzzing) to catch panics on malformed model-supplied tool input — this must never panic, since it processes untrusted model output.

---

## 19. Security Considerations

- **Tool input is untrusted model output**, full stop — this is called out prominently in the `RegisteredTool`/`Tool[TIn]` package documentation, not just this design doc. `Tool[TIn].Invoke` never panics on malformed input (returns an `IsError` `ToolResult` instead, see §18's fuzz-testing note), but *validating field values* (path traversal, command injection, SSRF via a URL field) is the handler author's responsibility — the library provides the typed-unmarshal safety net, not semantic validation, since that's inherently tool-specific.
- The design doc's guidance to tool authors (published in a `SECURITY.md` / package doc, mirroring the general tool-use security practices referenced earlier in this conversation): confine file-path tools to a resolved-and-checked root directory; allowlist (never blocklist) executables for any tool that shells out; treat any URL/hostname field as attacker-controlled for SSRF purposes.
- **API keys**: never logged. The `Hooks`/tracing helpers explicitly redact `Authorization`/`x-api-key`-shaped fields if a future `WithHTTPLogging()` debug helper is added (Roadmap) — flagged now so it's not an afterthought later.
- **Bounded execution**: `MaxIterations` (default 25) plus caller-supplied `context.WithTimeout` together bound both the number of model round-trips and wall-clock time, so a misbehaving tool or an unexpectedly chatty model can't run unbounded — directly addressing the "robust" requirement.
- **No implicit network egress** beyond the configured provider endpoint(s) and whatever a registered tool itself does — the library doesn't phone home, doesn't fetch remote config by default.

---

## 20. Distribution, Versioning & Installation

```sh
go get github.com/<you>/go-agent                    # core: agent.*
go get github.com/<you>/go-agent/provider/claude     # only if using Claude
go get github.com/<you>/go-agent/provider/openai     # only if using OpenAI
go get github.com/<you>/go-agent/provider/gemini     # only if using Gemini
```

Because this is a single module (§4), all four import paths resolve from one `go.mod`/tag — a consumer using all three providers still runs exactly one `go get github.com/<you>/go-agent@latest`. A consumer using only Claude has `anthropic-sdk-go` in `go.sum` (harmless — it's not compiled in unless imported) but never imports or ships OpenAI/Gemini SDK code, satisfying the "easy to include" and "pay for what you use" goals simultaneously.

- **Semantic versioning** from the first tagged release; `v0.x` while the API is still settling (see Roadmap phases), `v1.0.0` once the surface in §5–§13 is frozen.
- **Minimum Go version** pinned in `go.mod` (`go 1.22`); bumped deliberately, called out in release notes, since a library's minimum-version bump is a breaking change for some consumers.
- **No CGO, no OS-specific code** anywhere in the root module or first-class providers — pure Go, cross-compiles trivially (relevant for the "easy to include in any Go project" goal, including projects targeting multiple OS/arch).
- **`CHANGELOG.md`** maintained from `v0.1.0` onward; breaking changes to the root `agent` package are the highest-scrutiny category of change once `v1` ships.

---

## 21. End-to-End Examples

### Quickstart

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    agent "github.com/<you>/go-agent"
    "github.com/<you>/go-agent/provider/claude"
)

func main() {
    ctx := context.Background()
    provider := claude.New(os.Getenv("ANTHROPIC_API_KEY"))

    a := agent.New(
        agent.WithProvider(provider),
        agent.WithModel("claude-opus-4-8"),
        agent.WithSystemPrompt(agent.NewSystemPrompt().Add("You are a concise, helpful assistant.")),
        agent.WithMaxTokens(1024),
    )

    result, err := a.Run(ctx, "Explain the CAP theorem in two sentences.")
    if err != nil {
        log.Fatal(err)
    }
    for _, block := range result.FinalResponse.Message.Content {
        if tb, ok := block.(agent.TextBlock); ok {
            fmt.Println(tb.Text)
        }
    }
}
```

### Tools

```go
type WeatherInput struct {
    City string `json:"city" jsonschema:"required,description=City name"`
}

var GetWeather = agent.NewTool("get_weather", "Get current weather for a city.",
    func(ctx context.Context, in WeatherInput) (agent.ToolResult, error) {
        return agent.TextResult(fmt.Sprintf("72°F and sunny in %s", in.City)), nil
    })

a := agent.New(
    agent.WithProvider(provider),
    agent.WithModel("claude-opus-4-8"),
    agent.WithTools(GetWeather),
)
result, _ := a.Run(ctx, "What's the weather in Paris?")
```

### Streaming

```go
stream, err := a.RunStream(ctx, "Write a haiku about Go generics.")
if err != nil { log.Fatal(err) }
defer stream.Close()

for {
    ev, err := stream.Next(ctx)
    if errors.Is(err, io.EOF) { break }
    if err != nil { log.Fatal(err) }
    if ev.Type == agent.EventTextDelta {
        fmt.Print(ev.TextDelta)
    }
}
```

### Provider swap (same agent code, three vendors)

```go
func newProvider(name, apiKey string) agent.Provider {
    switch name {
    case "claude":
        return claude.New(apiKey)
    case "openai":
        return openai.New(apiKey)
    case "gemini":
        return gemini.New(apiKey)
    default:
        panic("unknown provider: " + name)
    }
}

provider := newProvider(cfg.ProviderName, cfg.APIKey)
a := agent.New(agent.WithProvider(provider), agent.WithModel(cfg.Model), agent.WithTools(GetWeather))
result, err := a.Run(ctx, userInput) // identical call regardless of provider
```

### Minimal custom provider (Step 1 of §8, in full)

```go
type EchoProvider struct{}

func (EchoProvider) Name() string { return "echo" }

func (EchoProvider) Generate(ctx context.Context, req *agent.Request) (*agent.Response, error) {
    last := req.Messages[len(req.Messages)-1]
    var text string
    for _, b := range last.Content {
        if tb, ok := b.(agent.TextBlock); ok {
            text += tb.Text
        }
    }
    return &agent.Response{
        Message:    agent.AssistantMessage(agent.TextBlock{Text: "echo: " + text}),
        StopReason: agent.StopEndTurn,
    }, nil
}

// Fully usable immediately:
a := agent.New(agent.WithProvider(EchoProvider{}))
```

---

## 22. Roadmap

| Phase | Scope | Exit criteria |
|---|---|---|
| **0 — Skeleton** | `message.go`, `request.go`, `response.go`, `provider.go`, `errors.go`; `Agent.Run` with no tool support | A hand-written fake `Provider` round-trips a plain text conversation |
| **1 — Tools + Claude** | `tool.go`, `schema/`, agent tool loop (§11), `provider/claude` complete (non-streaming) | Weather-tool example (§21) works end-to-end against real Claude API |
| **2 — OpenAI + Gemini** | `provider/openai`, `provider/gemini`, `Capabilities()` on all three, `providertest` conformance suite | Same tool example passes conformance suite against all three vendors |
| **3 — Streaming** | `stream.go`, `Stream` on all three adapters, `Agent.RunStream` with the tool-round-trip-aware unified stream | Streaming example (§21) works against all three; fallback mode verified against a Step-1-only mock provider |
| **4 — System prompts & retries** | `system.go` full builder, caching hints wired into Claude adapter, `SystemUpdater` on Claude, `RetryPolicy` + `IsRetryable` wired into the loop | Composed/cacheable/dynamic system prompt example works; induced 429 is retried and succeeds |
| **5 — Sessions & memory** | `session.go`, `ConversationStore`, `Compactor` interface + one reference implementation | Multi-turn CLI chat example holds state across turns via `Session` |
| **6 — Observability & polish** | `Hooks` fully wired, optional OTel tracing helper, `agenttest.MockProvider`, docs site / godoc pass, `v1.0.0` API freeze | 90%+ unit test coverage on root package; public API reviewed for breaking-change risk and tagged `v1.0.0` |
| **7 — Beyond v1 (exploratory)** | Multi-agent delegation (sub-agent as a `RegisteredTool`), MCP tool bridge via `schema` package, structured-output helpers (`agent.Parse[T]`), declarative YAML agent config, Bedrock/Vertex/Ollama first-party-quality adapters | Design docs per feature before implementation, same process as this document |

Phases 0–3 are the minimum viable version of everything explicitly required (unified interface, first-class Claude/OpenAI/Gemini, minimal extensibility, typed tools). Phases 4–6 deliver "very good system instructions" and general production robustness. Phase 7 is intentionally deferred and not load-bearing for the stated requirements.

---

## 23. Open Questions

These are decisions worth revisiting with the team/maintainers before or during implementation, rather than ones this document tries to force:

1. **Module path / org.** Placeholder `github.com/<you>/go-agent` needs a real home before `go get` works for anyone else — pick the GitHub org/user now so import paths in early code don't need a global rename later.
2. **`jsonschema` struct-tag library vs. hand-rolled reflection.** §9 proposes a small internal reflector (to keep the root module dependency-free) using a tag convention compatible with `anthropic-sdk-go`. Worth a quick spike to confirm hand-rolling reflection for the required feature set (required/description/enum/nested/arrays) is genuinely low-effort before committing, versus vendoring a minimal existing struct-to-schema library.
3. **Single-module vs. multi-module long-term.** §4's recommendation is single-module-with-subpackages for v1. If a provider SDK (most likely candidate: whichever Google Go GenAI package is current, given that ecosystem's churn) forces frequent unrelated version bumps, revisit splitting `provider/*` into independently-tagged nested modules.
4. **OpenAI's Responses API vs. Chat Completions.** OpenAI has more than one HTTP surface for "send messages, get tool calls back." The adapter should target whichever is OpenAI's current recommended agentic surface at implementation time — confirm before Phase 2 starts, since it affects `translate.go`'s shape non-trivially.
5. **Declarative/config-driven agent definition** (YAML/JSON describing an agent: model, system prompt, tool references by name). Explicitly deferred to Phase 7 in this draft, but if a driving use case needs it earlier (e.g. a platform team wants non-Go-developers to define agents), it should move up — flagging now so it's a conscious call, not a scope-creep surprise later.
6. **Cost/pricing table for the usage-tracking helper** (§16) — per-model pricing changes often enough that baking it into the library risks staleness; likely better as a caller-supplied table (`map[string]Pricing`) than a maintained-by-us constant, but worth confirming against how much value a maintained default table would add.
