package change_test

import (
	"bytes"
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
	p := change.NewPlanner(change.NewApplier(&bytes.Buffer{}), &out)
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
	p := change.NewPlanner(change.NewApplier(&bytes.Buffer{}), &out)
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

// Applier.Seed reads the source, so a plan that does not check it reports
// success where apply fails -- the exact divergence this package exists to
// prevent.
func TestPlannerSeedRefusesMissingTemplate(t *testing.T) {
	home := tempHome(t)
	missing := filepath.Join(home, "no such template")
	before := treeOf(t, home)

	var out bytes.Buffer
	p := change.NewPlanner(change.NewApplier(&bytes.Buffer{}), &out)
	err := p.Seed(missing, filepath.Join(home, "config dir", "dst"))

	var refusal *change.Refusal
	if !errorsAs(err, &refusal) {
		t.Fatalf("want *change.Refusal for a missing template, got %T: %v", err, err)
	}
	if refusal.Remediation == "" {
		t.Error("a refusal must name its remediation")
	}
	if after := treeOf(t, home); after != before {
		t.Errorf("plan mutated the tree:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if strings.Contains(out.String(), "seed") {
		t.Errorf("a refused seed must not be reported as planned:\n%s", out.String())
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

	err := change.NewPlanner(change.NewApplier(&bytes.Buffer{}), &bytes.Buffer{}).Link(src, target)
	var refusal *change.Refusal
	if !errorsAs(err, &refusal) {
		t.Fatalf("plan must surface refusals, not hide them until apply; got %T", err)
	}
}
