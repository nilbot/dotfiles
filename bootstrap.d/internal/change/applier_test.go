package change_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
)

// errorsAs keeps the package free of a testing-library dependency.
func errorsAs(err error, target any) bool { return errors.As(err, target) }

// unusedRoot is the checkout an executor serves in the cases that seed no
// @DOTFILES_ROOT@ token. A path that exists nowhere, deliberately: a case that
// silently starts depending on substitution then produces something obviously
// wrong rather than something plausible.
const unusedRoot = "/no such checkout"

// A space in every fixture path: paths with spaces broke the shell version and
// must never regress.
func tempHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home dir")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestApplierLinkCreatesSymlink(t *testing.T) {
	home := tempHome(t)
	src := filepath.Join(home, "source file")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "nested", "target link")

	a := change.NewApplier(&bytes.Buffer{}, unusedRoot)
	if err := a.Link(src, target); err != nil {
		t.Fatalf("Link: %v", err)
	}
	got, err := os.Readlink(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != src {
		t.Errorf("link points to %q, want %q", got, src)
	}
}

func TestApplierLinkIsIdempotent(t *testing.T) {
	home := tempHome(t)
	src := filepath.Join(home, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "dst")

	var out bytes.Buffer
	a := change.NewApplier(&out, unusedRoot)
	if err := a.Link(src, target); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := a.Link(src, target); err != nil {
		t.Fatalf("second Link: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("an already-correct link should report nothing, got %q", out.String())
	}
}

func TestApplierLinkRefusesForeignTarget(t *testing.T) {
	home := tempHome(t)
	src := filepath.Join(home, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "dst")
	if err := os.WriteFile(target, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := change.NewApplier(&bytes.Buffer{}, unusedRoot).Link(src, target)
	var refusal *change.Refusal
	if !errorsAs(err, &refusal) {
		t.Fatalf("want *change.Refusal, got %T: %v", err, err)
	}
	if refusal.Remediation == "" {
		t.Error("a refusal must name its remediation")
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil || string(content) != "mine" {
		t.Errorf("refusal clobbered the target: %v %q", readErr, content)
	}
}

func TestApplierSeedNeverOverwrites(t *testing.T) {
	home := tempHome(t)
	tmpl := filepath.Join(home, "tmpl")
	if err := os.WriteFile(tmpl, []byte("template"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "dst")
	if err := os.WriteFile(target, []byte("local edits"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := change.NewApplier(&bytes.Buffer{}, unusedRoot).Seed(tmpl, target); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	content, _ := os.ReadFile(target)
	if string(content) != "local edits" {
		t.Errorf("Seed overwrote an existing file: %q", content)
	}
}

// A seeded file is machine-local and written once, which makes it the right
// place for the one fact that varies per machine: where the checkout is. A
// template copied byte-for-byte cannot carry that fact, so Seed substitutes it.
//
// Two occurrences, not one: "every occurrence" is the requirement, and a
// single-shot replace would otherwise pass.
//
// The token must be gone from the result as well as the root present. Both
// consumers name a file that git and fish pass over IN SILENCE when it is not
// there, so a stub pointing at a literal @DOTFILES_ROOT@ does not fail loudly --
// every shared setting simply stops applying.
func TestApplierSeedSubstitutesTheRoot(t *testing.T) {
	home := tempHome(t)
	root := filepath.Join(home, "checkout dir")
	tmpl := filepath.Join(home, "tmpl")
	template := "source " + change.RootToken + "/fish/config.fish\n" +
		"# and once more: " + change.RootToken + "\n"
	if err := os.WriteFile(tmpl, []byte(template), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "config dir", "config.fish")

	if err := change.NewApplier(&bytes.Buffer{}, root).Seed(tmpl, target); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), change.RootToken) {
		t.Errorf("the seeded file still carries %s, which names no real path:\n%s",
			change.RootToken, got)
	}
	want := "source " + root + "/fish/config.fish\n# and once more: " + root + "\n"
	if string(got) != want {
		t.Errorf("seeded\n%q\nwant\n%q", got, want)
	}
}

// Substitution must not disturb a template that says nothing about the checkout.
// git/gitconfig.local.template's prose is the live example: whatever it happens
// to contain is what the machine gets.
func TestApplierSeedCopiesATokenlessTemplateVerbatim(t *testing.T) {
	home := tempHome(t)
	tmpl := filepath.Join(home, "tmpl")
	// Deliberately awkward: an @ that is not the token, a trailing blank line,
	// and no final newline after it.
	template := "# machine-local\n[user]\n\temail = someone@example.com\n\n"
	if err := os.WriteFile(tmpl, []byte(template), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "config dir", "dst")

	if err := change.NewApplier(&bytes.Buffer{}, filepath.Join(home, "root")).Seed(tmpl, target); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != template {
		t.Errorf("a template naming no checkout must be byte-identical:\ngot  %q\nwant %q",
			got, template)
	}
}

// Seed never overwrites, so a second run cannot substitute into a file a person
// has since edited. The second Applier carries a DIFFERENT root: if a re-run
// rewrote the target, the new root would appear and the local edit would be
// gone.
func TestApplierSeedDoesNotSubstituteAgainOnARerun(t *testing.T) {
	home := tempHome(t)
	first := filepath.Join(home, "first checkout")
	tmpl := filepath.Join(home, "tmpl")
	if err := os.WriteFile(tmpl, []byte("source "+change.RootToken+"/fish/config.fish\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "config dir", "config.fish")

	if err := change.NewApplier(&bytes.Buffer{}, first).Seed(tmpl, target); err != nil {
		t.Fatalf("first Seed: %v", err)
	}
	edited := "source " + first + "/fish/config.fish\nset -g EDITED_BY_HAND 1\n"
	if err := os.WriteFile(target, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	second := filepath.Join(home, "second checkout")
	var out bytes.Buffer
	if err := change.NewApplier(&out, second).Seed(tmpl, target); err != nil {
		t.Fatalf("second Seed: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != edited {
		t.Errorf("a re-run rewrote a machine-local file:\ngot  %q\nwant %q", got, edited)
	}
	if strings.Contains(string(got), second) {
		t.Errorf("the second root reached an already-seeded file:\n%s", got)
	}
}

// The ~/.gitconfig regression from spec 1 §8.4, and the fish regression.
func TestApplierSeedRefusesSymlinkTarget(t *testing.T) {
	home := tempHome(t)
	tmpl := filepath.Join(home, "tmpl")
	if err := os.WriteFile(tmpl, []byte("t"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "dst")
	if err := os.Symlink(tmpl, target); err != nil {
		t.Fatal(err)
	}

	err := change.NewApplier(&bytes.Buffer{}, unusedRoot).Seed(tmpl, target)
	var refusal *change.Refusal
	if !errorsAs(err, &refusal) {
		t.Fatalf("want *change.Refusal, got %T", err)
	}
	if !strings.Contains(refusal.Remediation, "migrate") {
		t.Errorf("refusal should point at the migration, got %q", refusal.Remediation)
	}
}

// Seed reads the source before creating the parent, so a missing template
// fails without leaving a stray directory behind.
func TestApplierSeedLeavesNoStrayDirOnMissingTemplate(t *testing.T) {
	home := tempHome(t)
	missing := filepath.Join(home, "no such template")
	parent := filepath.Join(home, "config dir")

	err := change.NewApplier(&bytes.Buffer{}, unusedRoot).Seed(missing, filepath.Join(parent, "dst"))
	if err == nil {
		t.Fatal("Seed from a missing template must fail")
	}
	if _, statErr := os.Lstat(parent); !os.IsNotExist(statErr) {
		t.Errorf("Seed left a stray parent directory behind: %v", statErr)
	}
}

func TestApplierDirRefusesSymlink(t *testing.T) {
	home := tempHome(t)
	real := filepath.Join(home, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "linked")
	if err := os.Symlink(real, target); err != nil {
		t.Fatal(err)
	}

	err := change.NewApplier(&bytes.Buffer{}, unusedRoot).Dir(target)
	var refusal *change.Refusal
	if !errorsAs(err, &refusal) {
		t.Fatalf("a symlink-to-directory must be refused, got %T: %v", err, err)
	}
}

func TestApplierLstatDistinguishesKinds(t *testing.T) {
	home := tempHome(t)
	dir := filepath.Join(home, "d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "l")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}

	a := change.NewApplier(&bytes.Buffer{}, unusedRoot)
	cases := []struct {
		path string
		want change.FileInfo
	}{
		{dir, change.FileInfo{Exists: true, IsDir: true}},
		{file, change.FileInfo{Exists: true, IsRegular: true}},
		{link, change.FileInfo{Exists: true, IsLink: true}},
		{filepath.Join(home, "absent"), change.FileInfo{}},
	}
	for _, tc := range cases {
		got, err := a.Lstat(tc.path)
		if err != nil {
			t.Fatalf("Lstat(%s): %v", tc.path, err)
		}
		if got != tc.want {
			t.Errorf("Lstat(%s) = %+v, want %+v", filepath.Base(tc.path), got, tc.want)
		}
	}
}
