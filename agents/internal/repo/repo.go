// Package repo answers the questions this tool asks of git: where the worktree
// root is, what branch is checked out, and where the caller is standing
// relative to the root.
//
// It shells out to git rather than reimplementing repository discovery. git is
// already a hard dependency of everything here, and .git layouts (worktrees,
// submodules, alternates) have more edge cases than are worth owning.
package repo

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotARepo means the given directory is not inside a git worktree. Callers
// that are hooks should treat this as exitcode.Skip, not as a failure.
var ErrNotARepo = errors.New("not inside a git repository")

type Context struct {
	Root     string // absolute, symlinks resolved, as git reports it
	Branch   string // empty when HEAD is detached
	Worktree string // basename of Root
	RelCwd   string // slash-separated, "." at the root
}

// AgentsDir is the tracked context directory for a repo root.
func AgentsDir(root string) string { return filepath.Join(root, ".agents") }

func Discover(cwd string) (*Context, error) {
	root, err := run(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, ErrNotARepo
	}

	rel := "."
	if abs, err := filepath.Abs(cwd); err == nil {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		if r, err := filepath.Rel(root, abs); err == nil {
			rel = filepath.ToSlash(r)
		}
	}

	// A detached HEAD has no branch. That is normal (bisect, CI, a rebase in
	// progress), so it is not an error -- lane resolution falls through.
	branch, _ := run(cwd, "symbolic-ref", "--short", "-q", "HEAD")

	return &Context{
		Root:     root,
		Branch:   branch,
		Worktree: filepath.Base(root),
		RelCwd:   rel,
	}, nil
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
