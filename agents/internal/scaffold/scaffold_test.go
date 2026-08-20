package scaffold

import (
	"errors"
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

// excludePath is where git itself would look for dir's exclude file. Asked of
// git rather than computed, so it does not agree with the code under test by
// construction.
func excludePath(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		t.Fatalf("git rev-parse --git-common-dir in %s: %v", dir, err)
	}
	common := strings.TrimSpace(string(out))
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	return filepath.Join(common, "info", "exclude")
}

// readExclude reads the exclude file git itself would consult for dir.
func readExclude(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(excludePath(t, dir))
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
	claude, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(claude), DoctorInstruction) {
		t.Fatalf("new scaffold omits doctor instruction:\n%s", claude)
	}

	for _, rel := range []string{
		".agents/skills",
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
	if strings.Contains(string(attrs), "merge=union") {
		t.Error("merge=union outlived the tracked index it protected")
	}
	if !strings.Contains(string(attrs), "linguist-generated=true") {
		t.Error(".agents/** should collapse in diffs")
	}
	// The attribute tokens above say nothing about WHICH paths carry them, and a
	// pathspec matching nothing is indistinguishable from a missing line: two
	// merge=union is gone with the tracked index it protected; see
	// gitattributesLines. The rendering rule is a verbatim constraint and is
	// asserted whole.
	for _, want := range []string{
		".agents/** linguist-generated=true",
	} {
		if !hasLine(string(attrs), want) {
			t.Errorf(".gitattributes missing the exact line %q:\n%s", want, attrs)
		}
	}
}

func TestCreateDoesNotMigrateExistingClaudeMD(t *testing.T) {
	root := newRepo(t)
	path := filepath.Join(root, "CLAUDE.md")
	want := []byte("user-owned context\n")
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("Create migrated existing CLAUDE.md: got %q want %q", got, want)
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
	assertIgnored(t, linked, ".claude/.agents-wire.lock")
	assertIgnored(t, linked, ".codex/hooks.json")
	assertIgnored(t, linked, ".codex/.agents-wire.lock")

	if _, err := os.Stat(filepath.Join(linked, ".agents", "skills")); err != nil {
		t.Errorf("layout missing in the worktree: %v", err)
	}
}

// --local's only mechanism is an exclude entry, and a linked worktree SHARES
// info/exclude with its main checkout. Measured consequence over there:
// already-tracked .agents/ content stays visible, but every NEW file under
// .agents/ becomes invisible -- git check-ignore matches it, it drops out of
// `git status --untracked-files=all`, and `git add .` skips it. That silently
// discards exactly the handoffs, memory notes and traces this tool exists to
// preserve, so Create refuses. A warning in scrollback would not be a defence.
func TestCreateLocalRefusesInALinkedWorktree(t *testing.T) {
	main := newRepo(t)
	if err := os.WriteFile(filepath.Join(main, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, main, "add", "f.txt")
	git(t, main, "commit", "-m", "init", "--no-verify")

	linked := filepath.Join(t.TempDir(), "linked")
	git(t, main, "worktree", "add", "-b", "feat", linked)

	// Precondition: a hand-made .git/info is not a repository git will accept,
	// and a second clone shares no exclude file. Only a real `git worktree add`
	// exercises this.
	if fi, err := os.Lstat(filepath.Join(linked, ".git")); err != nil || fi.IsDir() {
		t.Fatalf("fixture is not a linked worktree: .git must be a regular file (err=%v)", err)
	}

	// Errorf, not Fatalf: everything below is what refusing is FOR, and a run
	// that stops here would report the missing error without ever showing the
	// damage it prevents.
	if err := Create(linked, true); !errors.Is(err, ErrLocalInLinkedWorktree) {
		t.Errorf("Create(--local) in a linked worktree: err = %v, want ErrLocalInLinkedWorktree", err)
	}

	// Refused before writing anything: a repo left half-scaffolded and unwired is
	// the other way this goes wrong, and `init` reports the same failure either
	// way.
	for _, rel := range []string{".agents", "CLAUDE.md", "AGENTS.md", ".gitattributes"} {
		if _, err := os.Lstat(filepath.Join(linked, rel)); !os.IsNotExist(err) {
			t.Errorf("refusal left %s behind (err=%v)", rel, err)
		}
	}

	// The shared exclude is what the refusal protects. It may not exist at all
	// here, which is equally fine; what must not be true is that /.agents/ is in
	// it.
	if b, err := os.ReadFile(excludePath(t, linked)); err == nil && hasLine(string(b), "/.agents/") {
		t.Errorf("refusal still wrote /.agents/ to the shared exclude:\n%s", b)
	}
	// git is the arbiter, and the main checkout is where the damage would show.
	for _, dir := range []string{main, linked} {
		assertNotIgnored(t, dir, ".agents/reports/handoff/2026-08-07-lane.md")
	}

	// The main checkout of the very same repo is not a linked worktree, so
	// --local still works there. Without this the refusal could be "any repo that
	// has worktrees" and nothing would notice.
	if err := Create(main, true); err != nil {
		t.Fatalf("Create(--local) in the main checkout: %v", err)
	}
	if !hasLine(readExclude(t, main), "/.agents/") {
		t.Errorf("--local in the main checkout must still exclude .agents/:\n%s", readExclude(t, main))
	}
}

func assertNotIgnored(t *testing.T, dir, path string) {
	t.Helper()
	cmd := exec.Command("git", "check-ignore", "-q", path)
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		t.Errorf("git ignores %s in %s: new agent output would be dropped silently", path, dir)
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
	if !strings.Contains(string(attrs), "linguist-generated=true") {
		t.Error("our lines must still be appended")
	}

	// Idempotent: a second Create must not duplicate the appended lines.
	if err := Create(root, false); err != nil {
		t.Fatalf("second Create: %v", err)
	}
	attrs2, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if strings.Count(string(attrs2), "linguist-generated=true") != 1 {
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
	for _, want := range []string{
		"/.claude/settings.json",
		"/.claude/.agents-wire.lock",
		"/.codex/hooks.json",
		"/.codex/.agents-wire.lock",
	} {
		if !strings.Contains(exclude, want) {
			t.Errorf("exclude missing %q:\n%s", want, exclude)
		}
		assertIgnored(t, root, strings.TrimPrefix(want, "/"))
	}
}

func TestCreateNoLongerScaffoldsATrackedTraceDirectory(t *testing.T) {
	root := newRepo(t)
	if err := Create(root, false); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "reports", "traces")); !os.IsNotExist(err) {
		t.Error("scaffold still creates .agents/reports/traces; the index is machine-local now")
	}
	b, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "merge=union") {
		t.Error("merge=union survives, but nothing tracked appends concurrently now")
	}
	// The rendering rule is unrelated to traces and must stay.
	if !strings.Contains(string(b), "linguist-generated=true") {
		t.Error("Create dropped the linguist-generated attribute along with merge=union")
	}
}

// The scaffolded CLAUDE.md must not name a command the binary no longer has.
//
// Scaffolding is written into repositories that this session will never see
// again, so a stale command name here is a dead instruction with a long half
// life. The capture instruction it used to carry -- "record it before moving
// on ... write it with `agents handoff draft`" -- went with the apparatus on
// 2026-08-20; recording is a global instruction now and is deliberately not
// restated per repository.
func TestClaudeMDNamesNoRetiredCommand(t *testing.T) {
	for _, dead := range []string{
		"agents handoff", "agents review", "agents index",
		".agents/memory", ".agents/reports/handoff",
	} {
		if strings.Contains(ClaudeMD, dead) {
			t.Errorf("the scaffolded CLAUDE.md still names %q, which no longer exists", dead)
		}
	}
	// It still has to point somewhere, and at the thing that survived.
	for _, want := range []string{"docs/qna/", "docs/journal/", ".agents/skills/"} {
		if !strings.Contains(ClaudeMD, want) {
			t.Errorf("the scaffolded CLAUDE.md does not point at %s", want)
		}
	}
	if !strings.Contains(ClaudeMD, DoctorInstruction) {
		t.Error("the scaffolded CLAUDE.md dropped the doctor instruction")
	}
}
