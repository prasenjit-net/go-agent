# Agent Skill — Plan

**Status:** Draft v0.2 — scoped to Layer 2 (native skill packages) for
**Claude Code, OpenAI Codex, and GitHub Copilot**.
**Goal:** give these three coding agents accurate, current, example-grounded
guidance for using `go-agent`, so an agent asked to "build a Go AI agent" or
"add tool calling against Claude/OpenAI/Gemini in Go" produces correct code
against this library's real API instead of guessing from training data.

## Strategy: three layers, this plan builds Layer 2

Reach for a third-party skill breaks into three tiers of increasing install
friction and increasing in-agent quality:

| Layer | Mechanism | Reach | Status |
|---|---|---|---|
| 0 | `AGENTS.md` — passive, no install step, but only works if present in the repo actually being worked in | Broadest passive fallback (Codex reads it natively; many other tools honor it) | Kept as a cheap baseline (§6) |
| **2** | **Native skill package** (`SKILL.md` + reference files) installed via each agent's plugin/marketplace system | Claude Code, Codex, Copilot — one explicit install step, full quality (progressive disclosure, curated content) | **This plan's scope** |
| 1 | An MCP server exposing go-agent reference as tools/resources | Broadest *active* reach (every major agent surveyed speaks MCP) | Deferred — see §9 |

(Numbered 0/1/2 by increasing agent-side sophistication, not build order —
Layer 2 is being built first because all three target agents already
support it natively and it gives the best in-agent experience.)

**Why Layer 2 is viable as a single build, not three:** verified research
into all three vendors' skill mechanics (§3) confirms **Copilot explicitly
follows the same open `agentskills.io` SKILL.md schema** Claude Code and
Codex use — all three require only `name` + `description` in frontmatter,
and differences are additive (optional `allowed-tools`, Codex's optional
`agents/openai.yaml` sidecar, GitHub CLI provenance stamps). One canonical
`SKILL.md` + `reference/*.md` directory can be referenced, unmodified, from
all three vendors' plugin manifests — this is a single content build with
three thin manifest wrappers, not three forks.

---

## Table of Contents

1. [Canonical Skill Content](#1-canonical-skill-content)
2. [SKILL.md Content Plan](#2-skillmd-content-plan)
3. [Per-Vendor Mechanics (Verified)](#3-per-vendor-mechanics-verified)
4. [Repository Layout](#4-repository-layout)
5. [Consumer-Facing Install UX](#5-consumer-facing-install-ux)
6. [Layer 0 Fallback (AGENTS.md)](#6-layer-0-fallback-agentsmd)
7. [Keeping the Skill in Sync](#7-keeping-the-skill-in-sync)
8. [Step-by-Step Plan](#8-step-by-step-plan)
9. [Deferred: Layer 1 (MCP Server)](#9-deferred-layer-1-mcp-server)
10. [Open Questions](#10-open-questions)

---

## 1. Canonical Skill Content

One source of truth, shared across all three vendors (see §4 for how each
vendor's discovery path points at it):

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

Progressive disclosure (short router + linked reference files loaded on
demand) is explicitly documented by both Claude Code and Codex, and
structurally supported (via `references/`/`scripts/`/`assets/` folders) by
Copilot even though its docs don't formalize the term. All three benefit
from the same shape.

## 2. SKILL.md Content Plan

Target: under ~500 lines (Claude Code's stated guidance) — and note a
**harder, vendor-enforced constraint**: Codex caps the *initial* skill list
(every installed skill's `name`+`description`, before any one is selected)
at 2% of the context window or 8,000 characters combined. The
`description` field has to earn its space against every other skill the
user has installed, not just describe go-agent — front-load the trigger
phrases, cut anything decorative.

Sections, in order:

1. **Frontmatter** — `name`, and a `description` written with the explicit
   trigger phrases an agent's router matches against: "building an AI agent
   in Go", "go-agent", "Claude/OpenAI/Gemini in Go", "importing
   `github.com/prasenjit-net/go-agent`". Keep it terse per the Codex budget
   above; specific beats comprehensive.
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

## 3. Per-Vendor Mechanics (Verified)

Verified against current vendor docs (cited inline) rather than assumed.
Each row is independent — build all three manifests, but they all point at
the one `skills/go-agent/` directory from §1.

### Claude Code

- Skill discovery (no install): `.claude/skills/go-agent/SKILL.md` in a
  repo, auto-loaded when that repo is opened directly.
- Plugin manifest: `.claude-plugin/plugin.json` (`name`, `description`,
  `version`, `author`, `homepage`, `repository`, `license`); skill(s) live
  under the plugin's own `skills/<name>/SKILL.md`.
- Marketplace: `.claude-plugin/marketplace.json` at the repo root.
- Install UX: `/plugin marketplace add prasenjit-net/go-agent` then
  `/plugin install go-agent@go-agent`.
- *(code.claude.com/docs/en/skills.md, plugins.md, plugin-marketplaces.md)*

### OpenAI Codex

- Skill discovery (no install): `.agents/skills/go-agent/SKILL.md`,
  discovered walking from cwd up to the repo root.
- Plugin manifest: `.codex-plugin/plugin.json` (`name`, `version`,
  `description` required; references a `skills` dir, plus optional `apps`,
  `mcpServers`, `hooks` by relative path). Optional sidecar
  `agents/openai.yaml` for UI-only config — not required for the skill to
  work.
- Marketplace: added as a source via
  `codex plugin marketplace add prasenjit-net/go-agent[@ref]`, cached under
  `~/.codex/plugins/cache/<marketplace>/<plugin>/<version>/`.
- Install UX: browsed/installed via the in-CLI `/plugins` panel (ChatGPT
  desktop/web has an equivalent GUI panel) — no single documented
  `codex plugin install <name>` verb as of this research; standalone
  (non-plugin) skills can also be pulled individually via Codex's built-in
  `$skill-installer` skill. **Confirm the exact final install step against
  current Codex docs before writing the README instructions** — the
  marketplace-add step is solid, the last mile isn't fully pinned down (see
  §10).
- *(learn.chatgpt.com/docs/build-plugins, build-skills)*

### GitHub Copilot

- Skill discovery (no install): `.github/skills/go-agent/SKILL.md` — but
  Copilot **also recognizes `.claude/skills/` and `.agents/skills/`
  directly**, so the two directories already needed for Claude Code and
  Codex discovery likely cover Copilot too, with no third copy required
  (verify before deleting a dedicated `.github/skills/` path — see §10).
- Plugin manifest: `plugin.json` at the plugin root (not a hidden
  directory) — `name`, `description`, `version`, `author`, `license`,
  `keywords`, `agents`, `skills[]`, `hooks`, `mcpServers`.
- Marketplace: `.github/plugin/marketplace.json` — **Copilot CLI also
  checks `.claude-plugin/` for Claude-format compatibility**, meaning the
  Claude Code marketplace file may double as Copilot's without a separate
  file (again, verify depth of that compat before relying on it — see
  §10).
- Install UX: `copilot plugin marketplace add prasenjit-net/go-agent` then
  `copilot plugin install go-agent@go-agent` (or `/plugin install` in an
  active session).
- **Cross-agent shortcut**: GitHub CLI ≥2.90 ships
  `gh skill install OWNER/REPO SKILL[@ref] --agent <claude-code|codex|copilot> [--pin]`
  — installs a single skill for whichever agent you name, stamping
  provenance (repo/ref/tree-SHA) into the installed `SKILL.md`'s
  frontmatter. This works today for all three target agents through one
  command and is the recommended primary install path in the README (§5).
- *(docs.github.com/en/copilot/concepts/agents/about-agent-skills,
  about-plugins; docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/plugins-marketplace;
  github.blog/changelog/2026-04-16-manage-agent-skills-with-github-cli)*

## 4. Repository Layout

```
skills/go-agent/                          # canonical content (§1)
├── SKILL.md
└── reference/...

.claude/skills/go-agent -> ../../skills/go-agent   # Claude Code discovery
.agents/skills/go-agent -> ../../skills/go-agent   # Codex discovery (Copilot reads this too)

.claude-plugin/
├── marketplace.json                      # Claude Code marketplace registration
└── plugin.json                           # references skills/go-agent

.codex-plugin/
└── plugin.json                           # references skills/go-agent

.github/plugin/
└── marketplace.json                      # Copilot marketplace registration (add only if .claude-plugin/ compat proves insufficient — §3/§10)
plugin.json                               # Copilot plugin manifest (repo root, per §3)
```

`.claude/skills/go-agent` and `.agents/skills/go-agent` are shown as
symlinks into the canonical directory — the simplest option on macOS/Linux,
where all local development for this library has happened so far. Git
symlink support on Windows requires developer mode or an admin-elevated
git config and has historically been a source of contributor friction;
if that proves a problem, fall back to a generated copy (a `go generate`
step or a small sync script run in CI, per §7's anti-drift job) instead of
a real symlink. Decide this in implementation, not in this planning pass.

## 5. Consumer-Facing Install UX

Lead the README section with the single cross-agent command, then list
native fallbacks:

```sh
# Works for all three today, via one GitHub CLI command:
gh skill install prasenjit-net/go-agent go-agent --agent claude-code
gh skill install prasenjit-net/go-agent go-agent --agent codex
gh skill install prasenjit-net/go-agent go-agent --agent copilot
```

```sh
# Native per-vendor alternative (also installs the fuller plugin bundle,
# not just the standalone skill):
/plugin marketplace add prasenjit-net/go-agent   # Claude Code
codex plugin marketplace add prasenjit-net/go-agent  # then install via /plugins
copilot plugin marketplace add prasenjit-net/go-agent
```

Same caveat as before: this is always an explicit, one-time action the
consumer takes in their own agent — nothing about publishing this content
makes it appear automatically just because someone runs `go get
github.com/prasenjit-net/go-agent`.

## 6. Layer 0 Fallback (AGENTS.md)

Kept as a zero-cost baseline underneath Layer 2, not a replacement for it:

- Add a short `AGENTS.md` at the go-agent repo root (helps contributors
  working directly in this repo; Codex reads it with no install step at
  all).
- Add a README block consumers can copy into *their own* project's
  `AGENTS.md` if they'd rather not install anything — point it at the
  pitfalls list in §2, not the full reference set, so a hand-copy stays
  reasonable in size.
- `llms.txt` remains optional/low-priority — real but partial adoption,
  not a substitute for either Layer 0 or Layer 2.

## 7. Keeping the Skill in Sync

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
- If §4 falls back to generated copies instead of symlinks, the same job
  is the natural place to also assert the three vendor paths are in sync
  with the canonical directory (fail CI if `.claude/skills/go-agent` or
  `.agents/skills/go-agent` diverges from `skills/go-agent`).
- Where a snippet is meant to match a real runnable example verbatim (the
  quickstart in §2), generate it from the actual file in `examples/` at
  skill-build time instead of maintaining two copies, so they cannot drift.
- Bump all three plugin manifests' `version` together and add a short
  "verified against go-agent vX.Y.Z" line in `SKILL.md` as part of the
  release checklist, once `release.yml` is the trigger point people
  already use.

## 8. Step-by-Step Plan

1. Write `skills/go-agent/SKILL.md` + `reference/*.md` per §1–2, grounded
   in the real package (spot-check with `go doc` the same way the provider
   adapters were built, not from memory of writing them).
2. Wire up Claude Code discovery + plugin + marketplace (§3/§4) — most
   mature, most confidently specified; do this one first to validate the
   canonical-content-plus-thin-manifest approach end to end.
3. Wire up Codex discovery + `.codex-plugin/plugin.json` (§3/§4); confirm
   the actual install-command last mile against current Codex docs while
   doing this (§10 item 1).
4. Wire up Copilot: confirm whether `.claude-plugin/` compat and
   `.claude/skills`/`.agents/skills` discovery are sufficient before adding
   Copilot-native `plugin.json` / `.github/plugin/marketplace.json` (§10
   item 2) — only add the dedicated files if the compat path doesn't fully
   work.
5. Add the `skill-check` CI job per §7.
6. Add `AGENTS.md` + the consumer README section (§5, §6), leading with
   `gh skill install`.
7. Once stable, consider submitting to each vendor's official/community
   marketplace listing (Claude: `anthropics/claude-plugins-community`;
   Codex and Copilot equivalents to be identified at that point).

## 9. Deferred: Layer 1 (MCP Server)

Every agent surveyed (Claude Code, Codex, Copilot, Cursor, Windsurf, Cline,
Gemini CLI, Amazon Q) supports connecting to MCP servers, making it the
broadest *active* mechanism available — broader than Layer 2, since it
isn't limited to vendors with a native skill/plugin system. Deferred out of
this plan's scope (per current direction: Layer 2 for Claude/Codex/Copilot
first) rather than dropped — worth a follow-up plan once Layer 2 has
shipped and the canonical `skills/go-agent/reference/` content exists to
serve from it, since an MCP server exposing that same content as
tools/resources would be a thin layer on top rather than a fresh content
build.

## 10. Open Questions

1. **Codex's exact single-skill/plugin install command** — the
   marketplace-add step is confirmed; the final "install this specific
   plugin" verb wasn't. Confirm before writing README instructions (or
   just lead with `gh skill install`, which is confirmed, and treat the
   native Codex command as secondary).
2. **Depth of Copilot's `.claude-plugin/` compatibility** — "checks
   `.claude-plugin/` for Claude-format compat" is promising but unverified
   in depth. Test whether Copilot's marketplace-add actually works against
   a repo that only has `.claude-plugin/marketplace.json`, with no
   dedicated `.github/plugin/marketplace.json`, before deciding to skip the
   Copilot-native file.
3. **Symlinks vs. generated copies for `.claude/skills/` and
   `.agents/skills/`** — decide based on whether symlink-unfriendly
   contributor environments (chiefly Windows without git symlink support
   enabled) are a real concern for this project's contributor base.
4. **Exact `plugin.json` field-level schema per vendor** — this plan lists
   the fields each vendor's docs mention, but hasn't hand-verified a
   minimal working manifest against each vendor's schema validator.
   Confirm at implementation time (step 2 of §8), one vendor at a time.
5. **`skill-check` implementation** — decide between a standalone script
   (`internal/skillcheck/main.go`) versus a Go test using
   `go/parser`/`testing` to extract and compile fenced code blocks. Prefer
   whichever produces the clearest CI failure message when a snippet goes
   stale.
6. **Marketplace ownership** — self-hosting all three marketplaces in this
   repo is simplest to start; if go-agent ever ships more than one plugin,
   revisit whether they should live in a dedicated repo instead.
