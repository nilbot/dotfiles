package main

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

func TestRunVersion(t *testing.T) {
	origVersion, origCommit, origDate := version, commit, date
	t.Cleanup(func() {
		version, commit, date = origVersion, origCommit, origDate
	})

	version = "v1.2.3"
	commit = "abcdef123456"
	date = "2026-08-28T12:00:00Z"

	var buf bytes.Buffer
	code := runVersion(nil, &buf)
	if code != exitcode.OK {
		t.Fatalf("runVersion exit = %d, want OK (%d)", code, exitcode.OK)
	}

	want := fmt.Sprintf("agents %s (commit: %s, built: %s)\n", version, commit, date)
	if got := buf.String(); got != want {
		t.Errorf("runVersion output = %q, want %q", got, want)
	}
}

func TestRunVersionDefaultValues(t *testing.T) {
	origVersion, origCommit, origDate := version, commit, date
	t.Cleanup(func() {
		version, commit, date = origVersion, origCommit, origDate
	})

	version = "dev"
	commit = "none"
	date = "unknown"

	var buf bytes.Buffer
	code := runVersion([]string{"some", "args"}, &buf)
	if code != exitcode.OK {
		t.Fatalf("runVersion exit = %d, want OK (%d)", code, exitcode.OK)
	}

	want := "agents dev (commit: none, built: unknown)\n"
	if got := buf.String(); got != want {
		t.Errorf("runVersion output = %q, want %q", got, want)
	}
}
