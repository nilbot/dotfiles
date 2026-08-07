package repo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// git in a directory, failing the test rather than the caller.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

func resolved(t *testing.T, path string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", path, err)
	}
	return r
}

func TestInfoExcludePathInAPlainRepo(t *testing.T) {
	dir := initRepo(t)
	sub := filepath.Join(dir, "payments", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(resolved(t, dir), ".git", "info", "exclude")
	// The answer must not depend on where the caller is standing.
	for _, from := range []string{dir, sub} {
		got, err := InfoExcludePath(from)
		if err != nil {
			t.Fatalf("InfoExcludePath(%s): %v", from, err)
		}
		if resolved(t, filepath.Dir(filepath.Dir(got))) != resolved(t, filepath.Join(dir, ".git")) {
			t.Errorf("InfoExcludePath(%s) = %q, want it under %q", from, got, want)
		}
		if filepath.Base(got) != "exclude" || filepath.Base(filepath.Dir(got)) != "info" {
			t.Errorf("InfoExcludePath(%s) = %q, want .../info/exclude", from, got)
		}
	}
}

// In a linked worktree .git is a regular FILE, so <root>/.git/info/exclude is
// not a path that can be created -- MkdirAll on it fails with ENOTDIR, and
// `agents init` dies after scaffolding but before wiring. git keeps info/exclude
// in the common directory, shared with the main checkout.
func TestInfoExcludePathInALinkedWorktree(t *testing.T) {
	main := initRepo(t)
	if err := os.WriteFile(filepath.Join(main, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, main, "add", "f.txt")
	git(t, main, "-c", "commit.gpgsign=false", "commit", "-m", "init", "--no-verify")

	linked := filepath.Join(t.TempDir(), "linked")
	git(t, main, "worktree", "add", "-b", "feat", linked)

	// Precondition: this is a real linked worktree, not a second clone.
	if fi, err := os.Lstat(filepath.Join(linked, ".git")); err != nil || fi.IsDir() {
		t.Fatalf("fixture is not a linked worktree: .git must be a regular file (err=%v)", err)
	}

	got, err := InfoExcludePath(linked)
	if err != nil {
		t.Fatalf("InfoExcludePath: %v", err)
	}
	// Compare against the resolved main checkout: git reports an absolute,
	// symlink-resolved common dir for a worktree. .git/info itself may not exist
	// yet, so this cannot go through EvalSymlinks.
	want := filepath.Join(resolved(t, main), ".git", "info", "exclude")
	if got != want {
		t.Errorf("InfoExcludePath = %q, want the common dir's %q", got, want)
	}
	// The whole point: not the worktree's own .git, which is a file.
	if strings.HasPrefix(got, linked+string(filepath.Separator)+".git") {
		t.Errorf("InfoExcludePath = %q, which is inside the worktree's .git FILE", got)
	}
	// And the directory must actually be creatable, which is what broke.
	if err := os.MkdirAll(filepath.Dir(got), 0o755); err != nil {
		t.Errorf("cannot create %s: %v", filepath.Dir(got), err)
	}
}

// IsLinkedWorktree is what `init --local` consults before appending to a SHARED
// info/exclude, so both answers are load-bearing: a false negative hides every
// new .agents/ file from the main checkout too, and a false positive refuses
// --local in ordinary repositories.
//
// The subdirectory rows are the ones that catch a naive implementation. git
// answers --git-dir absolute and symlink-resolved from a subdirectory but
// --git-common-dir as "../../.git", so comparing the two raw labels every plain
// repo under a symlinked path -- /var/folders, where t.TempDir lives -- a linked
// worktree.
func TestIsLinkedWorktreeDistinguishesMainFromLinked(t *testing.T) {
	main := initRepo(t)
	if err := os.WriteFile(filepath.Join(main, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, main, "add", "f.txt")
	git(t, main, "-c", "commit.gpgsign=false", "commit", "-m", "init", "--no-verify")

	linked := filepath.Join(t.TempDir(), "linked")
	git(t, main, "worktree", "add", "-b", "feat", linked)

	// Precondition: a real linked worktree, not a second clone. Without this the
	// whole test degrades into asserting false twice.
	if fi, err := os.Lstat(filepath.Join(linked, ".git")); err != nil || fi.IsDir() {
		t.Fatalf("fixture is not a linked worktree: .git must be a regular file (err=%v)", err)
	}

	mainSub := filepath.Join(main, "payments", "api")
	linkedSub := filepath.Join(linked, "payments", "api")
	for _, d := range []string{mainSub, linkedSub} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		dir  string
		want bool
	}{
		{main, false},
		{mainSub, false},
		{linked, true},
		{linkedSub, true},
	} {
		got, err := IsLinkedWorktree(tc.dir)
		if err != nil {
			t.Fatalf("IsLinkedWorktree(%s): %v", tc.dir, err)
		}
		if got != tc.want {
			t.Errorf("IsLinkedWorktree(%s) = %v, want %v", tc.dir, got, tc.want)
		}
	}
}

func TestIsLinkedWorktreeOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	outside := filepath.Join(dir, "nested")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := IsLinkedWorktree(outside); !errors.Is(err, ErrNotARepo) {
		t.Fatalf("err = %v, want ErrNotARepo", err)
	}
}

func TestInfoExcludePathOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	outside := filepath.Join(dir, "nested")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InfoExcludePath(outside); !errors.Is(err, ErrNotARepo) {
		t.Fatalf("err = %v, want ErrNotARepo", err)
	}
}

func TestDiscoverFindsRootBranchAndRelativeCwd(t *testing.T) {
	dir := initRepo(t)
	sub := filepath.Join(dir, "payments", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	rc, err := Discover(sub)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// macOS puts TempDir under /var, a symlink to /private/var; git reports the
	// resolved path. Compare resolved forms or this test fails for the wrong
	// reason.
	wantRoot, _ := filepath.EvalSymlinks(dir)
	if rc.Root != wantRoot {
		t.Errorf("Root = %q, want %q", rc.Root, wantRoot)
	}
	if rc.Branch != "main" {
		t.Errorf("Branch = %q, want main", rc.Branch)
	}
	if rc.RelCwd != "payments/api" {
		t.Errorf("RelCwd = %q, want payments/api", rc.RelCwd)
	}
	if rc.Worktree != filepath.Base(wantRoot) {
		t.Errorf("Worktree = %q, want %q", rc.Worktree, filepath.Base(wantRoot))
	}
}

func TestDiscoverAtRootReportsDot(t *testing.T) {
	dir := initRepo(t)
	rc, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if rc.RelCwd != "." {
		t.Errorf("RelCwd = %q, want .", rc.RelCwd)
	}
}

func TestDiscoverOutsideRepo(t *testing.T) {
	// A directory that is definitively not inside a git repo.
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	outside := filepath.Join(dir, "nested")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Discover(outside); !errors.Is(err, ErrNotARepo) {
		t.Fatalf("err = %v, want ErrNotARepo", err)
	}
}

func TestDiscoverIgnoresGitDirEnv(t *testing.T) {
	// Hooks run with GIT_DIR set to a relative path like ".git". Discover should
	// ignore this env var and find the repo based on cwd, not the env-poisoned git.
	dir := initRepo(t)
	sub := filepath.Join(dir, "payments", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// Set GIT_DIR to a relative path (what git actually exports to hooks).
	t.Setenv("GIT_DIR", ".git")

	rc, err := Discover(sub)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// Should find the correct repo root and branch, not be confused by GIT_DIR.
	wantRoot, _ := filepath.EvalSymlinks(dir)
	if rc.Root != wantRoot {
		t.Errorf("Root = %q, want %q", rc.Root, wantRoot)
	}
	if rc.Branch != "main" {
		t.Errorf("Branch = %q, want main", rc.Branch)
	}
}

func TestDiscoverDetachedHead(t *testing.T) {
	dir := initRepo(t)

	// Create a commit so we can check out a detached HEAD at it.
	file := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(file, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "test.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "test")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	// Check out a detached HEAD.
	cmd = exec.Command("git", "checkout", "--detach", "HEAD")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout --detach: %v\n%s", err, out)
	}

	rc, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// Detached HEAD is not an error; Branch should be empty.
	if rc.Branch != "" {
		t.Errorf("Branch = %q, want empty string for detached HEAD", rc.Branch)
	}
}
