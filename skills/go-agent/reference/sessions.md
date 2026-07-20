# Sessions

`Agent` is stateless — it doesn't remember prior turns on its own, and is
safe to share across goroutines/requests (like a `*sql.DB` or
`*http.Client`). `Session` adds persisted, multi-turn history on top.

```go
session := a.NewSession("user-123")

result, err := session.Send(ctx, "My name is Alice.")
result, err = session.Send(ctx, "What's my name?") // remembers "Alice"

history, err := session.History(ctx)
err = session.Reset(ctx) // clear stored history for this session ID
```

`Session.Send` loads history via the Agent's `ConversationStore`, appends
the new user turn, runs the agent, and persists the updated history
(including any tool round-trips) back — the caller never manually threads
`[]agent.Message` between calls.

## Storage backend

Defaults to an in-memory store (`agent.NewInMemoryStore()`) — fine for
CLIs, tests, single-process use, but not shared across processes or
persisted across restarts. Back it with anything else by implementing the
two-method `agent.ConversationStore` interface and passing it via
`agent.WithConversationStore(store)`:

```go
type ConversationStore interface {
    Load(ctx context.Context, sessionID string) ([]Message, error)
    Save(ctx context.Context, sessionID string, msgs []Message) error
}
```

## Concurrency

`Session` is **not** safe for concurrent `Send` calls on the *same* session
ID — conversation history has an inherent sequential dependency (each turn
depends on the previous one being saved first). Serialize at the
application layer (a per-session mutex, or a single-writer queue) if
concurrent turns on one session are possible. Different session IDs are
independent and fine to run concurrently.
