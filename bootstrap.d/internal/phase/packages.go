package phase

import (
	"fmt"
	"path/filepath"
	"strings"
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
	brew, err := homebrew(c)
	if err != nil {
		return err
	}

	// Absolute, because this package has no cwd concept -- Machine has no cd and
	// no shell -- so a relative --file would resolve against wherever the user
	// happened to invoke ./bootstrap from.
	brewfile := filepath.Join(c.Root, "bootstrap.d", "Brewfile")
	c.logf("   brewfile    %s", brewfile)
	// Through the RESOLVED path rather than the bare name. See homebrew: on the
	// fresh machine this phase exists for, `brew` is not on this process's PATH
	// even after a successful install.
	return c.Change.Run(brew, "bundle", "--file", brewfile)
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

// brewLocations are the three prefixes Homebrew installs to, in probe order:
// Apple Silicon macOS, Intel macOS, Linux. They are consulted only after the
// installer has run -- see resolveBrew.
var brewLocations = []string{
	"/opt/homebrew/bin/brew",
	"/usr/local/bin/brew",
	"/home/linuxbrew/.linuxbrew/bin/brew",
}

// homebrew installs Homebrew when it is absent and returns the path to invoke it
// by. This is the ONLY place this design executes remote code.
//
// Three things keep that narrow, and a future reader tempted to "harden" it
// should weigh them before removing anything. It runs solely when LookPath
// fails, so a provisioned machine never fetches it again. It goes through
// Machine.Run like every other command, so `plan` reaches Planner.Run, which
// records the invocation and executes nothing -- the dry-run invariant covers
// this step exactly as it covers the rest. And the alternative to trusting
// Homebrew's installer is vendoring a copy of it here, which trades a fetch from
// the project that maintains it for a stale fork nobody updates.
func homebrew(c Context) (string, error) {
	// The already-installed path resolves through LookPath and stops there: no
	// probing, because PATH is the machine's own answer to "which brew" and a
	// guess from a fixed list could disagree with it.
	if path, err := c.Change.LookPath("brew"); err == nil {
		c.logf("   homebrew    already installed (%s)", path)
		return path, nil
	}
	c.logf("   homebrew    not on PATH; running the official installer")
	if err := c.Change.Run("/bin/bash", "-c", homebrewInstaller); err != nil {
		return "", err
	}
	return resolveBrew(c)
}

// resolveBrew finds the brew the installer just wrote, and exists because
// LookPath cannot find it.
//
// The installer appends a `shellenv` line to a shell PROFILE. A profile is read
// by the next login shell; it cannot alter the PATH of a process that is already
// running, and exec.LookPath -- which is what Machine.LookPath is -- resolves
// against exactly that inherited PATH. So on the fresh machine this phase exists
// for, `brew` is still not on PATH one statement after installing it, and
// invoking it by bare name fails with "executable file not found in $PATH". The
// contract that produced ("run bootstrap twice") is not one a provisioner should
// offer.
//
// LookPath is retried first anyway: it costs nothing, and it is right in the one
// case the probe list cannot cover -- a machine whose PATH already contained a
// Homebrew prefix that had no brew in it until just now.
//
// ACCEPTED LIMITATION: `plan` cannot preview a machine that has no Homebrew yet.
// Under `plan` the installer above reaches Planner.Run, which records it and
// executes nothing, so this probe finds nothing and the phase stops with the
// error below. That is not an oversight and must not be "fixed" by falling back
// to the bare name: this function cannot distinguish "the installer ran and
// produced nothing" from "the installer was recorded, not run", and the only
// general way to tell them apart is a per-operation "was this performed" signal
// on Machine -- a mode flag every phase could then branch on, which is the
// erosion the dry-run invariant exists to prevent. Known, loud and documented
// beats silently planning against a brew that is not there.
// TestPackagesRefusesUnderPlanOnAMachineWithNoHomebrew pins it.
func resolveBrew(c Context) (string, error) {
	if path, err := c.Change.LookPath("brew"); err == nil {
		c.logf("   homebrew    installed; %s", path)
		return path, nil
	}
	for _, candidate := range brewLocations {
		info, err := c.Change.Lstat(candidate)
		// Continue rather than return. An unreadable candidate is not an answer
		// about the others, and abandoning the list on the first error means an
		// EACCES on /opt -- which a hardened Linux box can have -- stops the walk
		// before it ever reaches /home/linuxbrew. Only a candidate that Lstats
		// cleanly can win; the error is reported so it is not lost.
		if err != nil {
			c.logf("   homebrew    %s could not be read (%v); trying the next prefix",
				candidate, err)
			continue
		}
		if info.Exists {
			c.logf("   homebrew    installed; %s (not yet on PATH -- Homebrew's "+
				"shellenv line is read by the next login shell)", candidate)
			return candidate, nil
		}
	}

	// A plain error rather than a change.Refusal, for the reason stageZero gives:
	// a Refusal says a path on this machine is in a state bootstrap will not
	// write over, and these three paths are in no state at all. Both answer 2.
	//
	// Guessing past this point is worse than stopping. The installer reported
	// success and brew is neither on PATH nor at any prefix Homebrew uses, which
	// means something upstream changed -- and the next step would hand a bad path
	// to `brew bundle`, whose failure would name the guess rather than the cause.
	return "", fmt.Errorf("Homebrew's installer reported success but no brew "+
		"could be found: not on PATH, and absent from all three locations "+
		"Homebrew installs to (%s); if this is a `plan` on a machine that has no "+
		"Homebrew yet, that is the same evidence -- the installer was recorded, "+
		"not run", strings.Join(brewLocations, ", "))
}
