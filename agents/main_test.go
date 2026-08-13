package main

import (
	"fmt"
	"os"
	"testing"
)

// TestMain moves every test in this package out of the checkout before any of
// them runs.
//
// Commands here discover their repository from the working directory, and the
// working directory during `go test` is the package directory -- which is
// inside this repository. A test that calls runInit or runWire without first
// calling t.Chdir therefore operates on the developer's own checkout: it
// scaffolds into the real .agents/ and wires the real .claude/settings.json
// with the path of the ephemeral test binary. Those entries are never cleaned
// up, because stripOurs deliberately refuses to delete a command whose basename
// is not exactly `agents`, so each run leaves four more dead hooks behind that
// the harness reports as errors at every session start.
//
// That happened. Rather than rely on every future test remembering, start
// somewhere that is not a repository at all: a forgotten t.Chdir now yields a
// clean "not inside a git repository" skip instead of silent damage.
//
// GIT_CEILING_DIRECTORIES stops git's upward search at the temp directory, so
// this holds even when the system temp path is itself inside a repository.
// packageDir is the directory the tests were compiled in -- this package's
// source directory, inside the checkout. Captured before the chdir below,
// because a handful of tests legitimately read tracked fixtures out of the
// checkout (git/install-hooks.sh and friends) and relative paths stop meaning
// that once we move. Read it through task18RepoRoot, never by resolving "..".
var packageDir string

func TestMain(m *testing.M) {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: %v\n", err)
		os.Exit(1)
	}
	packageDir = wd

	dir, err := os.MkdirTemp("", "agents-tests-cwd")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("GIT_CEILING_DIRECTORIES", dir); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: %v\n", err)
		os.Exit(1)
	}
	if err := os.Chdir(dir); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
