package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// pkgAliases maps the import alias used in skill snippets to the package's
// filesystem directory (relative to the module root) and import path, for
// error messages. Only identifiers written as "<alias>.<Name>" are checked
// — bare identifiers and method calls on local variables (session.Send,
// stream.Next, ...) are intentionally out of scope; this catches
// package-level API drift (a renamed or removed exported symbol), the
// highest-value and most mechanically checkable class of error, not full
// type-checking of necessarily-partial documentation snippets.
var pkgAliases = map[string]struct{ dir, importPath string }{
	"agent":     {".", "github.com/prasenjit-net/go-agent"},
	"claude":    {"provider/claude", "github.com/prasenjit-net/go-agent/provider/claude"},
	"openai":    {"provider/openai", "github.com/prasenjit-net/go-agent/provider/openai"},
	"gemini":    {"provider/gemini", "github.com/prasenjit-net/go-agent/provider/gemini"},
	"agenttest": {"agenttest", "github.com/prasenjit-net/go-agent/agenttest"},
	"schema":    {"schema", "github.com/prasenjit-net/go-agent/schema"},
}

var (
	fenceRE = regexp.MustCompile("(?s)```go\\n(.*?)```")
	identRE = regexp.MustCompile(`\b(agent|claude|openai|gemini|agenttest|schema)\.([A-Za-z_][A-Za-z0-9_]*)`)
)

// runCheck verifies the vendor copies match the canonical skill directory
// and that every package-prefixed identifier referenced in its Go code
// fences resolves to a real exported symbol.
func runCheck() error {
	var problems []string

	for _, dst := range vendorDirs {
		if equal, diffs := treesEqual(canonicalDir, dst); !equal {
			problems = append(problems, diffs...)
		}
	}

	refs, err := collectIdentifierRefs(canonicalDir)
	if err != nil {
		return fmt.Errorf("scanning skill content: %w", err)
	}

	exportsByDir := map[string]map[string]bool{}
	for alias, names := range refs {
		pkg := pkgAliases[alias]
		set, ok := exportsByDir[pkg.dir]
		if !ok {
			set, err = exportedNames(pkg.dir)
			if err != nil {
				return fmt.Errorf("parsing %s: %w", pkg.dir, err)
			}
			exportsByDir[pkg.dir] = set
		}
		for name := range names {
			if !set[name] {
				problems = append(problems, fmt.Sprintf("%s.%s: no such exported symbol in %s", alias, name, pkg.importPath))
			}
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "FAIL:", p)
		}
		return fmt.Errorf("%d problem(s) found", len(problems))
	}

	fmt.Println("skill content OK: vendor copies in sync, all referenced identifiers resolve")
	return nil
}

// collectIdentifierRefs walks every .md file under dir and returns, per
// alias, the set of identifier names referenced as "<alias>.<Name>" inside
// ```go fenced code blocks.
func collectIdentifierRefs(dir string) (map[string]map[string]bool, error) {
	refs := map[string]map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, fence := range fenceRE.FindAllSubmatch(data, -1) {
			for _, m := range identRE.FindAllSubmatch(fence[1], -1) {
				alias, name := string(m[1]), string(m[2])
				if refs[alias] == nil {
					refs[alias] = map[string]bool{}
				}
				refs[alias][name] = true
			}
		}
		return nil
	})
	return refs, err
}

// exportedNames parses every non-test .go file directly under dir (stdlib
// go/parser — no `go doc`/`go/packages` involved) and returns the set of
// exported top-level func/type/const/var names declared there. Parsing the
// real source avoids both `go doc`'s summary-view eliding of large const
// groups (observed eliding all but the first entry) and any risk of a doc
// comment's prose being mistaken for a declaration. Files are parsed one at
// a time via parser.ParseFile rather than the deprecated parser.ParseDir —
// this package has no build-tag-gated files, so per-file parsing is
// equivalent here.
func exportedNames(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	names := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() {
					names[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							names[s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, valueName := range s.Names {
							if valueName.IsExported() {
								names[valueName.Name] = true
							}
						}
					}
				}
			}
		}
	}
	return names, nil
}
