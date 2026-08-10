package phase

import (
	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/check"
)

// Verify runs the same checks the check verb runs, reports them, and returns
// nil even when one fails.
//
// That is the whole point: an advisory finding at the end of an apply must not
// look like a failed apply. `apply` converged what it was asked to converge;
// whether the machine is healthy afterwards is a separate question with its own
// verb and its own exit code. Only runCheck exits on the answer.
func Verify(c Context) error {
	c.logf("== verify")
	results, _ := check.All(check.Context{
		// A fresh Applier, deliberately not c.Change. Under `plan` that is a
		// Planner, whose Run records the command and returns nil without
		// running it -- so `brew bundle check` would never execute and the
		// packages check would read that nil as "everything is installed" on a
		// machine where nothing is.
		//
		// This does not weaken the dry-run invariant. Every check reads, and
		// `brew bundle check` is a query; there is no mutation here for a
		// Planner to hold back. See internal/check's package comment.
		Change: change.NewApplier(c.Out),

		Root: c.Root, Home: c.Home,
		Platform: c.Platform, Profile: c.Profile, Shell: c.Shell,
	})
	// The error says the manifest does not parse, which the check verb answers
	// with 3. Verify reports and never exits, and both manifest checks already
	// carry the same message in the results being written, so there is nothing
	// here for it to add.
	check.Write(c.Out, results)
	return nil
}
