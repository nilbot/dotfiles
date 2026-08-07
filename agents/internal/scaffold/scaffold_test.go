package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRepo builds a real git repository. An earlier version of these tests just
// mkdir'd .git/info, which git does not recognise as a repository at all -- so
// nothing here ever exercised the path Create actually takes.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		git(t, dir, args...)
	}
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// readExclude reads the exclude file git itself would consult for dir.
func readExclude(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		t.Fatalf("git rev-parse --git-common-dir in %s: %v", dir, err)
	}
	common := strings.TrimSpace(string(out))
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	b, err := os.ReadFile(filepath.Join(common, "info", "exclude"))
	if err != nil {
		t.Fatalf("exclude: %v", err)
	}
	return string(b)
}

func TestCreateBuildsLayout(t *testing.T) {
	root := newRepo(t)

	if err := Create(root, false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, rel := range []string{
		".agents/memory",
		".agents/reports/handoff",
		".agents/reports/specs",
		".agents/reports/plans",
		".agents/reports/analysis",
		".agents/reports/traces",
		".agents/skills",
	} {
		if fi, err := os.Stat(filepath.Join(root, rel)); err != nil || !fi.IsDir() {
			t.Errorf("missing directory %s", rel)
		}
	}

	// AGENTS.md is a symlink, not a copy: two byte-identical files silently
	// diverge, which is what the prior art actually did.
	fi, err := os.Lstat(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("AGENTS.md must be a symlink to CLAUDE.md")
	}
	target, _ := os.Readlink(filepath.Join(root, "AGENTS.md"))
	if target != "CLAUDE.md" {
		t.Fatalf("AGENTS.md -> %q, want CLAUDE.md", target)
	}

	attrs, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatalf(".gitattributes: %v", err)
	}
	if !strings.Contains(string(attrs), "merge=union") {
		t.Error("traces need merge=union or a concurrent append produces invalid JSON")
	}
	if !strings.Contains(string(attrs), "linguist-generated=true") {
		t.Error(".agents/** should collapse in diffs")
	}
	// The attribute tokens above say nothing about WHICH paths carry them, and a
	// pathspec matching nothing is indistinguishable from a missing line: two
	// branches appending traces on the same day then produce conflict markers
	// that are not valid JSON, which a line-oriented reader silently drops.
	// Both lines are verbatim plan constraints, so assert them whole.
	for _, want := range []string{
		".agents/reports/traces/*.jsonl merge=union",
		".agents/** linguist-generated=true",
	} {
		if !hasLine(string(attrs), want) {
			t.Errorf(".gitattributes missing the exact line %q:\n%s", want, attrs)
		}
	}
}

// A linked worktree's .git is a regular FILE, so <root>/.git/info/exclude is not
// a path that can be created -- MkdirAll fails with ENOTDIR. Create used to die
// there, after writing .agents/, CLAUDE.md, AGENTS.md and .gitattributes and
// before anything was wired, on every run.
func TestCreateWorksInALinkedWorktree(t *testing.T) {
	main := newRepo(t)
	if err := os.WriteFile(filepath.Join(main, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, main, "add", "f.txt")
	git(t, main, "commit", "-m", "init", "--no-verify")

	linked := filepath.Join(t.TempDir(), "linked")
	git(t, main, "worktree", "add", "-b", "feat", linked)

	// Precondition: without this the test silently degrades to a plain repo.
	if fi, err := os.Lstat(filepath.Join(linked, ".git")); err != nil || fi.IsDir() {
		t.Fatalf("fixture is not a linked worktree: .git must be a regular file (err=%v)", err)
	}

	if err := Create(linked, false); err != nil {
		t.Fatalf("Create in a linked worktree: %v", err)
	}

	// The exclude lines must land where git will actually read them, which for a
	// worktree is the common dir it shares with the main checkout.
	exclude := readExclude(t, linked)
	for _, want := range excludeLines {
		if !hasLine(exclude, want) {
			t.Errorf("exclude missing %q:\n%s", want, exclude)
		}
	}
	// git is the arbiter: ask it, rather than trusting our own path arithmetic.
	assertIgnored(t, linked, ".claude/settings.json")
	assertIgnored(t, linked, ".codex/hooks.json")

	if _, err := os.Stat(filepath.Join(linked, ".agents", "memory")); err != nil {
		t.Errorf("layout missing in the worktree: %v", err)
	}
}

func assertIgnored(t *testing.T, dir, path string) {
	t.Helper()
	cmd := exec.Command("git", "check-ignore", "-q", path)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Errorf("git does not ignore %s in %s", path, dir)
	}
}

func TestCreatePreservesExistingFiles(t *testing.T) {
	root := newRepo(t)
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("*.png binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Create(root, false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	claude, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if string(claude) != "# mine\n" {
		t.Error("an existing CLAUDE.md must not be overwritten")
	}
	attrs, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if !strings.Contains(string(attrs), "*.png binary") {
		t.Error("existing gitattributes lines must survive")
	}
	if !strings.Contains(string(attrs), "merge=union") {
		t.Error("our lines must still be appended")
	}

	// Idempotent: a second Create must not duplicate the appended lines.
	if err := Create(root, false); err != nil {
		t.Fatalf("second Create: %v", err)
	}
	attrs2, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if strings.Count(string(attrs2), "merge=union") != 1 {
		t.Errorf("gitattributes duplicated on re-run:\n%s", attrs2)
	}
}

func TestCreateLocalExcludesAgentsDir(t *testing.T) {
	root := newRepo(t)
	if err := Create(root, true); err != nil {
		t.Fatalf("Create: %v", err)
	}
	exclude := readExclude(t, root)
	if !strings.Contains(exclude, "/.agents/") {
		t.Errorf("--local must exclude .agents/:\n%s", exclude)
	}
	// The substring check above is satisfied by the always-written
	// /.agents/.trace-cache/ entry, so on its own it passes even when --local
	// adds nothing at all. The exclude entry has to be the bare directory.
	if !hasLine(string(exclude), "/.agents/") {
		t.Errorf("--local must exclude the whole directory, not just a subpath of it:\n%s", exclude)
	}
}

// Without --local, .agents/ is tracked. An exclude entry for it would make the
// scaffolded directory invisible to git in the mode whose entire point is that
// it is committed.
func TestCreateWithoutLocalTracksAgentsDir(t *testing.T) {
	root := newRepo(t)
	if err := Create(root, false); err != nil {
		t.Fatalf("Create: %v", err)
	}
	exclude := readExclude(t, root)
	if hasLine(exclude, "/.agents/") {
		t.Errorf("without --local, .agents/ must stay tracked:\n%s", exclude)
	}
}

// hasLine reports whether want is present as a whole line, which strings.Contains
// cannot distinguish from being a prefix of some longer entry.
func hasLine(content, want string) bool {
	for _, l := range strings.Split(content, "\n") {
		if strings.TrimSpace(l) == want {
			return true
		}
	}
	return false
}

func TestCreateAlwaysExcludesGeneratedHarnessConfigs(t *testing.T) {
	root := newRepo(t)
	if err := Create(root, false); err != nil {
		t.Fatalf("Create: %v", err)
	}
	exclude := readExclude(t, root)
	for _, want := range []string{"/.claude/settings.json", "/.codex/hooks.json", "/.agents/.trace-cache/"} {
		if !strings.Contains(exclude, want) {
			t.Errorf("exclude missing %q:\n%s", want, exclude)
		}
	}
}
