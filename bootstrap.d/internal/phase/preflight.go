package phase

import (
	"fmt"
	"strings"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/migrate"
)

func Preflight(c Context) error {
	// The two load-bearing inputs, checked in the phase whose entire job is
	// checking. HOME unset with XDG_CACHE_HOME set is a normal container shape
	// -- and containers are exactly why the dotfiles profile exists -- which
	// would otherwise resolve every managed path against "/".
	if c.Root == "" {
		return fmt.Errorf("repository root is empty; the shim exports BOOTSTRAP_ROOT")
	}
	if c.Home == "" {
		return fmt.Errorf("HOME is empty; every managed path is resolved against it")
	}

	c.logf("== preflight")
	c.logf("   platform    %s", c.Platform)
	c.logf("   repository  %s", c.Root)
	c.logf("   home        %s", c.Home)
	c.logf("   profile     %s", c.Profile)

	for _, tool := range []string{"git", "awk", "sed"} {
		if _, err := c.Change.LookPath(tool); err != nil {
			return fmt.Errorf("required stage-zero tool %q is not on PATH", tool)
		}
	}

	if c.Profile == "workstation" {
		c.logf("   needs sudo    (login shell change, /etc/shells)")
		c.logf("   needs network (Homebrew, packages, fisher plugins)")
	} else {
		c.logf("   needs neither sudo nor network")
	}

	return pendingMigrations(c)
}

// pendingMigrations refuses a machine whose shape a later phase would refuse
// anyway, here where the shape is assessed and where a remedy can be named.
//
// A machine with a pending migration is one apply cannot converge: config would
// hit ~/.config/fish or ~/.gitignore and refuse with a message about that one
// path, which says nothing about why the machine is in that state. Reading it
// gets you no closer to './bootstrap migrate'.
//
// RECONCILING only, and that is load-bearing rather than an oversight. A
// reclaiming migration is pending for as long as the thing it would reclaim
// exists, and a bare migrate never runs one -- so refusing apply on a reclaiming
// migration would be a deadlock whose named remedy does not clear it.
func pendingMigrations(c Context) error {
	due, err := migrate.Pending(migrate.Context{
		Change: c.Change, Root: c.Root, Home: c.Home, Out: c.Out,
	})
	if err != nil {
		return err
	}
	names := migrate.Names(due, migrate.Reconciling)
	if len(names) == 0 {
		return nil
	}
	return &change.Refusal{
		Path: c.Home,
		Problem: fmt.Sprintf(
			"this machine predates part of the current layout; %d migration(s) pending: %s",
			len(names), strings.Join(names, ", ")),
		Remediation: "run './bootstrap migrate', then retry",
	}
}
