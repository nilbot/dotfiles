package main

import (
	"os"
	"path/filepath"
	"testing"
)

// stampRoot sets the link-time stamp for one case and restores it afterwards.
//
// dotfilesRoot is a package variable that a stamped build sets, so a case that
// assumed it was empty would pass under `go test` and describe a binary nobody
// ships. These cases mutate shared package state and must not run in parallel
// with each other; t.Setenv already refuses to run under t.Parallel.
func stampRoot(t *testing.T, value string) {
	t.Helper()
	previous := dotfilesRoot
	t.Cleanup(func() { dotfilesRoot = previous })
	dotfilesRoot = value
}

func TestDotfilesRootReturnsEmptyWhenUnstampedAndEnvUnsetEvenIfHomeDotfilesExists(t *testing.T) {
	stampRoot(t, "")
	home := t.TempDir()
	t.Setenv("AGENTS_DOTFILES_ROOT", "")
	t.Setenv("HOME", home)

	if err := os.Mkdir(filepath.Join(home, "dotfiles"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := DotfilesRoot(); got != "" {
		t.Errorf("DotfilesRoot() = %q, want %q; unstamped binaries without AGENTS_DOTFILES_ROOT "+
			"must operate in Standalone Mode even if ~/dotfiles exists", got, "")
	}
}

func TestDotfilesRootPrefersTheEnvironmentOverEmpty(t *testing.T) {
	stampRoot(t, "")
	want := filepath.Join(t.TempDir(), "checkout")
	t.Setenv("AGENTS_DOTFILES_ROOT", want)
	t.Setenv("HOME", t.TempDir())

	if got := DotfilesRoot(); got != want {
		t.Errorf("DotfilesRoot() = %q, want the AGENTS_DOTFILES_ROOT value %q; "+
			"an unstamped binary must still be able to name its checkout via environment", got, want)
	}
}

func TestDotfilesRootStampWinsOverEnvironment(t *testing.T) {
	want := filepath.Join(t.TempDir(), "stamped")
	stampRoot(t, want)
	t.Setenv("AGENTS_DOTFILES_ROOT", filepath.Join(t.TempDir(), "from-env"))
	t.Setenv("HOME", t.TempDir())

	if got := DotfilesRoot(); got != want {
		t.Errorf("DotfilesRoot() = %q, want the build stamp %q; the stamp is the "+
			"builder's statement of which checkout this binary belongs to and "+
			"nothing at run time may override it", got, want)
	}
}
