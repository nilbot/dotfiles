package phase

import "github.com/nilbot/dotfiles/bootstrap/internal/check"

// Verify runs the same checks the check verb runs, reports them, and returns
// nil even when one fails.
//
// That is the whole point: an advisory finding at the end of an apply must not
// look like a failed apply. `apply` converged what it was asked to converge;
// whether the machine is healthy afterwards is a separate question with its own
// verb and its own exit code. Only runCheck exits on the answer.
func Verify(c Context) error {
	c.logf("== verify")
	results := check.All(check.Context{
		Change: c.Change, Root: c.Root, Home: c.Home,
		Platform: c.Platform, Profile: c.Profile, Shell: c.Shell,
	})
	check.Write(c.Out, results)
	return nil
}
