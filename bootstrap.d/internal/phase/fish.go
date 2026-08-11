package phase

import (
	"fmt"
	"path/filepath"
	"strings"
)

// appendShell runs under sh rather than this process because the redirection
// has to happen with root's privileges: `sudo tee -a` would need a stdin that
// change.Applier does not give a command. The path arrives as $1 and is never
// interpolated into the script text, so a prefix containing a quote, a space or
// $(...) cannot change what root runs.
const appendShell = `printf '%s\n' "$1" >> /etc/shells`

// Fish makes fish the login shell and installs the plugin set.
//
// Plugins are installed explicitly here rather than by a shell-start side
// effect: a provisioning step that only happens if you happen to open a shell
// is not a provisioning step.
//
// Like the packages phase, this one refuses under `plan` on a machine that does
// not have fish yet. That is the same accepted limitation, for the same reason:
// `plan` cannot preview a machine whose packages phase has not run, because
// Planner records a command without performing it and nothing in Machine
// reports which happened.
func Fish(c Context) error {
	c.logf("== fish")

	fishPath, err := c.Change.LookPath("fish")
	if err != nil {
		return fmt.Errorf("fish is not on PATH; the packages phase installs it -- " +
			"run './bootstrap apply workstation'")
	}
	c.logf("   fish        %s", fishPath)

	if err := registerShell(c, fishPath); err != nil {
		return err
	}
	if err := loginShell(c, fishPath); err != nil {
		return err
	}

	// fish expands $argv itself, so the root never enters a command string that
	// something else parses first.
	return c.Change.Run("fish", "--no-config", "-c",
		"source $argv[1]/fish/mypre.fish; install_fisher", c.Root)
}

// registerShell adds fish to /etc/shells. chsh refuses a shell that is not
// listed there, so this runs first.
func registerShell(c Context, fishPath string) error {
	body, err := c.Change.ReadFile("/etc/shells")
	if err != nil {
		// Unreadable is not absent. A duplicate line in /etc/shells is inert; a
		// missing one makes chsh refuse, so the append is the safe answer to not
		// knowing.
		c.logf("   /etc/shells unreadable (%v); adding the entry regardless", err)
	} else {
		for _, line := range strings.Split(string(body), "\n") {
			if strings.TrimSpace(line) == fishPath {
				c.logf("   /etc/shells already lists %s", fishPath)
				return nil
			}
		}
	}
	c.logf("   /etc/shells adding %s", fishPath)
	return c.Change.Sudo("/bin/sh", "-c", appendShell, "sh", fishPath)
}

func loginShell(c Context, fishPath string) error {
	if filepath.Base(c.Shell) == "fish" {
		c.logf("   chsh        login shell is already fish (%s)", c.Shell)
		return nil
	}
	if c.User == "" {
		return fmt.Errorf("cannot change the login shell: neither the passwd " +
			"database nor $USER names the current user, and `sudo chsh` without " +
			"a name would change root's shell")
	}
	c.logf("   chsh        %s -> %s for %s", c.Shell, fishPath, c.User)
	return c.Change.Sudo("chsh", "-s", fishPath, c.User)
}
