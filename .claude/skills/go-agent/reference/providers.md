# Providers

All three are constructed the same shape, then handed to `agent.WithProvider`
— nothing downstream of that call differs by vendor.

## Claude — `provider/claude`

```go
import "github.com/prasenjit-net/go-agent/provider/claude"

provider := claude.New("sk-ant-...")     // explicit key
provider := claude.NewFromEnv()          // reads ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN
```

Options: `claude.WithBaseURL(url)`, `claude.WithHTTPClient(c)`,
`claude.WithBetaHeader(value)` (repeatable, adds an `anthropic-beta`
header), `claude.WithMaxRetries(n)` (the SDK's own low-level HTTP retry
count — distinct from `agent.WithRetryPolicy`, which retries at the Agent
level).

Wraps `github.com/anthropics/anthropic-sdk-go` (Messages API). Implements
`Provider`, `StreamingProvider`, `TokenCounter` (`CountTokens`), and
`Capable`.

## OpenAI — `provider/openai`

```go
import "github.com/prasenjit-net/go-agent/provider/openai"

provider := openai.New("sk-...")
provider := openai.NewFromEnv()          // reads OPENAI_API_KEY / OPENAI_ORG_ID / OPENAI_PROJECT_ID
```

Options: `openai.WithBaseURL(url)` (Azure OpenAI, proxies, any
OpenAI-compatible endpoint), `openai.WithHTTPClient(c)`,
`openai.WithOrganization(id)`, `openai.WithMaxRetries(n)`.

Wraps `github.com/openai/openai-go/v3` targeting the **Chat Completions**
API (not the Responses API) — the Agent run loop resends full history on
every call and never relies on server-side conversation state, which maps
directly onto Chat Completions' stateless shape. Implements `Provider`,
`StreamingProvider`, and `Capable`. Does **not** implement `TokenCounter`.

## Gemini — `provider/gemini`

```go
import "github.com/prasenjit-net/go-agent/provider/gemini"

provider, err := gemini.New(ctx, "AI...")       // Gemini API backend
provider, err := gemini.NewFromEnv(ctx)         // reads GOOGLE_API_KEY / GEMINI_API_KEY
```

Constructor takes `context.Context` and returns `(*Client, error)` — the
one shape that differs from Claude/OpenAI's `*Client`-only return, because
building the underlying `genai.Client` can fail (e.g. Vertex AI auth).

Options: `gemini.WithVertexAI(project, location)` switches to the Vertex AI
backend (auth via Application Default Credentials instead of an API key),
`gemini.WithHTTPClient(hc)`.

Wraps `google.golang.org/genai`. Implements `Provider`, `StreamingProvider`,
and `Capable`. Does **not** implement `TokenCounter`.

Known gaps versus Claude/OpenAI, both documented in the adapter's own
comments rather than silently absent: `ThinkingBlock` content is dropped
when echoing assistant history back (Gemini's "thought" parts are
model-generated and read-only — there's no supported way to send one back
as client-authored history), and image/document `SourceURL` content is
best-effort (Gemini fetches its own Files API URIs or, on Vertex AI,
`gs://` URIs — not arbitrary public HTTP(S) URLs the way Claude/OpenAI do).

## Capability matrix

Query at runtime with `agent.CapabilitiesOf(provider)` rather than
hardcoding — it returns a zero-value `Capabilities{}` for any provider that
doesn't implement `Capable`.

| | Claude | OpenAI | Gemini |
|---|---|---|---|
| `Streaming` | ✅ | ✅ | ✅ |
| `Tools` / `ParallelToolCalls` | ✅ | ✅ | ✅ |
| `Vision` / `Documents` | ✅ | ✅ | ✅ |
| `Thinking` | ✅ (adaptive/budgeted) | ✅ (via `reasoning_effort`, approximated — see `agent.ThinkingConfig`) | ✅ (native token budget) |
| `SystemCaching` | ✅ (`cache_control` hints) | ➖ | ➖ |
| `MidConversationSystem` | ➖ (not yet implemented by this adapter) | ➖ | ➖ |
| `TokenCounter` interface | ✅ | ➖ | ➖ |

## Model IDs

`agent.WithModel(...)` takes the provider's own model ID string verbatim —
this library defines no model-name abstraction or aliasing. Use whatever
string the vendor's own docs specify for that provider (e.g.
`"claude-opus-4-8"`, `"gpt-4.1"`, `"gemini-2.5-pro"`).
