package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/harness"
)

// `agents wire` regenerates harness configs and nothing else. The brief ships
// no test for it; without one the whole command is a mutant's free lunch.
func TestWireRegeneratesConfigsWithoutScaffolding(t *testing.T) {
	root := newRepo(t)
	if err := os.RemoveAll(filepath.Join(root, ".agents")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	if code := runWire(nil, &out); code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, out.String())
	}

	for _, a := range harness.All() {
		if _, err := os.Stat(a.WireConfigPath(root)); err != nil {
			t.Errorf("wire did not write %s's config: %v", a.Name(), err)
		}
	}
	// Unlike init, wire does not scaffold. Creating .agents/ here would hide
	// the very "run agents init first" condition the hook reports.
	if _, err := os.Stat(filepath.Join(root, ".agents")); err == nil {
		t.Error("wire must not create .agents/; that is init's job")
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err == nil {
		t.Error("wire must not write CLAUDE.md; that is init's job")
	}
}

func TestWireOutsideRepoSkips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	nested := filepath.Join(dir, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	var out bytes.Buffer
	if code := runWire(nil, &out); code != 4 {
		t.Fatalf("exit = %d, want 4 (skip) outside a repo", code)
	}
	if _, err := os.Stat(filepath.Join(nested, ".claude")); err == nil {
		t.Error("wire wrote a config outside a repository")
	}
}

// A wiring failure must not be reported as success: the caller's next move is
// to start a session, and a half-wired repo records nothing.
func TestWireReportsFailureAsNoRecord(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)

	// A real .claude/skills directory is content linkSkills refuses to replace.
	if err := os.MkdirAll(filepath.Join(root, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runWire(nil, &out); code != 5 {
		t.Fatalf("exit = %d, want 5 (no-record); output:\n%s", code, out.String())
	}
}
