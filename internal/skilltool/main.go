// Command skilltool keeps the go-agent Skill in sync across the vendor
// directories that need a physical copy of it (Claude Code, Codex — see
// docs/AGENT-SKILL-PLAN.md) and checks the skill's Go code fences for
// drift against the real module.
//
// Usage:
//
//	go run ./internal/skilltool sync    # copy skills/go-agent/ into vendor paths
//	go run ./internal/skilltool check   # verify vendor copies + fenced-code identifiers
package main

import (
	"fmt"
	"os"
)

const canonicalDir = "skills/go-agent"

// vendorDirs are the physical copies vendor tooling auto-discovers without
// requiring a plugin install. Copies, not symlinks: git-for-Windows only
// checks out real symlinks with developer-mode/config opt-in enabled, and a
// SKILL.md that silently becomes a one-line path string on an unsupported
// platform is worse than a small sync step. See docs/AGENT-SKILL-PLAN.md §4.
var vendorDirs = []string{
	".claude/skills/go-agent", // Claude Code
	".agents/skills/go-agent", // Codex (Copilot also reads this path and .claude/skills)
}

func main() {
	if len(os.Args) != 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "sync":
		err = runSync()
	case "check":
		err = runCheck()
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "skilltool:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: skilltool <sync|check>")
	os.Exit(2)
}
