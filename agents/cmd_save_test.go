package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

// Fixture content. Every field carries a distinct, non-empty value so that a
// wrong file, or a stale version of the right file, is visible in a diff rather
// than plausible.
const (
	memEntryA   = "---\nname: a-thing\ndescription: A\nmetadata:\n  type: project\n---\n\nx\n"
	handoffEntA = "---\nlane: lane-a\nsession: s1\nstatus: reviewed\nwhen: 2026-08-10T09:00:00Z\n---\n\nbody\n"
)

// gitTry runs git and hands back its combined output and its error.
//
// It drops the GIT_* variables that redirect git at another repository, for the
// same reason internal/repo does: one test below sets a hostile GIT_DIR on
// purpose, and the assertions have to keep seeing the repository on disk.
func gitTry(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	env := os.Environ()
	kept := env[:0]
	for _, kv := range env {
		switch strings.SplitN(kv, "=", 2)[0] {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_COMMON_DIR", "GIT_NAMESPACE":
		default:
			kept = append(kept, kv)
		}
	}
	cmd.Env = kept
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitTry(t, dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func writeFileAt(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeHandoffEntry(t *testing.T, root, lane, name, body string) {
	t.Helper()
	writeFileAt(t, root, ".agents/reports/handoff/"+lane+"/"+name, body)
}

// committedFiles is the sorted path list of HEAD.
func committedFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	for _, l := range strings.Split(gitOut(t, root, "show", "--name-only", "--pretty=format:", "HEAD"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	sort.Strings(files)
	return files
}

// The scoping is the whole command, so the fixture puts two kinds of unrelated
// change alongside it: a NEW file and a MODIFIED TRACKED file, which take
// different paths through git's index. The tracked one is left in all three
// states at once -- v1 committed, v2 staged, v3 in the worktree -- because that
// is what pins preservation rather than mere survival: an implementation that
// clears the index and re-adds by filename restores the names but stages v3.
//
// Kills: staging with `git add -A`, or committing without the pathspec (code.go
// and tracked.go land in the commit); leaving out memory.WriteIndex or
// handoff.WriteIndex (the matching INDEX.md is missing from the commit);
// ignoring -m and hardcoding the default subject; and any implementation that
// buys the scoping by resetting or stashing the user's work first.
func TestSaveCommitsOnlyAgentsPaths(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)

	writeFileAt(t, root, "tracked.go", "package main // v1\n")
	gitOut(t, root, "add", "tracked.go")
	gitOut(t, root, "commit", "-m", "base")

	// Unrelated work: it must survive the save exactly as it was.
	writeFileAt(t, root, "tracked.go", "package main // v2\n")
	writeFileAt(t, root, "code.go", "package main\n")
	gitOut(t, root, "add", "tracked.go", "code.go")
	writeFileAt(t, root, "tracked.go", "package main // v3\n")

	writeMemory(t, root, "a.md", memEntryA)
	writeHandoffEntry(t, root, "lane-a", "2026-08-10-s1.md", handoffEntA)

	var out bytes.Buffer
	if code := runSave([]string{"-m", "chore: note a"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}

	// The generated indexes are regenerated as part of the same operation, so
	// they land in the same commit and the pre-commit guard has nothing to
	// complain about.
	want := []string{
		".agents/memory/INDEX.md",
		".agents/memory/a.md",
		".agents/reports/handoff/INDEX.md",
		".agents/reports/handoff/lane-a/2026-08-10-s1.md",
	}
	if got := committedFiles(t, root); !slices.Equal(got, want) {
		t.Errorf("committed files = %v, want %v", got, want)
	}
	if got := gitOut(t, root, "log", "-1", "--pretty=%s"); got != "chore: note a" {
		t.Errorf("subject = %q, want %q", got, "chore: note a")
	}
	if got := gitOut(t, root, "diff", "--cached", "--name-only"); got != "code.go\ntracked.go" {
		t.Errorf("still staged = %q, want %q", got, "code.go\ntracked.go")
	}
	// The user's own index-vs-worktree difference, and only that one.
	if got := gitOut(t, root, "diff", "--name-only"); got != "tracked.go" {
		t.Errorf("unstaged = %q, want just the user's own %q", got, "tracked.go")
	}
	if got := gitOut(t, root, "show", ":tracked.go"); got != "package main // v2" {
		t.Errorf("staged tracked.go = %q, want the user's v2", got)
	}
	b, err := os.ReadFile(filepath.Join(root, "tracked.go"))
	if err != nil || string(b) != "package main // v3\n" {
		t.Errorf("worktree tracked.go = %q (%v), want v3", b, err)
	}
}

// Kills: an implementation that ignores the flag default and hardcodes some
// other subject, and one that requires -m.
func TestSaveUsesItsDefaultMessage(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)
	writeMemory(t, root, "a.md", memEntryA)

	var out bytes.Buffer
	if code := runSave(nil, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	const want = "chore(agents): update agent context"
	if got := gitOut(t, root, "log", "-1", "--pretty=%s"); got != want {
		t.Errorf("subject = %q, want %q", got, want)
	}
}

// A save with nothing to save must not manufacture an empty commit.
//
// The fixture has to save once first. A repo where the two generated indexes
// have never been written has something to save the moment they are generated,
// so "nothing to do" is only reachable on a second run -- which makes this an
// idempotency test as well.
//
// Kills: dropping the staged-emptiness check (an empty commit appears, exit 0);
// regenerating an index non-deterministically (the second run finds a diff).
func TestSaveWithNothingToDo(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)
	writeMemory(t, root, "a.md", memEntryA)

	var first bytes.Buffer
	if code := runSave(nil, &first); code != exitcode.OK {
		t.Fatalf("first save exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, first.String())
	}
	head := gitOut(t, root, "rev-parse", "HEAD")

	var out bytes.Buffer
	if code := runSave(nil, &out); code != exitcode.Skip {
		t.Fatalf("exit = %d, want Skip (%d) when there is nothing to save; output:\n%s", code, exitcode.Skip, out.String())
	}
	if got := gitOut(t, root, "rev-parse", "HEAD"); got != head {
		t.Errorf("a save with nothing to save added a commit: %s -> %s", head, got)
	}
}

// git refuses a path-scoped commit during a merge and during a cherry-pick, and
// -- measured, git 2.50.1 -- does NOT refuse during a revert, a rebase, a `git
// am` or a bisect. In those it makes the commit: the revert case additionally
// clears REVERT_HEAD as part of committing, so the in-progress revert is simply
// lost, and the bisect case puts the commit on the bisect's detached HEAD, where
// `git bisect reset` leaves it reachable from nothing -- the record `save`
// reported writing is gone. A command that only handled git's refusal would be
// silently destructive in exactly the cases where git says nothing.
//
// The index assertions are the ones that pin the ORDERING, and they are the
// point of this test as much as the exit code is. Moving the check to just
// before the commit -- regenerating and staging first, refusing second -- passes
// every other assertion here. Mid-merge that mutant stages .agents/ into the
// conflicted merge, so the caller's next `git merge --continue` sweeps it into
// the merge commit: the precise accident `agents save` exists to prevent.
//
// Kills: no in-progress check at all (merge and cherry-pick come back as
// exitcode.Block with a bare "fatal: cannot do a partial commit"; revert, rebase,
// am and bisect come back OK with a new commit and, for revert, no REVERT_HEAD);
// checking only for MERGE_HEAD; missing BISECT_START; calling a `git am` a
// rebase and offering `git rebase --continue`, which during an am fails with
// "fatal: It looks like 'git am' is in progress. Cannot rebase."; reading the
// state out of the wrong directory, which finds nothing anywhere; and checking
// after staging instead of before.
func TestSaveRefusesWhileAGitOperationIsInProgress(t *testing.T) {
	cases := []struct {
		op     string // what the message must name
		state  string // what git leaves in .git/ while it is unfinished
		remedy string // the way out, which is not uniformly --continue/--abort
		stops  bool   // the fixture reaches its state by failing; a bisect just starts
		start  func(t *testing.T, root, orig string) (string, error)
	}{
		{"merge", "MERGE_HEAD", "`git merge --continue`", true, func(t *testing.T, root, orig string) (string, error) {
			return gitTry(t, root, "merge", "side")
		}},
		{"cherry-pick", "CHERRY_PICK_HEAD", "`git cherry-pick --continue`", true, func(t *testing.T, root, orig string) (string, error) {
			return gitTry(t, root, "cherry-pick", "side")
		}},
		{"revert", "REVERT_HEAD", "`git revert --continue`", true, func(t *testing.T, root, orig string) (string, error) {
			return gitTry(t, root, "revert", "--no-edit", "HEAD~1")
		}},
		{"rebase", "rebase-merge", "`git rebase --continue`", true, func(t *testing.T, root, orig string) (string, error) {
			gitOut(t, root, "checkout", "side")
			return gitTry(t, root, "rebase", orig)
		}},
		// The other rebase backend, which keeps its state under the other name.
		{"rebase", "rebase-apply", "`git rebase --continue`", true, func(t *testing.T, root, orig string) (string, error) {
			gitOut(t, root, "checkout", "side")
			return gitTry(t, root, "rebase", "--apply", orig)
		}},
		// `git am` shares .git/rebase-apply with the apply backend above, so the
		// marker alone cannot tell them apart -- and the rebase remedies do not
		// work here, which is why the difference has to be visible.
		{"am", "rebase-apply", "`git am --continue`", true, func(t *testing.T, root, orig string) (string, error) {
			out := t.TempDir()
			gitOut(t, root, "format-patch", "-1", "-o", out, "side")
			patches, err := filepath.Glob(filepath.Join(out, "*.patch"))
			if err != nil || len(patches) != 1 {
				t.Fatalf("fixture: want exactly one patch, got %v (%v)", patches, err)
			}
			return gitTry(t, root, "am", patches[0])
		}},
		// A bisect is not reached by conflicting: `git bisect start` succeeds,
		// and the good/bad pair is what detaches HEAD onto the midpoint.
		{"bisect", "BISECT_START", "`git bisect reset`", false, func(t *testing.T, root, orig string) (string, error) {
			gitOut(t, root, "bisect", "start")
			gitOut(t, root, "bisect", "bad", "HEAD")
			return gitTry(t, root, "bisect", "good", "HEAD~2")
		}},
	}

	for _, c := range cases {
		t.Run(c.op+"-"+c.state, func(t *testing.T) {
			root := newRepo(t)
			// symbolic-ref, not rev-parse --abbrev-ref: there is no commit yet.
			orig := gitOut(t, root, "symbolic-ref", "--short", "HEAD")
			writeFileAt(t, root, "f.txt", "base\n")
			gitOut(t, root, "add", "f.txt")
			gitOut(t, root, "commit", "-m", "base")
			// A side branch and a trunk change that cannot merge cleanly, plus a
			// second trunk commit so `revert HEAD~1` has a conflicting target and
			// `bisect good HEAD~2` has somewhere to land.
			gitOut(t, root, "checkout", "-b", "side")
			writeFileAt(t, root, "f.txt", "side\n")
			gitOut(t, root, "commit", "-am", "side")
			gitOut(t, root, "checkout", orig)
			writeFileAt(t, root, "f.txt", "one\n")
			gitOut(t, root, "commit", "-am", "one")
			writeFileAt(t, root, "f.txt", "two\n")
			gitOut(t, root, "commit", "-am", "two")

			started, err := c.start(t, root, orig)
			if c.stops && err == nil {
				t.Fatalf("fixture: %s was expected to stop on a conflict:\n%s", c.op, started)
			}
			if !c.stops && err != nil {
				t.Fatalf("fixture: %s failed to start: %v\n%s", c.op, err, started)
			}
			state := filepath.Join(root, ".git", c.state)
			if _, err := os.Stat(state); err != nil {
				t.Fatalf("fixture: %s did not leave %s behind: %v", c.op, c.state, err)
			}

			t.Chdir(root)
			writeMemory(t, root, "a.md", memEntryA)
			head := gitOut(t, root, "rev-parse", "HEAD")
			// Snapshotted after the memory entry exists, so the only thing that
			// could move these is `save` itself.
			stagedBefore := gitOut(t, root, "diff", "--cached", "--name-only")
			statusBefore := gitOut(t, root, "status", "--porcelain")

			var out bytes.Buffer
			if code := runSave([]string{"-m", "chore: note a"}, &out); code != exitcode.NoRecord {
				t.Fatalf("exit = %d, want NoRecord (%d); output:\n%s", code, exitcode.NoRecord, out.String())
			}
			if !strings.Contains(out.String(), "`git "+c.op+"`") {
				t.Errorf("the refusal must name the operation %q; got:\n%s", c.op, out.String())
			}
			if !strings.Contains(out.String(), c.remedy) {
				t.Errorf("the refusal must offer %s, which is the way out of a %s; got:\n%s", c.remedy, c.op, out.String())
			}
			if got := gitOut(t, root, "rev-parse", "HEAD"); got != head {
				t.Errorf("save committed during a %s: %s -> %s", c.op, head, got)
			}
			if _, err := os.Stat(state); err != nil {
				t.Errorf("save destroyed the in-progress %s (%s is gone): %v", c.op, c.state, err)
			}
			// The refusal has to come BEFORE anything is regenerated or staged.
			if got := gitOut(t, root, "diff", "--cached", "--name-only"); got != stagedBefore {
				t.Errorf("save staged into an in-progress %s: index held %q, now holds %q", c.op, stagedBefore, got)
			}
			if got := gitOut(t, root, "status", "--porcelain"); got != statusBefore {
				t.Errorf("save changed the worktree state during a %s:\n%q\nbecame\n%q", c.op, statusBefore, got)
			}
		})
	}
}

// A plain detached HEAD is deliberately NOT refused, and that decision needs
// pinning as much as the bisect one above does: the two look alike -- a commit
// made on either is reachable from HEAD alone -- so the obvious over-broad fix
// for the bisect hazard is to refuse whenever HEAD is detached. That would
// refuse every `git checkout <sha>` session, every CI checkout and every
// worktree pinned to a tag, none of which throw the commit away by themselves.
//
// Kills: adding a detached-HEAD test to repo.InProgress, or checking
// rc.Branch == "" in runSave, in place of (or as well as) the BISECT_START
// marker. Both come back NoRecord here instead of OK.
func TestSaveWorksOnAPlainDetachedHead(t *testing.T) {
	root := newRepo(t)
	writeFileAt(t, root, "f.txt", "base\n")
	gitOut(t, root, "add", "f.txt")
	gitOut(t, root, "commit", "-m", "base")
	gitOut(t, root, "checkout", "--detach", "HEAD")
	if _, err := gitTry(t, root, "symbolic-ref", "-q", "HEAD"); err == nil {
		t.Fatal("fixture: HEAD was expected to be detached")
	}

	t.Chdir(root)
	writeMemory(t, root, "a.md", memEntryA)
	var out bytes.Buffer
	if code := runSave([]string{"-m", "chore: note a"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	if !slices.Contains(committedFiles(t, root), ".agents/memory/a.md") {
		t.Errorf("committed files = %v, want the memory entry", committedFiles(t, root))
	}
}

// An agent working in pkg/foo is the ordinary invocation, not the exotic one,
// and every other test here saves from the root -- where running git in the
// caller's directory and running it at the worktree root are the same thing.
//
// Kills: repo.Git(filepath.Join(rc.Root, rc.RelCwd), ...) in place of
// repo.Git(rc.Root, ...). git resolves the `.agents` pathspec relative to its own
// working directory, so from a subdirectory `git add -- .agents` dies with
// "fatal: pathspec '.agents' did not match any files" and the save exits 5. All
// three git calls are covered: the add fails first, and with the add repaired
// the diff --cached and the commit would each still be scoped to the wrong place.
func TestSaveFromASubdirectory(t *testing.T) {
	root := newRepo(t)
	writeMemory(t, root, "a.md", memEntryA)
	deep := filepath.Join(root, "pkg", "deep")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(deep)

	var out bytes.Buffer
	if code := runSave([]string{"-m", "chore: note a"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	want := []string{".agents/memory/INDEX.md", ".agents/memory/a.md", ".agents/reports/handoff/INDEX.md"}
	if got := committedFiles(t, root); !slices.Equal(got, want) {
		t.Errorf("committed files = %v, want %v", got, want)
	}
}

// The two ways an ordinary caller reaches a `git commit` that fails. Neither was
// exercised before, so the exit code on that path was whatever it happened to be.
//
// Kills: exitcode.Block (2) on a failed commit. Block is documented as the only
// code that stops work and belongs to the pre-commit guard; a save that could not
// commit is a save that failed to record, which is NoRecord (5) -- the code every
// other failure in runSave already returns. Also kills discarding git's error
// (exit 0 with no commit) and swallowing git's own explanation.
func TestSaveReportsNoRecordWhenTheCommitFails(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		setup func(t *testing.T, root string)
		want  string // git's own words, which save must pass through
	}{
		{
			name:  "empty message",
			args:  []string{"-m", ""},
			setup: func(*testing.T, string) {},
			want:  "empty commit message",
		},
		{
			// Someone else's guard says no. newRepo inits with an empty
			// --template, which leaves no .git/hooks directory at all, so it has
			// to be created before the hook can be written into it.
			name: "pre-commit hook refuses",
			args: []string{"-m", "chore: note a"},
			setup: func(t *testing.T, root string) {
				hooks := filepath.Join(root, ".git", "hooks")
				if err := os.MkdirAll(hooks, 0o755); err != nil {
					t.Fatal(err)
				}
				hook := filepath.Join(hooks, "pre-commit")
				if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'refused by the guard' >&2\nexit 1\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			want: "refused by the guard",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := newRepo(t)
			writeFileAt(t, root, "f.txt", "base\n")
			gitOut(t, root, "add", "f.txt")
			gitOut(t, root, "commit", "-m", "base")
			// After the base commit: a hook installed earlier would refuse that
			// one too, and the fixture would fail instead of the code.
			c.setup(t, root)

			t.Chdir(root)
			writeMemory(t, root, "a.md", memEntryA)
			head := gitOut(t, root, "rev-parse", "HEAD")

			var out bytes.Buffer
			if code := runSave(c.args, &out); code != exitcode.NoRecord {
				t.Fatalf("exit = %d, want NoRecord (%d); output:\n%s", code, exitcode.NoRecord, out.String())
			}
			if !strings.Contains(out.String(), c.want) {
				t.Errorf("save must pass git's own refusal through; want %q in:\n%s", c.want, out.String())
			}
			if got := gitOut(t, root, "rev-parse", "HEAD"); got != head {
				t.Errorf("the commit was reported as failed but HEAD moved: %s -> %s", head, got)
			}
		})
	}
}

// `.agents` as a symlink to a directory outside the worktree. repoHere's
// os.Stat follows it, so without a check of its own this command regenerates
// both indexes outside the repository and commits `.agents` as a `120000` blob
// holding an absolute path on this machine -- at exit 0, with the memory entry
// nowhere in the commit.
//
// Kills: leaving repoHere's symlink-following os.Stat as save's only test of
// .agents/. Measured against that: exit 0, `git ls-tree -r HEAD` holds
// "120000 blob ... .agents", `git cat-file -p HEAD:.agents` prints the absolute
// path of the outside directory, and the generated INDEX.md files are sitting in
// that outside directory instead.
func TestSaveRefusesASymlinkedAgentsDir(t *testing.T) {
	root := newRepo(t)
	outside := filepath.Join(t.TempDir(), "context")
	if err := os.MkdirAll(filepath.Join(outside, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileAt(t, outside, "memory/a.md", memEntryA)
	if err := os.RemoveAll(filepath.Join(root, ".agents")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".agents")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	if code := runSave([]string{"-m", "chore: note a"}, &out); code != exitcode.NoRecord {
		t.Fatalf("exit = %d, want NoRecord (%d); output:\n%s", code, exitcode.NoRecord, out.String())
	}
	if !strings.Contains(out.String(), "symlink") {
		t.Errorf("the refusal must say what is wrong with .agents; got:\n%s", out.String())
	}
	if got := gitOut(t, root, "rev-list", "--count", "--all"); got != "0" {
		t.Errorf("save committed through the symlink: %s commit(s)", got)
	}
	// And it refused before regenerating, so nothing was written outside the repo.
	for _, rel := range []string{"memory/INDEX.md", "reports/handoff/INDEX.md"} {
		if _, err := os.Stat(filepath.Join(outside, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("save wrote %s outside the repository (%v)", rel, err)
		}
	}
}

// The other half of that decision: the refusal is save's own, not the shared
// rule's. repoHere still follows the link, because for a command that only reads
// and writes inside .agents/ a context directory kept elsewhere and linked in is
// a working setup -- `agents init --local` plus a symlink shared between
// worktrees. Only the command that COMMITS the directory is harmed by it.
//
// Kills: moving the symlink check into repoHere, which would take `agents
// index`, `agents trace` and `agents handoff` out with it for no gain.
func TestIndexStillFollowsASymlinkedAgentsDir(t *testing.T) {
	root := newRepo(t)
	outside := filepath.Join(t.TempDir(), "context")
	if err := os.MkdirAll(filepath.Join(outside, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileAt(t, outside, "memory/a.md", memEntryA)
	if err := os.RemoveAll(filepath.Join(root, ".agents")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".agents")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	if code := runIndex(nil, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	if _, err := os.Stat(filepath.Join(outside, "memory", "INDEX.md")); err != nil {
		t.Errorf("index did not regenerate through the link: %v", err)
	}
}

// git exports GIT_DIR into every hook it runs, and `agents save` is a command a
// hook or a harness can reach. An inherited GIT_DIR makes git operate on
// whatever repository fired the hook while the working tree stays the caller's
// -- so an unsanitized `git commit` writes the caller's .agents/ into someone
// else's history.
//
// Kills: a local git helper that does not strip the GIT_* variables the way
// internal/repo does. Without the sanitizing the commit lands in the decoy and
// the intended repo stays empty.
func TestSaveIgnoresAnInheritedGitDir(t *testing.T) {
	root := newRepo(t)
	decoy := newRepo(t)
	writeMemory(t, root, "a.md", memEntryA)
	t.Chdir(root)
	t.Setenv("GIT_DIR", filepath.Join(decoy, ".git"))

	var out bytes.Buffer
	if code := runSave([]string{"-m", "chore: note a"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	if got := gitOut(t, decoy, "rev-list", "--count", "--all"); got != "0" {
		t.Errorf("the commit landed in the repository GIT_DIR pointed at (%s commits there)", got)
	}
	if got := gitOut(t, root, "rev-list", "--count", "--all"); got != "1" {
		t.Fatalf("intended repo has %s commits, want 1", got)
	}
	if got := committedFiles(t, root); !slices.Contains(got, ".agents/memory/a.md") {
		t.Errorf("committed files = %v, want the memory entry", got)
	}
}

// The other half of the staging question: .agents/ changes that the user had
// ALREADY staged, alongside unrelated staged changes. `git commit -- <pathspec>`
// commits the working tree version of those paths and ignores what is staged for
// them, so the entry that lands is v3 and not the v2 the user staged -- pinned
// here because it is the one place `save` overrides an explicit choice of the
// caller's, and because a future implementation that stages differently would
// change it silently.
//
// Kills: `git reset` before staging, and anything else that empties the index to
// get the scoping (code.go stops being staged).
func TestSaveWithAgentsChangesAlreadyStaged(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)
	writeMemory(t, root, "a.md", strings.Replace(memEntryA, "description: A", "description: v1-committed", 1))
	gitOut(t, root, "add", "--", ".agents")
	gitOut(t, root, "commit", "-m", "base")

	writeMemory(t, root, "a.md", strings.Replace(memEntryA, "description: A", "description: v2-staged", 1))
	gitOut(t, root, "add", "--", ".agents")
	writeMemory(t, root, "a.md", strings.Replace(memEntryA, "description: A", "description: v3-worktree", 1))

	writeFileAt(t, root, "code.go", "package main\n")
	gitOut(t, root, "add", "code.go")

	var out bytes.Buffer
	if code := runSave([]string{"-m", "chore: note a"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	if got := gitOut(t, root, "show", "HEAD:.agents/memory/a.md"); !strings.Contains(got, "v3-worktree") {
		t.Errorf("committed entry is not the worktree version:\n%s", got)
	}
	if slices.Contains(committedFiles(t, root), "code.go") {
		t.Error("save swept the unrelated staged file into the commit")
	}
	if got := gitOut(t, root, "diff", "--cached", "--name-only"); got != "code.go" {
		t.Errorf("still staged = %q, want %q", got, "code.go")
	}
}

// The markers that say an operation is unfinished are per-worktree. A merge
// stopped in one worktree is not a reason to refuse a save in another, and it is
// the ordinary case for this tool: a linked worktree per lane is how several
// agents work the same repo at once.
//
// Kills: repo.InProgress resolving its directory with --git-common-dir, which a
// linked worktree shares with the main checkout -- one stalled merge anywhere
// would then wedge `agents save` everywhere.
func TestSaveIsNotBlockedByAnotherWorktreesMerge(t *testing.T) {
	main := newRepo(t)
	writeFileAt(t, main, "f.txt", "base\n")
	gitOut(t, main, "add", "f.txt")
	gitOut(t, main, "commit", "-m", "base")

	linked := filepath.Join(t.TempDir(), "wt")
	gitOut(t, main, "worktree", "add", "-b", "wt", linked, "HEAD")

	// A merge stopped on a conflict, in the MAIN worktree.
	orig := gitOut(t, main, "rev-parse", "--abbrev-ref", "HEAD")
	gitOut(t, main, "checkout", "-b", "side")
	writeFileAt(t, main, "f.txt", "side\n")
	gitOut(t, main, "commit", "-am", "side")
	gitOut(t, main, "checkout", orig)
	writeFileAt(t, main, "f.txt", "main\n")
	gitOut(t, main, "commit", "-am", "main")
	if out, err := gitTry(t, main, "merge", "side"); err == nil {
		t.Fatalf("fixture: the merge was expected to conflict:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(main, ".git", "MERGE_HEAD")); err != nil {
		t.Fatalf("fixture: no MERGE_HEAD in the main worktree: %v", err)
	}

	writeMemory(t, linked, "a.md", memEntryA)
	t.Chdir(linked)
	var out bytes.Buffer
	if code := runSave([]string{"-m", "chore: note a"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	if got := gitOut(t, linked, "log", "-1", "--pretty=%s"); got != "chore: note a" {
		t.Errorf("subject = %q, want %q", got, "chore: note a")
	}
}

// Kills: ignoring flag.Parse's error, which would treat an unknown flag as a
// save with the default message.
func TestSaveRefusesUnparseableFlags(t *testing.T) {
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	if code := runSave([]string{"--no-such-flag"}, &out); code != exitcode.Malformed {
		t.Fatalf("exit = %d, want Malformed (%d); output:\n%s", code, exitcode.Malformed, out.String())
	}
}

// A subcommand that is not registered in main.go is unreachable however well it
// works. Skip is only reachable through runSave; an unregistered command returns
// Malformed.
func TestMainRegistersSave(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	out := captureStdout(t, func() { code = run([]string{"save"}) })
	if code != exitcode.Skip {
		t.Fatalf("run(save) = %d, want Skip (%d); stdout:\n%s", code, exitcode.Skip, out)
	}
}
