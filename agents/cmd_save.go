package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/handoff"
	"github.com/nilbot/dotfiles/agents/internal/memory"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

// runSave commits .agents/ and nothing else.
//
// The scoping is the point: `git add -A` in a repo that is accumulating trace
// records sweeps them into whatever commit happens to be next, and the record
// then arrives in someone's code review.
func runSave(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("save", flag.ContinueOnError)
	fs.SetOutput(stdout)
	msg := fs.String("m", "chore(agents): update agent context", "commit message")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}

	// One rule for "am I in a repo that opted into this tool?", shared with
	// every other command that reads or writes inside .agents/. The repo context
	// comes back with it because the git calls below need the worktree root, and
	// working it out here again would be the second answer.
	rc, agentsDir, code := repoHere(stdout)
	if code != exitcode.OK {
		return code
	}

	// repoHere's os.Stat follows symlinks, and for every other caller that is the
	// right answer: a command that only reads and writes inside .agents/ works
	// perfectly well when the directory lives elsewhere and is linked in. This is
	// the one command that COMMITS it, and there the link is not a detail.
	// Measured: `git add -- .agents` stages the link itself, so the commit is a
	// single `120000 blob` holding an absolute path on this machine, none of the
	// entries are in it, and the two indexes regenerated below land outside the
	// repository -- at exit 0.
	fi, err := os.Lstat(agentsDir)
	if err != nil {
		fmt.Fprintf(stdout, "agents save: %v\n", err)
		return exitcode.NoRecord
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintf(stdout, "agents save: %s is a symlink. Committing it would record the link — an absolute"+
			" path on this machine — instead of the files under it, and the regenerated indexes would be"+
			" written outside the repository. Replace it with a real directory to save from here.\n", agentsDir)
		return exitcode.NoRecord
	}

	// Before anything is written or staged. A path-scoped commit is unsafe in the
	// middle of every operation repo.InProgress names, and git only refuses two
	// of them. Bailing out after staging would be worse than doing nothing:
	// mid-merge it leaves .agents/ staged into the conflicted merge, and the
	// caller's next `git merge --continue` sweeps it into the merge commit --
	// exactly the accident this command exists to prevent.
	op, err := repo.InProgress(rc.Root)
	if err != nil {
		fmt.Fprintf(stdout, "agents save: %v\n", err)
		return exitcode.NoRecord
	}
	if op != "" {
		fmt.Fprintf(stdout, "agents save: a `git %s` is in progress, and a commit scoped to .agents/ is not safe"+
			" during one — git either refuses it, or makes it and then loses either the operation or the commit."+
			" %s, then run `agents save` again.\n", op, inProgressRemedy(op))
		return exitcode.NoRecord
	}

	// Regenerate before staging so the generated files land in the same commit
	// as what they describe. Otherwise the pre-commit guard blocks on a
	// mismatch that this command itself created.
	if err := memory.WriteIndex(filepath.Join(agentsDir, "memory")); err != nil {
		fmt.Fprintf(stdout, "agents save: %v\n", err)
		return exitcode.NoRecord
	}
	// handoff.WriteIndex takes the .agents/ directory and finds reports/handoff
	// under it itself, the same way handoff.Write and handoff.Prune do.
	if err := handoff.WriteIndex(agentsDir); err != nil {
		fmt.Fprintf(stdout, "agents save: %v\n", err)
		return exitcode.NoRecord
	}

	// repo.Git, not exec.Command: it strips the GIT_* variables git exports into
	// every hook, which would otherwise aim `git commit` at the repository the
	// hook fired from while the working tree stayed this one.
	if out, err := repo.Git(rc.Root, "add", "--", ".agents"); err != nil {
		fmt.Fprintf(stdout, "agents save: git add: %v\n%s", err, out)
		return exitcode.NoRecord
	}

	staged, err := repo.Git(rc.Root, "diff", "--cached", "--name-only", "--", ".agents")
	if err != nil {
		fmt.Fprintf(stdout, "agents save: %v\n%s", err, staged)
		return exitcode.NoRecord
	}
	if strings.TrimSpace(staged) == "" {
		fmt.Fprintln(stdout, "agents save: nothing to save")
		return exitcode.Skip
	}

	// Pathspec-scoped commit: anything else the user had staged stays staged.
	out, err := repo.Git(rc.Root, "commit", "-m", *msg, "--", ".agents")
	fmt.Fprint(stdout, out)
	if err != nil {
		// NoRecord, not Block. Block is documented as the only code that stops
		// work, and it belongs to the pre-commit guard; `save` is not a guard,
		// and a commit git refused is precisely "wanted to record and could not"
		// -- the same thing every other failure in this function reports. The
		// path is ordinary rather than exotic: an empty `-m` reaches it, and so
		// does any pre-commit hook that says no.
		return exitcode.NoRecord
	}
	return exitcode.OK
}

// inProgressRemedy is the way out of an in-progress git operation, which is not
// uniformly `git <op> --continue` / `git <op> --abort`.
//
// A bisect has no --continue at all: `git bisect reset` is the only way back,
// and it is also the step that discards anything committed on the bisect's
// detached HEAD. `git am` does have both, and offering it the rebase spellings
// -- which sharing .git/rebase-apply with the rebase apply backend makes easy to
// do by accident -- offers two commands that both fail with "fatal: It looks
// like 'git am' is in progress. Cannot rebase."
func inProgressRemedy(op string) string {
	if op == "bisect" {
		return "End it with `git bisect reset`"
	}
	return fmt.Sprintf("Finish it with `git %s --continue` or abandon it with `git %s --abort`", op, op)
}
