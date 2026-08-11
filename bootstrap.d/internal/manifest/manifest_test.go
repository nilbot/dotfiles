package manifest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/manifest"
)

func TestParseSkipsCommentsAndBlanks(t *testing.T) {
	rows, err := manifest.Parse([]byte(`
   # indented comment
link    a   b   *

dir     -   c   darwin
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}
	want := manifest.Row{Kind: manifest.KindLink, Source: "a", Target: "b", Platform: "*"}
	if rows[0] != want {
		t.Errorf("got %+v, want %+v", rows[0], want)
	}
}

func TestParseRejectsWrongColumnCount(t *testing.T) {
	_, err := manifest.Parse([]byte("link  a  b\n"))
	if err == nil {
		t.Fatal("a three-column row must be an error")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error should name the line: %v", err)
	}
}

func TestParseRejectsUnknownKind(t *testing.T) {
	_, err := manifest.Parse([]byte("hardlink  a  b  *\n"))
	if err == nil {
		t.Fatal("an unknown kind must be an error")
	}
	if !strings.Contains(err.Error(), "hardlink") {
		t.Errorf("error should name the kind: %v", err)
	}
}

func TestForFiltersByPlatform(t *testing.T) {
	rows, err := manifest.Parse([]byte(
		"link  w  everywhere  *\nlink  d  mac  darwin\nlink  l  pengu  linux\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := manifest.For(rows, "darwin")
	if len(got) != 2 || got[0].Target != "everywhere" || got[1].Target != "mac" {
		t.Errorf("darwin filter gave %+v", got)
	}
	if len(manifest.For(rows, "linux")) != 2 {
		t.Errorf("linux filter gave %+v", manifest.For(rows, "linux"))
	}
}

func TestDuplicateTargets(t *testing.T) {
	rows, err := manifest.Parse([]byte(
		"link  one  same   *\nlink  two  same   darwin\nlink  three  other  *\n"))
	if err != nil {
		t.Fatal(err)
	}
	dupes := manifest.DuplicateTargets(manifest.For(rows, "darwin"))
	if len(dupes) != 1 || dupes[0] != "same" {
		t.Errorf("got %v, want [same]", dupes)
	}
	// The same target under two different platforms is not a conflict.
	if d := manifest.DuplicateTargets(manifest.For(rows, "linux")); len(d) != 0 {
		t.Errorf("got %v, want none", d)
	}
}

// fisher's own record of what it installed is a path fisher writes, so the
// placement rule makes it a seed and never a link. Its source is the tracked
// fishfile that install_fisher reads directly, which is what keeps the list
// bootstrap seeds and the list the shell installs from being two lists.
//
// The whole row is asserted, not merely the pair: a row that named the right
// files under kind `link` would publish fisher's writes back into the checkout,
// which is the exact fault §1's placement rule exists to prevent.
func TestRealManifestSeedsTheFisherRecordFromTheTrackedList(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "links.manifest"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := manifest.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	want := manifest.Row{
		Kind:     manifest.KindSeed,
		Source:   "fish/fishfile",
		Target:   ".config/fish/fish_plugins",
		Platform: "*",
	}
	for _, row := range rows {
		if row == want {
			return
		}
	}
	t.Errorf("no row %+v in the shipped manifest; fisher's record would be "+
		"whatever the machine happened to have:\n%+v", want, rows)
}

// The shipped manifest must parse and be conflict-free on both platforms.
func TestRealManifestIsWellFormed(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "links.manifest"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := manifest.Parse(data)
	if err != nil {
		t.Fatalf("the shipped manifest does not parse: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the shipped manifest is empty")
	}
	for _, platform := range []string{"darwin", "linux"} {
		if d := manifest.DuplicateTargets(manifest.For(rows, platform)); len(d) != 0 {
			t.Errorf("%s: duplicate targets %v", platform, d)
		}
	}
}
