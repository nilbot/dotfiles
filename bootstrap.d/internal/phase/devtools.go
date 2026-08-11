package phase

import "path/filepath"

// Devtools installs the tooling that is not a package: uv, the agents binary
// this repository builds from its own source, and the global git hooks that
// binary backs.
//
// The three steps are ordered by dependency, not by preference. The hooks
// installer symlinks four hook names AT the agents binary and refuses unless it
// is an executable regular file, so the build has to have happened first.
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
		if err := c.Change.Run("brew", "install", "uv"); err != nil {
			return err
		}
	}

	binary := filepath.Join(c.Home, "bin", "agents")
	installer := filepath.Join(c.Root, "git", "install-hooks.sh")

	// The hooks preflight runs BEFORE the build, and the reason is cost, not
	// symmetry with the Makefile. It validates the global config, the hooks
	// directory and the attributes link, refuses without touching anything, and
	// returns in about a second. `install` re-runs all of it internally, so
	// nothing goes unchecked either way -- but a machine whose ~/.gitconfig is
	// a symlink, or whose core.hooksPath already points somewhere else, should
	// find that out before it compiles a Go module rather than after.
	//
	// It is safe this early precisely because preflight mode does NOT validate
	// the binary; only install does, after the build has produced one.
	c.logf("   git hooks   git/install-hooks.sh")
	if err := c.Change.Run("bash", installer,
		"preflight", c.Root, c.Home, binary); err != nil {
		return err
	}

	if err := c.Change.Dir(filepath.Dir(binary)); err != nil {
		return err
	}
	// go build -C sets the build directory without a shell and without this
	// package needing a cwd concept -- there is no cd here, and reaching for
	// `sh -c` would put back the shell the rest of this design removed. Measured
	// on the installed toolchain: go1.26.5 accepts -C, and `go help build`
	// documents it as a flag that must come first on the command line, which is
	// where it is.
	//
	// The -X stamp is what tells the built binary which checkout it belongs to.
	// c.Root is the only party that knows: an unstamped binary falls back to
	// ~/dotfiles, which makes doctor fail three checks against a healthy machine
	// and makes the git hook chain skip every personal hook without saying so.
	// The repository Makefile's agents target carries the same flag.
	c.logf("   agents      %s", binary)
	if err := c.Change.Run("go", "build",
		"-C", filepath.Join(c.Root, "agents"),
		"-trimpath", "-ldflags", "-X main.dotfilesRoot="+c.Root,
		"-o", binary, "."); err != nil {
		return err
	}

	// Delegated, deliberately. git/install-hooks.sh validates the global config,
	// links ~/.gitattributes, symlinks the four hook names and writes
	// core.hooksPath LAST so a partial install cannot activate an incomplete
	// hooks directory -- and it is tested in the module that owns it.
	// Reimplementing it here would be a second copy of that ordering, subject to
	// drifting out of step with the one agents/install_hooks_test.go exercises.
	return c.Change.Run("bash", installer, "install", c.Root, c.Home, binary)
}
