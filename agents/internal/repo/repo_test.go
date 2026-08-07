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
