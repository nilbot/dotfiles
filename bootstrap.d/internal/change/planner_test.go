package change_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
)

// treeOf records path, kind, content hash and link target, so a plan that
// rewrote a file in place or repointed a link would be caught. The shell
// version compared names and kinds only.
func treeOf(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			dest, _ := os.Readlink(path)
			b.WriteString("link " + rel + " -> " + dest + "\n")
		case info.IsDir():
			b.WriteString("dir  " + rel + "\n")
		default:
			data, _ := os.ReadFile(path)
			b.WriteString("file " + rel + " " + string(data) + "\n")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestPlannerMutatesNothing(t *testing.T) {
	home := tempHome(t)
	src := filepath.Join(home, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(home, "existing")
	if err := os.WriteFile(existing, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := treeOf(t, home)

	var out bytes.Buffer
	p := change.NewPlanner(change.NewApplier(&bytes.Buffer{}, unusedRoot), &out, unusedRoot)
	if err := p.Dir(filepath.Join(home, "newdir")); err != nil {
		t.Fatal(err)
	}
	if err := p.Link(src, filepath.Join(home, "newlink")); err != nil {
		t.Fatal(err)
	}
	if err := p.Seed(src, filepath.Join(home, "newfile")); err != nil {
		t.Fatal(err)
	}
	if err := p.Run("touch", filepath.Join(home, "ran")); err != nil {
		t.Fatal(err)
	}
	if err := p.Sudo("chsh", "-s", "/bin/fish"); err != nil {
		t.Fatal(err)
	}

	if after := treeOf(t, home); after != before {
		t.Errorf("plan mutated the tree:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for _, want := range []string{"newdir", "newlink", "newfile", "touch", "chsh"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plan output omits %q:\n%s", want, out.String())
		}
	}
}

// The Planner overlays its own pending changes on what it reads. Without this,
// a link into a directory the plan just created re-reports that directory as
// missing -- output apply would never produce.
func TestPlannerOverlaysItsOwnChanges(t *testing.T) {
	home := tempHome(t)
	src := filepath.Join(home, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(home, "config")

	var out bytes.Buffer
	p := change.NewPlanner(change.NewApplier(&bytes.Buffer{}, unusedRoot), &out, unusedRoot)
	if err := p.Dir(parent); err != nil {
		t.Fatal(err)
	}
	if err := p.Link(src, filepath.Join(parent, "a")); err != nil {
		t.Fatal(err)
	}
	if err := p.Link(src, filepath.Join(parent, "b")); err != nil {
		t.Fatal(err)
	}

	if n := strings.Count(out.String(), "create directory"); n != 1 {
		t.Errorf("the parent directory should be planned once, got %d:\n%s", n, out.String())
	}

	info, err := p.Lstat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Exists || !info.IsDir {
		t.Errorf("Lstat should reflect the planned directory, got %+v", info)
	}
}

// Applier.Seed reads its source and Planner cannot, so both must decide a
// source's usability from the same shared verdict. Otherwise plan reports
// success for a source apply cannot read, and the two produce different error
// types for one condition. Subsumes the earlier planner-only missing-template
// test, which this covers as the missing/planner case.
func TestSeedRefusesUnusableSourceOnBothPaths(t *testing.T) {
	sources := []struct {
		kind string
		make func(t *testing.T, home string) string
	}{
		{"missing", func(t *testing.T, home string) string {
			return filepath.Join(home, "no such template")
		}},
		{"directory", func(t *testing.T, home string) string {
			t.Helper()
			dir := filepath.Join(home, "template dir")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			return dir
		}},
	}
	impls := []struct {
		name string
		make func(out io.Writer) change.Interface
	}{
		{"applier", func(out io.Writer) change.Interface { return change.NewApplier(out, unusedRoot) }},
		{"planner", func(out io.Writer) change.Interface {
			return change.NewPlanner(change.NewApplier(&bytes.Buffer{}, unusedRoot), out, unusedRoot)
		}},
	}

	for _, src := range sources {
		for _, impl := range impls {
			t.Run(src.kind+"/"+impl.name, func(t *testing.T) {
				home := tempHome(t)
				source := src.make(t, home)
				before := treeOf(t, home)

				var out bytes.Buffer
				err := impl.make(&out).Seed(source, filepath.Join(home, "config dir", "dst"))

				var refusal *change.Refusal
				if !errorsAs(err, &refusal) {
					t.Fatalf("want *change.Refusal, got %T: %v", err, err)
				}
				if refusal.Remediation == "" {
					t.Error("a refusal must name its remediation")
				}
				// Covers the stray parent directory too: treeOf walks everything.
				if after := treeOf(t, home); after != before {
					t.Errorf("a refused seed changed the tree:\nbefore:\n%s\nafter:\n%s", before, after)
				}
				if strings.Contains(out.String(), "seed") {
					t.Errorf("a refused seed must not be reported as done:\n%s", out.String())
				}
			})
		}
	}
}

// A link whose source is absent must refuse on both paths. Without the guard
// Applier happily creates a DANGLING SYMLINK and reports success, so a manifest
// typo -- or a row naming a file a later commit will create -- leaves the
// machine broken with nothing reporting it. Plan said nothing either, so the
// two agreed on the wrong answer.
func TestLinkRefusesMissingSourceOnBothPaths(t *testing.T) {
	for _, impl := range impls() {
		t.Run(impl.name, func(t *testing.T) {
			home := tempHome(t)
			source := filepath.Join(home, "no such source")
			before := treeOf(t, home)

			var out bytes.Buffer
			err := impl.make(&out).Link(source, filepath.Join(home, "config dir", "dst"))

			var refusal *change.Refusal
			if !errorsAs(err, &refusal) {
				t.Fatalf("want *change.Refusal, got %T: %v", err, err)
			}
			if refusal.Remediation == "" {
				t.Error("a refusal must name its remediation")
			}
			// Catches the dangling symlink and the stray parent directory both:
			// treeOf walks everything and records each link's destination.
			if after := treeOf(t, home); after != before {
				t.Errorf("a refused link changed the tree:\nbefore:\n%s\nafter:\n%s", before, after)
			}
			if strings.Contains(out.String(), "link") {
				t.Errorf("a refused link must not be reported as done:\n%s", out.String())
			}
		})
	}
}

// Existence is the whole test for a link source -- deliberately NOT the seed's
// Exists && !IsDir. links.manifest links directories on purpose (claude/skills,
// macOS/ghostty), so tightening the guard to match seedSourceVerdict would
// refuse rows the design requires.
func TestLinkAcceptsADirectorySource(t *testing.T) {
	for _, impl := range impls() {
		t.Run(impl.name, func(t *testing.T) {
			home := tempHome(t)
			source := filepath.Join(home, "skills dir")
			if err := os.MkdirAll(source, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := impl.make(&bytes.Buffer{}).Link(source, filepath.Join(home, "dst")); err != nil {
				t.Fatalf("linking a directory must be allowed: %v", err)
			}
		})
	}
}

// impls is the pair every "plan and apply must agree" case runs against. The
// two implementations are interchangeable through change.Interface, which is
// what makes one table body cover both.
func impls() []struct {
	name string
	make func(out io.Writer) change.Interface
} {
	return []struct {
		name string
		make func(out io.Writer) change.Interface
	}{
		{"applier", func(out io.Writer) change.Interface { return change.NewApplier(out, unusedRoot) }},
		{"planner", func(out io.Writer) change.Interface {
			return change.NewPlanner(change.NewApplier(&bytes.Buffer{}, unusedRoot), out, unusedRoot)
		}},
	}
}

// seedSourceVerdict names the two cases worth a remediation. Everything else is
// predicted by performing the identical read, so these must fail the same way on
// both paths -- byte-for-byte the same error, since it is the same read.
func TestSeedPredictsReadFailuresExactly(t *testing.T) {
	cases := []struct {
		kind string
		make func(t *testing.T, home string) string
	}{
		{"dangling symlink", func(t *testing.T, home string) string {
			t.Helper()
			source := filepath.Join(home, "dangling template")
			if err := os.Symlink(filepath.Join(home, "gone"), source); err != nil {
				t.Fatal(err)
			}
			return source
		}},
		{"permission denied", func(t *testing.T, home string) string {
			t.Helper()
			if os.Geteuid() == 0 {
				t.Skip("root reads a mode-000 file regardless, so this cannot be exercised")
			}
			source := filepath.Join(home, "unreadable template")
			if err := os.WriteFile(source, []byte("x"), 0o000); err != nil {
				t.Fatal(err)
			}
			return source
		}},
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			// One home for both runs, so the two errors name the same path and
			// are directly comparable. Safe because plan mutates nothing and
			// apply fails before it acts.
			home := tempHome(t)
			source := tc.make(t, home)
			target := filepath.Join(home, "config dir", "dst")
			before := treeOf(t, home)

			var planOut, applyOut bytes.Buffer
			planErr := change.NewPlanner(change.NewApplier(&bytes.Buffer{}, unusedRoot), &planOut, unusedRoot).Seed(source, target)
			applyErr := change.NewApplier(&applyOut, unusedRoot).Seed(source, target)

			if planErr == nil || applyErr == nil {
				t.Fatalf("both paths must fail; plan = %v, apply = %v", planErr, applyErr)
			}
			if planErr.Error() != applyErr.Error() {
				t.Errorf("plan and apply disagree:\nplan:  %v\napply: %v", planErr, applyErr)
			}
			// Covers the stray parent directory: the read must precede Dir.
			if after := treeOf(t, home); after != before {
				t.Errorf("a failed seed changed the tree:\nbefore:\n%s\nafter:\n%s", before, after)
			}
			for name, out := range map[string]*bytes.Buffer{"plan": &planOut, "apply": &applyOut} {
				if strings.Contains(out.String(), "seed") {
					t.Errorf("%s reported a seed that failed:\n%s", name, out.String())
				}
			}
		})
	}
}

// Applier.Dir uses MkdirAll, which creates the whole ancestor chain, so a
// Planner recording only the exact path announces directories apply never
// creates. The fixture is the ordinary manifest shape -- two links into a
// shared chain, deepest first -- which is what makes the extra line appear.
func TestPlanAndApplyAgreeOnCreatedDirectories(t *testing.T) {
	manifest := func(t *testing.T, c change.Interface, home string) {
		t.Helper()
		src := filepath.Join(home, "src file")
		if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		fish := filepath.Join(home, "config dir", "fish")
		if err := c.Dir(filepath.Join(fish, "functions")); err != nil {
			t.Fatal(err)
		}
		if err := c.Link(src, filepath.Join(fish, "functions", "a.fish")); err != nil {
			t.Fatal(err)
		}
		if err := c.Dir(fish); err != nil {
			t.Fatal(err)
		}
		if err := c.Link(src, filepath.Join(fish, "config.fish")); err != nil {
			t.Fatal(err)
		}
	}

	var planOut, applyOut bytes.Buffer
	manifest(t, change.NewPlanner(change.NewApplier(&bytes.Buffer{}, unusedRoot), &planOut, unusedRoot), tempHome(t))
	manifest(t, change.NewApplier(&applyOut, unusedRoot), tempHome(t))

	// "create directory" is not a substring of "created directory", so the two
	// counts cannot contaminate each other.
	planned := strings.Count(planOut.String(), "create directory")
	created := strings.Count(applyOut.String(), "created directory")
	if created == 0 {
		t.Fatal("the fixture created no directories; it no longer covers anything")
	}
	if planned != created {
		t.Errorf("plan announced %d directories, apply created %d:\nplan:\n%sapply:\n%s",
			planned, created, planOut.String(), applyOut.String())
	}
}

// The last plan/apply asymmetry, one level up the tree from the seed and link
// source verdicts: Dir("a/b/c") when a/b cannot hold c. Measured on darwin,
// before the fix:
//
//	regular file       Lstat("a/b/c") fails ENOTDIR, so BOTH paths return a raw
//	                   *fs.PathError -- no Refusal, no remediation, and nothing
//	                   naming a/b as the reason.
//	dangling symlink   Lstat("a/b/c") is ENOENT, so Planner plans the directory
//	                   and returns nil while Applier's MkdirAll fails EEXIST.
//	                   Plan reported success for something apply cannot do.
//
// A symlink to a real directory is the third case and the one that changes
// behaviour: MkdirAll would follow it and succeed. It is refused deliberately,
// because FileInfo.IsDir means a REAL directory throughout this package and
// dirVerdict already refuses a symlinked target with the same remediation.
// Accepting a symlinked ancestor while refusing a symlinked target would be
// incoherent, and writing into a symlinked tree is the "wrong tree" failure
// this design exists to prevent.
//
// The refusal must name the ancestor that blocks, not the leaf being created:
// "a/b/c cannot be created" sends you looking at the wrong path.
func TestDirRefusesANonDirectoryAncestorOnBothPaths(t *testing.T) {
	ancestors := []struct {
		kind string
		make func(t *testing.T, home string) string
	}{
		{"regular file", func(t *testing.T, home string) string {
			t.Helper()
			blocker := filepath.Join(home, "config dir", "fish")
			if err := os.WriteFile(blocker, []byte("a file, not a directory"), 0o644); err != nil {
				t.Fatal(err)
			}
			return blocker
		}},
		{"dangling symlink", func(t *testing.T, home string) string {
			t.Helper()
			blocker := filepath.Join(home, "config dir", "fish")
			if err := os.Symlink(filepath.Join(home, "gone"), blocker); err != nil {
				t.Fatal(err)
			}
			return blocker
		}},
		{"symlink to a directory", func(t *testing.T, home string) string {
			t.Helper()
			real := filepath.Join(home, "elsewhere")
			if err := os.MkdirAll(real, 0o755); err != nil {
				t.Fatal(err)
			}
			blocker := filepath.Join(home, "config dir", "fish")
			if err := os.Symlink(real, blocker); err != nil {
				t.Fatal(err)
			}
			return blocker
		}},
	}

	for _, ancestor := range ancestors {
		for _, impl := range impls() {
			t.Run(ancestor.kind+"/"+impl.name, func(t *testing.T) {
				home := tempHome(t)
				if err := os.MkdirAll(filepath.Join(home, "config dir"), 0o755); err != nil {
					t.Fatal(err)
				}
				blocker := ancestor.make(t, home)
				before := treeOf(t, home)

				var out bytes.Buffer
				// Two levels below the blocker, not one: the shallow case is
				// caught by the leaf Lstat alone, so a guard that only handled
				// it would still leave the deeper path returning a raw error.
				err := impl.make(&out).Dir(filepath.Join(blocker, "functions", "nested"))

				var refusal *change.Refusal
				if !errorsAs(err, &refusal) {
					t.Fatalf("want *change.Refusal, got %T: %v", err, err)
				}
				if refusal.Path != blocker {
					t.Errorf("the refusal should name the blocking ancestor %q, got %q",
						blocker, refusal.Path)
				}
				if refusal.Remediation == "" {
					t.Error("a refusal must name its remediation")
				}
				// Catches the symlinked-directory case in particular: without a
				// refusal MkdirAll follows the link and creates the directory
				// under home/elsewhere, which treeOf walks.
				if after := treeOf(t, home); after != before {
					t.Errorf("a refused directory changed the tree:\nbefore:\n%s\nafter:\n%s", before, after)
				}
				if strings.Contains(out.String(), "directory") {
					t.Errorf("a refused directory must not be reported as done:\n%s", out.String())
				}
			})
		}
	}
}

func TestPlannerStillRefuses(t *testing.T) {
	home := tempHome(t)
	src := filepath.Join(home, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "dst")
	if err := os.WriteFile(target, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := change.NewPlanner(change.NewApplier(&bytes.Buffer{}, unusedRoot), &bytes.Buffer{}, unusedRoot).Link(src, target)
	var refusal *change.Refusal
	if !errorsAs(err, &refusal) {
		t.Fatalf("plan must surface refusals, not hide them until apply; got %T", err)
	}
}
