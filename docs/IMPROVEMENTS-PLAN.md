# go-agent Improvements Plan

Status: v0.2 — P1 (provider test coverage, PR #4), P2 (session compaction +
filestore, this branch), and P3 (OpenTelemetry tracing helper, this branch)
done. P4 (benchmarks) not started.
Scope: closes measured gaps in test coverage and observability; two of the
four items below are already-committed, currently-unmet exit criteria from
[`docs/DESIGN.md`](DESIGN.md)'s own [Roadmap](DESIGN.md#22-roadmap) (Phase 5
and Phase 6), not new scope invented for this document.

## 1. Baseline (measured 2026-07-20)

```
go test ./... -cover
```

| Package | Coverage |
|---|---|
| root (`agent`) | 71.6% |
| `schema` | 82.8% |
| `provider/claude` | 23.9% |
| `provider/openai` | 30.6% |
| `provider/gemini` | 27.5% |

Phase 6's exit criterion is "90%+ unit test coverage on root package" — root
is 18.4 points short. The provider packages have no stated target in the
roadmap but are the most exposed to vendor SDK drift (the exact failure mode
hit twice while building `skills/go-agent/reference/*.md` earlier this
project: `go doc` eliding const groups, then a deprecated `parser.ParseDir`
call — both caught only by reading real source, not by any existing test).

Also true today, both confirmed by grep, zero false negatives:
- No `Compactor` interface exists anywhere in the codebase — Phase 5's exit
  criterion ("`ConversationStore`, `Compactor` interface + one reference
  implementation") is entirely unmet; only `NewInMemoryStore` exists.
- No OpenTelemetry or tracing code exists anywhere — Phase 6's "optional OTel
  tracing helper" is entirely unmet.
- No `func Benchmark...` exists anywhere — not a roadmap item, proposed here
  as a new addition to Phase 6 ("polish").

## 2. Priority order and rationale

| # | Item | Roadmap tie-in | Effort | Why this order |
|---|---|---|---|---|
| P1 | Provider adapter test coverage | New (not in DESIGN.md, but the highest-risk gap) | M | Highest value per hour: no new API surface, closes the exact hole that already caused real bugs this project. Safe to do first and in isolation. |
| P2 | `Compactor` + second `ConversationStore` | Closes Phase 5 | M–L | Named, already-committed exit criterion. Needs one design decision (below) before coding starts. |
| P3 | OpenTelemetry tracing helper | Closes Phase 6 (partially) | M | Named, already-committed exit criterion. Depends on nothing else here. |
| P4 | Benchmarks (schema reflection, tool dispatch) | New, proposed fold-in to Phase 6 "polish" | S | Lowest risk, lowest urgency — do last or opportunistically. |

P1–P3 are independent of each other and can be done in any order or in
parallel across sessions; the order above is by value-per-effort, not a hard
dependency chain.

## 3. P1 — Provider adapter test coverage

**Target:** each of `provider/claude`, `provider/openai`, `provider/gemini`
from ~24–31% to 70%+, concentrated on `translate.go` (request/response
mapping) and `errors.go` (vendor-error → `agent.Error` code mapping) — the
two files most likely to silently drift when a vendor SDK changes.

**Approach:** table-driven unit tests against the existing `translate.go`
functions directly (construct a vendor SDK request/response value, call
`translateX`, assert the resulting `agent.Request`/`agent.Response` shape) —
same style as the existing `translate_test.go` files, just more cases:
multi-block messages, tool calls with empty/nested-JSON input, all
`StopReason` variants, streaming-delta edge cases, and every mapped error
code in `errors.go`. No live network calls, no `httptest` server needed for
this tier.

**Open question (needs a decision before starting):** should this also add
one `httptest`-backed round-trip test per provider (a stub HTTP server
returning a canned vendor response, exercised through the real SDK client)?
That catches wire-format mismatches translate-level tests can't, at the cost
of more test infrastructure per provider. Recommendation: start with
translate-level tests only; add an `httptest` tier per-provider only if that
turns out insufficient (i.e. don't build it preemptively).

## 4. P2 — `Compactor` interface + second `ConversationStore`

Two deliverables, both explicitly named in Phase 5:

1. **`Compactor` interface** — something like
   `Compact(ctx context.Context, msgs []Message) ([]Message, error)`,
   invoked by `Session` (exact trigger point — every `Save`, or only past a
   message-count/token threshold — is an open question below) with one
   reference implementation (e.g. a simple oldest-messages-summarized or
   sliding-window-truncation strategy).
2. **A second `ConversationStore` implementation** — proves the interface is
   actually implementable outside the package, not just documented. `Load`/
   `Save` are the whole contract, so this is a small surface either way.

**Open questions (need your input before implementation):**

- **Compactor trigger point:** on every `Session.Send`, on a caller-supplied
  policy (message count / estimated tokens), or left entirely to the caller
  to invoke manually? Recommendation: a caller-supplied threshold via a
  `Session` option, defaulting to off (no behavior change for existing
  callers) — compaction is lossy, so it shouldn't be silently on by default.
- **Second store backend:** the real tradeoff is the root module's
  near-zero-dependency promise (§"Why" in `README.md`). Three options:
  - *File-based (JSON on disk)* — zero new dependencies, proves the
    interface, but least "production-realistic."
  - *SQLite* — realistic, but needs either CGO (`mattn/go-sqlite3`) or a
    pure-Go driver (`modernc.org/sqlite`, sizable dependency) — either way
    it must live in its own subpackage (matching the `provider/*` pattern)
    so it's opt-in, not pulled into every consumer's build.
  - *Redis* — realistic and common in practice, but requires a running
    service to test against in CI (testcontainers or similar), the heaviest
    option here.

  Recommendation: file-based store as its own subpackage now (zero
  dependency cost, ships this quarter); document a Redis/SQL implementation
  pattern in `skills/go-agent/reference/sessions.md` as an example rather
  than shipping the dependency weight.

**Resolved:** both recommendations taken as-is. `Compactor` lives in
`session.go`, triggered via a caller-supplied `WithCompactor(c, thresholdTokens)`
option (off by default); `NewWindowCompactor` in `compactor.go` is the
reference implementation, made tool-pairing-aware after realizing a naive
last-N-messages window can orphan a kept `ToolResultBlock` whose
`ToolUseBlock` fell outside the window — every first-class provider rejects
that shape. The second store shipped as `filestore/`, which needed more
than "just marshal `[]Message` to JSON": `ContentBlock` is a closed
interface, so a discriminated-union wire envelope (`filestore/wire.go`) was
necessary for `Load` to actually reconstruct concrete block types.

## 5. P3 — OpenTelemetry tracing helper

The natural attachment point already exists and requires no core-library
change: `Hooks` ([`hooks.go`](../hooks.go)) already exposes `BeforeGenerate`
/ `AfterGenerate` / `BeforeToolCall` / `AfterToolCall` / `OnError` — a
tracing helper is just a constructor that returns a `Hooks` value wired to
emit spans, shipped as its own subpackage (e.g. `otelagent`) exactly like
`provider/*`, so it costs importers nothing unless they import it.

Proposed shape: `otelagent.NewHooks(tracer trace.Tracer) agent.Hooks` —
one span per `Generate` call (`BeforeGenerate`→`AfterGenerate`, with
`Response.Usage` as span attributes), one child span per tool call
(`BeforeToolCall`→`AfterToolCall`), errors recorded via `OnError`.

**Open question:** OpenTelemetry's GenAI semantic conventions are still
marked experimental/incubating upstream, so exact attribute names may churn.
Recommendation: implement against the current incubating semconv names, but
document in the package doc comment that attribute names are
not-yet-stable and may change before `v1.0.0` — don't block on upstream
stabilizing first.

**Resolved:** shipped as `otelagent.NewHooks(tracer trace.Tracer, provider
string) agent.Hooks`, matching the proposed shape with one addition — a
`provider` parameter — because neither `agent.Request` nor `agent.Response`
actually carries the provider name, so the caller (who already chose the
provider) supplies it once. The bigger design question turned out to be
correlation, not attribute naming: `Hooks` gives Before/After pairs no way
to thread a per-call token through beyond the `ctx` and `ToolCall` values
they're already passed, and `ctx` is literally the same object across
concurrent tool-call goroutines within one turn. Generate spans are
correlated by `ctx` identity (safe — sequential, never concurrent, per
`ctx`); tool-call spans by `(ctx, ToolCall.ID)` (safe — concurrent calls in
one turn always have distinct IDs). `OnError` closes a still-open Generate
span for the abort paths that skip `AfterGenerate` entirely (a composed
outer hook rejecting `BeforeGenerate`, or `Generate` failing after retries
exhaust) — otherwise those spans would leak.

## 6. P4 — Benchmarks

Two target areas, both hot paths for a library whose whole pitch is "thin
layer, pay for what you import":

- `schema/reflect.go` — the reflection-based JSON Schema generator, run once
  per `Tool[TIn]` registration; a regression here is easy to introduce
  silently (e.g. an accidental extra reflection pass on nested structs).
- `Agent.Run`'s tool-dispatch path — marshal/unmarshal + handler invocation
  overhead per tool call.

Recommendation: plain `go test -bench=. ./schema/... .` runbook documented
in `README.md`'s Development section, run manually before releases — not
wired into CI as a gate. Perf CI gates on shared runners are noisy enough
(runner-to-runner variance) that a hard threshold would produce false
failures more often than it catches real regressions; a benchstat-compared
`go test -bench` run is more useful as a human-in-the-loop pre-release check
than an automated blocker.

## 7. Explicitly out of scope

Multi-agent delegation, MCP tool bridge, structured-output helpers,
declarative YAML config — all Phase 7 in `DESIGN.md`, untouched by this plan.

## 8. Suggested sequencing

1. P1 (provider test coverage) — no open design questions, can start
   immediately.
2. P2 and P3 — each has one open question above; resolve those first, then
   implement in either order (they don't depend on each other).
3. P4 — opportunistic, whenever convenient, no blockers.

Each item should land as its own PR against `main` (branch protection
requires it), verified against the same suite already enforced in CI:
`gofmt`, `go vet`, `go build`, `go test -race`, `golangci-lint`,
`govulncheck`, and (for P2/P3, since they add subpackages) the existing
provider-adapter conventions in `docs/DESIGN.md` §8.
