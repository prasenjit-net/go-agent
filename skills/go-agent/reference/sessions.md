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

The `filestore` subpackage ships a second implementation — one JSON file
per session on local disk — for CLIs/single-process apps that want history
to survive a restart without a database:

```go
import "github.com/prasenjit-net/go-agent/filestore"

store, err := filestore.New("./sessions") // creates the dir if needed
a := agent.New(agent.WithProvider(provider), agent.WithConversationStore(store))
```

## Compaction

Long conversations grow unbounded by default. `agent.WithCompactor` enables
automatic compaction on `Session.Send`, triggered when the provider
implements `agent.TokenCounter` and the estimated token count of the
current history is at or above a threshold you choose:

```go
a := agent.New(
    agent.WithProvider(provider), // must implement agent.TokenCounter for this to ever trigger
    agent.WithCompactor(agent.NewWindowCompactor(50), 100_000), // keep last 50 messages once ~100k tokens
)
```

`agent.NewWindowCompactor(maxMessages)` is the built-in reference
implementation: it keeps only the most recent `maxMessages`, and is
tool-pairing-aware — a kept `ToolResultBlock` whose originating
`ToolUseBlock` fell outside the window is dropped too, since every
first-class provider rejects a tool result with no matching call in the
same request. It's a blunt strategy (dropped context is gone, not
summarized); implement the one-method `agent.Compactor` interface directly
for a smarter (e.g. summarizing) strategy. Compaction is off by default —
it's lossy, so it's an explicit opt-in, not automatic.

## Concurrency

`Session` is **not** safe for concurrent `Send` calls on the *same* session
ID — conversation history has an inherent sequential dependency (each turn
depends on the previous one being saved first). Serialize at the
application layer (a per-session mutex, or a single-writer queue) if
concurrent turns on one session are possible. Different session IDs are
independent and fine to run concurrently.
