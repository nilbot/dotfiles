package phase

import (
	"fmt"
	"path/filepath"
)

// homebrewInstaller is Homebrew's documented one-liner, verbatim, as the
// argument to a shell that Machine cannot otherwise provide.
//
// The nesting is not decoration. The documented command is
// `/bin/bash -c "$(curl -fsSL ...)"`, in which the INVOKING shell runs curl and
// substitutes the whole script as bash's -c argument -- so the download
// completes before a byte of it executes. Run takes a command and arguments and
// performs no substitution, so the outer `/bin/bash -c` is what supplies the
// shell that does. Reaching instead for `curl ... | bash`, the obvious
// simplification, would execute a partial script if the transfer were cut short
// halfway; Homebrew documents the substitution form rather than the pipe for
// exactly that reason, and the point of running the official installer is to run
// what it documents.
const homebrewInstaller = `/bin/bash -c ` +
	`"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`

// Packages provisions this machine's software in three steps: the native package
// manager installs what Homebrew itself requires and nothing else, Homebrew
// installs itself if it is absent, and one Brewfile covers everything above that
// on both platforms.
//
// The alternative -- per-distro native package lists, which the deleted
// super-install-dep.sh maintained -- means two or three manifests kept in step
// across distributions whose package names disagree (fd vs fd-find, bat vs
// batcat), and several tools in use here are not consistently packaged natively
// at all.
func Packages(c Context) error {
	c.logf("== packages")

	if err := stageZero(c); err != nil {
		return err
	}
	if err := homebrew(c); err != nil {
		return err
	}

	// Absolute, because this package has no cwd concept -- Machine has no cd and
	// no shell -- so a relative --file would resolve against wherever the user
	// happened to invoke ./bootstrap from.
	brewfile := filepath.Join(c.Root, "bootstrap.d", "Brewfile")
	c.logf("   brewfile    %s", brewfile)
	return c.Change.Run("brew", "bundle", "--file", brewfile)
}

// stageZero installs Homebrew's own prerequisites -- a C toolchain, curl, file
// and git -- and deliberately nothing else.
//
// It does not run on darwin at all. Those prerequisites arrive with the Xcode
// command line tools, which this phase does not manage: the deleted
// super-install-dep.sh called `xcode-select --install`, an interactive GUI
// installer that returns immediately whether or not the user completes it, so
// what it actually contributed was the appearance of a step.
func stageZero(c Context) error {
	if c.Platform == "darwin" {
		c.logf("   stage zero  not applicable on darwin (the Xcode command line " +
			"tools carry Homebrew's prerequisites)")
		return nil
	}

	if _, err := c.Change.LookPath("apt-get"); err == nil {
		c.logf("   stage zero  apt-get: build-essential curl file git")
		// The index first. An install against an index months out of date does
		// not install an old version, it 404s on the URL the old index names.
		if err := c.Change.Sudo("apt-get", "update"); err != nil {
			return err
		}
		return c.Change.Sudo("apt-get", "install", "-y",
			"build-essential", "curl", "file", "git")
	}
	if _, err := c.Change.LookPath("pacman"); err == nil {
		c.logf("   stage zero  pacman: base-devel curl file git")
		// --needed rather than a plain -S, so a re-apply does not reinstall four
		// packages that are already there.
		return c.Change.Sudo("pacman", "-S", "--needed", "--noconfirm",
			"base-devel", "curl", "file", "git")
	}

	// A plain error rather than a change.Refusal, matching preflight's handling
	// of a missing stage-zero tool: a Refusal says a PATH on this machine is in a
	// state bootstrap will not write over, and nothing here is in any state at
	// all. Both answer exit 2.
	//
	// Both names are in the message because "no package manager found" tells the
	// reader nothing about which distributions are supported, and this is the
	// moment they need to know.
	return fmt.Errorf("no supported native package manager on PATH: neither "+
		"apt-get (Debian/Ubuntu) nor pacman (Arch/Manjaro) is present, so "+
		"Homebrew's prerequisites cannot be installed on %s; install a C "+
		"toolchain, curl, file and git by hand, then retry", c.Platform)
}

// homebrew installs Homebrew when it is absent, and this is the ONLY place this
// design executes remote code.
//
// Three things keep that narrow, and a future reader tempted to "harden" it
// should weigh them before removing anything. It runs solely when LookPath
// fails, so a provisioned machine never fetches it again. It goes through
// Machine.Run like every other command, so `plan` reaches Planner.Run, which
// records the invocation and executes nothing -- the dry-run invariant covers
// this step exactly as it covers the rest. And the alternative to trusting
// Homebrew's installer is vendoring a copy of it here, which trades a fetch from
// the project that maintains it for a stale fork nobody updates.
func homebrew(c Context) error {
	if path, err := c.Change.LookPath("brew"); err == nil {
		c.logf("   homebrew    already installed (%s)", path)
		return nil
	}
	c.logf("   homebrew    not on PATH; running the official installer")
	return c.Change.Run("/bin/bash", "-c", homebrewInstaller)
}
