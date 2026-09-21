package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/harness"
)

func TestInitScaffoldsWiresAndReportsTrust(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
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

	// What init creates. The harness config files are deliberately NOT here:
	// with no entries to write there is nothing for init to put in them, and a
	// fresh repository with no .claude/settings.json is the correct state.
	for _, rel := range []string{
		".agents/skills", "CLAUDE.md", "AGENTS.md",
		"docs/design", "docs/plans", "docs/journal", "docs/qna",
		".claude/skills",
	} {
		if _, err := os.Lstat(filepath.Join(root, rel)); err != nil {
			t.Errorf("init did not create %s: %v", rel, err)
		}
	}
	for _, rel := range []string{".claude/settings.json", ".codex/hooks.json", ".agents/hooks.json"} {
		if _, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			t.Errorf("init created %s; it has nothing to write there any more", rel)
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
	t.Setenv("XDG_STATE_HOME", t.TempDir())
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
	t.Setenv("XDG_STATE_HOME", t.TempDir())
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
	t.Setenv("XDG_STATE_HOME", t.TempDir())
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

// A freshly initialized repository must be committable. Without the generated
// indexes the pre-commit guard regenerates them, finds them unstaged, and
// blocks -- so `agents init` would produce a tree whose first commit fails.
func TestInitLeavesARepositoryThatCanCommit(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newRepo(t)
	t.Chdir(root)
	var out bytes.Buffer
	runInit(nil, &out)

	// The generated indexes went with the stores they described. What init
	// still owes a repository is the scaffolding that makes it usable at all.
	for _, rel := range []string{
		"CLAUDE.md",
		filepath.Join(".agents", "skills"),
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("init did not create %s: %v", rel, err)
		}
	}

	git(t, root, "add", "-A")
	cmd := exec.Command("git", "commit", "-m", "agents init")
	cmd.Dir = root
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the first commit after `agents init` was blocked:\n%s", b)
	}
	if s := git(t, root, "status", "--porcelain"); strings.TrimSpace(s) != "" {
		t.Errorf("init left uncommittable residue:\n%s", s)
	}
}

// A test that registers into the machine's real fleet registry is a test that
// escaped its sandbox. machine.StateDir() reads XDG_STATE_HOME first and
// os.UserHomeDir() otherwise, so a test that sets neither writes to the
// developer's own ~/.local/state/agents/registry.json -- measured 2026-08-14 at
// 56 entries on this machine, of which 2 were real repositories.
//

// The pre-flight asks both paths too: `/.agents/**` is invisible to a
// directory-only query, and `--local` itself writes the `/.agents/` form.
//
