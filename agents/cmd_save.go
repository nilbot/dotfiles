package main

import (
	"flag"
	"fmt"
	"io"
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

	// Before anything is written or staged. A path-scoped commit is unsafe in
	// the middle of a merge, a cherry-pick, a revert or a rebase, and git only
	// refuses the first two -- see repo.InProgress. Bailing out after staging
	// would leave the caller's index rearranged by a command that then did
	// nothing.
	op, err := repo.InProgress(rc.Root)
	if err != nil {
		fmt.Fprintf(stdout, "agents save: %v\n", err)
		return exitcode.NoRecord
	}
	if op != "" {
		fmt.Fprintf(stdout, "agents save: a %s is in progress, and a commit scoped to .agents/ is not safe during one"+
			" — git either refuses it or makes it and loses the %s. Finish it with `git %s --continue`"+
			" or abandon it with `git %s --abort`, then run `agents save` again.\n", op, op, op, op)
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
		return exitcode.Block
	}
	return exitcode.OK
}
