package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/harness"
)

func TestInitScaffoldsWiresAndReportsTrust(t *testing.T) {
	root := newRepo(t)
	if err := os.RemoveAll(filepath.Join(root, ".agents")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	// Advisory, not OK: the trust step is still outstanding, and an exit code
	// of 0 would report a working setup that is not yet working.
	if code := runInit(nil, &out); code != 1 {
		t.Fatalf("exit = %d, want 1 (advisory); output:\n%s", code, out.String())
	}

	for _, rel := range []string{
		".agents/memory", "CLAUDE.md", "AGENTS.md",
		".claude/settings.json", ".codex/hooks.json",
	} {
		if _, err := os.Lstat(filepath.Join(root, rel)); err != nil {
			t.Errorf("init did not create %s: %v", rel, err)
		}
	}
	if !strings.Contains(out.String(), "trust") {
		t.Errorf("init must print the outstanding trust steps:\n%s", out.String())
	}
	// The word "trust" alone is satisfied by the section heading, so an init
	// that printed the heading and no steps under it would pass the check
	// above. The steps are the point: a wired repo that records nothing until
	// someone performs them is exactly what this output exists to prevent.
	for _, a := range harness.All() {
		steps := a.TrustSteps(root)
		if len(steps) == 0 {
			t.Errorf("%s reports no trust steps; no harness runs a freshly wired repo's hooks unattended", a.Name())
		}
		for _, s := range steps {
			if !strings.Contains(out.String(), s) {
				t.Errorf("init did not print %s's step %q:\n%s", a.Name(), s, out.String())
			}
		}
	}
}

// The --local flag has to reach scaffold.Create. Nothing else in the command
// observes it, so a dropped argument is silent: the layout is identical either
// way and only the exclude file differs.
func TestInitLocalKeepsAgentsDirOutOfTheRepo(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runInit([]string{"--local"}, &out); code != 1 {
		t.Fatalf("exit = %d, want 1 (advisory); output:\n%s", code, out.String())
	}
	exclude, err := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("exclude: %v", err)
	}
	var found bool
	for _, l := range strings.Split(string(exclude), "\n") {
		// A whole-line match: /.agents/.trace-cache/ is written unconditionally
		// and contains "/.agents/" as a substring.
		if strings.TrimSpace(l) == "/.agents/" {
			found = true
		}
	}
	if !found {
		t.Errorf("init --local must exclude the whole .agents/ directory:\n%s", exclude)
	}
}

func TestInitIsIdempotent(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)
	var out bytes.Buffer
	runInit(nil, &out)
	before, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	runInit(nil, &out)
	after, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if string(before) != string(after) {
		t.Errorf("init is not idempotent:\n%s\n---\n%s", before, after)
	}
}

func TestInitOutsideRepoSkips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	nested := filepath.Join(dir, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	var out bytes.Buffer
	if code := runInit(nil, &out); code != 4 {
		t.Fatalf("exit = %d, want 4 (skip) outside a repo", code)
	}
}
