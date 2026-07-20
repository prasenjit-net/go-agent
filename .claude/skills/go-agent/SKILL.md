---
name: go-agent
description: Use when writing Go code that builds an AI agent, or calls Claude, OpenAI, or Gemini from Go — tool/function calling, streaming, system prompts, retries, or multi-provider support. Triggers on "go-agent", "github.com/prasenjit-net/go-agent", "AI agent in Go", "Go SDK for Claude/OpenAI/Gemini", or code that already imports this module. Covers the root `agent` package and the provider/claude, provider/openai, provider/gemini subpackages.
---

# go-agent

[github.com/prasenjit-net/go-agent](https://github.com/prasenjit-net/go-agent) —
one Go interface over Claude, OpenAI, and Gemini, with a built-in
tool-use loop. Full rationale: `docs/DESIGN.md` in the module (this file
only covers *how* to use it).

## Decision table

| Need | Use |
|---|---|
| Send one message, get one answer, no tools | `agent.New(...).Run(ctx, text)` |
| The model should call functions you define | add `agent.WithTools(...)`; input is a typed Go struct, see [Tools](#tools) |
| Token-by-token output | `a.RunStream(ctx, text)`, not `Run` |
| Multi-turn conversation with saved history | `a.NewSession(id)`, then `session.Send(ctx, text)` per turn |
| Talking to Claude | `import "github.com/prasenjit-net/go-agent/provider/claude"` |
| Talking to OpenAI | `import "github.com/prasenjit-net/go-agent/provider/openai"` |
| Talking to Gemini | `import "github.com/prasenjit-net/go-agent/provider/gemini"` |
| A backend with no official adapter | implement `agent.Provider` yourself — one method, see [custom-provider.md](reference/custom-provider.md) |
| Testing code that uses an Agent | `agenttest.MockProvider` / `MockStreamingProvider`, see [testing.md](reference/testing.md) |

## Quickstart

```go
import (
    "context"

    agent "github.com/prasenjit-net/go-agent"
    "github.com/prasenjit-net/go-agent/provider/claude"
)

func main() {
    ctx := context.Background()
    provider := claude.NewFromEnv() // reads ANTHROPIC_API_KEY

    a := agent.New(
        agent.WithProvider(provider),
        agent.WithModel("claude-opus-4-8"),
        agent.WithSystemPrompt(agent.NewSystemPrompt().Add("You are a concise, helpful assistant.")),
        agent.WithMaxTokens(1024),
    )

    result, err := a.Run(ctx, "Explain the CAP theorem in two sentences.")
    // result.FinalResponse.Message.Text() is the answer.
}
```

OpenAI/Gemini are constructed the same way from their own subpackage
(`openai.NewFromEnv()`, `gemini.NewFromEnv(ctx)` — Gemini's constructor
additionally takes a `context.Context` and returns an error). Everything
below `agent.New(...)` is identical regardless of provider.

## Pitfalls

Things training data gets wrong about this specific library:

- **Tool input is a typed struct, never `map[string]any`.** Define a Go
  struct, tag it, pass it to `agent.NewTool[TIn]` — the handler receives an
  already-unmarshalled `TIn`. See [tools.md](reference/tools.md).
- **Import alias convention**: `agent "github.com/prasenjit-net/go-agent"`
  — the package name is `agent`, not `goagent` or `go_agent`.
  `provider/claude`, `provider/openai`, `provider/gemini` are separate
  subpackages; importing one does not pull the others' vendor SDKs into
  the build.
- **`ToolResultBlock.Content` is text-only today** — build results with
  `agent.TextResult`, `agent.JSONResult`, or `agent.ErrorResultf`, not a
  hand-built multi-modal content list.
- **Defaults**: `MaxIterations` is 25 (`agent.DefaultMaxIterations`),
  retries default to 2 attempts with jittered exponential backoff
  (`agent.DefaultRetryPolicy()`). Both are overridable via
  `agent.WithMaxIterations` / `agent.WithRetryPolicy`.
- **`StreamingProvider` is optional.** A plain `agent.Provider` (just
  `Generate`) still works with `RunStream` via a documented single-shot
  fallback — don't assume streaming requires a different code path.
- **A tool handler's Go `error` return is fatal to the run**, not
  model-recoverable. To let the model see a failure and try something
  else, return `agent.ErrorResultf(...)` with a `nil` error instead.
- **`Session` is not concurrency-safe for the same session ID** — `Agent`
  itself is safe to share across goroutines, but concurrent `Send` calls on
  one `Session` are not; serialize them at the call site if needed.
- **Error handling**: use `agent.IsRetryable(err)` / `agent.CodeOf(err)`
  against the `agent.ErrorCode` constants, not string-matching error text.
  See [errors.md](reference/errors.md).

## Reference

| File | Covers |
|---|---|
| [providers.md](reference/providers.md) | Claude/OpenAI/Gemini construction options, capability matrix, model ID conventions |
| [tools.md](reference/tools.md) | `Tool[TIn]`, the `jsonschema` struct-tag reference, `ToolSet` |
| [streaming.md](reference/streaming.md) | `RunStream`, `Event`/`EventType`, streaming-fallback modes |
| [system-prompts.md](reference/system-prompts.md) | `SystemPrompt` composition, cacheable sections |
| [sessions.md](reference/sessions.md) | `Session`, `ConversationStore` |
| [custom-provider.md](reference/custom-provider.md) | Implementing `agent.Provider` for a backend with no built-in adapter |
| [errors.md](reference/errors.md) | `agent.Error`, `ErrorCode`, `RetryPolicy` |
| [testing.md](reference/testing.md) | `agenttest.MockProvider` / `MockStreamingProvider` |

For why the library is shaped this way (interface design, tool-schema
generation approach, streaming contract for provider authors), see
`docs/DESIGN.md` in the module — this skill intentionally doesn't
duplicate that rationale.
