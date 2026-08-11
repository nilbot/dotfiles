# Dotfiles Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `make dotfiles` with `./bootstrap`, a phased, idempotent, cross-platform workstation provisioner, and remove the dead weight the old machinery accumulated.

**Architecture:** A ~40-line shell shim at the repo root reaches Go and hands over; everything else is a Go program in `bootstrap.d/`. All machine access — reads included — goes through `change.Interface`, with an `Applier` that performs operations and a `Planner` that records intent and touches nothing. `internal/phase` imports no I/O package, so "a phase cannot mutate outside dry-run control" is a fact about the import graph, checked by one test. A tracked `links.manifest` declares each managed path's *kind* (`link`, `seed`, `dir`), making spec 1 §8.4's placement rule mechanically checkable.

**Tech Stack:** Go 1.26 (no dependencies), bash 3.2 for the shim only, Homebrew on both platforms, fish 4.x.

**Spec:** [docs/superpowers/specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md](../specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md)

> **This plan replaced a shell-based one after one task of implementation.** The evidence is in the spec's [six bash defects](../specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md#the-six-bash-defects-that-decided-1-2026-08-10). Commits `14dab82` and `5ffeba3` are the shell version; `1dbbfe4` removed it. Do not reintroduce shell for anything but the shim.

## Global Constraints

Every task's requirements implicitly include this section.

- **Go module:** `bootstrap.d/go.mod`, module path `github.com/nilbot/dotfiles/bootstrap`, `go 1.26`. **No third-party dependencies** — stdlib only. No `go.work`.
- **Test command:** `cd bootstrap.d && go test ./...` — there is no Go module at the repo root.
- **`internal/phase` must import no I/O package.** Not `os`, `os/exec`, `io/fs`, `net`, or anything that reaches the machine. Phases receive a `change.Interface`. Task 5 enforces this with a test; do not weaken it.
- **Refuse, never clobber.** A target that exists and is not the exact intended thing produces a `*change.Refusal` naming the remediation. Never delete a user path outside a declared reclaiming migration.
- **Exit codes:** `0` ok, `1` advisory, `2` block/refuse, `3` malformed input, `4` not applicable. Defined once in `main.go`.
- **Platform values** are exactly `darwin` and `linux`. Manifest wildcard is `*`.
- **`$HOME`** comes from the environment so tests can redirect it. Never hardcode `/Users/...`. Repo root is resolved from the executable's location or an explicit flag, never from `pwd`.
- **Shim:** `#!/usr/bin/env bash` + `set -euo pipefail`, bash 3.2 compatible. It is the *only* shell file in this design.
- **Commit messages:** no AI attribution, no `Co-Authored-By` trailer. Conventional prefixes matching repo history (`feat(bootstrap):`, `refactor(fish):`, `chore(remove):`).
- **Staging:** commit the exact paths each task names. Never `git add -A` — this worktree has untracked `.agents/reports/traces/*.jsonl` and a git-ignored `.superpowers/`.

### Three rules from defects this plan already produced

Every Critical/Important finding so far fell into one of three classes, all of
them errors in this plan's text. The rules exist so the next instance is caught
by construction rather than by review.

- **Any decision about whether an operation can proceed goes in a shared
  `verdict` function that both `Applier` and `Planner` consult.** Never in one
  implementation alone. Four divergences came from this: a missing seed source,
  an unreadable one, `MkdirAll`'s ancestor chain, and an absent link source. If
  `Applier` can fail somewhere `Planner` returns nil, that is a defect, not a
  limitation — and if the prediction is approximate, have `Planner` *perform*
  the read rather than predict it. Reading is not a mutation.
  *Known and permanent exception:* `Run` and `Sudo`. `Planner` cannot predict
  whether an arbitrary command will succeed, so it prints intent and returns
  nil. Document it; do not pretend to close it.
- **A task may only reference files that already exist when it runs.** A
  manifest row, a check, or a test that names a file a later task creates will
  fail from the moment it lands. If a task needs a file, either it creates it or
  a earlier task does. This produced the Task 2 manifest defect and Task 6's
  skips.
- **In the shim, `die` and `exec` are the only ways out.** Every failure routes
  through `die` (exit 2). `${var:?}` exits 1, a failing pipeline exits its own
  status, and a failed `exec` exits 126/127 — all of which escape the shared
  table and read to CI as something other than a block.
- **A mode given to a file-*creating* call is masked by the umask; a mode given
  to `Chmod` is not.** So a test fixture that asserts a mode must `Chmod` after
  creating it — `os.WriteFile(p, b, 0o640)` yields `0600` under `umask 077`, and
  the test then fails on a machine that is not yours. Product code wanting an
  exact mode must `Chmod` too; product code wanting a umask-respecting default
  must *not*. Three separate instances of this appeared in Task 9 alone, two of
  them in tests written to pin other fixes. **Run the suite under `umask 077` as
  well as your own** before claiming it passes.
- A `pre-commit` hook runs `agents guard --staged`. It should pass. If it blocks, report BLOCKED with its output rather than using `--no-verify`.

## File Structure

| File | Responsibility |
|---|---|
| `bootstrap` | Shell shim: find or install Go, build to a cache, exec |
| `bootstrap.d/main.go` | Verb and profile parsing, phase dispatch, exit codes |
| `bootstrap.d/internal/change/` | `Interface`, `Applier`, `Planner`, `Refusal`. **Owns all I/O** |
| `bootstrap.d/internal/manifest/` | `Kind`, `Row`, parsing, platform filter, duplicate detection |
| `bootstrap.d/internal/phase/` | The six phases. Pure logic over `change.Interface` |
| `bootstrap.d/internal/check/` | The eight checks, shared by the `check` verb and phase 50 |
| `bootstrap.d/internal/migrate/` | Reconciling and reclaiming migrations |
| `bootstrap.d/links.manifest` | The declarative path table |
| `bootstrap.d/Brewfile` | Package manifest, platform-sectioned |
| `fish/config.fish.template` | Seed source for the machine-local stub |

---

### Task 1: The `change` package

**Files:**
- Create: `bootstrap.d/internal/change/change.go`, `applier.go`, `planner.go`
- Test: `bootstrap.d/internal/change/applier_test.go`, `planner_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: everything below. Every later task depends on these exact signatures.

```go
package change

type FileInfo struct {
	Exists    bool
	IsDir     bool   // a real directory, never a symlink to one
	IsLink    bool
	IsRegular bool
}

type Interface interface {
	Lstat(path string) (FileInfo, error)
	Readlink(path string) (string, error)
	LookPath(name string) (string, error)
	ReadFile(path string) ([]byte, error)

	Dir(path string) error
	Link(source, target string) error
	Seed(source, target string) error
	Run(name string, args ...string) error
	Sudo(name string, args ...string) error
}

type Refusal struct{ Path, Problem, Remediation string }

func NewApplier(out io.Writer) *Applier
func NewPlanner(reader Interface, out io.Writer) *Planner
```

- [ ] **Step 1: Write the failing tests**

Create `bootstrap.d/internal/change/applier_test.go`:

```go
package change_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
)

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

	a := change.NewApplier(&bytes.Buffer{})
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
	a := change.NewApplier(&out)
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

	err := change.NewApplier(&bytes.Buffer{}).Link(src, target)
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

	if err := change.NewApplier(&bytes.Buffer{}).Seed(tmpl, target); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	content, _ := os.ReadFile(target)
	if string(content) != "local edits" {
		t.Errorf("Seed overwrote an existing file: %q", content)
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

	err := change.NewApplier(&bytes.Buffer{}).Seed(tmpl, target)
	var refusal *change.Refusal
	if !errorsAs(err, &refusal) {
		t.Fatalf("want *change.Refusal, got %T", err)
	}
	if !strings.Contains(refusal.Remediation, "migrate") {
		t.Errorf("refusal should point at the migration, got %q", refusal.Remediation)
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

	err := change.NewApplier(&bytes.Buffer{}).Dir(target)
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

	a := change.NewApplier(&bytes.Buffer{})
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
```

Add this helper to the same file — the package must not depend on a testing library:

```go
func errorsAs(err error, target any) bool { return errors.As(err, target) }
```

with `"errors"` imported.

Create `bootstrap.d/internal/change/planner_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./...`
Expected: FAIL — `no required module provides package .../internal/change`.

- [ ] **Step 3: Write `change.go`**

```go
// Package change is the only route by which the rest of bootstrap touches the
// machine -- reads included. internal/phase imports this package and nothing
// capable of I/O, which makes "a phase cannot mutate outside dry-run control"
// a property of the import graph rather than of a lexical scan.
package change

import (
	"fmt"
	"io"
	"path/filepath" // pure string manipulation; performs no I/O
)

// FileInfo is the deliberately small view phases get of a path. It answers the
// three questions the placement rule asks -- is it a real directory, a symlink,
// or a regular file -- and nothing else.
type FileInfo struct {
	Exists    bool
	IsDir     bool // a real directory; a symlink to one has IsLink instead
	IsLink    bool
	IsRegular bool
}

type Interface interface {
	Lstat(path string) (FileInfo, error)
	Readlink(path string) (string, error)
	LookPath(name string) (string, error)
	ReadFile(path string) ([]byte, error)

	Dir(path string) error
	Link(source, target string) error
	Seed(source, target string) error
	Run(name string, args ...string) error
	Sudo(name string, args ...string) error
}

// Refusal is "refuse, never clobber": the operation was not performed and
// nothing was changed. Remediation is required -- a refusal that does not tell
// you what to do is a dead end.
type Refusal struct {
	Path        string
	Problem     string
	Remediation string
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("refusing: %s: %s; %s", r.Path, r.Problem, r.Remediation)
}

func refuse(path, problem, remediation string) *Refusal {
	return &Refusal{Path: path, Problem: problem, Remediation: remediation}
}

const moveAside = "move it aside deliberately, then retry"

// classify decides what an existing target means for each kind, so Applier and
// Planner cannot disagree about what counts as a conflict.
type verdict int

const (
	verdictProceed verdict = iota // nothing there; do the work
	verdictSatisfied              // already exactly right; no-op
)

// linkSourceVerdict refuses a link whose source is absent. Without it a
// manifest typo -- or a row referring to a file a later commit will create --
// produces a dangling symlink silently, which is worse than a refusal: the
// machine ends up in a broken state that nothing reports. This masked half of
// a real plan-ordering defect in Task 4.
//
// Unlike a seed, the source is not read, so existence is the whole test.
func linkSourceVerdict(info FileInfo, source string) error {
	if !info.Exists {
		return refuse(source, "link source does not exist",
			"restore it, or correct the manifest row that names it")
	}
	return nil
}

func linkVerdict(info FileInfo, current, want, target string) (verdict, error) {
	switch {
	case info.IsLink && current == want:
		return verdictSatisfied, nil
	case info.IsLink:
		return 0, refuse(target, fmt.Sprintf("points to %q, not %q", current, want), moveAside)
	case info.Exists:
		return 0, refuse(target, "exists and is not a symlink", moveAside)
	}
	return verdictProceed, nil
}

func seedVerdict(info FileInfo, target string) (verdict, error) {
	switch {
	case info.IsLink:
		return 0, refuse(target,
			"must be a machine-local regular file but is a symlink",
			"run './bootstrap migrate', or "+moveAside)
	case info.IsRegular:
		return verdictSatisfied, nil
	case info.Exists:
		return 0, refuse(target, "exists and is not a regular file", moveAside)
	}
	return verdictProceed, nil
}

// seedSourceVerdict decides whether a seed's SOURCE is usable. Applier reads it
// with os.ReadFile and Planner cannot, so both consult this rather than each
// deciding separately -- otherwise plan reports success for a source apply
// cannot read, and the two produce different error types for one condition.
//
// The test is Exists && !IsDir, deliberately not IsRegular: Lstat reports a
// symlinked template as IsLink, and os.ReadFile follows symlinks happily, so an
// IsRegular test would diverge in the opposite direction.
func seedSourceVerdict(info FileInfo, source string) error {
	if !info.Exists {
		return refuse(source, "seed template is missing",
			"restore it, or correct the manifest row that names it")
	}
	if info.IsDir {
		return refuse(source, "seed template is a directory, not a file",
			"correct the manifest row that names it")
	}
	return nil
}

// ancestorConflict refuses when any ancestor of path exists but is not a
// directory. Applier's MkdirAll fails ENOTDIR there with a raw *fs.PathError
// while Planner would happily plan the directory -- the same asymmetry class as
// seedSourceVerdict, one level up the tree. Both implementations consult this.
func ancestorConflict(m Interface, path string) error {
	for dir := filepath.Dir(path); ; {
		info, err := m.Lstat(dir)
		if err != nil {
			return err
		}
		if info.Exists {
			if !info.IsDir {
				return refuse(dir,
					"exists and is not a directory, so it cannot contain "+path,
					moveAside)
			}
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir { // reached the filesystem root
			return nil
		}
		dir = parent
	}
}

func dirVerdict(info FileInfo, target string) (verdict, error) {
	switch {
	case info.IsDir:
		return verdictSatisfied, nil
	case info.Exists:
		return 0, refuse(target, "exists and is not a real directory",
			"run './bootstrap migrate', or "+moveAside)
	}
	return verdictProceed, nil
}

func report(out io.Writer, format string, args ...any) {
	fmt.Fprintf(out, format+"\n", args...)
}
```

- [ ] **Step 4: Write `applier.go`**

```go
package change

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Applier performs operations for real.
type Applier struct{ out io.Writer }

func NewApplier(out io.Writer) *Applier { return &Applier{out: out} }

func (a *Applier) Lstat(path string) (FileInfo, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return FileInfo{}, nil
	}
	if err != nil {
		return FileInfo{}, err
	}
	return FileInfo{
		Exists:    true,
		IsDir:     info.IsDir(),
		IsLink:    info.Mode()&os.ModeSymlink != 0,
		IsRegular: info.Mode().IsRegular(),
	}, nil
}

func (a *Applier) Readlink(path string) (string, error) { return os.Readlink(path) }
func (a *Applier) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (a *Applier) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (a *Applier) Dir(path string) error {
	info, err := a.Lstat(path)
	if err != nil {
		return err
	}
	v, err := dirVerdict(info, path)
	if err != nil || v == verdictSatisfied {
		return err
	}
	if err := ancestorConflict(a, path); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	report(a.out, "ok    created directory %s", path)
	return nil
}

func (a *Applier) Link(source, target string) error {
	info, err := a.Lstat(target)
	if err != nil {
		return err
	}
	var current string
	if info.IsLink {
		if current, err = a.Readlink(target); err != nil {
			return err
		}
	}
	v, err := linkVerdict(info, current, source, target)
	if err != nil || v == verdictSatisfied {
		return err
	}
	if err := a.Dir(filepath.Dir(target)); err != nil {
		return err
	}
	if err := os.Symlink(source, target); err != nil {
		return err
	}
	report(a.out, "ok    linked %s -> %s", target, source)
	return nil
}

func (a *Applier) Seed(source, target string) error {
	info, err := a.Lstat(target)
	if err != nil {
		return err
	}
	v, err := seedVerdict(info, target)
	if err != nil || v == verdictSatisfied {
		return err
	}
	srcInfo, err := a.Lstat(source)
	if err != nil {
		return err
	}
	if err := seedSourceVerdict(srcInfo, source); err != nil {
		return err
	}
	// Read before creating the parent, so a bad template leaves no stray
	// directory behind.
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := a.Dir(filepath.Dir(target)); err != nil {
		return err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return err
	}
	report(a.out, "ok    seeded %s from %s", target, source)
	return nil
}

func (a *Applier) Run(name string, args ...string) error  { return a.run(name, args, false) }
func (a *Applier) Sudo(name string, args ...string) error { return a.run(name, args, true) }

func (a *Applier) run(name string, args []string, elevate bool) error {
	if elevate {
		args = append([]string{name}, args...)
		name = "sudo"
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = a.out
	cmd.Stderr = a.out
	return cmd.Run()
}
```

- [ ] **Step 5: Write `planner.go`**

```go
package change

import (
	"io"
	"path/filepath"
	"strings"
)

// Planner records what would happen and touches nothing.
//
// Queries go through it too, overlaid with its own pending changes, so a link
// into a directory the plan just created sees that directory as present. Plan
// output therefore matches what apply would print, instead of re-reporting the
// same parent directory once per link.
type Planner struct {
	reader  Interface
	out     io.Writer
	pending map[string]FileInfo
	links   map[string]string
}

func NewPlanner(reader Interface, out io.Writer) *Planner {
	return &Planner{
		reader:  reader,
		out:     out,
		pending: map[string]FileInfo{},
		links:   map[string]string{},
	}
}

func (p *Planner) Lstat(path string) (FileInfo, error) {
	if info, ok := p.pending[path]; ok {
		return info, nil
	}
	return p.reader.Lstat(path)
}

func (p *Planner) Readlink(path string) (string, error) {
	if dest, ok := p.links[path]; ok {
		return dest, nil
	}
	return p.reader.Readlink(path)
}

func (p *Planner) LookPath(name string) (string, error) { return p.reader.LookPath(name) }
func (p *Planner) ReadFile(path string) ([]byte, error) { return p.reader.ReadFile(path) }

func (p *Planner) Dir(path string) error {
	info, err := p.Lstat(path)
	if err != nil {
		return err
	}
	v, err := dirVerdict(info, path)
	if err != nil || v == verdictSatisfied {
		return err
	}
	if err := ancestorConflict(p, path); err != nil {
		return err
	}
	// Applier uses MkdirAll, which creates the whole ancestor chain in one
	// call and reports one line. Record every ancestor the plan would bring
	// into existence, or a later Link into a sibling re-plans a directory
	// apply already made silently.
	p.recordAncestors(path)
	p.pending[path] = FileInfo{Exists: true, IsDir: true}
	report(p.out, "plan  create directory %s", path)
	return nil
}

func (p *Planner) recordAncestors(path string) {
	for dir := filepath.Dir(path); ; {
		info, err := p.Lstat(dir)
		if err != nil || info.Exists {
			return
		}
		p.pending[dir] = FileInfo{Exists: true, IsDir: true}
		parent := filepath.Dir(dir)
		if parent == dir { // reached the filesystem root
			return
		}
		dir = parent
	}
}

func (p *Planner) Link(source, target string) error {
	info, err := p.Lstat(target)
	if err != nil {
		return err
	}
	var current string
	if info.IsLink {
		if current, err = p.Readlink(target); err != nil {
			return err
		}
	}
	v, err := linkVerdict(info, current, source, target)
	if err != nil || v == verdictSatisfied {
		return err
	}
	if err := p.Dir(filepath.Dir(target)); err != nil {
		return err
	}
	p.pending[target] = FileInfo{Exists: true, IsLink: true}
	p.links[target] = source
	report(p.out, "plan  link %s -> %s", target, source)
	return nil
}

func (p *Planner) Seed(source, target string) error {
	info, err := p.Lstat(target)
	if err != nil {
		return err
	}
	v, err := seedVerdict(info, target)
	if err != nil || v == verdictSatisfied {
		return err
	}
	// p.Lstat, not the reader, so a template an earlier step planned into
	// existence is honoured.
	srcInfo, err := p.Lstat(source)
	if err != nil {
		return err
	}
	if err := seedSourceVerdict(srcInfo, source); err != nil {
		return err
	}
	// seedSourceVerdict names the two cases worth a remediation message.
	// Everything else -- a dangling symlink, a permission error -- is predicted
	// EXACTLY by performing the same read Applier will and discarding the
	// bytes. Reading is not a mutation, so Planner may do it.
	//
	// This ends a regress rather than narrowing it once more: predicting
	// os.ReadFile from Lstat is approximate by construction, and each fix
	// uncovered a narrower case that still diverged.
	//
	// Safe because a seed source is always a tracked repository file -- the
	// manifest's source column is repo-relative and no plan step creates one --
	// so the overlay can never need to satisfy this read.
	if _, err := p.ReadFile(source); err != nil {
		return err
	}
	if err := p.Dir(filepath.Dir(target)); err != nil {
		return err
	}
	p.pending[target] = FileInfo{Exists: true, IsRegular: true}
	report(p.out, "plan  seed %s from %s", target, source)
	return nil
}

func (p *Planner) Run(name string, args ...string) error {
	report(p.out, "plan  run: %s %s", name, strings.Join(args, " "))
	return nil
}

func (p *Planner) Sudo(name string, args ...string) error {
	report(p.out, "plan  run (sudo): %s %s", name, strings.Join(args, " "))
	return nil
}
```

- [ ] **Step 6: Run to verify it passes**

Run: `cd bootstrap.d && go test ./... && go vet ./...`
Expected: PASS, eleven tests, vet clean.

- [ ] **Step 7: Commit**

```bash
git add bootstrap.d/internal/change
git commit -m "feat(bootstrap): add the change interface with plan and apply

All machine access -- reads included -- goes through one interface with
two implementations, so plan and apply are the same code path by
construction rather than by discipline.

Routing reads through the interface is what lets Planner overlay its own
pending changes: a link into a directory the plan just created sees that
directory as present, so plan output matches what apply would print. The
shell version re-emitted a create-directory line for every link into a
shared parent.

Conflict semantics live in three verdict functions shared by both
implementations, so they cannot disagree about what counts as a conflict."
```

---

### Task 2: The `manifest` package and `links.manifest`

**Files:**
- Create: `bootstrap.d/internal/manifest/manifest.go`, `bootstrap.d/links.manifest`
- Test: `bootstrap.d/internal/manifest/manifest_test.go`

**Interfaces:**
- Consumes: nothing (parses text; the caller supplies bytes).
- Produces:

```go
package manifest

type Kind string
const (KindLink Kind = "link"; KindSeed Kind = "seed"; KindDir Kind = "dir")

type Row struct {
	Kind     Kind
	Source   string // repo-relative; "-" for dir rows
	Target   string // $HOME-relative
	Platform string // "darwin", "linux", or "*"
}

func Parse(data []byte) ([]Row, error)         // error on bad column count or unknown kind
func For(rows []Row, platform string) []Row    // platform filter
func DuplicateTargets(rows []Row) []string     // sorted; call on an already-filtered slice
```

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/internal/manifest/manifest_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./internal/manifest/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write `manifest.go`**

```go
// Package manifest parses links.manifest, the declarative table of paths this
// repository manages. Each row's Kind is spec 1 §8.4's placement rule made
// mechanical: a path another program writes to is seeded, never linked.
package manifest

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strings"
)

type Kind string

const (
	KindLink Kind = "link" // symlink target -> repo source; nothing else writes here
	KindSeed Kind = "seed" // copy once, never overwrite; another program writes here
	KindDir  Kind = "dir"  // a real machine-owned directory
)

type Row struct {
	Kind     Kind
	Source   string // repo-relative; "-" for dir rows
	Target   string // $HOME-relative
	Platform string // "darwin", "linux", or "*"
}

// SyntaxError is a malformed manifest, which is exit code 3 -- not 2. The
// distinction is the whole reason both codes exist: a refusal says the machine
// is in a state bootstrap will not touch, while this says the input is wrong
// and touching nothing was never in question.
type SyntaxError struct {
	Line    int // 0 when the fault is not attributable to one line
	Problem string
}

func (e *SyntaxError) Error() string {
	if e.Line == 0 {
		return "manifest: " + e.Problem
	}
	return fmt.Sprintf("manifest line %d: %s", e.Line, e.Problem)
}

func Parse(data []byte) ([]Row, error) {
	var rows []Row
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 4 {
			return nil, &SyntaxError{line, fmt.Sprintf("%d columns, want 4", len(fields))}
		}
		kind := Kind(fields[0])
		switch kind {
		case KindLink, KindSeed, KindDir:
		default:
			return nil, &SyntaxError{line, fmt.Sprintf("unknown kind %q", fields[0])}
		}
		switch fields[3] {
		case "*", "darwin", "linux":
		default:
			return nil, &SyntaxError{line, fmt.Sprintf("unknown platform %q", fields[3])}
		}
		rows = append(rows, Row{kind, fields[1], fields[2], fields[3]})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func For(rows []Row, platform string) []Row {
	var out []Row
	for _, r := range rows {
		if r.Platform == "*" || r.Platform == platform {
			out = append(out, r)
		}
	}
	return out
}

// DuplicateTargets reports targets claimed more than once. Two owners for one
// path is how softlinks.sh and the Makefile drifted apart.
func DuplicateTargets(rows []Row) []string {
	seen := map[string]int{}
	for _, r := range rows {
		seen[r.Target]++
	}
	var dupes []string
	for target, n := range seen {
		if n > 1 {
			dupes = append(dupes, target)
		}
	}
	sort.Strings(dupes)
	return dupes
}
```

- [ ] **Step 4: Write the manifest**

Create `bootstrap.d/links.manifest`. `git/gitignore_global` and `fish/config.fish.template` do not exist yet — Tasks 7 and 8 create them.

```
# Managed paths. See spec 2 §6.
#
#   kind    what it means                              use when
#   ----    ------------------------------------       -------------------------------
#   link    symlink target -> repo source              nothing else writes to this path
#   seed    copy source -> target ONCE, never again    another program writes here
#   dir     a real machine-owned directory             a program owns the whole directory
#
# Targets are relative to $HOME. Sources are relative to the repository root.
# ~/.gitattributes and core.hooksPath are deliberately absent: git/install-hooks.sh
# owns them. One owner per path.

# kind  source                            target                            platform
link    starship.toml                     .config/starship.toml             *
link    tmux/tmux.conf                    .tmux.conf                        *
link    claude/skills                     .claude/skills                    *
link    gemini/skills                     .gemini/skills                    *
link    macOS/ghostty                     .config/ghostty                   darwin
link    macOS/alacritty/alacritty.toml    .config/alacritty/alacritty.toml  darwin
dir     -                                 .config/fish                      *
seed    git/gitconfig.local.template      .gitconfig                        *
```

**A row and the file it names ship in the same commit.** Declaring a path this
repository does not contain is simply false, and the tool is right to refuse it.
The manifest's own header comment must state that invariant **without naming
plan task numbers** — the manifest ships permanently and a sentence saying "this
row arrives with Task 7" becomes false the moment Task 7 lands, with nothing
scheduled to remove it.

An earlier draft listed `git/gitignore_global` and `fish/config.fish.template`
here, which Tasks 7 and 8 create — so from Task 4 onward `plan` correctly
refused and three Task 3 tests went red. Task 7 adds the fish seed row with the
template; Task 8 adds the gitignore link row with the rename.

- [ ] **Step 5: Run to verify it passes**

Run: `cd bootstrap.d && go test ./... && go vet ./...`
Expected: PASS, six new tests.

- [ ] **Step 6: Commit**

```bash
git add bootstrap.d/internal/manifest bootstrap.d/links.manifest
git commit -m "feat(bootstrap): declare managed paths in links.manifest

Each row names a kind -- link, seed, or dir -- which turns spec 1 §8.4's
placement rule into something a check verifies rather than a convention
somebody has to remember.

Duplicate-target detection is platform-aware: the same target under
different platforms is not a conflict. Parsing rejects unknown kinds and
platforms at load, so a typo fails loudly instead of silently skipping a
managed path."
```

---

### Task 3: The shim, `main.go`, and the preflight phase

**Files:**
- Create: `bootstrap` (executable), `bootstrap.d/main.go`, `bootstrap.d/internal/phase/phase.go`, `preflight.go`
- Test: `bootstrap.d/main_test.go`, `bootstrap.d/internal/phase/preflight_test.go`

**Interfaces:**
- Consumes: `change.Interface`, `change.NewApplier`, `change.NewPlanner`, `manifest.Parse`.
- Produces:

```go
package phase

type Context struct {
	Change   change.Interface
	Root     string // repository root, absolute
	Home     string // absolute
	Platform string // "darwin" | "linux"
	Profile  string // "workstation" | "dotfiles"
	Out      io.Writer
}

type Phase struct {
	Name string
	Run  func(Context) error
}

func All() []Phase          // preflight, packages, config, fish, devtools, verify -- in order
func For(profile string) ([]Phase, error)
```

- [ ] **Step 1: Write the failing tests**

Create `bootstrap.d/main_test.go`:

```go
package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// runShim invokes the real ./bootstrap from an unrelated working directory, so
// a regression to pwd-based root resolution fails immediately.
func runShim(t *testing.T, home string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(filepath.Join(repoRoot(t), "bootstrap"), args...)
	cmd.Dir = t.TempDir()
	// XDG_CACHE_HOME must be redirected too, or an inherited value sends every
	// case into the developer's real ~/.cache and the suite stops being
	// hermetic -- the cache tests would then pass without exercising anything.
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CACHE_HOME="+home+"/cache")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), stderr.String(), code
}

func tempHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home dir")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestHelpExitsZero(t *testing.T) {
	stdout, _, code := runShim(t, tempHome(t), "--help")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	for _, want := range []string{"plan", "apply", "check", "migrate", "workstation", "dotfiles"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help omits %q:\n%s", want, stdout)
		}
	}
}

func TestUnknownVerbIsMalformedInput(t *testing.T) {
	_, stderr, code := runShim(t, tempHome(t), "frobnicate")
	if code != 3 {
		t.Fatalf("exit %d, want 3 (malformed input)", code)
	}
	if !strings.Contains(stderr, "frobnicate") {
		t.Errorf("error should name the verb: %s", stderr)
	}
}

func TestUnknownProfileIsMalformedInput(t *testing.T) {
	_, stderr, code := runShim(t, tempHome(t), "plan", "laptop")
	if code != 3 {
		t.Fatalf("exit %d, want 3", code)
	}
	if !strings.Contains(stderr, "laptop") {
		t.Errorf("error should name the profile: %s", stderr)
	}
}

func TestPlanRunsFromAnyDirectoryAndNamesItsPhases(t *testing.T) {
	stdout, stderr, code := runShim(t, tempHome(t), "plan", "workstation")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"preflight", "packages", "config", "fish", "devtools", "verify"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("workstation plan omits %q:\n%s", want, stdout)
		}
	}
}

func TestDotfilesProfileSkipsPrivilegedPhases(t *testing.T) {
	stdout, stderr, code := runShim(t, tempHome(t), "plan", "dotfiles")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, forbidden := range []string{"packages", "devtools"} {
		if strings.Contains(stdout, forbidden) {
			t.Errorf("dotfiles must not run the %s phase:\n%s", forbidden, stdout)
		}
	}
}

func TestPreflightDeclaresPrivilegeAndNetwork(t *testing.T) {
	stdout, _, code := runShim(t, tempHome(t), "plan", "workstation")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "sudo") || !strings.Contains(stdout, "network") {
		t.Errorf("preflight must declare what needs sudo and network:\n%s", stdout)
	}
}

// The shim must not rebuild when nothing changed.
func TestShimCachesTheBuild(t *testing.T) {
	home := tempHome(t)
	if _, stderr, code := runShim(t, home, "--help"); code != 0 {
		t.Fatalf("first run exit %d: %s", code, stderr)
	}
	_, stderr, code := runShim(t, home, "--help")
	if code != 0 {
		t.Fatalf("second run exit %d: %s", code, stderr)
	}
	if strings.Contains(stderr, "building") {
		t.Errorf("second run rebuilt; the cache is not working: %s", stderr)
	}
}
```

Create `bootstrap.d/internal/phase/preflight_test.go`:

```go
package phase_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/phase"
)

func TestForProfileSelectsPhases(t *testing.T) {
	work, err := phase.For("workstation")
	if err != nil {
		t.Fatal(err)
	}
	if len(work) != 6 {
		t.Errorf("workstation has %d phases, want 6", len(work))
	}
	dots, err := phase.For("dotfiles")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range dots {
		names = append(names, p.Name)
	}
	got := strings.Join(names, ",")
	if got != "preflight,config,verify" {
		t.Errorf("dotfiles phases = %s, want preflight,config,verify", got)
	}
}

func TestForRejectsUnknownProfile(t *testing.T) {
	if _, err := phase.For("laptop"); err == nil {
		t.Fatal("an unknown profile must error")
	}
}

func TestPreflightRefusesWithoutStageZeroTools(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeChange{lookPathErr: map[string]bool{"git": true}}
	ctx := phase.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: "darwin",
		Profile: "workstation", Out: &out,
	}
	err := phase.Preflight(ctx)
	if err == nil {
		t.Fatal("preflight must refuse when a stage-zero tool is missing")
	}
	if !strings.Contains(err.Error(), "git") {
		t.Errorf("refusal should name the tool: %v", err)
	}
}

// fakeChange satisfies change.Interface with no I/O at all. Phase logic is
// tested against this; only the change package touches a real filesystem.
type fakeChange struct {
	info        map[string]change.FileInfo
	links       map[string]string
	files       map[string][]byte
	lookPathErr map[string]bool
	Ops         []string
}

func (f *fakeChange) Lstat(p string) (change.FileInfo, error) { return f.info[p], nil }
func (f *fakeChange) Readlink(p string) (string, error)       { return f.links[p], nil }
func (f *fakeChange) ReadFile(p string) ([]byte, error)       { return f.files[p], nil }
func (f *fakeChange) LookPath(n string) (string, error) {
	if f.lookPathErr[n] {
		return "", errNotFound
	}
	return "/usr/bin/" + n, nil
}
func (f *fakeChange) Dir(p string) error   { f.Ops = append(f.Ops, "dir "+p); return nil }
func (f *fakeChange) Link(s, t string) error {
	f.Ops = append(f.Ops, "link "+t+" -> "+s)
	return nil
}
func (f *fakeChange) Seed(s, t string) error {
	f.Ops = append(f.Ops, "seed "+t+" from "+s)
	return nil
}
func (f *fakeChange) Run(n string, a ...string) error {
	f.Ops = append(f.Ops, "run "+n+" "+strings.Join(a, " "))
	return nil
}
func (f *fakeChange) Sudo(n string, a ...string) error {
	f.Ops = append(f.Ops, "sudo "+n+" "+strings.Join(a, " "))
	return nil
}

var errNotFound = errors.New("not found")
```

with `"errors"` imported.

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./...`
Expected: FAIL — `internal/phase` does not exist; `main_test.go` fails because `./bootstrap` is absent.

- [ ] **Step 3: Write `internal/phase/phase.go`**

```go
// Package phase holds the six provisioning phases.
//
// It imports NO package capable of I/O -- not os, not os/exec. Everything it
// does to the machine goes through change.Interface, which is what makes
// "a phase cannot mutate outside dry-run control" a property of the import
// graph rather than of a lexical scan. An architecture test enforces this.
package phase

import (
	"fmt"
	"io"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
)

type Context struct {
	Change   change.Interface
	Root     string
	Home     string
	Platform string
	Profile  string
	Out      io.Writer
}

func (c Context) logf(format string, args ...any) {
	fmt.Fprintf(c.Out, format+"\n", args...)
}

type Phase struct {
	Name string
	Run  func(Context) error
}

func All() []Phase {
	return []Phase{
		{"preflight", Preflight},
		{"packages", Packages},
		{"config", Config},
		{"fish", Fish},
		{"devtools", Devtools},
		{"verify", Verify},
	}
}

// dotfilesPhases is the narrow profile: no sudo, no network, no package
// manager, no login-shell change. That is what makes it safe in a container.
var dotfilesPhases = map[string]bool{"preflight": true, "config": true, "verify": true}

func For(profile string) ([]Phase, error) {
	switch profile {
	case "workstation":
		return All(), nil
	case "dotfiles":
		var out []Phase
		for _, p := range All() {
			if dotfilesPhases[p.Name] {
				out = append(out, p)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown profile %q; expected workstation or dotfiles", profile)
}
```

- [ ] **Step 4: Write `internal/phase/preflight.go` and the five stubs**

```go
package phase

import "fmt"

func Preflight(c Context) error {
	// The two load-bearing inputs, checked in the phase whose entire job is
	// checking. HOME unset with XDG_CACHE_HOME set is a normal container shape
	// -- and containers are exactly why the dotfiles profile exists -- which
	// would otherwise resolve every managed path against "/".
	if c.Root == "" {
		return fmt.Errorf("repository root is empty; the shim exports BOOTSTRAP_ROOT")
	}
	if c.Home == "" {
		return fmt.Errorf("HOME is empty; every managed path is resolved against it")
	}

	c.logf("== preflight")
	c.logf("   platform    %s", c.Platform)
	c.logf("   repository  %s", c.Root)
	c.logf("   home        %s", c.Home)
	c.logf("   profile     %s", c.Profile)

	for _, tool := range []string{"git", "awk", "sed"} {
		if _, err := c.Change.LookPath(tool); err != nil {
			return fmt.Errorf("required stage-zero tool %q is not on PATH", tool)
		}
	}

	if c.Profile == "workstation" {
		c.logf("   needs sudo    (login shell change, /etc/shells)")
		c.logf("   needs network (Homebrew, packages, fisher plugins)")
	} else {
		c.logf("   needs neither sudo nor network")
	}
	return nil
}
```

Create the five stubs in their own files, each replaced by a later task —
`packages.go` (Task 12), `config.go` (Task 4), `fish.go` (Task 13),
`devtools.go` (Task 11), `verify.go` (Task 6):

```go
package phase

func Packages(c Context) error { c.logf("== packages (not implemented)"); return nil }
```

- [ ] **Step 5: Write `main.go`**

```go
// Command bootstrap provisions this workstation.
// See docs/superpowers/specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/phase"
)

// Exit codes are spec 1 §6's shared table, so one vocabulary covers both tools
// in this repository.
const (
	exitOK            = 0
	exitAdvisory      = 1
	exitBlock         = 2
	exitMalformed     = 3
	exitNotApplicable = 4
)

const usage = `usage: bootstrap <verb> [argument]

verbs:
  plan  [profile]   show what would change; writes nothing
  apply [profile]   converge this machine
  check [profile]   report whether this machine is healthy
  migrate [name]    run reconciling migrations; list reclaiming ones
                    with a name, run that one migration
  --help            this text

profiles:
  workstation       everything (default): packages, config, shell, devtools
  dotfiles          preflight + config + verify only.
                    No sudo, no network, no package manager, no shell change.

exit codes: 0 ok  1 advisory  2 block  3 malformed input  4 not applicable
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	verb := "--help"
	if len(args) > 0 {
		verb = args[0]
	}
	arg := ""
	if len(args) > 1 {
		arg = args[1]
	}

	switch verb {
	case "--help", "-h", "help":
		fmt.Fprint(stdout, usage)
		return exitOK
	case "plan", "apply":
		return runProfile(verb, orDefault(arg, "workstation"), stdout, stderr)
	case "check":
		return runCheck(orDefault(arg, "workstation"), stdout, stderr)
	case "migrate":
		return runMigrate(arg, stdout, stderr)
	}
	fmt.Fprintf(stderr, "bootstrap: unknown verb %q; try --help\n", verb)
	return exitMalformed
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func platform() (string, error) {
	switch runtime.GOOS {
	case "darwin", "linux":
		return runtime.GOOS, nil
	}
	return "", fmt.Errorf("unsupported operating system %q", runtime.GOOS)
}

// root is the repository root. BOOTSTRAP_ROOT is set by the shim; the fallback
// walks up from the executable. Never pwd.
func root() (string, error) {
	if r := os.Getenv("BOOTSTRAP_ROOT"); r != "" {
		return r, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(exe)), nil
}

func runProfile(verb, profile string, stdout, stderr io.Writer) int {
	phases, err := phase.For(profile)
	if err != nil {
		fmt.Fprintf(stderr, "bootstrap: %v\n", err)
		return exitMalformed
	}
	plat, err := platform()
	if err != nil {
		fmt.Fprintf(stderr, "bootstrap: %v\n", err)
		return exitNotApplicable
	}
	repoRoot, err := root()
	if err != nil {
		fmt.Fprintf(stderr, "bootstrap: %v\n", err)
		return exitBlock
	}

	applier := change.NewApplier(stdout)
	var machine change.Interface = applier
	if verb == "plan" {
		machine = change.NewPlanner(applier, stdout)
	}

	ctx := phase.Context{
		Change: machine, Root: repoRoot, Home: os.Getenv("HOME"),
		Platform: plat, Profile: profile, Out: stdout,
	}
	for _, p := range phases {
		if err := p.Run(ctx); err != nil {
			// A Refusal carries a remediation; surfacing it on its own line is
			// the entire reason the type has that field. Everything else prints
			// plainly.
			var refusal *change.Refusal
			if errors.As(err, &refusal) {
				fmt.Fprintf(stderr, "bootstrap: %s: refusing: %s\n  problem: %s\n  remedy:  %s\n",
					p.Name, refusal.Path, refusal.Problem, refusal.Remediation)
				return exitBlock
			}
			fmt.Fprintf(stderr, "bootstrap: %s: %v\n", p.Name, err)
			// A malformed manifest is bad INPUT, not a refused machine. A
			// wrapping script must be able to tell "fix your typo" from
			// "bootstrap declined to touch this box".
			var syntax *manifest.SyntaxError
			if errors.As(err, &syntax) {
				return exitMalformed
			}
			return exitBlock
		}
	}
	return exitOK
}
```

`runCheck` and `runMigrate` arrive in Tasks 6 and 9. Until then, define them as:

```go
func runCheck(string, io.Writer, io.Writer) int  { return exitNotApplicable }
func runMigrate(string, io.Writer, io.Writer) int { return exitNotApplicable }
```

- [ ] **Step 6: Write the shim**

Create `bootstrap` at the repo root, `chmod +x`:

```bash
#!/usr/bin/env bash
#
# Stage zero, and the only shell in this design. Its whole job is to reach Go
# and hand over -- it does no reconciliation and has no dry-run mode to keep
# honest, which is why it can be shell when nothing else is.
#
# See docs/superpowers/specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md §2.1

set -euo pipefail

# die and exec are the ONLY ways out of this script. Exit 2 is "block" in the
# shared table; 1 is "advisory", which is how a CI job keying off codes would
# read a hard stop. It is defined first so every later failure has somewhere to
# go -- an earlier draft defined it below the root resolution, which therefore
# exited 1.
die() {
	printf 'bootstrap: %s\n' "$1" >&2
	exit 2
}

# CDPATH= and -- are load-bearing. Without them `cd` searches CDPATH for a
# relative $0 and echoes the directory it resolved to, so BOOTSTRAP_ROOT can
# silently become a same-named decoy directory AND arrive as two lines. That is
# the "wrong tree" failure this whole redesign exists to prevent; it was
# reproduced against the previous version of this file.
BOOTSTRAP_ROOT=$(CDPATH= cd -- "$(dirname "$0")" && pwd) ||
	die "cannot resolve the repository root from '$0'"
export BOOTSTRAP_ROOT

src=$BOOTSTRAP_ROOT/bootstrap.d

# Validate the OUTCOME, not each input to it. With `dirname` off PATH the
# substitution yields "", and `cd -- ""` SUCCEEDS on bash 3.2.57 -- so
# BOOTSTRAP_ROOT silently becomes the caller's cwd. (5.x rejects a null
# directory, so the existing || die catches it there.) The hole is therefore
# live precisely on the macOS system bash, which is the only bash on an
# unprovisioned machine -- the machine this shim exists for.
#
# Both conditions are needed and neither is redundant:
#   -d  catches an incomplete checkout: right tree, sources missing.
#   -ef catches a DIFFERENT checkout: -d passes, and without this the shim
#       builds and runs the other tree with exit 0 and no symptom at all.
# The second is the shim's half of "never exec a binary you cannot attribute
# to the current checkout".
[ -d "$src" ] ||
	die "'$BOOTSTRAP_ROOT' does not look like a dotfiles checkout: $src is missing"
[ "$BOOTSTRAP_ROOT/bootstrap" -ef "$0" ] ||
	die "'$BOOTSTRAP_ROOT' is not the checkout this script belongs to"

# The cache is keyed on the checkout. Without the key, a main clone and a git
# worktree share one binary, and whichever built first wins -- old code against
# a new tree, silently.
key=$(printf '%s' "$BOOTSTRAP_ROOT" | cksum | tr -cd '0-9') ||
	die "cannot derive a cache key; cksum and tr must be on PATH"

# An explicit check, not ${HOME:?...}: a :? failure exits 1, measured on bash
# 3.2.57 and 5.x. Exit 1 is "advisory" in the shared table, so a container with
# neither variable set would report a hard stop as a soft warning.
if [ -z "${XDG_CACHE_HOME:-}" ] && [ -z "${HOME:-}" ]; then
	die "neither XDG_CACHE_HOME nor HOME is set; there is nowhere to cache the build"
fi
cache=${XDG_CACHE_HOME:-$HOME/.cache}/dotfiles-bootstrap/$key
binary=$cache/bootstrap

# No auto-install: see spec §2.1. A helper called as go_bin=$(find_go) would
# swallow the failure, because set -e does not apply inside $( ). A plain
# assignment in an if-condition does not have that problem.
if ! go_bin=$(command -v go); then
	case "$(uname -s)" in
		Darwin) hint='brew install go   (Homebrew itself: https://brew.sh)' ;;
		Linux)  hint='sudo apt-get install -y golang-go   (or: sudo pacman -S go)' ;;
		*)      hint='install Go from https://go.dev/dl/' ;;
	esac
	die "Go is required and was not found.
  $hint
Then re-run this command."
fi

needs_build=0
if [ ! -x "$binary" ]; then
	needs_build=1
else
	# Directories are included so a DELETED source counts: removing a .go file
	# updates its parent's mtime but leaves every surviving file older.
	sources=$(find "$src" \( -name '*.go' -o -name 'go.mod' -o -type d \) -print) ||
		die "cannot scan $src for sources"
	while IFS= read -r file; do
		[ -n "$file" ] || continue
		if [ "$file" -nt "$binary" ]; then
			needs_build=1
			break
		fi
	done <<EOF
$sources
EOF
fi

if [ "$needs_build" -eq 1 ]; then
	printf 'bootstrap: building\n' >&2
	mkdir -p "$cache" || die "cannot create the build cache at $cache"
	( cd -- "$src" && "$go_bin" build -trimpath -o "$binary" . ) ||
		die "the build failed; the compiler output is above"
fi

# `exec cmd || die` does NOT work: a failed execve terminates the shell before
# the || arm can run -- measured as exit 1 on bash 3.2.57 and 126/127 on 5.3.15,
# with and without `execfail`. Check before exec instead.
[ -x "$binary" ] || die "the built binary at $binary is missing or not executable"
exec "$binary" "$@"
```

- [ ] **Step 7: Run to verify it passes**

Run: `chmod +x bootstrap && cd bootstrap.d && go test ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add bootstrap bootstrap.d/main.go bootstrap.d/main_test.go bootstrap.d/internal/phase
git commit -m "feat(bootstrap): add the shim, dispatcher, and preflight

The shim is stage zero and the only shell in the design: find Go, or
install it via Homebrew, or refuse with the exact command for the
platform. It then builds bootstrap.d to a cache and execs it, rebuilding
only when a source file is newer.

Building from the checkout rather than installing a release means the
binary always matches the tree you cloned -- no version coordination, and
no dependency on the unwritten spec 5.

Tests run ./bootstrap from an unrelated working directory, so a
regression to pwd-based root resolution fails immediately."
```

---

### Task 4: The config phase

**Files:**
- Modify: `bootstrap.d/internal/phase/config.go` (replaces the Task 3 stub)
- Test: `bootstrap.d/internal/phase/config_test.go`

**Interfaces:**
- Consumes: `manifest.Parse`, `manifest.For`, `manifest.DuplicateTargets`, `change.Interface`, the `fakeChange` from Task 3.
- Produces: `phase.Config(Context) error`. Reads the manifest via `c.Change.ReadFile(filepath.Join(c.Root, "bootstrap.d", "links.manifest"))` — note `path/filepath` is pure string manipulation and is permitted in `internal/phase`; it performs no I/O.

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/internal/phase/config_test.go`:

```go
package phase_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/phase"
)

func configCtx(t *testing.T, manifestBody string) (*fakeChange, phase.Context, *bytes.Buffer) {
	t.Helper()
	fake := &fakeChange{
		info:  map[string]change.FileInfo{},
		links: map[string]string{},
		files: map[string][]byte{
			filepath.Join("/repo", "bootstrap.d", "links.manifest"): []byte(manifestBody),
		},
		lookPathErr: map[string]bool{},
	}
	out := &bytes.Buffer{}
	return fake, phase.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: "darwin",
		Profile: "workstation", Out: out,
	}, out
}

func TestConfigAppliesEveryKind(t *testing.T) {
	fake, ctx, _ := configCtx(t,
		"link  starship.toml  .config/starship.toml  *\n"+
			"dir   -              .config/fish           *\n"+
			"seed  tmpl           .gitconfig             *\n")
	if err := phase.Config(ctx); err != nil {
		t.Fatalf("Config: %v", err)
	}
	want := []string{
		"link /home/.config/starship.toml -> /repo/starship.toml",
		"dir /home/.config/fish",
		"seed /home/.gitconfig from /repo/tmpl",
	}
	if strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\nwant:\n%s", strings.Join(fake.Ops, "\n"), strings.Join(want, "\n"))
	}
}

func TestConfigSkipsOtherPlatforms(t *testing.T) {
	fake, ctx, _ := configCtx(t,
		"link  a  keep  darwin\nlink  b  skip  linux\n")
	if err := phase.Config(ctx); err != nil {
		t.Fatal(err)
	}
	if len(fake.Ops) != 1 || !strings.Contains(fake.Ops[0], "keep") {
		t.Errorf("expected only the darwin row, got %v", fake.Ops)
	}
}

func TestConfigRefusesDuplicateTargets(t *testing.T) {
	_, ctx, _ := configCtx(t,
		"link  one  .config/x  *\nlink  two  .config/x  *\n")
	err := phase.Config(ctx)
	if err == nil {
		t.Fatal("two owners for one path must be refused")
	}
	if !strings.Contains(err.Error(), "more than once") {
		t.Errorf("error should explain: %v", err)
	}
}

func TestConfigRejectsUnparseableManifest(t *testing.T) {
	_, ctx, _ := configCtx(t, "hardlink  a  b  *\n")
	if err := phase.Config(ctx); err == nil {
		t.Fatal("an unknown kind must be rejected")
	}
}

// A refusal must stop the run, not be logged and skipped.
func TestConfigStopsAtTheFirstRefusal(t *testing.T) {
	fake, ctx, _ := configCtx(t,
		"link  a  first   *\nlink  b  second  *\n")
	fake.failOn = "first"
	err := phase.Config(ctx)
	if err == nil {
		t.Fatal("expected the refusal to propagate")
	}
	for _, op := range fake.Ops {
		if strings.Contains(op, "second") {
			t.Errorf("rows after a refusal must not be processed: %v", fake.Ops)
		}
	}
}
```

Extend `fakeChange` in `preflight_test.go` with the failure seam:

```go
	failOn string // when a mutation's target contains this, return a Refusal
```

and at the top of `Dir`, `Link` and `Seed`:

```go
	if f.failOn != "" && strings.Contains(t, f.failOn) {
		return &change.Refusal{Path: t, Problem: "test", Remediation: "test"}
	}
```

(for `Dir`, the parameter is `p`).

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./internal/phase/...`
Expected: FAIL — the stub `Config` records no ops.

- [ ] **Step 3: Implement**

Replace `bootstrap.d/internal/phase/config.go`:

```go
package phase

import (
	"fmt"
	"path/filepath"

	"github.com/nilbot/dotfiles/bootstrap/internal/manifest"
)

// Config reconciles every applicable row of links.manifest.
func Config(c Context) error {
	c.logf("== config")

	path := filepath.Join(c.Root, "bootstrap.d", "links.manifest")
	data, err := c.Change.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	rows, err := manifest.Parse(data)
	if err != nil {
		return err
	}
	rows = manifest.For(rows, c.Platform)

	if dupes := manifest.DuplicateTargets(rows); len(dupes) > 0 {
		// A SyntaxError, not a plain one: two owners for one path is malformed
		// input (exit 3), not a refusal to touch the machine (exit 2).
		return &manifest.SyntaxError{
			Problem: fmt.Sprintf("these targets are claimed more than once: %v", dupes),
		}
	}

	for _, row := range rows {
		target := filepath.Join(c.Home, row.Target)
		source := filepath.Join(c.Root, row.Source)
		switch row.Kind {
		case manifest.KindLink:
			err = c.Change.Link(source, target)
		case manifest.KindSeed:
			err = c.Change.Seed(source, target)
		case manifest.KindDir:
			err = c.Change.Dir(target)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd bootstrap.d && go test ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add bootstrap.d/internal/phase
git commit -m "feat(bootstrap): reconcile the manifest in the config phase

Phase logic is tested against a fake change.Interface -- no temp HOME, no
filesystem, no subprocess -- because the phase is a pure function over
that interface. The change package's own tests cover real I/O.

A refusal propagates and stops the run. In the shell version this needed
process substitution to avoid a subshell swallowing the exit."
```

---

### Task 5: Invariant enforcement

Three guards for invariants earlier tasks established. Grouped because they are
one job — making this design's stated properties fail the build when violated —
and because none is large enough to gate on its own.

**Files:**
- Test: `bootstrap.d/architecture_test.go` (new)
- Test: `bootstrap.d/main_test.go` (two added cases)
- Modify: `bootstrap` (route the last three exits through `die`)
- Modify: `bootstrap.d/internal/change/` (`ancestorConflict`)

**Interfaces:**
- Consumes: the packages built so far.
- Produces: `ancestorConflict(m Interface, path string) error` in `change`.

**Fix 4 — the last plan/apply asymmetry: `ancestorConflict`.** Call the shared
function specified in Task 1's `change.go` section from **both** `Applier.Dir`
and `Planner.Dir`, after the verdict and before acting. `change.go` gains
`path/filepath`.

*Corrected diagnosis, from implementation.* An earlier draft of this paragraph
named a **regular-file** ancestor as the diverging case. That was wrong: there,
both paths fail identically at `Lstat` with `ENOTDIR`, so they already agree and
`ancestorConflict` never runs. **The real asymmetry is a dangling-symlink
ancestor**, and closing it also requires `Applier.Lstat` to report `ENOTDIR` as
not-exists — a change which, shipped alone, would introduce a *new* asymmetry.
Both halves are needed together.

*Deliberate behaviour change.* `ancestorConflict` also refuses an ancestor that
is a symlink to a directory, which `MkdirAll` would have followed — so a machine
with, say, `~/.config` symlinked elsewhere is now refused rather than silently
provisioned through the link. This is consistent with `dirVerdict`, which
already refuses a symlink at a `dir` target, and it fails loudly with a
remediation rather than quietly. Distinguishing a symlink-to-directory from a
dangling one would require a following `Stat` on `Interface`; that is not worth
adding for a case with no live instance, and the refusal names the fix.

**Fix 5 — the shim's last three escapes.** `die` moves to the top of the file
so it exists before anything can fail; then the root resolution, the `cksum`
key derivation, and `exec` itself each route through it. All three currently
exit 1, the pipeline's status, or 126/127. The exact text is in Task 3's shim
section, already updated.

**Guard 2 — a `CDPATH` regression test.** The shim's `CDPATH= cd --` fix was
verified by hand and never landed in the suite, so nothing stops it regressing.
Add a case that exports a poisoned `CDPATH` pointing at a directory containing
a decoy named like the repository's own basename, invokes the shim through a
**relative** `$0` (the vulnerable shape — an absolute path never consults
`CDPATH`), and asserts the run succeeds and preflight reports the real
repository root. Mutation-test it: with `CDPATH=` removed from the shim it must
fail.

**Guard 3 — the shim's single exit path.** Add a case asserting `bootstrap`
contains no `exit` outside the `die` function body and no `${var:?}` expansion.
This is a lexical check, which Task 5's own architecture test rejects as a
method for the phase files — say why it is acceptable here in the test's
comment: the shim is sixty lines, fixed in shape, and has exactly two intended
ways out, so the check is exhaustive rather than approximate.

- [ ] **Step 1: Write the architecture test**

Create `bootstrap.d/architecture_test.go`:

```go
package main_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The dry-run invariant, enforced exactly.
//
// internal/phase reaches the machine only through change.Interface. If it
// could import os or os/exec it could mutate while the user asked for a plan,
// and no behavioural test would necessarily catch it.
//
// The shell version could only approximate this by scanning for command names
// like "rm" at statement starts -- a heuristic that was written wrong twice.
// An import set is exact.
func TestPhasePackageCannotPerformIO(t *testing.T) {
	forbidden := map[string]bool{
		"os": true, "os/exec": true, "io/fs": true, "net": true,
		"net/http": true, "os/user": true, "syscall": true,
		"path/filepath/walk": true,
	}
	// path/filepath is pure string manipulation and is allowed; io is allowed
	// for the Writer interface. Neither can reach the filesystem on its own.

	dir := filepath.Join("internal", "phase")
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue // tests may use os to build fixtures
			}
			for _, imp := range file.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if forbidden[path] {
					t.Errorf("%s imports %q; phases must reach the machine only "+
						"through change.Interface", name, path)
				}
			}
		}
	}
}

// Only the change package may import the I/O primitives it wraps.
func TestOnlyChangeImportsOSExec(t *testing.T) {
	fset := token.NewFileSet()
	for _, dir := range []string{
		filepath.Join("internal", "manifest"),
		filepath.Join("internal", "phase"),
	} {
		pkgs, err := parser.ParseDir(fset, dir, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, pkg := range pkgs {
			for name, file := range pkg.Files {
				if strings.HasSuffix(name, "_test.go") {
					continue
				}
				for _, imp := range file.Imports {
					path, _ := strconv.Unquote(imp.Path.Value)
					if path == "os/exec" {
						t.Errorf("%s imports os/exec; only internal/change may", name)
					}
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run it**

Run: `cd bootstrap.d && go test ./... -run Architecture`
Expected: PASS. If it fails, a phase imported an I/O package — fix the phase, never the test.

- [ ] **Step 3: Commit**

```bash
git add bootstrap.d/architecture_test.go
git commit -m "test(bootstrap): enforce the dry-run invariant by import set

internal/phase reaches the machine only through change.Interface, so the
invariant the whole design rests on is a fact about the import graph.

This replaces the shell version's lexical scan for mutating command
names, which was written wrong twice: once flagging the correct
'do_run mkdir' form, once missing a real '; do cp' violation. A guarantee
enforced by a heuristic its author cannot write correctly is not a
guarantee."
```

---

### Task 6: The `check` package, the `check` verb, and the verify phase

**Files:**
- Create: `bootstrap.d/internal/check/check.go`, `checks.go`
- Modify: `bootstrap.d/main.go` (`runCheck`), `bootstrap.d/internal/phase/verify.go`
- Test: `bootstrap.d/internal/check/check_test.go`, `bootstrap.d/main_test.go`

**Interfaces:**
- Consumes: `change.Interface`, `manifest.Parse`/`For`/`DuplicateTargets`.
- Produces:

```go
package check

type Status string
const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
	NA   Status = "n/a"
)

type Result struct {
	Status Status
	Name   string // stable identifier; tests assert on these
	Detail string
}

// Context mirrors phase.Context but is declared here: phase imports check for
// the verify phase, so the dependency must run one way only.
type Context struct {
	Change   change.Interface
	Root     string
	Home     string
	Platform string
	Profile  string
}

func All(c Context) []Result
func ExitCode(results []Result) int // 0 ok, 1 any Warn, 2 any Fail
func Write(w io.Writer, results []Result)
```

`internal/check` is subject to the same import restriction as `internal/phase`:
no I/O package. Extend Task 5's architecture test to cover it.

**The eight checks**, with the exact `Name` values tests assert on:

| Name | Checks | Profile |
|---|---|---|
| `platform` | reports the detected platform | both |
| `manifest-owners` | no target claimed twice | both |
| `manifest-kinds` | every row's target present and of the declared kind | both |
| `fish-source` | `~/.config/fish/config.fish` contains a line matching `^source .*/fish/config\.fish$` | both |
| `gitconfig-include` | `~/.gitconfig` contains `git/gitconfig.shared` | both |
| `login-shell` | `$SHELL` ends in `fish` and that path is in `/etc/shells` | workstation |
| `agents` | `agents` resolves on `PATH` | workstation |
| `packages` | every `Brewfile` entry installed | workstation |

Three rules the tests must pin:

1. **Under the `dotfiles` profile, `login-shell`, `agents` and `packages`
   report `NA`, not `Fail`.** They cover state that profile deliberately does
   not manage; treating them as failures makes every container run report three
   false problems.
2. **`packages` reports `Fail` with "the packages phase has not run" when
   `bootstrap.d/Brewfile` is absent** — Task 12 creates it, so between here and
   there the file genuinely does not exist and handing a missing path to
   `brew bundle check` would produce a confusing error instead of a clear one.
3. **`fish-source` and `gitconfig-include` are the two silent-total-failure
   guards.** If the fish stub loses its `source` line the entire shared config
   goes dark with no error; if `~/.gitconfig` stops including the shared file
   every shared git setting vanishes. Both exist *because* this design
   introduces those failure modes.

**`ExitCode` maps to the shared table**: any `Fail` → `2`, else any `Warn` →
`1`, else `0`. `NA` never affects the code.

**A malformed manifest must exit `3` from `check`, exactly as it does from
`apply`.** `All` therefore returns `([]Result, error)`, and `runCheck` maps a
`*manifest.SyntaxError` to `exitMalformed` before consulting `ExitCode`. The
same typo answering `3` to one verb and `2` to another is precisely the
confusion the shared table exists to prevent.

**A check must never be given a `Planner`.** Checking is reading; a `Planner`'s
`Run` is a no-op that returns `nil`, which turns "I could not check this" into
"this is fine" — a silent false pass in the layer whose entire job is catching
silent failures. So `phase.Verify` constructs its own `change.NewApplier` rather
than using `c.Change`. `Verify` performs no mutation either way: every check
reads, and the one subprocess (`brew bundle check`) is a query.

**But `check` must not hold a mutating type either.** Handing checks a live
`Applier` means the layer that runs during `plan` holds an object with `Sudo` on
it, and only convention stops a future check from calling it. `check` therefore
declares its own narrow interface — `Lstat`, `Readlink`, `ReadFile`, `LookPath`,
`Run` — which `change.Interface` satisfies implicitly, so no call site changes
and the invariant is a property of the type rather than of everyone's care.

**Each consumer declares the narrowest interface it needs.** Task 9 adds
`Copy`, `Rename`, `RemoveAll` and `WriteFile` to `change` because the migrations
cannot be written without them — but those four can destroy data that is not in
git, and only `migrate` has any business with them. Without narrowing, adding
them hands every phase a `RemoveAll`, reopening one layer up the hole
`check.Machine` closed.

| Consumer | Interface | Methods |
|---|---|---|
| `check` | `check.Machine` | the 5 read-ish ones |
| `phase` | `phase.Machine` | the 9 non-destructive ones |
| `migrate` | `migrate.Machine` | all 13 |
| `migrate` detection | `migrate.Reader` | the 3 needed to *notice* |

`change.Applier` and `change.Planner` satisfy all of these implicitly, so no
call site changes. This is the design's standing answer to "who may do what": a
capability nobody declares a need for is one nobody can reach.

**The fourth interface is what the narrowing taught.** Preflight must ask
whether a migration is pending, but preflight holds nine methods and running a
migration needs thirteen — so a `Pending` taking the same `Context` as `Run`
would hand every phase back the four destructive methods, defeating the point.
Detection and execution are different capabilities and get different inputs:
`Pending` takes a `Reader`, `Run` takes the full `Machine`. A `Pending` that
grew a `RemoveAll` would not compile, which is stronger than the convention it
replaces.

**A guard that matches a path string must also confirm the path resolves.**
`gitconfig-include` and `fish-source` both read a file and pattern-match a path
out of it. On a clone that is not at `~/dotfiles`, the seeded `~/.gitconfig`
includes a file that does not exist, git ignores it silently, every shared
setting goes inert — and a string-matching guard reports `ok`. That is the exact
failure the guard exists for. Both must `Lstat` the referenced path (expanding a
leading `~` against `Home`) and `Fail` when it is absent.

*Note for Task 8:* `git/gitconfig.local.template` hardcodes `~/dotfiles/…` and
`Seed` copies it verbatim, so a relocated clone seeds a broken include. The
check above turns that from silent into loud; making it *correct* is Task 8's,
which owns the template.

**`phase.Verify` reports but does not exit.** It runs the same `check.All` and
writes the results, then returns `nil` even on `Fail` — an advisory finding at
the end of an `apply` must not look like a failed `apply`. `runCheck` is the one
that exits.

**`Context` carries `Shell`.** The login-shell check needs `$SHELL`, which
nothing else in the context yields; `phase.Context` gains it too, on the
precedent already set by `Home`.

- [ ] **Step 1: Write `check_test.go` against a fake `change.Interface`**

Reuse the `fakeChange` shape from `internal/phase/preflight_test.go` — copy it
into the check package's test file rather than exporting it; forty lines of
duplicated test fixture across two packages is cheaper than a shared testing
package that production code would have to carry.

Cover, at minimum: each of the three `NA` results under `dotfiles`; each of the
same three producing a real verdict under `workstation`; `manifest-kinds`
failing when a `link` target is a regular file and when a `seed` target is a
symlink; `fish-source` failing on a stub without the line; `gitconfig-include`
failing on the pre-rename path; `packages` failing with the Brewfile absent;
and `ExitCode` returning 2/1/0 for the three shapes.

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./internal/check/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement `check.go` and `checks.go`, then wire `runCheck` and `Verify`**

`runCheck` builds a `check.Context` from the environment exactly as
`runProfile` builds a `phase.Context`, calls `check.All`, `check.Write`s to
stdout, and returns `check.ExitCode`.

- [ ] **Step 4: Add the end-to-end cases to `main_test.go`**

`./bootstrap check dotfiles` on a bare `$HOME` exits 1 or 2 and names the
missing rows; after `apply dotfiles` it exits 0. **Add
`t.Skip("unskip in Task 8")` to the cases covering `fish-source` and
`gitconfig-include`** — the manifest has no fish seed row until Task 7 and
`gitconfig.local.template` still names the pre-rename path until Task 8, so
both genuinely fail until then. Task 8 removes the skips.

- [ ] **Step 5: Extend the architecture test**

Add `internal/check` to the packages whose import closure must contain no I/O
package.

- [ ] **Step 6: Verify and commit**

Run: `cd bootstrap.d && go test -count=1 ./... && go vet ./...`, `gofmt -l`.

```bash
git add bootstrap.d/internal/check bootstrap.d/main.go bootstrap.d/main_test.go \
        bootstrap.d/internal/phase/verify.go bootstrap.d/architecture_test.go
git commit -m "feat(bootstrap): add the check verb and verify phase

Three checks report n/a under the dotfiles profile rather than failing:
they cover state that profile deliberately does not manage, so treating
them as failures would make every container run report three false
problems.

fish-source and gitconfig-include exist because this design introduces
two silent total-failure modes -- a fish stub that lost its source line,
and a ~/.gitconfig that stopped including the shared file. A design that
creates a silent failure owes a check for it.

Verify reports and returns nil; only the check verb exits. An advisory
finding at the end of an apply must not look like a failed apply."
```

---

### Task 7: Root substitution in `Seed`, and the fish inversion

**Files:**
- Modify: `bootstrap.d/internal/change/change.go`, `applier.go`, `planner.go`
- Modify: every `NewApplier`/`NewPlanner` call site (`main.go`, `internal/phase/verify.go`, tests)
- Create: `fish/config.fish.template`
- Modify: `fish/config.fish`, `fish/mypre.fish`, `.gitignore`, `bootstrap.d/links.manifest`
- Test: `bootstrap.d/internal/change/applier_test.go`, `bootstrap.d/main_test.go`

**Interfaces:**
- Produces: `change.RootToken = "@DOTFILES_ROOT@"`;
  `NewApplier(out io.Writer, root string) *Applier` and
  `NewPlanner(reader Interface, out io.Writer, root string) *Planner`.

#### Part A — substitution

`Applier.Seed` replaces every occurrence of `change.RootToken` in the template
with the executor's `root` before writing. The executor takes the root at
construction because an executor operating on a machine must know which checkout
it serves; threading it through `Seed`'s arguments would let two call sites
disagree.

`Planner.Seed` performs no substitution — it writes nothing — but must keep
reading the source exactly as it does now. Substitution cannot fail, so this
introduces no plan/apply asymmetry; say that in a comment so the next reader does
not "fix" it.

Tests: a template containing the token seeds a file containing the root and no
token; a template without the token is byte-identical to its source (the
substitution must not disturb ordinary templates); and a re-run does not
re-substitute, because `Seed` never overwrites.

#### Part B — the fish inversion

Create `fish/config.fish.template`:

```fish
# Machine-local fish configuration. NOT tracked by dotfiles.
#
# Shareable settings belong in the tracked config this sources -- edits made
# HERE are invisible to the repository and will not follow you to another
# machine.
#
# This file exists so that installers which append managed blocks to
# config.fish write to a machine-local file instead of into published content.
# fish sources conf.d/*.fish before this file, so anything appended below lands
# last and therefore wins -- the same ordering argument
# git/gitconfig.local.template makes for [include].

source @DOTFILES_ROOT@/fish/config.fish

# --- installer-managed blocks appear below this line ---
```

Rewrite tracked `fish/config.fish` to source its siblings from
`(status dirname)` — so the clone path appears exactly once on a machine, in the
seeded stub — and delete the `# >>> grok installer >>>` block, which belongs in
the machine-local file.

Rewrite the two functions in `fish/mypre.fish` that are built around the old
layout: `install_fisher` reads `(status dirname)/fishfile`, and `fish_reset_all`
targets `$__fish_config_dir` instead of `rm -rf`-ing paths inside the repository.

Delete the five now-pointless `.gitignore` lines: `fish/fish_variables`,
`fish/fish_plugins`, `fish/functions/`, `fish/completions/`, `fish/conf.d/`.

Add the manifest row, in this commit, alongside the template it names:

```
seed    fish/config.fish.template         .config/fish/config.fish          *
```

#### Part C — unskip what this unblocks

Task 6 skipped `TestCheckFindsTheFishSourceLineAfterApply` with
`t.Skip("unskip in Task 8")`. Part B makes it passable **now**: the manifest row
exists and the seeded stub's `source` line resolves. Remove that one skip and
confirm it passes. Leave the `gitconfig-include` skip for Task 8.

- [ ] **Steps:** tests first for Part A, then the substitution; then Part B's
  files; then Part C. Mutation-test the substitution — a `Seed` that silently
  fails to substitute must fail a test, not produce a stub pointing at a
  literal `@DOTFILES_ROOT@`.

```bash
git add bootstrap.d/internal/change bootstrap.d/main.go bootstrap.d/main_test.go \
        bootstrap.d/internal/phase bootstrap.d/links.manifest \
        fish/config.fish.template fish/config.fish fish/mypre.fish .gitignore
git commit -m "refactor(fish): stop installers writing into the repository

~/.config/fish was a symlink to fish/, so fisher wrote functions/,
completions/, conf.d/, fish_plugins and fish_variables into tracked
content, and installers appended managed blocks straight into
config.fish -- the grok block in this diff is one of them.

Per-file symlinks do not fix that: the file that must be tracked is the
same file installers append to. So ~/.config/fish/config.fish becomes a
seeded machine-local stub that sources the tracked config, and the five
.gitignore lines holding back those writes are no longer load-bearing.

Seed now substitutes the checkout path into templates. A template copied
byte-for-byte cannot carry a per-machine fact, which is what the seeded
file exists to hold."
```

---

### Task 8: The git renames, and the Makefile's destructive targets

**Files:**
- Rename: `git/gitconfig.symlink` → `git/gitconfig.shared`;
  `git/gitignore_global.symlink` → `git/gitignore_global`
- Modify: `git/gitconfig.local.template`, `git/gitconfig.shared`, `Makefile`,
  `bootstrap.d/links.manifest`, `bootstrap.d/main_test.go`

**Interfaces:** none. This is repository surgery.

#### Part A — the renames

The `.symlink` suffix stopped carrying information, for two different reasons.
`git/gitconfig.symlink` is *included* by a machine-local `~/.gitconfig`, not
symlinked to it, since `37f00a0`; `git/gitignore_global.symlink` is still
genuinely symlinked, but the manifest's `kind` column now says so.

Use `git mv` for both so the rename is recorded.

**`git/gitconfig.local.template`'s include becomes tokenised**, which is the
payoff for Task 7's substitution:

```
[include]

	path = @DOTFILES_ROOT@/git/gitconfig.shared
```

This is what finally makes the seeded `~/.gitconfig` correct on a checkout that
is not at `~/dotfiles`. Update the surrounding prose too — it names
`~/dotfiles/git/gitconfig.symlink` in at least one comment.

`git/gitconfig.shared` contains a comment describing itself as a symlink target
("if `~/.gitconfig` is a symlink to this file…"). That has been false since
`37f00a0`. Rewrite it to say `~/.gitconfig` is a machine-local file that
*includes* this one.

Add the manifest row in this commit, now that the file it names exists:

```
link    git/gitignore_global              .gitignore                        *
```

Then confirm `grep -rn 'gitconfig\.symlink\|gitignore_global\.symlink' . --exclude-dir=.git --exclude-dir=docs`
is empty.

#### Part B — remove the Makefile's newly destructive targets

Task 7 changed `make fishshell` from harmless to destructive and the window is
open now. `Makefile:94-97` does `rm -rf $(HOME)/.config/fish` then relinks it
into the repository. `rm -rf` does not follow symlinks, so before Task 7 it
deleted a symlink and recreated it — idempotent. After Task 7, on a machine
where that path is a real directory, it destroys the seeded stub, any
installer-appended blocks, and fisher's `functions/`, `completions/`, `conf.d/`,
`fish_plugins` and `fish_variables`. **None of that is in git.**

Delete the `fish`, `fishshell` and `starship` targets outright, and drop `fish`
from `all`'s prerequisites. The config phase owns every path they touched;
Task 15 was going to remove them anyway, and waiting seven more tasks to close a
data-loss path is not a trade worth making.

Leave the rest of the Makefile alone — Task 15 reduces it to the `agents`
target. Update the `dotfiles` target's `gitignore_global.symlink` reference to
the new name so it does not break in the interim.

#### Part C — remove the last Task 6 skip

`TestCheckIsHealthyAfterApply` carries `t.Skip("unskip in Task 8")`. Parts A and
B should make it pass: the template now tokenises the include, `Seed`
substitutes the real checkout, and `gitconfig-include` resolves the result.

**If it does not pass, do not loosen the guard.** Task 6's `resolves` check
exists because a `~/.gitconfig` naming a nonexistent file is the silent failure
this design is built to prevent. Report what you measured instead.

- [ ] **Steps:** Part A, then Part B, then Part C. Verify with
  `cd bootstrap.d && go test -count=1 ./... && go vet ./...` and `gofmt -l`.

```bash
git add git/ Makefile bootstrap.d/links.manifest bootstrap.d/main_test.go
git commit -m "refactor(git): rename gitconfig.symlink to gitconfig.shared

The suffix stopped carrying information, for two different reasons. This
file is included by a machine-local ~/.gitconfig since 37f00a0, not
symlinked to it; gitignore_global is still genuinely symlinked, but the
manifest's kind column now says so.

The include is tokenised, so a checkout anywhere gets a ~/.gitconfig that
actually resolves -- previously it named ~/dotfiles unconditionally and
git ignores a missing include in silence.

Also removes the fish, fishshell and starship Makefile targets. Their
rm -rf used to delete a symlink; since the fish inversion it would
destroy the seeded stub and fisher's machine-local state, none of which
is in git."
```

---

### Task 9: `migrate` — the reconciling migrations

**Files:**
- Create: `bootstrap.d/internal/migrate/migrate.go`, `migrations.go`
- Modify: `bootstrap.d/main.go` (`runMigrate`), `bootstrap.d/internal/phase/preflight.go`
- Test: `bootstrap.d/internal/migrate/migrate_test.go`, `bootstrap.d/main_test.go`

**Interfaces:**

```go
package migrate

type Kind string
const (
	Reconciling Kind = "reconciling" // moves or rewrites; runs from a bare `migrate`
	Reclaiming  Kind = "reclaiming"  // destroys untracked data; must be NAMED
)

type Migration struct {
	Name    string
	Kind    Kind
	Pending func(Context) (bool, error)
	Run     func(Context) error
}

type Context struct {
	Change change.Interface
	Root   string
	Home   string
	Out    io.Writer
}

func All() []Migration
func Pending(c Context) ([]Migration, error)
func Run(c Context, name string) error // "" runs every reconciling one and lists reclaiming ones
```

`internal/migrate` imports no I/O package, same rule as `phase` and `check`;
extend the architecture test.

**Three reconciling migrations.** Task 10 adds the one reclaiming migration.

1. **`fish`** — `~/.config/fish` is a symlink into the checkout. Move fisher's
   generated state (`fish_variables`, `fish_plugins`, `functions/`,
   `completions/`, `conf.d/`) out of the checkout into a real
   `~/.config/fish`, then replace the symlink. **Copy before removing**, so an
   interrupted run leaves the old state intact — none of it is in git and none
   of it is recoverable.

   *Carried from Task 7's review:* the migration must **not** leave a copy of
   the old tracked `config.fish` at `~/.config/fish/config.fish`. `Seed` never
   overwrites, so it would skip the stub. The failure is guarded rather than
   silent — that file's `source (status dirname)/alias.fish` does not match
   `fishSourceLine`, so `fish-source` reports `fail` — but do not create it.

2. **`gitconfig`** — `~/.gitconfig` includes the pre-rename
   `git/gitconfig.symlink`. Rewrite that one line to the resolved
   `<root>/git/gitconfig.shared`, preserving every other line: the file
   accumulates real machine-local settings that `git config --global` wrote.

3. **`gitignore`** — `~/.gitignore` is a symlink to the pre-rename
   `git/gitignore_global.symlink`. Retarget it to `<root>/git/gitignore_global`.
   Added because Task 8's rename makes `apply` refuse this path on every
   existing machine; the refusal is correct but the remedy should not be manual
   when the other two renames get one.

**Preflight refuses when any migration is pending**, naming
`./bootstrap migrate`. A bare refusal from deep inside the config phase explains
nothing; preflight is where the machine's shape is assessed.

**Detection must be precise.** A migration whose `Pending` returns true on an
already-migrated machine will run twice; one that returns false on a machine
needing it leaves `apply` refusing forever. Each `Pending` gets a test for both
directions.

- [ ] **Steps:** tests first, then the package, then wire `runMigrate` and
  preflight. **Mutation-test every `Pending`** — invert it and confirm a test
  fails.

```bash
git add bootstrap.d/internal/migrate bootstrap.d/main.go bootstrap.d/main_test.go \
        bootstrap.d/internal/phase bootstrap.d/architecture_test.go
git commit -m "feat(bootstrap): add the reconciling migrations

Three exist: move fisher's state out of the checkout, repoint a
~/.gitconfig that includes the pre-rename path, and retarget a
~/.gitignore symlink that names it.

The fish migration copies before it removes, so an interrupted run leaves
the old state intact -- fish_variables and the installed plugin set are
not in git and cannot be recovered if lost.

Preflight refuses when a migration is pending and names the remedy,
rather than letting apply hit a refusal that explains nothing."
```

---

### Task 10: `migrate` — the reclaiming kind

**Files:**
- Modify: `bootstrap.d/internal/migrate/migrations.go`, `migrate.go`
- Test: `bootstrap.d/internal/migrate/migrate_test.go`, `bootstrap.d/main_test.go`

**Interfaces:** no new types. `migrate.Reclaiming` and the listing mechanism
already exist from Task 9 and are already tested with a fake; this task supplies
the first real one.

**One reclaiming migration: `mambaforge`.** It removes `~/sdk/mambaforge`,
measured at 3.5 GB on 2026-08-10. Python environments are managed by `uv` now;
`micromamba` is not on `PATH`, so the mamba block in `fish/mypost.fish` is
already inert, and the directory's four environments are named by Python version
alone (`3_9`–`3_12`) — exactly what `uv python install` provides.

**The kind is the whole point.** A reclaiming migration destroys data that is
not in git and cannot be recovered, so:

- It **never** runs from a bare `./bootstrap migrate`. That verb lists it with
  the exact command and performs nothing.
- It runs only when named: `./bootstrap migrate mambaforge`.
- **Preflight does not refuse when it is pending.** Task 9 already filters
  preflight to `Reconciling` only, and the reason is sharper here: a reclaiming
  migration is pending until someone deliberately runs it, possibly forever, so
  refusing on it would deadlock `apply` on a machine that is otherwise perfectly
  healthy.

**It refuses if anything on `PATH` still resolves inside the directory** —
`conda`, `mamba`, `micromamba`, `python`, `python3`, `pip`. Deleting 3.5 GB out
from under a live toolchain is the failure this guard exists for, and the check
is cheap. Use `LookPath` and compare against the resolved target.

Tests: pending both directions; a bare `migrate` lists it and deletes nothing
(assert the directory still exists); the named form removes it; the `PATH` guard
refuses with the tool named and **deletes nothing**. Mutation-test the guard by
removing it and confirming the refusal test fails.

**Do not run this against the repo owner's real `~/sdk/mambaforge`.** It exists,
it is 3.5 GB, and it is not in git. Every test constructs its own tree under a
temp `HOME`.

```bash
git add bootstrap.d/internal/migrate bootstrap.d/main_test.go
git commit -m "feat(bootstrap): add the reclaiming migrations, starting with mambaforge

Reclaiming migrations destroy untracked data irreversibly, so a bare
migrate lists them with the exact command and runs none of them. Nothing
has to be rediscovered later, and a routine invocation can never delete
anything.

mambaforge refuses if any of conda, mamba, micromamba, python, python3 or
pip still resolves inside the directory."
```

---

### Task 11: The devtools phase

**Files:**
- Modify: `bootstrap.d/internal/phase/devtools.go` (replaces the Task 3 stub)
- Test: `bootstrap.d/internal/phase/devtools_test.go`

**Interfaces:** `phase.Devtools(Context) error`. No new types.

Three steps, in order:

1. **`uv`** — if `LookPath("uv")` succeeds, log and skip. Otherwise
   `Run("brew", "install", "uv")`.
2. **Build `agents`** — `Dir(<home>/bin)`, then build the sibling Go module to
   `<home>/bin/agents`. The build needs a working directory, and `phase` has no
   way to `cd`; use `Run("go", "build", "-C", <root>/agents, "-trimpath", "-o", <binary>, ".")`.
   `go build -C` sets the directory without a shell and without the executor
   needing a cwd concept. Verify `-C` is accepted by the installed Go before
   relying on it; if not, report the measurement rather than reaching for
   `sh -c`.
3. **Global git hooks** — **delegate**, do not reimplement:
   `Run("bash", <root>/git/install-hooks.sh, "install", <root>, <home>, <binary>)`.

**The delegation is the point of this phase's test.** `git/install-hooks.sh`
already validates `~/.gitconfig`, links `~/.gitattributes`, symlinks the four
hook names, and writes `core.hooksPath` last so a partial install cannot
activate an incomplete directory — and it has its own tests in the `agents`
module. This phase's test asserts **the invocation and its arguments**, not hook
installation. Re-testing the installer here would duplicate coverage that
already exists in the module that owns it.

Use the fake `Machine` to capture `Ops`; assert the three steps appear in order
with exact arguments, and that `uv` is skipped when `LookPath` succeeds.

**A test seam is permitted for the build.** Compiling the `agents` module in a
unit test is slow and needs a real toolchain; `BOOTSTRAP_SKIP_BUILD` already
exists from an earlier draft. If you use it, the seam must be visible in the
phase's own output so a skipped build is never silent.

```bash
git add bootstrap.d/internal/phase
git commit -m "feat(bootstrap): add the devtools phase

Git hooks delegate to git/install-hooks.sh rather than being
reimplemented, so this phase's test asserts the invocation and its
arguments. That installer validates the global config, links the
attributes file, symlinks four hook names and writes core.hooksPath last
so a partial install cannot activate an incomplete directory -- and it is
already tested in the module that owns it."
```

---

### Task 12: The packages phase and the Brewfile

**Files:**
- Modify: `bootstrap.d/internal/phase/packages.go` (replaces the Task 3 stub)
- Create: `bootstrap.d/Brewfile`
- Delete: `super-install-dep.sh`, `user-install-dep.sh`, `brew/`
- Test: `bootstrap.d/internal/phase/packages_test.go`

**Interfaces:** `phase.Packages(Context) error`. No new types.

**Stage zero installs only what Homebrew itself needs; one Brewfile covers
everything above it on both platforms.** Per-distro native lists — what
`super-install-dep.sh` does today — mean two or three manifests kept in sync
across distros whose package names disagree (`fd` vs `fd-find`, `bat` vs
`batcat`).

Order:

1. **Linux only** — `Sudo("apt-get", "update")` then
   `Sudo("apt-get", "install", "-y", "build-essential", "curl", "file", "git")`,
   or `Sudo("pacman", "-S", "--needed", "--noconfirm", "base-devel", "curl", "file", "git")`.
   Choose by `LookPath`. Refuse, naming both, if neither is present.
   **On darwin this step does not run at all.**
2. **Homebrew** — if `LookPath("brew")` succeeds, log and skip. Otherwise run
   its official installer. This is the **only** place this design executes
   remote code; it runs solely when `brew` is absent, and it is skipped entirely
   in plan mode because `Planner.Run` records rather than executes.
3. **`Run("brew", "bundle", "--file", <root>/bootstrap.d/Brewfile)`.**

**The Brewfile is an audit, not a translation** of `brew/brew-*.list`:

- **`micromamba` goes** — superseded by `uv`, and Task 10 reclaims its install.
- **`youtube-dl` goes** — unmaintained since 2021; `yt-dlp` is the live fork.
- **`gitleaks` is added** — spec 1's `agents guard --staged` shells out to it and
  blocks the commit without it, and nothing ever declared it.
- **`uv` is added** — the devtools phase requires it.
- **Casks are guarded by `OS.mac?`.** An unguarded `cask` line aborts the whole
  bundle on Linux.
- `font-symbols-only-nerd-font` replaces the vendored `install-font-linux.sh`
  that Task 14 removes.

```ruby
brew "bazelisk"
brew "boost"
brew "fish"
brew "go"
brew "node"
brew "rustup-init"
brew "tmux"
brew "yarn"

brew "bat"
brew "bingrep"
brew "btop"
brew "diff-so-fancy"
brew "dua-cli"
brew "fd"
brew "fzf"
brew "git-delta"
brew "git-interactive-rebase-tool"
brew "gitleaks"
brew "jql"
brew "jump"
brew "lsd"
brew "procs"
brew "re2"
brew "ripgrep"
brew "starship"
brew "tokei"
brew "uv"
brew "yt-dlp"
brew "zstd"

if OS.mac?
  cask "macfuse"
  cask "mactex"
  cask "font-symbols-only-nerd-font"
end
```

**Tests drive the fake `Machine` only.** Assert: darwin skips stage zero
entirely; linux with `apt-get` produces the apt ops and not pacman, and vice
versa; neither present refuses naming both; `brew` present skips the installer;
`brew bundle` is always last and names the Brewfile by absolute path. Also
assert the Brewfile parses as the audit describes — `gitleaks` and `uv` present,
`micromamba` and `youtube-dl` absent, every `cask` inside the `OS.mac?` guard.

**Then delete `super-install-dep.sh`, `user-install-dep.sh` and `brew/`** in the
same commit. Their content is now here. For the record, all three were broken:
`user-install-dep.sh` read `brew/brew-cask.list` while the file was
`brew-casks.list`, called the long-removed `brew cask install`, and installed
Homebrew from the retired ruby `master`-branch URL.

```bash
git add bootstrap.d/internal/phase bootstrap.d/Brewfile
git rm super-install-dep.sh user-install-dep.sh
git rm -r brew/
git commit -m "feat(bootstrap): unify packages on Homebrew for both platforms

The native package manager now installs only Homebrew's own
prerequisites; one Brewfile covers everything above that on macOS and
Linux alike.

Replaces super-install-dep.sh and user-install-dep.sh, which were broken
three ways: user-install-dep.sh read brew/brew-cask.list while the file
was brew-casks.list, called the long-removed 'brew cask install', and
installed Homebrew from the retired ruby master-branch URL.

Audit: micromamba and youtube-dl removed; gitleaks and uv added, both
required by tooling that had never declared them."
```

---

### Task 13: The fish phase

**Files:**
- Modify: `bootstrap.d/internal/phase/fish.go` (replaces the Task 3 stub)
- Modify: `bootstrap.d/internal/phase/phase.go` (add `Context.User`)
- Modify: `bootstrap.d/main.go` (supply `User`)
- Modify: `bootstrap.d/main_test.go` (stub `fish` alongside the stub `brew`)
- Modify: `fish/mypre.fish` (pin the fisher installer URL)
- Test: `bootstrap.d/internal/phase/fish_test.go`

**Interfaces:**
- Consumes: `Context` (`Change`, `Root`, `Shell`, `Out`), `Machine.LookPath`,
  `Machine.ReadFile`, `Machine.Sudo`, `Machine.Run`.
- Produces: `func Fish(c Context) error`; `Context.User string`.

**Why this phase exists at all.** The Makefile's `chsh` was guarded by
`sudo -v || if [ -z $? ]`, which skips on *both* branches and so has never run.
Corroborated on this machine: fish is the login shell, and `/etc/shells`
contains no fish entry at all — the shell was changed by hand at some point and
the Makefile never did it. This phase adds the `/etc/shells` entry *before*
changing the shell, and changes nothing when the login shell is already fish.

**Two hazards specific to this task.**

1. **No test may execute `fish`, `chsh`, `sudo` or `tee`.** Earlier in this
   project an agent ran `fish` without `--no-config`, which loaded the owner's
   real configuration and triggered a live `fisher install` that reinstalled
   four plugins at upstream HEAD. Every test here drives `fakeChange`.
2. **`sudo chsh -s <shell>` with no user argument changes *root's* shell.** The
   login name is therefore not optional, and `internal/phase` cannot read the
   passwd database or the environment. That is what `Context.User` is for.

- [ ] **Step 1: Add `Context.User`**

In `internal/phase/phase.go`, directly below the existing `Shell` field:

```go
	// User is the current user's login name, supplied by main exactly as Shell
	// is. `sudo chsh -s <shell>` with no user argument changes ROOT's login
	// shell, so the name is not optional -- and nothing in this package can
	// reach the passwd database or the environment on its own.
	User string
```

In `main.go`, add `"os/user"` to the imports, add this helper, and set
`User: currentUser()` in **both** `phase.Context` literals (the apply/plan one
and the one the `check` verb builds) so they cannot drift:

```go
// currentUser prefers the passwd database over $USER. $USER is inherited and
// can name someone else entirely -- after `sudo -E`, or a container started
// with `docker exec -u` -- and the login shell being changed belongs to the
// account this process actually runs as, not to whoever exported the variable.
func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}
```

- [ ] **Step 2: Write the failing test**

Create `bootstrap.d/internal/phase/fish_test.go`. `fakeChange.LookPath` returns
`"/usr/bin/" + name`, so `/usr/bin/fish` is the resolved path throughout.

```go
package phase_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/phase"
)

func fishContext(fake *fakeChange, out *bytes.Buffer, shell string) phase.Context {
	return phase.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: "darwin",
		Profile: "workstation", Shell: shell, User: "someone", Out: out,
	}
}

func TestFishRegistersTheShellThenChangesItThenInstallsPlugins(t *testing.T) {
	fake := &fakeChange{}
	var out bytes.Buffer
	if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		`sudo /bin/sh -c printf '%s\n' "$1" >> /etc/shells sh /usr/bin/fish`,
		"sudo chsh -s /usr/bin/fish someone",
		"run fish --no-config -c source $argv[1]/fish/mypre.fish; install_fisher /repo",
	}
	if strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\n\nwant:\n%s",
			strings.Join(fake.Ops, "\n"), strings.Join(want, "\n"))
	}
}

// The order is load-bearing, not cosmetic: chsh refuses a shell that is not
// listed in /etc/shells, so a run that changed the shell first would fail on
// exactly the fresh machine this phase exists for.
func TestFishAddsTheShellsEntryBeforeChangingTheShell(t *testing.T) {
	fake := &fakeChange{}
	var out bytes.Buffer
	if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	shells, chsh := -1, -1
	for i, op := range fake.Ops {
		if strings.Contains(op, "/etc/shells") {
			shells = i
		}
		if strings.Contains(op, "chsh") {
			chsh = i
		}
	}
	if shells == -1 || chsh == -1 {
		t.Fatalf("both steps must appear: %v", fake.Ops)
	}
	if shells > chsh {
		t.Errorf("chsh at %d precedes the /etc/shells entry at %d; chsh refuses "+
			"an unlisted shell, so this order fails on a fresh machine", chsh, shells)
	}
}

// The path is passed as $1 and never interpolated into the script text, so a
// prefix containing a quote, a space or $(...) cannot change what root runs.
func TestFishPassesTheShellPathAsAnArgumentNotInsideTheScript(t *testing.T) {
	fake := &fakeChange{}
	var out bytes.Buffer
	if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var op string
	for _, o := range fake.Ops {
		if strings.Contains(o, "/etc/shells") {
			op = o
		}
	}
	if !strings.Contains(op, `printf '%s\n' "$1" >> /etc/shells`) {
		t.Errorf("the script must use the positional $1: %s", op)
	}
	if !strings.HasSuffix(op, `>> /etc/shells sh /usr/bin/fish`) {
		t.Errorf("the path must follow the script as $0 and $1: %s", op)
	}
}

func TestFishSkipsTheShellsEntryWhenItIsAlreadyListed(t *testing.T) {
	fake := &fakeChange{files: map[string][]byte{
		"/etc/shells": []byte("# comment\n/bin/sh\n/bin/bash\n/usr/bin/fish\n"),
	}}
	var out bytes.Buffer
	if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range fake.Ops {
		if strings.Contains(op, "/etc/shells") {
			t.Errorf("must not append an entry that is already present: %s", op)
		}
	}
	if !strings.Contains(out.String(), "already") {
		t.Errorf("the skip must be visible in the phase's output:\n%s", out.String())
	}
}

// An unreadable /etc/shells is not an absent entry. Appending a duplicate line
// is inert; omitting a missing one makes chsh refuse.
func TestFishAddsTheEntryWhenShellsCannotBeRead(t *testing.T) {
	fake := &fakeChange{readFileErr: map[string]bool{"/etc/shells": true}}
	var out bytes.Buffer
	if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, op := range fake.Ops {
		if strings.Contains(op, "/etc/shells") {
			found = true
		}
	}
	if !found {
		t.Errorf("an unreadable /etc/shells must not be read as 'already listed': %v", fake.Ops)
	}
}

func TestFishSkipsChshWhenTheLoginShellIsAlreadyFish(t *testing.T) {
	for _, shell := range []string{"/opt/homebrew/bin/fish", "/usr/local/bin/fish"} {
		fake := &fakeChange{}
		var out bytes.Buffer
		if err := phase.Fish(fishContext(fake, &out, shell)); err != nil {
			t.Fatalf("%s: unexpected error: %v", shell, err)
		}
		for _, op := range fake.Ops {
			if strings.Contains(op, "chsh") {
				t.Errorf("%s: must not plan a shell change: %s", shell, op)
			}
		}
	}
}

func TestFishRefusesWhenTheLoginNameIsUnknown(t *testing.T) {
	fake := &fakeChange{}
	var out bytes.Buffer
	ctx := fishContext(fake, &out, "/bin/bash")
	ctx.User = ""
	err := phase.Fish(ctx)
	if err == nil {
		t.Fatal("an empty login name must refuse: `sudo chsh -s <shell>` with no " +
			"user argument changes root's shell")
	}
	for _, op := range fake.Ops {
		if strings.Contains(op, "chsh") {
			t.Errorf("no chsh may be recorded without a login name: %s", op)
		}
	}
}

func TestFishRefusesWhenFishIsAbsentAndDoesNothing(t *testing.T) {
	fake := &fakeChange{lookPathErr: map[string]bool{"fish": true}}
	var out bytes.Buffer
	err := phase.Fish(fishContext(fake, &out, "/bin/bash"))
	if err == nil {
		t.Fatal("want a refusal when fish is not installed")
	}
	if !strings.Contains(err.Error(), "packages phase") {
		t.Errorf("the refusal must point at the phase that installs fish: %v", err)
	}
	if len(fake.Ops) != 0 {
		t.Errorf("nothing may run before fish is known to exist: %v", fake.Ops)
	}
}

// --no-config is not tidiness. Sourcing the owner's configuration to install
// plugins runs whatever that configuration does; this invocation needs exactly
// one function from one tracked file and asks for nothing else.
func TestFishInstallsPluginsWithoutLoadingTheUsersConfiguration(t *testing.T) {
	fake := &fakeChange{}
	var out bytes.Buffer
	if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := fake.Ops[len(fake.Ops)-1]
	if !strings.HasPrefix(last, "run fish --no-config -c ") {
		t.Errorf("the plugin install must be last and must use --no-config: %s", last)
	}
	if !strings.Contains(last, "$argv[1]") {
		t.Errorf("the root must reach fish as $argv[1], not interpolated: %s", last)
	}
	if !strings.HasSuffix(last, " /repo") {
		t.Errorf("the resolved root must be the trailing argument: %s", last)
	}
}

func TestFishStopsAtTheFirstFailure(t *testing.T) {
	for _, tc := range []struct{ name, failOn string }{
		{"shells entry", "/bin/sh"},
		{"chsh", "chsh"},
		{"fisher", "fish --no-config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeChange{failOn: tc.failOn}
			var out bytes.Buffer
			if err := phase.Fish(fishContext(fake, &out, "/bin/bash")); err == nil {
				t.Fatal("want the refusal to propagate, got nil")
			}
			for _, op := range fake.Ops {
				if strings.Contains(op, tc.failOn) {
					t.Errorf("the failing operation must not be recorded: %s", op)
				}
			}
		})
	}
}

// git.io is GitHub's retired URL shortener. Measured 2026-08-11: it still
// answers 301 to
// https://raw.githubusercontent.com/jorgebucaran/fisher/HEAD/functions/fisher.fish
// -- so this is hardening, not a repair. Provisioning a new machine should not
// route through a service its owner has already announced the end of.
func TestMypreNamesTheFisherInstallerDirectly(t *testing.T) {
	body, err := os.ReadFile("../../../fish/mypre.fish")
	if err != nil {
		t.Fatalf("the fish phase invokes install_fisher from this file: %v", err)
	}
	if strings.Contains(string(body), "git.io") {
		t.Error("git.io is a retired shortener; name the raw URL it redirects to")
	}
	if !strings.Contains(string(body),
		"https://raw.githubusercontent.com/jorgebucaran/fisher/HEAD/functions/fisher.fish") {
		t.Error("install_fisher must fetch the installer from its canonical URL")
	}
}
```

`fakeChange` needs one new seam, alongside the existing `lstatErr`:

```go
	// readFileErr makes a path UNREADABLE rather than empty, the same
	// distinction lstatErr draws. fakeChange.ReadFile otherwise returns
	// (nil, nil) for an absent path, which would let "/etc/shells cannot be
	// read" and "/etc/shells lists nothing" be the same case -- and they call
	// for opposite behaviour.
	readFileErr map[string]bool
```

```go
func (f *fakeChange) ReadFile(p string) ([]byte, error) {
	if f.readFileErr[p] {
		return nil, errPermission
	}
	return f.files[p], nil
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd bootstrap.d && go test ./internal/phase/ -run 'Fish|Mypre' -v`
Expected: FAIL. The stub is
`func Fish(c Context) error { c.logf("== fish (not implemented)"); return nil }`,
so it records no operations at all — the failures should read as empty `ops:`
lists and "nothing may run" assertions, not as mismatched arguments.

- [ ] **Step 4: Implement the phase**

Replace `bootstrap.d/internal/phase/fish.go`:

```go
package phase

import (
	"fmt"
	"path/filepath"
	"strings"
)

// appendShell runs under sh rather than this process because the redirection
// has to happen with root's privileges: `sudo tee -a` would need a stdin that
// change.Applier does not give a command. The path arrives as $1 and is never
// interpolated into the script text, so a prefix containing a quote, a space or
// $(...) cannot change what root runs.
const appendShell = `printf '%s\n' "$1" >> /etc/shells`

// Fish makes fish the login shell and installs the plugin set.
//
// Plugins are installed explicitly here rather than by a shell-start side
// effect: a provisioning step that only happens if you happen to open a shell
// is not a provisioning step.
//
// Like the packages phase, this one refuses under `plan` on a machine that does
// not have fish yet. That is the same accepted limitation, for the same reason:
// `plan` cannot preview a machine whose packages phase has not run, because
// Planner records a command without performing it and nothing in Machine
// reports which happened.
func Fish(c Context) error {
	c.logf("== fish")

	fishPath, err := c.Change.LookPath("fish")
	if err != nil {
		return fmt.Errorf("fish is not on PATH; the packages phase installs it -- " +
			"run './bootstrap apply workstation'")
	}
	c.logf("   fish        %s", fishPath)

	if err := registerShell(c, fishPath); err != nil {
		return err
	}
	if err := loginShell(c, fishPath); err != nil {
		return err
	}

	// fish expands $argv itself, so the root never enters a command string that
	// something else parses first.
	return c.Change.Run("fish", "--no-config", "-c",
		"source $argv[1]/fish/mypre.fish; install_fisher", c.Root)
}

// registerShell adds fish to /etc/shells. chsh refuses a shell that is not
// listed there, so this runs first.
func registerShell(c Context, fishPath string) error {
	body, err := c.Change.ReadFile("/etc/shells")
	if err != nil {
		// Unreadable is not absent. A duplicate line in /etc/shells is inert; a
		// missing one makes chsh refuse, so the append is the safe answer to not
		// knowing.
		c.logf("   /etc/shells unreadable (%v); adding the entry regardless", err)
	} else {
		for _, line := range strings.Split(string(body), "\n") {
			if strings.TrimSpace(line) == fishPath {
				c.logf("   /etc/shells already lists %s", fishPath)
				return nil
			}
		}
	}
	c.logf("   /etc/shells adding %s", fishPath)
	return c.Change.Sudo("/bin/sh", "-c", appendShell, "sh", fishPath)
}

func loginShell(c Context, fishPath string) error {
	if filepath.Base(c.Shell) == "fish" {
		c.logf("   chsh        login shell is already fish (%s)", c.Shell)
		return nil
	}
	if c.User == "" {
		return fmt.Errorf("cannot change the login shell: neither the passwd " +
			"database nor $USER names the current user, and `sudo chsh` without " +
			"a name would change root's shell")
	}
	c.logf("   chsh        %s -> %s for %s", c.Shell, fishPath, c.User)
	return c.Change.Sudo("chsh", "-s", fishPath, c.User)
}
```

In `fish/mypre.fish`, replace the `git.io` URL inside `install_fisher` with the
URL it redirects to, keeping `HEAD` (fisher's own documented form, meaning the
default branch):

```fish
    curl -sL https://raw.githubusercontent.com/jorgebucaran/fisher/HEAD/functions/fisher.fish | source && fisher install jorgebucaran/fisher $plugins
```

In `bootstrap.d/main_test.go`, extend the stub directory Task 12 added so it
provides `fish` as well as `brew`. `plan workstation` reaches this phase and
`Planner.LookPath` consults the real PATH, so without this the shim cases pass
or fail according to whether the developer happens to have fish installed.
`Planner` executes nothing, so the stub is never run — only resolved.

- [ ] **Step 5: Run to verify it passes**

Run: `cd bootstrap.d && go test -count=1 ./...`
Expected: PASS, whole suite. Then run it again under `umask 077`.

Confirm with recording sentinels — `fish`, `chsh`, `sudo`, `tee`, `curl` first
on PATH, each appending its argv to a log and exiting 127 — that the suite
invokes none of them, and report the log.

- [ ] **Step 6: Commit**

```bash
git add bootstrap.d/internal/phase/fish.go bootstrap.d/internal/phase/fish_test.go \
        bootstrap.d/internal/phase/phase.go bootstrap.d/internal/phase/preflight_test.go \
        bootstrap.d/main.go bootstrap.d/main_test.go fish/mypre.fish
git commit -m "feat(bootstrap): add the fish phase

The Makefile's chsh was guarded by 'sudo -v || if [ -z \$? ]', which skips
on both branches and so has never run -- corroborated by fish being the
login shell while /etc/shells contains no fish entry at all. This phase
adds the entry before changing the shell, because chsh refuses a shell
that is not listed, and changes nothing when the login shell is already
fish.

Plugins install explicitly rather than through a shell-start side effect.
The invocation passes --no-config: installing plugins does not need the
owner's configuration, and sourcing it would run whatever it does.

install_fisher now names the fisher installer's canonical URL. git.io
still redirects there today, but it is GitHub's retired shortener and
provisioning should not depend on it."
```

---

### Task 14: The removal campaign

**Files:**
- Delete: `zsh/`, `tools/`, `miniforge/`, `bin/`, `spacemacs/`, `gnupg/`,
  `macOS/iterm2/`, `git/hooks/go.pre-commit`, `snapshot.sh`, `recover.sh`,
  `mountcrypt.sh`, `mountsshfs.sh`, `post-install.sh`, `softlinks.sh`,
  `install-font-linux.sh`
- Modify: `fish/mypost.fish`, `fish/alias.fish`
- Test: `bootstrap.d/removals_test.go`

**Interfaces:**
- Consumes: `repoRoot(t)` from `bootstrap.d/main_test.go`.
- Produces: nothing. This task is pure deletion.

**Counts measured on the tree, not estimated:** `zsh/` is 344 tracked files;
`tools/`, `bin/`, `miniforge/`, `spacemacs/`, `gnupg/` and `macOS/iterm2/` are
20 between them.

**One commit per group, and each message names its own recovery command.** The
message must carry the *parent's* short sha, not `HEAD~1` — `HEAD~1` is read
relative to whatever HEAD is when someone later reads the log, which after eight
removal commits points at the wrong tree. Capture it first:

```bash
parent=$(git rev-parse --short HEAD)
```

and write that value into the message.

**Known consequence, sequenced deliberately:** `Makefile:37-38` name
`./softlinks.sh` and `spacemacs/dotspacemacs`, so `make dotfiles` is broken
between this task's commits and Task 15's, which deletes those lines. Task 12
left `make dep` broken the same way. Both are retired targets in a Makefile
Task 15 reduces to one target; leaving them momentarily broken is cheaper than
editing a file twice.

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/removals_test.go`. It uses `repoRoot(t)`, the helper
`main_test.go` already provides — there is no `newFixture` in this module.

```go
package bootstrap_test

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
	for _, name := range []string{"mypost.fish", "alias.fish", "mypre.fish", "config.fish"} {
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run 'Removed|NoFishConfig|Mypost'`
Expected: FAIL — every listed path still exists, and `mypost.fish` and
`alias.fish` still name mamba. `TestMypostKeepsTheMojoBlock` passes from the
start; that is correct, it is a guard against overshooting, not a red test.

- [ ] **Step 3: Remove, one commit per group**

```bash
parent=$(git rev-parse --short HEAD)
git rm -r -q zsh/
git commit -m "chore(remove): drop zsh

344 tracked files, mostly vendored oh-my-zsh themes and plugins. The login
shell has been fish for years and ~/.oh-my-zsh no longer exists on this
machine. Recover with: git show $parent:zsh/<path>"
```

```bash
parent=$(git rev-parse --short HEAD)
git rm -r -q tools/
git commit -m "chore(remove): drop tools/

Four scripts for etcd, protoc and a mysql compose file, two of them
#!/usr/bin/env zsh, unreferenced by anything and untouched since 2021.
Recover with: git show $parent:tools/<path>"
```

Before the miniforge commit, edit the two fish files. In `fish/mypost.fish`
delete lines 1–19 — the commented-out `# >>> conda initialize >>>` block and
the live `# >>> mamba initialize >>>` block — leaving the mojo block as the
file's only content. In `fish/alias.fish` delete this branch:

```fish
if type -q (command -v micromamba)
    alias mamba=micromamba
    alias conda=micromamba
end
```

```bash
parent=$(git rev-parse --short HEAD)
git rm -r -q miniforge/
git add fish/mypost.fish fish/alias.fish
git commit -m "chore(remove): drop miniforge/ and the mamba wiring

Python environments are managed by uv now. The mamba block in
fish/mypost.fish was live but inert: it tested /opt/homebrew/bin/micromamba,
which the packages phase no longer installs. The alias branch was dead on
arrival -- 'type -q (command -v micromamba)' passes command -v's OUTPUT to
type, so it tested a path as if it were a command name.

~/sdk/mambaforge itself (3.5 GB, untracked) is reclaimed separately by
'./bootstrap migrate mambaforge'. Recover with: git show $parent:miniforge/<path>"
```

```bash
parent=$(git rev-parse --short HEAD)
git rm -q snapshot.sh recover.sh mountcrypt.sh mountsshfs.sh post-install.sh
git commit -m "chore(remove): drop unreferenced personal scripts

snapshot.sh and recover.sh use BSD-only 'date -j' and an rclone remote
from 2021. mountcrypt.sh points at the Intel /usr/local encfs path.
mountsshfs.sh has an inverted -d guard and a host that no longer exists.
post-install.sh is not valid bash at all: 'bash -n' reports a syntax
error at line 9 from its empty then-branches.

None is referenced by anything. Recover with: git show $parent:<name>"
```

```bash
parent=$(git rev-parse --short HEAD)
git rm -q softlinks.sh install-font-linux.sh
git commit -m "chore(remove): drop softlinks.sh and the vendored font installer

softlinks.sh is superseded by the config phase, which carries its
alacritty and ghostty links as manifest rows -- and resolves the repo root
from the executable rather than pwd, which softlinks.sh got wrong.

install-font-linux.sh is an 8.9 KB copy of nerd-fonts' installer pinned to
a 2019 commit and referenced by nothing. On macOS the Brewfile's
font-symbols-only-nerd-font replaces it. On LINUX IT REPLACES NOTHING:
that entry is a cask, casks do not exist on Linux, and Homebrew publishes
no formula for the font. A Linux workstation has no managed nerd font
after this commit -- install it by hand until spec 5 gives fonts an owner.

Recover with: git show $parent:<name>"
```

```bash
parent=$(git rev-parse --short HEAD)
git rm -r -q bin/
git commit -m "chore(remove): drop bin/

Checked rather than assumed: rgr.bin invokes 'e' with ripgrep's flags,
which resolves to /usr/local/plan9/bin/e -- Plan 9's editor -- so it
silently runs the wrong program. git-chdate.bin single-quotes its
--env-filter body, so \$hash and \$proper never expand and it exports
empty dates; it also uses deprecated git filter-branch and BSD-only
'date -v'. git-stats.bin works but is fifteen lines. infernowm.bin
launches a Plan 9 window manager.

They were also linked with the .bin suffix intact, so the commands were
'rgr.bin' and 'git-stats.bin' -- the latter defeating the git- prefix
that would have made it a git subcommand.

Recover with: git show $parent:bin/<name>"
```

```bash
parent=$(git rev-parse --short HEAD)
git rm -r -q spacemacs/ gnupg/ macOS/iterm2/
git commit -m "chore(remove): drop unlinked tracked configuration

spacemacs/dotspacemacs was linked by the editors target, which rm -rf'd
~/.emacs.d and cloned spacemacs afresh -- destructive, and failing on any
second run. gnupg/ and macOS/iterm2/ were linked by nothing at all.

On an existing machine ~/.spacemacs is now a dangling symlink: rm it.
~/.emacs.d, ~/.vim and ~/.tmux/plugins/tpm stay on disk; dropping a
target stops managing a thing, it does not delete it.

Recover with: git show $parent:<path>"
```

```bash
parent=$(git rev-parse --short HEAD)
git rm -q git/hooks/go.pre-commit
git commit -m "chore(remove): drop the Go pre-commit hook

It ran 'go build -n && go test && go fmt && go vet' on every commit in
any repo with .go files at the root, with every command redirected to
/dev/null -- so a failure gave a generic message and no diagnostic.
Worse, 'go fmt' rewrote files mid-commit without staging them, so a
formatting fix silently failed to be committed.

This is CI's job. See spec 5. Recover with: git show $parent:git/hooks/go.pre-commit"
```

- [ ] **Step 4: Run the whole suite**

Run: `cd bootstrap.d && go test -count=1 ./...`
Expected: PASS, all six packages. Then again under `umask 077`.

This is the step that catches a removal something still depends on. If a case
fails, do not delete the case — report what depends on the removed path.

- [ ] **Step 5: Commit the test**

```bash
git add bootstrap.d/removals_test.go
git commit -m "test(bootstrap): pin the removals

Guards against a later commit reintroducing a path this spec retired, and
against fish config regaining a reference to tooling that is no longer
installed. The reasons live in the eight removal commits; nothing else
reads those, so the paths are asserted here."
```

---

### Tasks 13 through 16

The remaining tasks are unchanged in *intent* from the shell plan; only their
implementation language differs. Each follows the same shape: write the failing
test, run it, implement, run it, commit.

| # | Task | Files | Key requirement |
|---|---|---|---|
| 6 | `check` package, `check` verb, verify phase | `internal/check/`, `main.go` | The eight checks of spec §10. Checks 6–8 report **not applicable** under the `dotfiles` profile, not failure. Exit `0`/`1`/`2` per the shared table. Adds five `t.Skip("unskip in Task 8")` for checks that need files Tasks 7–8 create. **`check_packages` must report `fail` with "the packages phase has not run" when `bootstrap.d/Brewfile` is absent** rather than passing a nonexistent path to `brew bundle check` — Task 12 creates it, so between here and there the file genuinely does not exist |
| 7 | Fish inversion | `fish/config.fish.template`, `fish/config.fish`, `fish/mypre.fish`, `.gitignore`, `bootstrap.d/links.manifest` | Stub sources the tracked config; tracked config uses `(status dirname)`; rewrite `install_fisher` and `fish_reset_all` to target `$__fish_config_dir`; delete the five `fish/*` `.gitignore` lines. **Add the manifest row `seed fish/config.fish.template .config/fish/config.fish *` in this commit** — the row and the file it names ship together |
| 8 | Git renames | `git/gitconfig.shared`, `git/gitignore_global`, `git/gitconfig.local.template`, `Makefile`, `bootstrap.d/links.manifest` | Repoint the include; update the self-referential comment; confirm `grep -rn 'gitconfig\.symlink\|gitignore_global\.symlink'` is empty outside docs; remove Task 6's skips. **Add the manifest row `link git/gitignore_global .gitignore *` in this commit**, after the rename creates the file. **See the warning below — repointing alone will not make Task 6's skipped cases pass.** |

#### Seeded templates carry the clone location by substitution

**Decided 2026-08-10 (human).** Templates name the checkout with the token
`@DOTFILES_ROOT@`; `change.Seed` replaces it with the resolved root as it
writes. Task 7 implements the mechanism and is its first consumer; Task 8 is the
second.

This makes spec §7's existing claim true rather than aspirational. §7 already
says the seeded stub *"necessarily names the clone location, and that is
correct: it is machine-local and seeded once, so it is the right place for the
one fact that varies per machine."* A static template copied byte-for-byte
cannot carry a per-machine fact; substitution is what closes that gap.

**Do not loosen Task 6's `resolves` guard to make tests green.** After
substitution the seeded paths are correct and the guard passes honestly. If it
ever fails again, it is reporting a genuinely broken machine — reverting it
restores a guard that cannot detect its own failure mode.

**Known consequence, accepted:** running bootstrap from a git worktree seeds
that worktree's path. Deleting the worktree then breaks the include — loudly,
because the guard resolves the path rather than merely matching it. Provisioning
from an experimental tree is not a supported workflow; the failure is visible
rather than silent, which is the standard this design holds itself to.
| 9 | `migrate`: reconciling | `internal/migrate/`, `main.go`, `preflight.go` | `fish` and `gitconfig` migrations. Fish **copies before removing** so an interrupt leaves the old state intact. Preflight refuses when one is pending and names `bootstrap migrate` |
| 10 | `migrate`: reclaiming | `internal/migrate/` | `mambaforge`. Never runs from a bare `migrate`; bare `migrate` **lists** it with the exact command. Refuses if `conda`, `mamba`, `micromamba`, `python`, `python3` or `pip` resolves inside it |
| 11 | Devtools phase | `internal/phase/devtools.go` | `uv`; build `agents`; **delegate** git hooks to `git/install-hooks.sh` with `<root> <home> <home>/bin/agents` — the binary is `<home>/bin/agents`, NOT `<root>/../bin/agents`, which are only the same path when the checkout is at `~/dotfiles`. Test asserts the invocation, not hook installation |
| 12 | Packages phase + Brewfile | `internal/phase/packages.go`, `Brewfile` | Native stage zero (`build-essential`/`base-devel`) then Homebrew then `brew bundle`. Audit: drop `micromamba` and `youtube-dl`; **add `gitleaks` and `uv`**, both required by tooling that never declared them. Casks guarded by `OS.mac?`. Removes `super-install-dep.sh`, `user-install-dep.sh`, `brew/` |
| 13 | Fish phase | `internal/phase/fish.go` | `/etc/shells` via `tee -a`; `chsh` only when the login shell is not already fish; explicit fisher install. Refuses if fish is absent, naming the packages phase |
| 14 | Removal campaign | many | One commit per group, each naming what was removed and the `git show <sha>:<path>` recovery. Groups: zsh, tools, conda/mamba, stale scripts, softlinks+fonts, bin, spacemacs/gnupg/iterm2, go.pre-commit |
| 15 | Reduce the Makefile | `Makefile` | Keep only the `agents` target; point provisioning at `./bootstrap` |
| 16 | Update the specs | `docs/…/spec-2…md`, `README.md` | Mark implemented; keep the Linux risk honest; add the plan row |

Full task text for 6–16 is generated on demand: the shell plan's corresponding
sections (commits `0974c7b`, `1dbbfe4`) carry the exact file contents for the
Brewfile, the fish template, the removal commit messages and the manifest,
none of which are language-dependent. Recover any of them with
`git show 0974c7b:docs/superpowers/plans/2026-08-10-dotfiles-bootstrap.md`.

---

## Self-Review

**Spec coverage.** §1 the rule → Tasks 2, 4, 6; §2 interface → Task 3; §2.1 shim → Task 3; §3 phases → Tasks 3, 4, 11, 12, 13; §3.1 Homebrew on Linux → Task 12; §4 dry-run invariant → Tasks 1, 5; §5 refuse-never-clobber → Task 1; §6 manifest → Task 2; §6.1 one owner per path → Tasks 2, 4, 11; §7 fish inversion → Task 7; §8 renames → Task 8; §8.1 migrate → Tasks 9, 10; §9 removals → Tasks 12, 14; §10 check → Task 6; §11 layout and testing → Tasks 1–5; §12 commit order → the task order.

**Ordering constraint.** Task 6's checks cannot pass until Tasks 7 and 8 create `fish/config.fish.template` and `git/gitconfig.shared`. Task 6 adds explicit `t.Skip` lines naming Task 8; Task 8 removes them. Deliberate — the alternative is one unreviewable task spanning checks, fish, and the git renames.

**Type consistency.** `change.Interface`'s ten methods are used identically in `Applier`, `Planner` and `fakeChange`. `phase.Context` carries `Change`, `Root`, `Home`, `Platform`, `Profile`, `Out` in every task. `manifest.Row`'s field order (`Kind`, `Source`, `Target`, `Platform`) matches the manifest's column order.

**Language reversal.** Commits `14dab82` and `5ffeba3` are the shell implementation; `1dbbfe4` removed it and kept `go.mod`. Do not reintroduce shell outside `bootstrap`.
