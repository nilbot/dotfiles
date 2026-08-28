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

func TestVersionCommandAndFlags(t *testing.T) {
	origVersion, origCommit, origDate := version, commit, date
	t.Cleanup(func() {
		version, commit, date = origVersion, origCommit, origDate
	})

	version = "v0.3.0"
	commit = "1234567"
	date = "2026-08-28T15:04:05Z"

	wantOutput := fmt.Sprintf("agents %s (commit: %s, built: %s)\n", version, commit, date)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"command", []string{"version"}},
		{"long flag", []string{"--version"}},
		{"short flag", []string{"-v"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			var code int
			stdout, stderr := captureStdoutAndStderr(t, func() {
				code = run(tc.args)
			})
			if code != 0 {
				t.Errorf("run(%v) exit = %d, want 0", tc.args, code)
			}
			if stdout != wantOutput {
				t.Errorf("run(%v) stdout = %q, want %q", tc.args, stdout, wantOutput)
			}
			if stderr != "" {
				t.Errorf("run(%v) stderr = %q, want empty", tc.args, stderr)
			}
		})
	}

	t.Run("extra args with --version is malformed", func(t *testing.T) {
		t.Chdir(t.TempDir())
		var code int
		stdout, stderr := captureStdoutAndStderr(t, func() {
			code = run([]string{"--version", "extra"})
		})
		if code == 0 {
			t.Errorf("run([--version extra]) exit = %d, want non-zero", code)
		}
		if stdout != "" {
			t.Errorf("run([--version extra]) stdout = %q, want empty", stdout)
		}
		if stderr == "" {
			t.Errorf("run([--version extra]) stderr is empty, want error message")
		}
	})
}
