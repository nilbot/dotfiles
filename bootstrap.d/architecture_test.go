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
// internal/phase and internal/check reach the machine only through
// change.Interface. If either could import os or os/exec it could mutate while
// the user asked for a plan -- or while they asked a question -- and no
// behavioural test would necessarily catch it.
//
// The shell version could only approximate this by scanning for command names
// like "rm" at statement starts -- a heuristic that was written wrong twice.
// Imports are structural rather than textual, so this is exact about every name
// it names. It is a DENYLIST of nine, though, not an allowlist: a standard
// library package that can reach the filesystem and is absent from the list
// below passes. Inverting it is the standing improvement, and the honest reading
// until then is "these nine cannot appear", not "nothing that can do I/O can".
//
// The whole module-internal closure is walked, not just the direct imports of
// each package: a phase that imported a helper which imported os would
// otherwise launder the capability through one hop. internal/change is where
// the walk stops -- it is the one package permitted to touch the machine, and
// phase reaching it is the design.
//
// All three roots are seeded into one walk. internal/phase imports both
// internal/check and internal/migrate today, so checking phase alone would
// already cover them; naming each explicitly means the guard survives those
// imports going away.
//
// internal/migrate is the one that most needs it. It is the only package that
// destroys anything, so it is the only one where a stray os.RemoveAll would
// reach a real filesystem outside change.Interface -- unlogged, unrefusable, and
// invisible to every behavioural test that goes through an Applier.
func TestPhasePackageCannotPerformIO(t *testing.T) {
	forbidden := map[string]bool{
		"os": true, "os/exec": true, "os/signal": true, "os/user": true,
		"io/fs": true, "io/ioutil": true, "syscall": true,
		"net": true, "net/http": true,
	}
	// io is allowed for the Writer interface and regexp for matching text
	// already read; neither can open anything.
	//
	// path/filepath is allowed on a narrower claim than it once carried here.
	// Most of it is pure string manipulation, but Glob, Walk, WalkDir,
	// EvalSymlinks and Abs do reach the filesystem or the process's working
	// directory. What is true is that no non-test file the walk below reaches
	// calls any of them (checked); what is NOT true is that importing
	// the package cannot reach the filesystem. The denylist above works at
	// import granularity and cannot tell those apart, so this one is held by
	// reading rather than by the guard -- which is the reason to say so here
	// rather than to leave a sentence a future reader would rely on.

	stop := map[string]bool{"internal/change": true}

	seen := map[string]bool{}
	queue := []string{
		filepath.Join("internal", "phase"),
		filepath.Join("internal", "check"),
		filepath.Join("internal", "migrate"),
	}
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
					t.Errorf("%s imports %q; phases, checks and migrations must reach "+
						"the machine only through change.Interface", file, path)
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
		filepath.Join("internal", "check"),
		filepath.Join("internal", "migrate"),
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
