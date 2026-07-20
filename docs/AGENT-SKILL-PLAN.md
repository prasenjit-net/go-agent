# Agent Skill — Plan

**Status:** Draft v0.1
**Goal:** give coding agents (Claude Code first, other agents where a shared
convention exists) accurate, current, example-grounded guidance for using
`go-agent`, so an agent asked to "build a Go AI agent" or "add tool calling
against Claude/OpenAI/Gemini in Go" produces correct code against this
library's real API instead of guessing from training data.

This document plans the skill's content, how it stays correct as the library
evolves, and how it actually reaches consumers of the library — which turns
out to be the harder problem, since the two available publishing mechanisms
have very different reach (see [§3](#3-publishing)).

---

## Table of Contents

1. [Skill Structure](#1-skill-structure)
2. [SKILL.md Content Plan](#2-skillmd-content-plan)
3. [Publishing](#3-publishing)
4. [Cross-Agent Reach (AGENTS.md)](#4-cross-agent-reach-agentsmd)
5. [Keeping the Skill in Sync](#5-keeping-the-skill-in-sync)
6. [Step-by-Step Plan](#6-step-by-step-plan)
7. [Open Questions](#7-open-questions)

---

## 1. Skill Structure

Mirrors the shape of Anthropic's own `claude-api` skill (used hands-on to
build this library's three provider adapters): a short **SKILL.md router**
plus **reference files** that only load when the task actually needs that
depth. This keeps the base cost of having the skill installed low.

```
skills/go-agent/
├── SKILL.md                    # index: triggers, quickstart, decision table, pitfalls
└── reference/
    ├── providers.md            # claude/openai/gemini construction, capability matrix, model IDs
    ├── tools.md                # Tool[T], jsonschema tag reference, ToolSet
    ├── streaming.md            # RunStream, EventStream, fallback modes
    ├── system-prompts.md       # SystemPrompt composition, caching hints
    ├── sessions.md             # ConversationStore, Session
    ├── custom-provider.md      # minimal-Provider recipe, conformance suite
    ├── errors.md               # Error taxonomy, RetryPolicy
    └── testing.md              # agenttest.MockProvider / MockStreamingProvider
```

This same directory doubles as the payload for the plugin path in §3 — no
separate copy is maintained (see §3's marketplace layout).

## 2. SKILL.md Content Plan

Target: under ~500 lines. Sections, in order:

1. **Frontmatter** — `name`, and a `description` written with the explicit
   trigger phrases an agent's router matches against: "building an AI agent
   in Go", "go-agent", "Claude/OpenAI/Gemini in Go", "importing
   `github.com/prasenjit-net/go-agent`". This field is what decides whether
   the skill enters context at all — it needs to be specific, not just a
   one-line description of what the library does.
2. **Decision table** — Agent vs. raw `Provider`; when to register tools;
   when to reach for `RunStream` vs `Run`; which provider subpackage to
   import for which vendor.
3. **Quickstart** — the smallest working snippet (provider construction →
   `agent.New` → `Run`), taken verbatim from `examples/quickstart` so it's
   never hand-duplicated out of sync with a real, building example.
4. **Pitfalls** — the section doing the most work, modeled on the
   `claude-api` skill's "API Drift" table. Concrete, testable claims an
   agent will otherwise get wrong from generic Go/LLM training data:
   - Tool input is a typed struct via `agent.NewTool[TIn]`, never
     `map[string]any`.
   - `ToolResultBlock` content is text-only today (no image/multi-block
     tool results yet).
   - Default `MaxIterations` is 25; default retry is 2 attempts.
   - Import convention: `agent "github.com/prasenjit-net/go-agent"`.
   - Provider adapters are separate subpackages
     (`provider/claude`, `provider/openai`, `provider/gemini`) — importing
     one does not pull in the others' SDKs.
   - `StreamingProvider` is optional; a plain `Provider` still works via
     the documented fallback.
5. **Reference table** — one line per file in `reference/`, so the agent
   knows what exists without loading it yet.
6. **Pointer to `docs/DESIGN.md`** for architecture rationale — the skill
   answers "how do I use this," the design doc answers "why does it work
   this way." Explicitly avoid duplicating design rationale into the skill.

## 3. Publishing

Two mechanisms exist today, verified against current Claude Code docs
(`code.claude.com/docs/en/skills.md`, `.../plugins.md`,
`.../plugin-marketplaces.md`) rather than assumed from memory. They reach
different audiences, and conflating them is the main way a plan like this
goes wrong.

| Mechanism | What it does | Reaches |
|---|---|---|
| `.claude/skills/go-agent/SKILL.md` committed in this repo | Auto-loads for anyone who opens **this repo** in Claude Code | Contributors to go-agent itself |
| Claude Code **plugin**, distributed via a **marketplace** | User runs `/plugin marketplace add prasenjit-net/go-agent` then `/plugin install go-agent` once; active across all their repos after that | Consumers of the library (the actual target audience) |

**The catch:** publishing the skill inside the go-agent repo does **not**
propagate to a consumer's repo just because they `go get` the library —
there is no auto-discovery through `go.mod` or any Go tooling. The
plugin/marketplace install is an explicit, one-time opt-in step the
consumer takes in *their own* Claude Code environment. No mechanism today
makes "publish" alone reach every consumer automatically; the README needs
to actively tell people to install it.

**No Anthropic-hosted public registry exists** (nothing npm/PyPI-equivalent
for skills). The real options, in order of effort:

1. **Self-hosted marketplace** — this repo doubles as its own marketplace.
   Add `.claude-plugin/marketplace.json` at the repo root and
   `.claude-plugin/plugin.json` alongside the `skills/go-agent/` directory
   from §1. Zero external dependency; consumers install directly from this
   repo.
2. **Submit to `anthropics/claude-plugins-community`** once the skill is
   stable, for discoverability beyond people who already know to look at
   this repo. Review-gated submission via
   `platform.claude.com/plugins/submit`.

Marketplace/plugin file shapes to implement in §6:

```
.claude-plugin/
├── marketplace.json     # registers this repo as a marketplace
└── plugin.json           # name, description, version, author, repository
skills/
└── go-agent/
    ├── SKILL.md
    └── reference/...
```

`plugin.json`'s `version` should track go-agent release tags (see §5) —
add it explicitly from the start, since retrofitting version tracking onto
an already-published plugin is more painful than starting with it.

## 4. Cross-Agent Reach (AGENTS.md)

`AGENTS.md` is a real, now broadly-honored convention (Claude Code, Cursor,
Copilot, Codex, and others read it per current docs) for repo-root agent
instructions — but it has the *same* reach limitation as the repo-embedded
skill: an `AGENTS.md` in the go-agent repo only helps agents working **on**
go-agent, not agents working in a consumer's separate repo.

Practical mitigation, since there's no automatic propagation path for
either mechanism:

- Add a short `AGENTS.md` at the go-agent repo root anyway (helps
  contributors, costs little).
- Add a README section ("Using go-agent with an AI coding agent") giving
  consumers two explicit options: run the `/plugin install` command, or
  copy a short, ready-made go-agent usage block into *their own* project's
  `AGENTS.md` / `CLAUDE.md`. Make the copy-paste block small enough that
  doing this by hand is actually reasonable — point it at the pitfalls
  list in §2, not the full reference set.
- `llms.txt` has real but partial adoption (per current research, roughly
  5–15% of sites, growing) and several IDE agents do fetch it
  opportunistically from documentation sites. Anthropic supports but does
  not formally recommend it. Low effort to add a minimal `llms.txt`
  pointing at the skill/docs as a low-cost extra surface, but it's not a
  substitute for the plugin install path — treat it as a bonus, not the
  plan.

## 5. Keeping the Skill in Sync

The library's real exported API is the source of truth — the same
discipline used to build the three provider adapters (verifying every type
against `go doc` output rather than trained-in memory of the SDKs) applies
here in reverse: the skill's snippets must be verified against the module,
not hand-typed from memory of what was written.

Concrete mechanism, layered onto the CI already in place
(`.github/workflows/ci.yml`):

- Add a `skill-check` job that extracts fenced Go code blocks from
  `skills/go-agent/SKILL.md` and `skills/go-agent/reference/*.md` and
  `go build`s them against the module (a small script under
  `internal/skillcheck/` or a `go:generate`-driven test is enough — no new
  dependency required).
- Where a snippet is meant to match a real runnable example verbatim (the
  quickstart in §2), generate it from the actual file in `examples/` at
  skill-build time instead of maintaining two copies, so they cannot drift.
- Bump `plugin.json`'s `version` and add a short "verified against
  go-agent vX.Y.Z" line in `SKILL.md` as part of the release checklist,
  once `release.yml` is the trigger point people already use.

## 6. Step-by-Step Plan

1. Write `SKILL.md` + `reference/*.md` per §1–2, grounded in the real
   package (spot-check with `go doc` the same way the provider adapters
   were built, not from memory of writing them).
2. Commit at `.claude/skills/go-agent/` in this repo.
3. Add `.claude-plugin/marketplace.json` + `.claude-plugin/plugin.json` per
   §3 so the same content is installable via
   `/plugin marketplace add prasenjit-net/go-agent`. Document the install
   command prominently in the README.
4. Add `AGENTS.md` at the repo root and the consumer-facing README section
   per §4.
5. Add the `skill-check` CI job per §5.
6. Once stable, submit to `anthropics/claude-plugins-community`.

## 7. Open Questions

1. **Duplication between `.claude/skills/go-agent/` and the plugin's
   `skills/go-agent/`** — §1 assumes one physical copy referenced by both
   the auto-discovery path and the plugin manifest. Confirm Claude Code
   actually supports a plugin's skill directory living somewhere other than
   directly under the plugin root before committing to that layout; fall
   back to a build step that copies/symlinks if not.
2. **`skill-check` implementation** — decide between a standalone script
   (`internal/skillcheck/main.go`) versus a Go test using
   `go/parser`/`testing` to extract and compile fenced code blocks. Prefer
   whichever produces the clearest CI failure message when a snippet goes
   stale.
3. **Marketplace ownership** — self-hosting the marketplace in this repo is
   simplest to start, but if go-agent ever ships more than one plugin,
   revisit whether the marketplace should live in a dedicated repo instead.
4. **Submission timing** — no fixed bar defined yet for "stable enough" to
   submit to `anthropics/claude-plugins-community`; revisit once the
   `skill-check` job has run clean across a couple of real library releases.
