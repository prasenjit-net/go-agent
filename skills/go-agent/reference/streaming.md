# Streaming

## Basic loop

```go
stream, err := a.RunStream(ctx, "Write a haiku about Go generics.")
if err != nil {
    log.Fatal(err)
}
defer func() { _ = stream.Close() }()

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
    case agent.EventRunDone:
        // event.Result is the same *agent.Result Run() would have returned.
    }
}
```

`RunStream` returns **one logical stream for the whole run**, including any
tool round-trips — the caller doesn't need to detect a `tool_use` stop and
re-call anything; the Agent drives that internally and keeps yielding
events on the same stream. `Next` returns `io.EOF` (check with
`errors.Is`, not `==`) when the run is fully done, after the terminal
`EventRunDone`.

## Event types

`agent.EventType` values on `Event.Type`:

| Type | Populated field | Meaning |
|---|---|---|
| `EventTextDelta` | `TextDelta string` | incremental assistant text |
| `EventThinkingDelta` | `ThinkingDelta string` | incremental extended-reasoning text |
| `EventToolCallStart` | `ToolCall *ToolCall` | model started a tool call (ID/Name; Input may still be filling in) |
| `EventToolCallDelta` | `ToolCall` | streamed partial JSON input (not emitted by every provider) |
| `EventToolCallEnd` | `ToolCall` | tool call finished streaming in |
| `EventToolResult` | `ToolCall`, `ToolResult *ToolResult` | the Agent finished executing a tool and is about to send the result back |
| `EventMessageDone` | `Response *Response` | one full provider turn is complete |
| `EventRunDone` | `Result *Result` | the whole `RunStream` call is complete — terminal event |

## Providers without native streaming

Not every `Provider` implements `StreamingProvider` (`Stream` is optional
— see `agent.CapabilitiesOf`). `RunStream` still works against a
`Provider`-only implementation via `agent.WithStreamingFallback`:

```go
agent.WithStreamingFallback(agent.FallbackSingleShot) // default: one blocking Generate call,
                                                        // its result emitted as a single burst of events
agent.WithStreamingFallback(agent.FallbackError)       // instead: RunStream's first Next() call
                                                        // returns agent.ErrStreamUnsupported
```

All three first-class providers (Claude, OpenAI, Gemini) implement
`StreamingProvider`, so this only matters for a hand-written or
third-party `Provider`.

## Contract for a custom `StreamingProvider.Stream` implementation

If implementing `Stream` for a new provider: intermediate events
(`EventTextDelta`, `EventToolCallStart`, ...) can be emitted incrementally
as they arrive, but the stream **must** emit exactly one terminal
`EventMessageDone` as its last event, with `Response` fully populated
(final content blocks, `StopReason`, `Usage`, `ID`, `Model`) — the Agent's
internal accumulator (`turnAccumulator` in `agent_stream.go`) relies on
that event being authoritative and doesn't reconstruct state from the
intermediate deltas itself.
