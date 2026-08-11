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

// buildCommand returns the one line of text that compiles the binary. Searching
// a whole file or a whole `make -n` dump instead lets any other line answer for
// the build command: dropping the flag from the recipe and naming it in the
// target's @echo greens every guard below while producing an unstamped binary.
//
// Exactly one line, not the first: two would mean this repository grew a second
// way to build the thing, and a guard that quietly checked one of them would be
// making a claim it had stopped verifying.
func buildCommand(t *testing.T, text, source string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "go build") {
			found = append(found, stripMakeComment(line))
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s has %d lines running `go build`, want exactly 1:\n%s",
			source, len(found), text)
	}
	return found[0]
}

// stripMakeComment removes a trailing comment so the assertions read the command
// and not prose about it.
//
// Anchoring to the build line closed the case where an explanation elsewhere in
// the file answered for the recipe. It left the same trick one line shorter: a
// recipe with the path hardcoded and `# stamp: -X main.dotfilesRoot=$(CURDIR)`
// after it satisfied a search of that very line while building the wrong thing.
// Verified -- it passed before this existed.
//
// Quote-aware, because a `#` inside the -ldflags string is part of the command.
func stripMakeComment(line string) string {
	quoted := false
	for i, r := range line {
		switch r {
		case '"':
			quoted = !quoted
		case '#':
			if !quoted {
				return strings.TrimRight(line[:i], " \t")
			}
		}
	}
	return line
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
// make has already expanded whatever variables the recipe is written in and the
// emitted command is what actually builds the binary. -n is required, not a
// convenience: `make agents` writes ~/bin/agents on the machine running the
// tests.
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
	build := buildCommand(t, string(out), "make -n agents")
	_, after, found := strings.Cut(build, marker)
	if !found {
		t.Fatalf("make -n agents builds with:\n%s\ncarrying no %q; the binary this "+
			"produces falls back to ~/dotfiles whichever checkout it was built from",
			build, marker)
	}
	// To the closing quote, not to the first space: the flag is one shell-quoted
	// argument, so a checkout whose path contains a space would otherwise fail
	// this test rather than the Makefile.
	stamped := after
	if i := strings.IndexByte(stamped, '"'); i >= 0 {
		stamped = stamped[:i]
	} else if i := strings.IndexAny(stamped, " \t"); i >= 0 {
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
// only the unexpanded file can say whether the value is derived or written down.
//
// Read from the build command line rather than from anywhere in the file, for
// the same reason as above: a Makefile with the path hardcoded in the recipe and
// $(CURDIR) quoted in a comment explaining the stamp passes a whole-file search
// while building the wrong thing.
func TestMakefileDerivesTheAgentsStampRatherThanHardcodingIt(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	build := buildCommand(t, string(data), "the Makefile")
	if want := "-X main.dotfilesRoot=$(CURDIR)"; !strings.Contains(build, want) {
		t.Errorf("the Makefile builds with:\n%s\nwhich does not carry %q; the stamp "+
			"must be the directory make was invoked in, and a path written down "+
			"here is right on exactly one machine", build, want)
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
