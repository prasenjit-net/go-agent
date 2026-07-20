# Testing Code That Uses an Agent

`github.com/prasenjit-net/go-agent/agenttest` provides scriptable providers
for testing application code with zero network calls or API cost.

## `MockProvider` — non-streaming

```go
mock := &agenttest.MockProvider{
    Responses: []*agent.Response{
        {Message: agent.AssistantMessage(agent.TextBlock{Text: "hello"}), StopReason: agent.StopEndTurn},
    },
}
a := agent.New(agent.WithProvider(mock), agent.WithTools(GetWeather))
result, err := a.Run(context.Background(), "hi")
```

`Responses` is consumed in order across `Generate` calls; the last entry
repeats if `Generate` is called more times than there are entries. For
dynamic scripting (e.g. asserting on tool results from a previous turn
before deciding what to return next), set `OnGenerate func(*agent.Request)
(*agent.Response, error)` instead — it takes priority over `Responses` when
both are set. `mock.Calls()` returns every request `Generate` received, in
order, for assertions. `CapabilitiesValue agent.Capabilities` is returned
by `Capabilities()` for testing capability-gated code paths.

## Scripting a tool round-trip

```go
mock := &agenttest.MockProvider{
    Responses: []*agent.Response{
        {
            Message:    agent.AssistantMessage(agent.ToolUseBlock{ID: "1", Name: "get_weather", Input: json.RawMessage(`{"city":"Paris"}`)}),
            StopReason: agent.StopToolUse,
        },
        {Message: agent.AssistantMessage(agent.TextBlock{Text: "It's sunny."}), StopReason: agent.StopEndTurn},
    },
}
```

The Agent run loop executes the real tool handler against this scripted
`ToolUseBlock` exactly as it would against a real provider response — the
tool itself isn't mocked, only the model's responses are.

## `MockStreamingProvider` — for `RunStream`

```go
mock := &agenttest.MockStreamingProvider{
    MockProvider: agenttest.MockProvider{Responses: [...]},
}
```

A distinct type embedding `MockProvider`, not a flag on `MockProvider`
itself — this lets tests also exercise the Agent's **non-streaming-provider
fallback path** (`agent.WithStreamingFallback`) using a plain
`MockProvider`, which genuinely has no `Stream` method, the same way a
minimal real `Provider` would. Set `StreamFunc func(*agent.Request)
(agent.EventStream, error)` for custom event sequences; if left nil,
`Stream` synthesizes a reasonable event sequence from `Generate`'s
scripted response automatically.
