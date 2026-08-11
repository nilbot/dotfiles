package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The removals are pinned rather than merely performed. Nothing else stops a
// later commit from reintroducing a path this spec retired, and the reasons are
// in commit messages that no test reads.
func TestRemovedPathsAreGone(t *testing.T) {
	root := repoRoot(t)
	gone := []string{
		"zsh", "tools", "miniforge", "bin", "spacemacs", "gnupg",
		"macOS/iterm2", "git/hooks/go.pre-commit",
		"snapshot.sh", "recover.sh", "mountcrypt.sh", "mountsshfs.sh",
		"post-install.sh", "softlinks.sh", "install-font-linux.sh",
	}
	for _, path := range gone {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed", path)
		}
	}
}

// Deleting miniforge/ without deleting the fish wiring would leave the machine
// still trying to initialise a package manager that is no longer installed --
// the config outlives the thing it configures, which is the failure this whole
// spec is about.
func TestNoFishConfigReferencesRemovedTooling(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{"mypost.fish", "alias.fish", "mypre.fish", "config.fish", "config.fish.template"} {
		data, err := os.ReadFile(filepath.Join(root, "fish", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, gone := range []string{"mambaforge", "MAMBA_EXE", "micromamba", "conda"} {
			if strings.Contains(string(data), gone) {
				t.Errorf("fish/%s still references %q", name, gone)
			}
		}
	}
}

// The mojo block is the only thing mypost.fish carries once the conda and mamba
// blocks are gone. Asserting it survives is what makes the case above a
// deletion of two blocks rather than of the file.
func TestMypostKeepsTheMojoBlock(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "fish", "mypost.fish"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "MODULAR_HOME") {
		t.Error("the mojo block is not part of this removal")
	}
}
