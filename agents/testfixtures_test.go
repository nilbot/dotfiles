package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

// newRepo is the shared git fixture: a work tree on a branch, isolated from the
// machine's Git templates and hook chain, with a deterministic identity so a
// test can commit.
//
// It lives in its own file because it outlived the test file it was written in.
// When the session-record command went, so did cmd_hook_test.go, and six other
// test files were still calling this.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// --template with an empty directory isolates this test from any Git
	// templates configured on the machine running it. The local relative
	// core.hooksPath also isolates it from a machine-wide hook chain while
	// keeping .git/hooks/ live for tests that deliberately install a hook there.
	empty := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "sq-123/payments", "--template=" + empty},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
		{"config", "core.hooksPath", ".git/hooks"},
		// git's background maintenance must not run under a fixture a test is
		// concurrently walking.
		{"config", "maintenance.auto", "false"},
		{"config", "gc.auto", "0"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	return resolved
}

// newRepoWithAgents is newRepo with the tracked .agents/ tree in place, which
// is the state `init` leaves a repository in.
func newRepoWithAgents(t *testing.T) string {
	t.Helper()
	root := newRepo(t)
	if err := scaffold.Create(root, false); err != nil {
		t.Fatal(err)
	}
	return root
}

// git runs a git command in root and fails the test on error, returning stdout.
func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// snapshotTree records every path under root with its mode and bytes, so a test
// can assert that a refused command wrote nothing at all -- including a file it
// rewrote with the bytes it already had, which a content-only comparison would
// call unchanged.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			snap[rel] = "dir " + info.Mode().String()
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			snap[rel] = "symlink " + target
		default:
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			snap[rel] = info.Mode().String() + " " + string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snap
}
