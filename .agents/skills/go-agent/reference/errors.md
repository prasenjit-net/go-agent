# Errors & Retries

## The unified error type

Every provider adapter maps its vendor-specific errors onto one shared
`*agent.Error`:

```go
type Error struct {
    Code       ErrorCode
    Provider   string        // which Provider.Name() produced it
    Message    string
    Retryable  bool
    RetryAfter time.Duration // honored when the provider sends an explicit delay
    Cause      error         // wrapped original error
}
```

Check it with the package helpers, not string-matching or a raw type
assertion:

```go
if agent.IsRetryable(err) { ... }
switch agent.CodeOf(err) {
case agent.ErrRateLimited:
case agent.ErrInvalidRequest:
}
```

`agent.CodeOf` returns `agent.ErrUnknown` for any error that isn't (or
doesn't wrap) an `*agent.Error` — safe to call on any `error`.

## `ErrorCode` values

```
ErrAuthentication    ErrPermission        ErrInvalidRequest
ErrRateLimited        ErrOverloaded        ErrContextExceeded
ErrRefusal            ErrProviderInternal  ErrMaxIterations
ErrStreamUnsupported  ErrNotFound          ErrUnknown
```

`ErrMaxIterations` is what `Run`/`RunStream` return when the tool-use loop
hits `WithMaxIterations` (default `agent.DefaultMaxIterations` = 25)
without producing a final answer. `ErrStreamUnsupported` is only returned
by `RunStream` when `WithStreamingFallback(agent.FallbackError)` is set
against a provider with no native `Stream` method.

## Retries

```go
agent.WithRetryPolicy(agent.RetryPolicy{
    MaxRetries: 3,
    BaseDelay:  500 * time.Millisecond,
    MaxDelay:   20 * time.Second,
    Jitter:     true,
})
```

Default is `agent.DefaultRetryPolicy()` — 2 retries, exponential backoff
starting at 500ms capped at 20s, with jitter. Only errors where
`IsRetryable(err)` is true are retried (rate limit, overloaded, transient
network/5xx) — `ErrInvalidRequest`, `ErrAuthentication`, `ErrRefusal`, etc.
are never retried, since retrying can't succeed and just wastes a call. A
provider-supplied `RetryAfter` (e.g. from a 429's `Retry-After` header)
takes precedence over the computed backoff delay when present.
