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

	for _, tool := range stageZeroTools(c.Profile) {
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

// stageZeroTools is what the phases THIS profile runs will actually invoke, and
// it depends on the profile for the same reason check.managesMachine does: a
// refusal has to be about the run that was asked for.
//
// git is required by both. Nothing in this module execs it, but the checkout is
// a git repository and the tooling around it -- git/install-hooks.sh, the agents
// binary -- has no meaning without it.
//
// The other three are workstation's, because they belong to the three phases the
// dotfiles profile excludes:
//
//   - bash runs git/install-hooks.sh twice in devtools (devtools.go), and that
//     script counts git's config output with awk (git/install-hooks.sh).
//   - curl fetches Homebrew's installer in packages and, through
//     install_fisher in fish/mypre.fish, fisher itself in the fish phase. That
//     same file also reaches for awk on its WSL branch.
//
// The dotfiles profile runs preflight, config and verify, which execute nothing
// at all -- so demanding bash, curl or awk there would refuse the container the
// profile exists to serve over tools it would never invoke. That is not
// hypothetical: awk was on the old list, and a container without it was turned
// away for a phase it does not run.
//
// sed is on neither list. It was the Makefile's substitution, which died with
// the Makefile; nothing the provisioner runs uses it.
//
// Nothing narrower than "workstation" gets the short list, so a profile added
// later is refused for a tool it might not need rather than failing inside a
// phase for one it does.
func stageZeroTools(profile string) []string {
	if profile == "dotfiles" {
		return []string{"git"}
	}
	return []string{"git", "awk", "bash", "curl"}
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
	// A migrate.Query, not a migrate.Context: deciding whether a migration
	// applies is a read, and a phase holds a Machine that cannot destroy
	// anything. Preflight therefore cannot perform a migration even by mistake
	// -- it can only ask whether one is due.
	due, err := migrate.Pending(migrate.Query{
		Read: c.Change, Root: c.Root, Home: c.Home,
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
