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

	fishPath, err := resolveFish(c)
	if err != nil {
		return err
	}

	// Validated BEFORE the first step, because the first step is a privileged
	// append to /etc/shells. A refusal is supposed to mean nothing was performed;
	// checking this inside loginShell, where it used to live, left a line in
	// /etc/shells on a run that could not proceed -- a partial application under
	// sudo, and the ordering that produced it was arbitrary.
	//
	// It asks the same question loginShell does, through the same predicate: a
	// machine already running fish needs no name, because no chsh is issued.
	if !isFish(c.Shell) && c.User == "" {
		return fmt.Errorf("cannot change the login shell: neither the passwd " +
			"database nor $USER names the current user, and `sudo chsh` without " +
			"a name would change root's shell")
	}

	if err := registerShell(c, fishPath); err != nil {
		return err
	}
	if err := loginShell(c, fishPath); err != nil {
		return err
	}

	// fishPath, not the bare name -- for the same reason resolveFish exists, one
	// step further on. Measured locally 2026-08-17: resolveFish found the
	// prefixed fish, /etc/shells and chsh both used it, and THIS line then died
	// with `exec: "fish": executable file not found in $PATH`, because a name is
	// resolved against the PATH this process inherited and the Homebrew prefix is
	// not on it. Finding the binary and then invoking it by name is the same
	// defect twice.
	//
	// fish expands $argv itself, so the root never enters a command string that
	// something else parses first.
	return c.Change.Run(fishPath, "--no-config", "-c",
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

// isFish is the one place this package decides whether a login shell is already
// fish. Fish and loginShell both need the answer -- one to know whether a name is
// required before anything is written, the other to know whether to write -- and
// two spellings of it could disagree.
func isFish(shell string) bool { return filepath.Base(shell) == "fish" }

// loginShell trusts Fish to have established that c.User is non-empty whenever a
// chsh is due. The check lives there rather than here so that a run which cannot
// proceed has not already appended to /etc/shells.
func loginShell(c Context, fishPath string) error {
	if isFish(c.Shell) {
		c.logf("   chsh        login shell is already fish (%s)", c.Shell)
		return nil
	}
	c.logf("   chsh        %s -> %s for %s", c.Shell, fishPath, c.User)
	return c.Change.Sudo("chsh", "-s", fishPath, c.User)
}

// resolveFish finds the fish the packages phase just installed, which is not
// necessarily one this process can see.
//
// resolveBrew documents the mechanism for `brew` itself: Homebrew's installer
// appends a shellenv line to a shell PROFILE, a profile is read by the next
// login shell, and nothing can alter the PATH of a process already running. The
// same is true of everything `brew bundle` then installs INTO that prefix. On
// the fresh machine this phase exists for, fish is installed and unfindable by
// name in the same run.
//
// Measured in CI 2026-08-17 on archlinux:base and debian:stable-slim under
// `apply workstation`: stage zero succeeded, Homebrew installed to
// /home/linuxbrew/.linuxbrew, fish was installed there, and this phase failed
// telling the operator to run the command that was already running. macOS never
// showed it because /opt/homebrew/bin is already on PATH there long before
// bootstrap runs.
//
// LookPath is still tried first, and still wins when it answers: a machine that
// already has fish somewhere deliberate should use that one, not a Homebrew
// copy.
func resolveFish(c Context) (string, error) {
	if path, err := c.Change.LookPath("fish"); err == nil {
		return path, nil
	}
	for _, brew := range brewLocations {
		candidate := filepath.Join(filepath.Dir(brew), "fish")
		info, err := c.Change.Lstat(candidate)
		// Continue rather than return, for the reason resolveBrew gives: an
		// unreadable candidate is not an answer about the others.
		if err != nil {
			c.logf("   fish        %s could not be read (%v); trying the next prefix",
				candidate, err)
			continue
		}
		if info.Exists {
			c.logf("   fish        %s (not yet on PATH -- Homebrew's shellenv "+
				"line is read by the next login shell)", candidate)
			return candidate, nil
		}
	}
	// Unchanged in substance from the original message, minus the instruction to
	// run the command that is running. Under `plan` this is still the accepted
	// limitation the doc comment above describes; under `apply` it now means
	// fish is genuinely absent from PATH and from every Homebrew prefix.
	return "", fmt.Errorf("fish is not on PATH, and is absent from every " +
		"Homebrew prefix; the packages phase installs it, so under `plan` on a " +
		"machine without fish this is expected -- under `apply` it means the " +
		"packages phase did not install it")
}
