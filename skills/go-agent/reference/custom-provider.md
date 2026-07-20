# Implementing a Custom Provider

`agent.Provider` has exactly one required method:

```go
type Provider interface {
    Name() string
    Generate(ctx context.Context, req *Request) (*Response, error)
}
```

That's enough for a fully working `Agent` — the tool-use loop, hooks, and
retries all work against a provider that only implements `Generate`.
`RunStream` also works via the single-shot fallback (see
[streaming.md](streaming.md)).

```go
type EchoProvider struct{}

func (EchoProvider) Name() string { return "echo" }

func (EchoProvider) Generate(_ context.Context, req *agent.Request) (*agent.Response, error) {
    last := req.Messages[len(req.Messages)-1]
    return &agent.Response{
        Message:    agent.AssistantMessage(agent.TextBlock{Text: "echo: " + last.Text()}),
        StopReason: agent.StopEndTurn,
    }, nil
}

a := agent.New(agent.WithProvider(EchoProvider{}))
```

## Optional interfaces (each unlocks more behavior, none required)

- `StreamingProvider` — add `Stream(ctx, req) (EventStream, error)` to
  unlock real `RunStream` streaming. Must emit a terminal `EventMessageDone`
  with a fully populated `Response` as the last event — see the streaming
  contract in [streaming.md](streaming.md).
- `TokenCounter` — add `CountTokens(ctx, req) (int, error)`.
- `Capable` — add `Capabilities() Capabilities` so `agent.CapabilitiesOf`
  reports real values instead of a conservative zero-value `Capabilities{}`.
- `SystemUpdater` — add `SystemUpdateMessage(note string) (Message, error)`
  for a provider-native mid-conversation system update mechanism.

## What `Request`/`Response` translation involves

A real adapter's `Generate` needs to translate the provider-agnostic
`*agent.Request` (`Model`, `System []SystemBlock`, `Messages []Message`,
`Tools []RegisteredTool`, `ToolChoice`, `MaxTokens`, `Thinking`) into that
vendor's wire format, call the vendor SDK, and translate the response back
into `*agent.Response`. Look at `provider/claude/translate.go`,
`provider/openai/translate.go`, or `provider/gemini/translate.go` in this
module for worked, real examples of that translation layer, including how
each handles error-code mapping (`translateError` in each package's
`errors.go`) onto the shared `agent.ErrorCode` taxonomy.

Do not guess a vendor SDK's exact type/method names from memory — verify
against the installed SDK (e.g. `go doc <import path> <Type>`) the same way
this module's own three adapters were built, especially for fast-moving
SDKs.
