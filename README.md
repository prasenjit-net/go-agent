# go-agent

[![CI](https://github.com/prasenjit-net/go-agent/actions/workflows/ci.yml/badge.svg)](https://github.com/prasenjit-net/go-agent/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/prasenjit-net/go-agent.svg)](https://pkg.go.dev/github.com/prasenjit-net/go-agent)
[![Go Report Card](https://goreportcard.com/badge/github.com/prasenjit-net/go-agent)](https://goreportcard.com/report/github.com/prasenjit-net/go-agent)

A Go library for building AI agents against **any** inference provider through one
strongly-typed, idiomatic interface — with **Claude, OpenAI, and Gemini as
first-class citizens** and a minimal-effort path to add more.

```go
provider := claude.NewFromEnv()

a := agent.New(
    agent.WithProvider(provider),
    agent.WithModel("claude-opus-4-8"),
    agent.WithTools(GetWeather),
)

result, err := a.Run(ctx, "What's the weather in Paris?")
```

The same `Agent` code runs unchanged against OpenAI or Gemini — swap the
`Provider` and nothing else.

Full architecture and design rationale: [docs/DESIGN.md](docs/DESIGN.md).

---

## Why

Most multi-provider Go SDKs either wrap every vendor's API 1:1 (so you still
write provider-specific code) or flatten everything to `map[string]any` (so
you lose type safety exactly where bugs are most expensive — tool call
arguments). go-agent takes a different position:

- **One small `Provider` interface.** `Name()` + `Generate()` — that's the
  entire contract a backend must satisfy. Streaming, capability
  declaration, and token counting are separate, optional interfaces a
  provider can add incrementally, mirroring `io.Reader` / `io.ReaderAt`.
- **Tools are Go structs, not JSON blobs.** `Tool[TIn]` derives the JSON
  Schema from your struct's tags and hands your handler a fully-typed,
  already-unmarshalled `TIn` — no `map[string]any`, no manual type
  assertions, no schema/implementation drift.
- **A real agent loop, not just a client.** Tool execution, retries with
  backoff, bounded iterations, human-in-the-loop approval hooks, and a
  unified streaming model are built in, not bolted on.
- **Pay for what you import.** The root module has effectively zero
  third-party dependencies. Each provider adapter lives in its own
  subpackage, so importing `provider/claude` never pulls in the OpenAI or
  Gemini SDKs.

## Install

```sh
go get github.com/prasenjit-net/go-agent
```

Each provider adapter is a separate subpackage — import only the ones you
use:

```sh
go get github.com/prasenjit-net/go-agent/provider/claude
go get github.com/prasenjit-net/go-agent/provider/openai
go get github.com/prasenjit-net/go-agent/provider/gemini
```

Requires Go 1.22+.

## Using go-agent with an AI coding agent

This repo ships a [Skill](docs/AGENT-SKILL-PLAN.md) that teaches Claude
Code, OpenAI Codex, and GitHub Copilot the library's real API — tool
registration, streaming, provider differences, and the pitfalls a coding
agent would otherwise guess wrong from generic training data. Installing
it is optional but recommended if you're building against go-agent with
one of these agents.

The simplest path, one command for any of the three (requires
[GitHub CLI](https://cli.github.com/) ≥2.90):

```sh
gh skill install prasenjit-net/go-agent go-agent --agent claude-code
gh skill install prasenjit-net/go-agent go-agent --agent codex
gh skill install prasenjit-net/go-agent go-agent --agent copilot
```

Or install the native plugin directly in each agent:

```sh
# Claude Code
/plugin marketplace add prasenjit-net/go-agent
/plugin install go-agent@go-agent

# Codex — adds this repo as a plugin source, then install via the /plugins panel
codex plugin marketplace add prasenjit-net/go-agent

# GitHub Copilot
copilot plugin marketplace add prasenjit-net/go-agent
copilot plugin install go-agent@go-agent
```

This is always an explicit, one-time step you take in your own agent —
nothing about `go get`-ing the library installs it automatically.

## Quickstart

```go
package main

import (
    "context"
    "fmt"
    "log"

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
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.FinalResponse.Message.Text())
}
```

Runnable versions of every example below live in [`examples/`](examples/).

## Strongly-typed tools

Define a tool's input as a plain struct. `json` tags drive field naming;
`jsonschema` tags drive the description/required/enum the model sees. The
handler receives `WeatherInput` already parsed — no map, no type assertion.

```go
type WeatherInput struct {
    City  string `json:"city" jsonschema:"required,description=City name, e.g. Paris"`
    Units string `json:"units,omitempty" jsonschema:"enum=celsius;fahrenheit"`
}

var GetWeather = agent.NewTool(
    "get_weather",
    "Get the current weather for a city. Call this when the user asks about current conditions.",
    func(ctx context.Context, in WeatherInput) (agent.ToolResult, error) {
        return agent.TextResult(fmt.Sprintf("72°F and sunny in %s", in.City)), nil
    },
)

a := agent.New(
    agent.WithProvider(provider),
    agent.WithModel("claude-opus-4-8"),
    agent.WithTools(GetWeather),
)
```

The `Agent.Run` loop executes every tool call the model makes, feeds the
result back, and repeats until the model produces a final answer — bounded
by `agent.WithMaxIterations` (default 25) so a runaway loop can't run
forever.

## Streaming

`Agent.RunStream` returns one logical event stream for the whole run,
including any tool round-trips:

```go
stream, err := a.RunStream(ctx, "Write a haiku about Go generics.")
if err != nil {
    log.Fatal(err)
}
defer stream.Close()

for {
    event, err := stream.Next(ctx)
    if errors.Is(err, io.EOF) {
        break
    }
    if err != nil {
        log.Fatal(err)
    }
    switch event.Type {
    case agent.EventTextDelta:
        fmt.Print(event.TextDelta)
    case agent.EventToolCallStart:
        fmt.Printf("\n[calling %s]\n", event.ToolCall.Name)
    }
}
```

Providers that don't implement native streaming still work with
`RunStream` via a documented fallback (`agent.WithStreamingFallback`) — one
blocking call synthesized into a single-burst stream, rather than an error.

## System instructions

`SystemPrompt` composes static, cacheable, and per-request-dynamic sections,
and translates to each provider's native mechanism (including
prompt-caching hints where supported):

```go
sp := agent.NewSystemPrompt().
    Add("You are a customer support agent for Acme Corp.").
    AddCacheable(knowledgeBaseDump). // hinted for prompt caching on providers that support it
    AddFunc(func(ctx context.Context) (string, error) {
        return "Current user: " + userFromContext(ctx).Name, nil
    })
```

## Multi-turn conversations

`Agent` itself is stateless (safe to share across goroutines/requests).
`Session` adds persisted history via a pluggable `ConversationStore`
(in-memory by default):

```go
session := a.NewSession("user-123")
result, err := session.Send(ctx, "My name is Alice.")
result, err = session.Send(ctx, "What's my name?") // remembers "Alice"
```

## Adding a new provider

Implementing `agent.Provider` requires exactly one method:

```go
type EchoProvider struct{}

func (EchoProvider) Name() string { return "echo" }

func (EchoProvider) Generate(ctx context.Context, req *agent.Request) (*agent.Response, error) {
    last := req.Messages[len(req.Messages)-1]
    return &agent.Response{
        Message:    agent.AssistantMessage(agent.TextBlock{Text: "echo: " + last.Text()}),
        StopReason: agent.StopEndTurn,
    }, nil
}
```

That's a fully working `agent.Provider` — `agent.New(agent.WithProvider(EchoProvider{}))`
already gets the tool loop, hooks, and retries. Add `Stream` to unlock
`RunStream`, and `Capabilities` to unlock capability-aware validation —
both optional, both additive. See [`examples/customprovider`](examples/customprovider)
and [docs/DESIGN.md](docs/DESIGN.md#8-adding-a-new-provider-minimum-implementation)
for the full recipe, including the shared conformance test suite.

## Testing your own agent code

`agenttest.MockProvider` scripts responses with zero network calls:

```go
mock := &agenttest.MockProvider{
    Responses: []*agent.Response{
        {Message: agent.AssistantMessage(agent.TextBlock{Text: "hello"}), StopReason: agent.StopEndTurn},
    },
}
a := agent.New(agent.WithProvider(mock), agent.WithTools(GetWeather))
result, err := a.Run(context.Background(), "hi")
```

## Package layout

```
go-agent/                  package agent — core types, Agent, tools, streaming
├── schema/                JSON Schema generation (reflection-based)
├── provider/
│   ├── claude/            wraps anthropic-sdk-go
│   ├── openai/            wraps openai-go (Chat Completions API)
│   └── gemini/             wraps google.golang.org/genai
├── agenttest/             MockProvider for testing application code
├── examples/
├── skills/go-agent/       coding-agent Skill content (source of truth; see AGENTS.md)
├── internal/skilltool/    syncs & drift-checks the skill against the real API
└── docs/DESIGN.md         full design document
```

## Status

Core agent loop, tool calling, streaming, system prompts, retries, and all
three first-class providers (non-streaming + streaming) are implemented and
tested. See [docs/DESIGN.md](docs/DESIGN.md#22-roadmap) for the phased
roadmap of what's next (sessions/compaction strategies, OpenTelemetry
tracing helper, declarative config, multi-agent delegation).

A coding-agent Skill for Claude Code, Codex, and Copilot is built and
installable (see [Using go-agent with an AI coding
agent](#using-go-agent-with-an-ai-coding-agent) above) — plan and design
notes in [docs/AGENT-SKILL-PLAN.md](docs/AGENT-SKILL-PLAN.md). The actual
`/plugin install` / `gh skill install` flows haven't been exercised
end-to-end against a live agent session yet (not something scriptable from
a shell), so treat the install commands as verified-by-schema, not
verified-by-use, until someone runs them for real.

## Development

```sh
go build ./...
go vet ./...
go test ./... -race
gofmt -l .              # should print nothing
golangci-lint run ./... # errcheck, govet, ineffassign, staticcheck, unused
```

**CI** ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs on every push/PR to
`main`: format check, vet, build, race-enabled tests with coverage,
`golangci-lint`, `govulncheck`, a cross-compile check across
linux/darwin/windows × amd64/arm64, and a GoReleaser config/snapshot
validation.

**Releases** ([`.github/workflows/release.yml`](.github/workflows/release.yml)) are
manual — trigger it from the Actions tab and choose a version bump
(`patch` / `minor` / `major`), or supply an exact version to override the
bump. The workflow re-verifies build/vet/test as a gate, tags, and runs
[GoReleaser](https://goreleaser.com) (config: [`.goreleaser.yaml`](.goreleaser.yaml))
to publish a GitHub Release with an auto-generated changelog. The library
needs no build step to "release" — `go get github.com/prasenjit-net/go-agent@vX.Y.Z`
resolves directly from the git tag — so GoReleaser doesn't build or attach
any binaries; it only tags and writes release notes.

## License

[MIT](LICENSE)
