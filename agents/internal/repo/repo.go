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
	"fmt"
	"os"
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

// InfoExcludePath resolves the machine-local exclude file for a repo.
//
// It is deliberately not <root>/.git/info/exclude. In a linked worktree .git is
// a regular file holding a gitdir: pointer, so that path cannot even be created
// -- MkdirAll on it fails with ENOTDIR. git reads info/exclude from the common
// directory, which a worktree shares with its main checkout, so this is also the
// only spelling under which the two agree about what is ignored.
func InfoExcludePath(dir string) (string, error) {
	common, err := run(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", ErrNotARepo
	}
	// git answers relative to its own working directory, which is dir.
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	return filepath.Join(common, "info", "exclude"), nil
}

// StoreDir resolves the machine-local store for a repository.
//
// One store rather than two halves under different rationales. It holds the
// trace index, the transcript cache, and the untracked draft queue -- which is
// to say everything upstream of a conclusion. The tracked tier holds
// conclusions and nothing else.
//
// The common directory, for two reasons that both come from what the store
// holds. Once a harness deletes a transcript the cached copy is the only one
// that will ever exist, and a queued draft exists nowhere else at all, so
// neither must be filed anywhere a worktree takes with it:
// <root>/.agents/.trace-cache gave every linked worktree its own, and
// `git worktree remove` then deleted the only surviving copies. And transcript
// content must never be committed by the tool whose premise is that it records
// where transcripts are, never what they say -- inside the common directory git
// does not track it, structurally, rather than because something remembered to
// write an ignore file next to it.
//
// Not $XDG_CACHE_HOME, whose contract is non-essential data that can be
// regenerated: cleaners act on that, and this cannot be regenerated. Not XDG
// state either, which would need a stable repository identity -- paths and
// remotes both move, so the key would have to be the root-commit hash -- and
// would strand orphaned stores for repositories deleted months ago, needing a
// GC command. The common directory gives per-repository scoping and lifecycle
// for free.
//
// The cost, unmitigated: delete-and-re-clone loses the store, and any
// unpromoted draft with it.
func StoreDir(dir string) (string, error) {
	common, err := gitPath(dir, "--git-common-dir")
	if err != nil {
		return "", ErrNotARepo
	}
	return filepath.Join(common, "agents"), nil
}

// TraceCacheDir resolves where copied transcripts live for a repo. See StoreDir
// for why this location, and for what it costs.
func TraceCacheDir(dir string) (string, error) {
	store, err := StoreDir(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(store, "trace-cache"), nil
}

// LegacyHooksPath resolves the repository-owned hooks directory that a global
// core.hooksPath shadows. It asks Git for the common directory so linked
// worktrees do not incorrectly append hooks below their regular .git file.
func LegacyHooksPath(dir string) (string, error) {
	common, err := gitPath(dir, "--git-common-dir")
	if err != nil {
		return "", ErrNotARepo
	}
	return filepath.Join(common, "hooks"), nil
}

// IsLinkedWorktree reports whether dir sits in a linked worktree rather than in
// the main checkout, told apart the way git itself tells them apart: a linked
// worktree has its own git dir under <common>/worktrees/<name>, while the common
// directory -- and everything in it, info/exclude included -- is shared with the
// main checkout.
//
// The caller that needs this is `init --local`, which appends /.agents/ to that
// shared exclude file. In a worktree that entry also applies to the main
// checkout, where every NEW file under .agents/ then disappears from
// `git status --untracked-files=all` and is skipped by `git add`.
func IsLinkedWorktree(dir string) (bool, error) {
	gitDir, err := gitPath(dir, "--git-dir")
	if err != nil {
		return false, ErrNotARepo
	}
	common, err := gitPath(dir, "--git-common-dir")
	if err != nil {
		return false, ErrNotARepo
	}
	return gitDir != common, nil
}

// gitPath normalizes one of git's own directory answers into something two of
// them can be compared by. git answers relative to its working directory and not
// in a single spelling: from a subdirectory of a plain repo --git-dir comes back
// absolute and symlink-resolved while --git-common-dir comes back as "../../.git".
// Comparing those two answers raw labels every plain repo under a symlinked path
// a linked worktree -- /var on macOS, which is where the tests run.
func gitPath(dir, arg string) (string, error) {
	out, err := run(dir, "rev-parse", arg)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(dir, out)
	}
	out = filepath.Clean(out)
	// Best effort: the path exists in every case that reaches here, but a
	// failure to resolve is not a reason to refuse to answer.
	if resolved, err := filepath.EvalSymlinks(out); err == nil {
		out = resolved
	}
	return out, nil
}

func Discover(cwd string) (*Context, error) {
	root, err := discoverRoot(cwd)
	if err != nil {
		return nil, err
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

func discoverRoot(cwd string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	cmd.Env = withCLocale(sanitizeEnv(os.Environ()))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if strings.HasPrefix(message, "fatal: not a git repository (or any of the parent directories):") {
			return "", ErrNotARepo
		}
		return "", fmt.Errorf("discover Git repository: %w", err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func withCLocale(env []string) []string {
	var out []string
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if key != "LC_ALL" && key != "LANG" {
			out = append(out, item)
		}
	}
	return append(out, "LC_ALL=C", "LANG=C")
}

// Git runs a git command that changes the repository and returns its combined
// output, which is where git puts the reason it refused.
//
// It exists so that a caller which mutates a repository -- `agents save` runs
// `git add` and `git commit` -- goes through the same environment sanitizing as
// every question this package asks. git exports GIT_DIR into every hook it runs,
// and an inherited GIT_DIR aims the command at the repository that fired the
// hook while the working tree stays the caller's. Answering the wrong question
// is a wrong answer; committing into the wrong repository is someone else's
// history.
func Git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = sanitizeEnv(os.Environ())
	b, err := cmd.CombinedOutput()
	return string(b), err
}

// InProgress names the multi-step git operation the worktree is in the middle
// of -- "merge", "cherry-pick", "revert", "rebase", "am" or "bisect" -- and
// returns "" when there is none.
//
// It exists because `git commit -- <pathspec>` is not safe in any of them and
// git only refuses two. Measured against git 2.50.1:
//
//	merge, cherry-pick  git refuses: "cannot do a partial commit during a ..."
//	revert              git makes the commit, and committing clears REVERT_HEAD
//	                    -- the in-progress revert is gone with no message
//	rebase, am          git makes the commit, inserting it into the operation's
//	                    detached HEAD
//	bisect              git makes the commit on the bisect's detached HEAD and
//	                    prints "[detached HEAD 63344ea] ...", so the caller is
//	                    told the record was written; `git bisect reset` then
//	                    leaves it reachable from nothing (`git branch --contains`
//	                    comes back empty)
//
// So a caller that only handled git's own refusal would be silently destructive
// in exactly the cases where git says nothing.
//
// Deliberately NOT in the table: a plain detached HEAD. The hazard has the same
// shape -- a commit made there is reachable from HEAD alone -- but it is a state
// people work in on purpose (`git checkout <sha>` to look at something, a CI
// checkout, a worktree pinned to a tag), losing the commit takes a later choice
// of the user's, and there is no operation to finish or abandon, so there is
// nothing a refusal could tell them to do. A bisect differs in kind rather than
// degree: the next `git bisect good`/`bad` moves HEAD by itself and the closing
// `git bisect reset` always discards whatever was committed there, so the
// operation throws the record away without the user ever deciding to.
//
// The markers are read from --git-dir rather than --git-common-dir because all
// of them are per-worktree: a merge in one linked worktree must not stop a save
// in another.
func InProgress(dir string) (string, error) {
	gitDir, err := gitPath(dir, "--git-dir")
	if err != nil {
		return "", ErrNotARepo
	}
	for _, m := range []struct{ op, marker string }{
		{"merge", "MERGE_HEAD"},
		{"cherry-pick", "CHERRY_PICK_HEAD"},
		{"revert", "REVERT_HEAD"},
		// The two rebase backends keep their state under different names, and
		// neither leaves a *_HEAD marker of its own.
		{"rebase", "rebase-merge"},
		{"rebase", "rebase-apply"},
		// Written by `git bisect start`, before it has even detached HEAD.
		{"bisect", "BISECT_START"},
	} {
		if _, err := os.Lstat(filepath.Join(gitDir, m.marker)); err != nil {
			continue
		}
		// `git am` keeps its state in .git/rebase-apply too -- the same
		// directory the rebase apply backend uses -- and is told apart the way
		// git itself tells them apart: an am writes an `applying` file there, a
		// rebase writes `rebasing`. Calling it a rebase is not a cosmetic slip,
		// because the remedies do not carry over: `git rebase --continue` and
		// `git rebase --abort` during an am both fail with "fatal: It looks like
		// 'git am' is in progress. Cannot rebase."
		if m.marker == "rebase-apply" {
			if _, err := os.Lstat(filepath.Join(gitDir, m.marker, "applying")); err == nil {
				return "am", nil
			}
		}
		return m.op, nil
	}
	return "", nil
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	// Sanitize git environment so our invocations answer questions about the
	// caller's working directory, not whatever repo a hook was fired from.
	cmd.Env = sanitizeEnv(os.Environ())
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

func sanitizeEnv(env []string) []string {
	forbid := map[string]bool{
		"GIT_DIR":              true,
		"GIT_WORK_TREE":        true,
		"GIT_INDEX_FILE":       true,
		"GIT_OBJECT_DIRECTORY": true,
		"GIT_COMMON_DIR":       true,
		"GIT_NAMESPACE":        true,
	}
	var result []string
	for _, kv := range env {
		if idx := strings.IndexByte(kv, '='); idx >= 0 {
			key := kv[:idx]
			if !forbid[key] {
				result = append(result, kv)
			}
		}
	}
	return result
}
