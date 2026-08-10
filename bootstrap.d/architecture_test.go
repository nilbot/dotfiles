package main_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// modulePath is this module's import prefix, used to tell an import of our own
// code from an import of the standard library.
const modulePath = "github.com/nilbot/dotfiles/bootstrap/"

// nonTestImports returns the import paths of every non-test file in dir, keyed
// by file. Test files are excluded: they may use os to build fixtures.
//
// A directory holding no package is a hard failure rather than an empty result.
// parser.ParseDir reports no error for a path that does not exist, so a
// mistyped or renamed directory would otherwise make every caller pass
// vacuously -- the exact way an architecture test rots into decoration.
func nonTestImports(t *testing.T, dir string) map[string][]string {
	t.Helper()
	pkgs, err := parser.ParseDir(token.NewFileSet(), dir, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) == 0 {
		t.Fatalf("no package in %s; this guard would pass without checking anything", dir)
	}
	imports := map[string][]string{}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			for _, spec := range file.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				imports[name] = append(imports[name], path)
			}
		}
	}
	return imports
}

// The dry-run invariant, enforced exactly.
//
// internal/phase reaches the machine only through change.Interface. If it could
// import os or os/exec it could mutate while the user asked for a plan, and no
// behavioural test would necessarily catch it.
//
// The shell version could only approximate this by scanning for command names
// like "rm" at statement starts -- a heuristic that was written wrong twice. An
// import set is exact.
//
// The whole module-internal closure is walked, not just the direct imports of
// internal/phase: a phase that imported a helper which imported os would
// otherwise launder the capability through one hop. internal/change is where
// the walk stops -- it is the one package permitted to touch the machine, and
// phase reaching it is the design.
func TestPhasePackageCannotPerformIO(t *testing.T) {
	forbidden := map[string]bool{
		"os": true, "os/exec": true, "os/signal": true, "os/user": true,
		"io/fs": true, "io/ioutil": true, "syscall": true,
		"net": true, "net/http": true,
	}
	// path/filepath is pure string manipulation and is allowed; io is allowed
	// for the Writer interface. Neither can reach the filesystem on its own.

	start := filepath.Join("internal", "phase")
	stop := map[string]bool{"internal/change": true}

	seen := map[string]bool{}
	queue := []string{start}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		if seen[dir] {
			continue
		}
		seen[dir] = true

		for file, paths := range nonTestImports(t, dir) {
			for _, path := range paths {
				if rel, ours := strings.CutPrefix(path, modulePath); ours {
					if !stop[rel] {
						queue = append(queue, filepath.FromSlash(rel))
					}
					continue
				}
				if forbidden[path] {
					t.Errorf("%s imports %q; phases must reach the machine only "+
						"through change.Interface", file, path)
				}
			}
		}
	}
}

// Only the change package may import the I/O primitives it wraps.
func TestOnlyChangeImportsOSExec(t *testing.T) {
	for _, dir := range []string{
		filepath.Join("internal", "manifest"),
		filepath.Join("internal", "phase"),
	} {
		for file, paths := range nonTestImports(t, dir) {
			for _, path := range paths {
				if path == "os/exec" {
					t.Errorf("%s imports os/exec; only internal/change may", file)
				}
			}
		}
	}
}
