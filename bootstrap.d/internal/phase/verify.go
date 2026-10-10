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
		// Planner, and a Planner answers Lstat as though every link it recorded
		// already existed -- so a check handed one would report the state the
		// plan intends rather than the state the machine is in, and the verify
		// section would call a machine converged before anything ran.
		//
		// This does not weaken the dry-run invariant. Every check reads, and
		// none can do anything else: check.Machine has no Run, so a check that
		// executed a command would not compile. See internal/check's package
		// comment.
		Change: change.NewApplier(c.Out, c.Root),

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
