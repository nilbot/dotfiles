package main_test

import (
	"os"
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
