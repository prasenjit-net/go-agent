# AGENTS.md

Instructions for any AI coding agent working in this repository.

## What this is

`go-agent` — a Go library for building AI agents against Claude, OpenAI, and
Gemini through one interface (`github.com/prasenjit-net/go-agent`). Root
package name is `agent`; provider adapters live in `provider/claude`,
`provider/openai`, `provider/gemini` as separate subpackages.

For a task-oriented reference on *using* the library (quickstart, tool
registration, streaming, pitfalls), read `skills/go-agent/SKILL.md` and its
`skills/go-agent/reference/*.md` files — that content is the same skill
this repo ships to consumers via Claude Code / Codex / Copilot plugin
installs (see `docs/AGENT-SKILL-PLAN.md`), so it's kept accurate and
current on purpose. For architecture rationale (why the interfaces are
shaped this way), read `docs/DESIGN.md`.

## Working in this repo

- Before writing code against a vendor SDK (`anthropic-sdk-go`,
  `openai-go`, `google.golang.org/genai`), verify the exact API with
  `go doc <import path> <Symbol>` rather than from memory — these SDKs
  change quickly and every existing provider adapter was built that way.
- Run `gofmt -l .`, `go vet ./...`, `go build ./...`, `go test ./...
  -race`, and `golangci-lint run ./...` before considering a change done —
  matches exactly what CI (`.github/workflows/ci.yml`) checks.
- If you edit anything under `skills/go-agent/`, run
  `go run ./internal/skilltool sync` afterward (it copies the canonical
  content into `.claude/skills/go-agent/` and `.agents/skills/go-agent/`)
  and `go run ./internal/skilltool check` to confirm the vendor copies are
  in sync and every `agent.Xxx`/`claude.Xxx`/`openai.Xxx`/`gemini.Xxx`/
  `agenttest.Xxx`/`schema.Xxx` reference in its code fences still resolves
  to a real exported symbol. CI enforces both.
- Don't hand-edit `.claude/skills/go-agent/` or `.agents/skills/go-agent/`
  directly — they're generated copies of `skills/go-agent/`; edit the
  canonical directory and re-run `sync`.
- This project has no build/generate step for anything else — `go build
  ./...` at the repo root is the whole build.
