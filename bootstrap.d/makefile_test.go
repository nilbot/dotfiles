package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The Makefile is a developer convenience now. Provisioning is ./bootstrap's,
// and a second entry point that half works is worse than none.
func TestMakefileIsDeveloperTargetsOnly(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, gone := range []string{
		"all:", "dep:", "links:", "bins:", "omz:", "editors:", "extra:",
		"tmux:", "dotfiles:", "githooks:", "fishshell:", "starship:",
	} {
		if strings.Contains(content, gone) {
			t.Errorf("Makefile still defines the %q target; ./bootstrap owns provisioning now", gone)
		}
	}
	if !strings.Contains(content, "agents:") {
		t.Error("the agents build target stays; it is the inner-loop convenience")
	}
	if !strings.Contains(content, "./bootstrap") {
		t.Error("the Makefile should say where provisioning went")
	}
}

// The agents binary cannot work out which checkout it came from, so whoever
// builds it has to say. Two things build it -- this target and ./bootstrap's
// devtools phase -- and they sit in different files, in different languages, in
// different modules. Nothing else would notice one of them losing the stamp:
// the binary still builds, still runs, and only misbehaves on a machine whose
// checkout is not ~/dotfiles, where doctor fails three checks that are fine and
// the git hook chain runs no personal hooks and reports nothing.
//
// This asks make what the target emits rather than reading the recipe, because
// the recipe is free to grow variables and the emitted command is what actually
// builds the binary. -n is required, not a convenience: `make agents` writes
// ~/bin/agents on the machine running the tests.
func TestMakeEmitsAnAgentsBuildStampedWithThisCheckout(t *testing.T) {
	root := repoRoot(t)
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Fatalf("make is not on PATH (%v); `make agents` is this repository's "+
			"inner loop, and without make this guard cannot see what it emits", err)
	}
	cmd := exec.Command(makePath, "-n", "agents")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n agents: %v\n%s", err, out)
	}

	const marker = "-X main.dotfilesRoot="
	_, after, found := strings.Cut(string(out), marker)
	if !found {
		t.Fatalf("make -n agents emitted:\n%s\nwith no %q; the binary this builds "+
			"falls back to ~/dotfiles whichever checkout it was built from",
			out, marker)
	}
	stamped := after
	if i := strings.IndexAny(stamped, "\" \n"); i >= 0 {
		stamped = stamped[:i]
	}
	if !filepath.IsAbs(stamped) {
		t.Fatalf("the emitted stamp is %q, which is not an absolute path; the binary "+
			"would resolve it against wherever it happens to be run", stamped)
	}
	// Identity, not string equality. make derives $(CURDIR) from getcwd, so on a
	// checkout reached through a symlink it names the physical path while this
	// test names the lexical one. Both are this checkout, which is the claim.
	stampedInfo, err := os.Stat(stamped)
	if err != nil {
		t.Fatalf("the emitted stamp %q does not exist: %v", stamped, err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(stampedInfo, rootInfo) {
		t.Errorf("make stamped %q, but the checkout make was invoked in is %q; the "+
			"binary would name a checkout that is not the one it was built from",
			stamped, root)
	}
}

// The one thing the check above cannot see. It runs make in this checkout, so a
// stamp hardcoded to whatever path this machine happens to use would satisfy it
// here and hand every other machine a binary naming a directory it does not
// have. $(CURDIR) is what makes the value the checkout make was invoked in, and
// only the file can say whether the value is derived or written down.
//
// Deliberately not scoped to the agents recipe: a variable holding the flags is
// a fair way to write this Makefile, and the check above already establishes
// that whatever holds it reaches the build command.
func TestMakefileDerivesTheAgentsStampRatherThanHardcodingIt(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "-X main.dotfilesRoot=$(CURDIR)"; !strings.Contains(string(data), want) {
		t.Errorf("the Makefile does not contain %q; the stamp must be the directory "+
			"make was invoked in, and a path written down here is right on exactly "+
			"one machine", want)
	}
}

// Every one of these was a way the Makefile could damage a machine. They are
// asserted individually rather than as "the file got shorter", because a later
// edit that reintroduces one is exactly what this pins.
func TestMakefileDoesNothingDestructive(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"rm -rf", "sudo", "chsh", "git clone", "ln -s"} {
		if strings.Contains(string(data), gone) {
			t.Errorf("Makefile still contains %q; the phases own that, and they refuse rather than clobber", gone)
		}
	}
}
