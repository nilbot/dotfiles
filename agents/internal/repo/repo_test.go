package repo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
