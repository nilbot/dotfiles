package phase

import "path/filepath"

// Devtools installs the tooling that is not a package: uv, the released agents
// binary the git hook chain points at, and the chain itself.
//
// It does NOT build agents. The binary comes from the tap, through the Brewfile
// the packages phase has already run, so one owner installs it and one path
// names it. That is the fix for the leak
// docs/design/2026-09-20-agents-and-bootstrap-boundary.md §4a recorded: this
// phase used to compile $HOME/bin/agents and hand the installer that path,
// while the package manager installed its own -- "both are correct for their
// owner; whichever runs second fails".
//
// Do not give this phase a build back. The Makefile's `agents` target built the
// same binary into one global $HOME/bin/agents stamped with the checkout it was
// built from, so from a linked worktree it published a binary naming a
// temporary directory; deleting that worktree left the stamp naming nothing and
// the failure was silent -- doctor passed, because the two paths agreed with
// each other, while the hook chain found no extras directory and ran no
// personal hooks, at exit 0. README.md's Makefile section keeps the account.
func Devtools(c Context) error {
	c.logf("== devtools")

	// PATH, not a Brewfile entry, decides. uv's own installer puts it in
	// ~/.local/bin outside any package manager, and a machine that already has
	// it that way does not need a second copy from Homebrew.
	//
	// Each line below is a label and a value rather than a verb, because this
	// phase runs under `plan` too, where nothing it announces has happened yet.
	// The executor reports the operations; the phase reports what it found.
	if path, err := c.Change.LookPath("uv"); err == nil {
		c.logf("   uv          already installed (%s)", path)
	} else {
		c.logf("   uv          not on PATH")
		// The resolved brew, not the bare name. Third site to need this, same
		// mechanism as the two before it: Homebrew's installer appends its
		// shellenv line to a PROFILE, a profile is read by the next login
		// shell, and nothing can alter the PATH of a process already running --
		// so on the fresh machine this phase exists for, brew is installed and
		// unfindable by name in the same run.
		//
		// Measured in CI 2026-08-17 on debian:stable-slim under `apply
		// workstation`: stage zero succeeded, Homebrew installed to
		// /home/linuxbrew/.linuxbrew, the fish phase found it there, and THIS
		// line died with `exec: "brew": executable file not found in $PATH`.
		// macOS never showed it because /opt/homebrew/bin is on PATH long
		// before bootstrap runs.
		brew, err := resolveBrew(c)
		if err != nil {
			return err
		}
		if err := c.Change.Run(brew, "install", "uv"); err != nil {
			return err
		}
	}

	installer := filepath.Join(c.Root, "git", "install-hooks.sh")

	// Resolved before anything reaches the installer, because the path is an
	// ARGUMENT to both steps below: a machine with no binary should be told
	// which path is missing rather than have the installer refuse a chain it
	// cannot compare against.
	binary := resolveAgents(c)
	if binary == "" {
		// Nothing has installed it yet. Under `plan` that is the ordinary state
		// of a machine being previewed -- plan executes nothing, so the packages
		// phase that installs it from the Brewfile has not run -- and refusing
		// here made every preview on a bare machine fail. Under `apply` the
		// packages phase has just run, so the path named below is the one that
		// should exist, and the INSTALLER is what refuses it when it does not:
		// validate_binary rejects a missing or non-executable file, and the
		// githook probe rejects a release too old to answer the subcommand the
		// entries run. Both are Runs, which plan does not perform.
		//
		// resolveBrew, not homebrew: this phase runs AFTER packages, so a
		// missing brew here is a real answer about the machine rather than a
		// cue to install one. homebrew would run the installer again on any
		// machine whose brew is at a prefix but not yet on PATH -- which is
		// every fresh Homebrew install, and every CI container.
		brew, err := resolveBrew(c)
		if err != nil {
			return err
		}
		binary = filepath.Join(filepath.Dir(brew), "agents")
		c.logf("   agents      not installed yet; the chain would name %s", binary)
	}

	// The hooks preflight runs BEFORE the install, and its whole value is in
	// where it sits. It validates the global config, the hooks directory and the
	// attributes link, refuses without touching anything, and returns in about a
	// second -- so a machine whose ~/.gitconfig is unusable, or whose chain
	// points somewhere this installer will not adopt, learns that before
	// anything is linked. `install` re-runs all of it internally, so nothing
	// goes unchecked either way.
	//
	// --adopt-owned is passed to BOTH invocations, and the preflight is the one
	// that needs it explained. Without the flag the preflight refuses any
	// existing link that does not already name $binary -- including the links
	// this installer wrote for an earlier binary, which is exactly the machine
	// this step exists to convert. A bare preflight would therefore stop the
	// phase one statement before the install that carries the flag could repair
	// it. The flag does not widen what may be adopted: only links shaped like
	// this installer's own are repointed, foreign ones are still refused.
	c.logf("   git hooks   git/install-hooks.sh")
	if err := c.Change.Run("bash", installer,
		"preflight", "--adopt-owned", c.Root, c.Home, binary); err != nil {
		return err
	}

	// Delegated, deliberately. git/install-hooks.sh validates the global config,
	// links ~/.gitattributes, symlinks the four hook names and writes
	// core.hooksPath LAST so a partial install cannot activate an incomplete
	// hooks directory -- and it is tested in the module that owns it.
	// Reimplementing it here would be a second copy of that ordering, subject to
	// drifting out of step with the one agents/install_hooks_test.go exercises.
	return c.Change.Run("bash", installer,
		"install", "--adopt-owned", c.Root, c.Home, binary)
}
