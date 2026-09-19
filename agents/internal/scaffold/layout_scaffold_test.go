package scaffold

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/layout"
)

// initGitRepo inits an existing directory as a git repository. The scaffold
// tests need one because CreateWithLayout ends by asking git where the exclude
// file lives (repo.InfoExcludePath) and returns ErrNotARepo for a path that is
// not a repository.
//
// The plan's Task 9 tests were written against plain t.TempDir() fixtures,
// which cannot pass that last step; every fixture here is initialised instead,
// and the assertions are the plan's.
func initGitRepo(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-b", "agents-test"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		gitOutput(t, root, args...)
	}
}

// gitOutput runs one git command in root and returns its combined output,
// failing the test on error. `git check-attr` answers through it, so the
// attribute assertions ask git itself rather than reading .gitattributes back.
func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, root, err, out)
	}
	return string(out)
}

// snapshotTree records every path under root except .git, with its mode and its
// bytes, so a scaffold that rewrites a file with the bytes it already had is
// still a change. .git is excluded because scaffolding never owns it and a
// fixture's git metadata is not the tree under test.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".git" {
			return fs.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			snap[rel] = "dir " + info.Mode().String()
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			snap[rel] = "symlink " + target
		default:
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			snap[rel] = info.Mode().String() + " " + string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snap
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// testLayout is the four-role v2 layout the scaffold tests create. Its
// ManifestPath is empty on purpose: this is a layout a caller constructed in
// order to create it, which is what tells CreateWithLayout to write the stores
// rather than treat the manifest as already on disk (design §7.5).
func testLayout(storeRoot string) layout.Layout {
	stores := make(map[string]string, len(layout.Roles()))
	for _, role := range layout.Roles() {
		stores[role] = storeRoot + "/" + role
	}
	return layout.Layout{
		Manifest: layout.Manifest{
			Schema:         layout.SchemaV2,
			MinMutVerFloor: layout.MinMutVerFloorV2,
			LayoutStatus:   layout.StatusActive,
			Archive:        storeRoot + "/archive",
			Stores:         stores,
		},
	}
}

func TestCreateWithLayoutV2UsesManifestPathsAndLeavesNoDocsShell(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	l := testLayout(".context")
	if err := CreateWithLayout(root, false, l); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		".context/design/README.md", ".context/plans/README.md",
		".context/journal/README.md", ".context/qna/README.md",
		"AGENTS.md",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Fatal("v2 scaffold created a docs/ shell")
	}
	got, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if string(got) != layout.V2AgentsMD {
		t.Fatal("v2 scaffold did not write the v2 router")
	}
}

func TestCreateWithLayoutV2IsIdempotent(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	l := testLayout(".context")
	if err := CreateWithLayout(root, false, l); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, root)
	if err := CreateWithLayout(root, false, l); err != nil {
		t.Fatal(err)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("second scaffold changed the tree")
	}
}

// A manifest already in the repository is the authority for which stores exist:
// CreateWithLayout adds nothing to it, not even a store the manifest declares
// and the tree lacks. A missing store is the migration command's `store_missing`
// blocker, not init's remedy, and inventing one here would be the same
// "create a shell the layout does not describe" failure the v1 docs/ shell was.
func TestCreateWithLayoutLeavesAManifestBackedRepositoryAlone(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	want := testLayout(".context")
	if err := layout.WriteManifest(root, want.Manifest); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, root)

	if err := CreateWithLayout(root, false, layout.Resolve(root)); err != nil {
		t.Fatal(err)
	}

	if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("CreateWithLayout wrote into a repository whose manifest is on disk")
	}
	for _, role := range layout.Roles() {
		store, _ := layout.Path(want, role)
		if _, err := os.Stat(filepath.Join(root, store)); !os.IsNotExist(err) {
			t.Errorf("created the declared store %s, which the tree did not have", store)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("wrote a router into a repository whose layout the manifest already declares")
	}
}

func TestLayoutManifestIsVisibleToLinguist(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	if err := CreateWithLayout(root, false, testLayout(".context")); err != nil {
		t.Fatal(err)
	}
	out := gitOutput(t, root, "check-attr", "linguist-generated", "--", ".agents/layout.json")
	if !strings.Contains(out, "unset") {
		t.Fatalf("layout manifest is still linguist-generated: %s", out)
	}
	// The blanket rule still hides everything else under .agents/, and the
	// manifest exception does not leak to its neighbours. `git check-attr`
	// renders a bare rule as `true`, not `set`.
	other := gitOutput(t, root, "check-attr", "linguist-generated", "--", ".agents/AGENTS.md")
	if !strings.Contains(other, "linguist-generated: true") {
		t.Fatalf("the manifest exception reached .agents/AGENTS.md: %s", other)
	}
}

// init writes both skills, and the text it writes is the one the created layout
// selects (design §0.8): a v1 repository must never receive the v2 prose.
func TestCreateWithLayoutWritesTheLayoutSelectedSkillText(t *testing.T) {
	for _, tc := range []struct {
		name string
		l    func(root string) layout.Layout
	}{
		{"v1", layout.V1ForRoot},
		{"v2", func(string) layout.Layout { return testLayout(".context") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			initGitRepo(t, root)
			l := tc.l(root)
			if err := CreateWithLayout(root, false, l); err != nil {
				t.Fatal(err)
			}
			for _, skill := range []string{"recording-what-you-learn", "migrating-fleet-context"} {
				asset, err := SkillAssetPath(l.Schema, skill)
				if err != nil {
					t.Fatal(err)
				}
				wantBytes, err := AssetsFS.ReadFile(asset)
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(filepath.Join(root, ".agents/skills", skill, "SKILL.md"))
				if err != nil || !bytes.Equal(got, wantBytes) {
					t.Fatalf("%s scaffold wrote the wrong %s text: %v", l.Schema, skill, err)
				}
			}
		})
	}
}

// The recording skill is user-owned: init populates it only when it is absent.
func TestCreateWithLayoutNeverOverwritesAnExistingSkill(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	writeFile(t, filepath.Join(root, ".agents/skills/recording-what-you-learn/SKILL.md"), "# mine\n")
	if err := CreateWithLayout(root, false, testLayout(".context")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, ".agents/skills/recording-what-you-learn/SKILL.md"))
	if string(got) != "# mine\n" {
		t.Fatal("init overwrote a user-owned skill")
	}
}
