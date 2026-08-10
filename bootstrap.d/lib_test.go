package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoLinkCreatesSymlink(t *testing.T) {
	f := newFixture(t)
	src := filepath.Join(f.home, "source file")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(f.home, "target link")

	_, stderr, code := f.runLib(t, `do_link "$HOME/source file" "$HOME/target link"`)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	got, err := os.Readlink(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != src {
		t.Errorf("link points to %q, want %q", got, src)
	}
}

func TestDoLinkIsIdempotent(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.home, "src"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `do_link "$HOME/src" "$HOME/dst"`
	if _, stderr, code := f.runLib(t, body); code != 0 {
		t.Fatalf("first run exit %d: %s", code, stderr)
	}
	stdout, stderr, code := f.runLib(t, body)
	if code != 0 {
		t.Fatalf("second run exit %d: %s", code, stderr)
	}
	if strings.Contains(stdout, "linked") {
		t.Errorf("second run should be a no-op, got: %s", stdout)
	}
}

func TestDoLinkRefusesForeignTarget(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.home, "src"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.home, "dst"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := f.runLib(t, `do_link "$HOME/src" "$HOME/dst"`)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "move it aside") {
		t.Errorf("refusal lacks remediation: %s", stderr)
	}
	// The foreign file must survive untouched.
	content, err := os.ReadFile(filepath.Join(f.home, "dst"))
	if err != nil || string(content) != "mine" {
		t.Errorf("refusal clobbered the target: %v %q", err, content)
	}
}

func TestDoSeedNeverOverwrites(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.home, "tmpl"), []byte("template"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.home, "dst"), []byte("local edits"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := f.runLib(t, `do_seed "$HOME/tmpl" "$HOME/dst"`)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	content, _ := os.ReadFile(filepath.Join(f.home, "dst"))
	if string(content) != "local edits" {
		t.Errorf("seed overwrote an existing file: %q", content)
	}
}

func TestDoSeedRefusesSymlinkTarget(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.home, "tmpl"), []byte("t"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.home, "tmpl"), filepath.Join(f.home, "dst")); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := f.runLib(t, `do_seed "$HOME/tmpl" "$HOME/dst"`)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "machine-local regular file") {
		t.Errorf("refusal should name the rule: %s", stderr)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.home, "src"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.home, "tmpl"), []byte("t"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := tree(t, f.home)

	stdout, stderr, code := f.runLib(t, `
BOOTSTRAP_DRY_RUN=1
do_dir  "$HOME/newdir"
do_link "$HOME/src"  "$HOME/newlink"
do_seed "$HOME/tmpl" "$HOME/newfile"
do_run  touch "$HOME/ran"
`)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if after := tree(t, f.home); after != before {
		t.Errorf("dry run mutated the tree:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for _, want := range []string{"newdir", "newlink", "newfile", "run: touch"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("plan output missing %q, got:\n%s", want, stdout)
		}
	}
}

func TestPlatformDetection(t *testing.T) {
	f := newFixture(t)
	stdout, stderr, code := f.runLib(t, `bootstrap_platform`)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := strings.TrimSpace(stdout)
	if got != "darwin" && got != "linux" {
		t.Errorf("platform %q is neither darwin nor linux", got)
	}
}

func TestDryRunFailsClosedOnUnparseableValue(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.home, "src"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := tree(t, f.home)

	// Test with BOOTSTRAP_DRY_RUN=true (unparseable as a number)
	stdout, stderr, code := f.runLib(t, `
BOOTSTRAP_DRY_RUN=true
do_link "$HOME/src" "$HOME/link"
`)
	if code != 0 {
		t.Fatalf("exit %d (BOOTSTRAP_DRY_RUN=true): %s", code, stderr)
	}
	if strings.Contains(stderr, "integer expression expected") {
		t.Errorf("bash error leaked (BOOTSTRAP_DRY_RUN=true): %s", stderr)
	}
	if after := tree(t, f.home); after != before {
		t.Errorf("dry run with BOOTSTRAP_DRY_RUN=true mutated the tree:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if !strings.Contains(stdout, "link") {
		t.Errorf("dry run with BOOTSTRAP_DRY_RUN=true should output plan, got: %s", stdout)
	}

	// Test with BOOTSTRAP_DRY_RUN unset (should behave as 0, not dry run)
	before = tree(t, f.home)
	stdout, stderr, code = f.runLib(t, `
unset BOOTSTRAP_DRY_RUN
do_link "$HOME/src" "$HOME/link2"
`)
	if code != 0 {
		t.Fatalf("exit %d (BOOTSTRAP_DRY_RUN unset): %s", code, stderr)
	}
	if strings.Contains(stderr, "integer expression expected") {
		t.Errorf("bash error leaked (BOOTSTRAP_DRY_RUN unset): %s", stderr)
	}
	if after := tree(t, f.home); after == before {
		t.Errorf("apply with BOOTSTRAP_DRY_RUN unset should not be a dry run, tree should have changed")
	}
}
