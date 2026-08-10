# Dotfiles Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `make dotfiles` with `./bootstrap`, a phased, idempotent, cross-platform workstation provisioner, and remove the dead weight the old machinery accumulated.

**Architecture:** A thin `bootstrap` dispatcher at the repo root sources helpers and phase files from `bootstrap.d/`. All mutation flows through five primitives in `lib.sh` that no-op under `BOOTSTRAP_DRY_RUN=1`, so `plan` and `apply` are literally the same code path. A tracked `links.manifest` declares each managed path's *kind* (`link`, `seed`, `dir`), which makes spec 1 §8.4's placement rule mechanically checkable. Tests live in a second Go module at `bootstrap.d/` that drives the shell scripts with a redirected `$HOME`.

**Tech Stack:** bash 3.2+ (macOS ships 3.2 — no `mapfile`, no associative arrays), Go 1.26 for tests only, Homebrew on both platforms, fish 4.x.

**Spec:** [docs/superpowers/specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md](../specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md)

## Global Constraints

Every task's requirements implicitly include this section.

- **Shell:** `#!/usr/bin/env bash` + `set -eu`. Target bash 3.2 (macOS system bash). No `mapfile`, no `declare -A`, no `${var,,}`.
- **No bare mutating commands in phase files.** `ln`, `rm`, `cp`, `mkdir`, `brew`, `chsh`, `sudo` may appear only in `bootstrap.d/lib.sh`. Everything else calls `do_link`, `do_seed`, `do_dir`, `do_run`, `do_sudo`. Task 7 enforces this with a test.
- **Refuse, never clobber.** A target that exists and is not the exact intended thing is a refusal naming the remediation. Never `rm -rf` a user path.
- **Exit codes** (from spec 1 §6): `0` ok, `1` advisory, `2` block/refuse, `3` malformed input, `4` not applicable.
- **Platform values** are exactly `darwin` and `linux`. Manifest wildcard is `*`.
- **Repo root** is resolved from `$0`, never from `pwd`.
- **`$HOME`** is read from the environment so tests can redirect it. Never hardcode `/Users/...`.
- **Go module:** `bootstrap.d/go.mod`, module path `github.com/nilbot/dotfiles/bootstrap`, `go 1.26`, package `bootstrap_test`. No `go.work`. No dependencies.
- **Test command:** `cd bootstrap.d && go test ./...`
- **Commit messages:** no AI attribution, no `Co-Authored-By`. Conventional-commit prefixes matching repo history (`feat(bootstrap):`, `refactor(fish):`, `chore(remove):`).

## File Structure

| File | Responsibility |
|---|---|
| `bootstrap` | Dispatcher: parse verb + profile, resolve root, source helpers, map profile→phases, `--help` |
| `bootstrap.d/lib.sh` | The five primitives, `refuse`, logging, platform detection. The **only** file allowed to mutate |
| `bootstrap.d/manifest.sh` | Parse and filter `links.manifest`; detect duplicate targets |
| `bootstrap.d/checks.sh` | The eight checks, shared by phase 50 and the `check` verb |
| `bootstrap.d/migrations.sh` | Detect and run reconciling + reclaiming migrations |
| `bootstrap.d/phase-00-preflight.sh` … `phase-50-verify.sh` | One file per phase |
| `bootstrap.d/links.manifest` | The declarative path table |
| `bootstrap.d/Brewfile` | Package manifest, platform-sectioned |
| `bootstrap.d/*_test.go` | Go tests driving the shell with redirected `$HOME` |
| `fish/config.fish.template` | Seed source for the machine-local stub |

---

### Task 1: Test harness and `lib.sh` primitives

**Files:**
- Create: `bootstrap.d/go.mod`
- Create: `bootstrap.d/lib.sh`
- Test: `bootstrap.d/harness_test.go`, `bootstrap.d/lib_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `newFixture(t) fixture` with fields `root`, `home` and method `runLib(t, script string) (stdout, stderr string, code int)`. Shell functions `refuse`, `dry_run`, `bootstrap_platform`, `do_dir`, `do_link`, `do_seed`, `do_run`, `do_sudo`, all sourced from `bootstrap.d/lib.sh`.

- [ ] **Step 1: Create the Go module**

```bash
mkdir -p bootstrap.d
cat > bootstrap.d/go.mod <<'EOF'
module github.com/nilbot/dotfiles/bootstrap

go 1.26
EOF
```

- [ ] **Step 2: Write the test harness**

Create `bootstrap.d/harness_test.go`:

```go
package bootstrap_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	root string // repository root
	home string // redirected HOME
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	// A space in the path catches unquoted expansions.
	home := filepath.Join(t.TempDir(), "home dir")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return fixture{root: root, home: home}
}

// runLib sources lib.sh and runs the supplied script body under the fixture HOME.
//
// This harness runs shell by design -- that is the thing under test. Every
// script body is a literal in a _test.go file and every interpolated path comes
// from t.TempDir(), so there is no untrusted input to inject. Do not copy this
// pattern into non-test code.
func (f fixture) runLib(t *testing.T, body string) (string, string, int) {
	t.Helper()
	script := ". \"$BOOTSTRAP_ROOT/bootstrap.d/lib.sh\"\n" + body
	return f.runScript(t, script)
}

func (f fixture) runScript(t *testing.T, script string) (string, string, int) {
	t.Helper()
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(),
		"HOME="+f.home,
		"BOOTSTRAP_ROOT="+f.root,
	)
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

// tree returns a sorted listing of paths under dir with their kinds, for
// asserting that a dry run changed nothing.
func tree(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		kind := "file"
		if info.IsDir() {
			kind = "dir"
		} else if info.Mode()&os.ModeSymlink != 0 {
			kind = "link"
		}
		rel, _ := filepath.Rel(dir, path)
		lines = append(lines, kind+" "+rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}
```

Note: `filepath.Walk` uses `Lstat`, so symlinks report `ModeSymlink` correctly.

- [ ] **Step 3: Write the failing tests for the primitives**

Create `bootstrap.d/lib_test.go`:

```go
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
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `cd bootstrap.d && go test ./...`
Expected: FAIL — every test errors because `bootstrap.d/lib.sh` does not exist (`bash: .../lib.sh: No such file or directory`, exit 1).

- [ ] **Step 5: Write `lib.sh`**

Create `bootstrap.d/lib.sh`:

```bash
# shellcheck shell=bash
#
# The ONLY file in this tree permitted to mutate the filesystem or invoke a
# package manager. Phase files call these primitives; a test enforces it.
#
# Every primitive is a no-op under BOOTSTRAP_DRY_RUN=1, which is what makes
# `bootstrap plan` and `bootstrap apply` the same code path.

BOOTSTRAP_DRY_RUN=${BOOTSTRAP_DRY_RUN:-0}

log()    { printf '%s\n' "$*"; }
plan()   { printf 'plan  %s\n' "$*"; }
did()    { printf 'ok    %s\n' "$*"; }
warn()   { printf 'warn  %s\n' "$*" >&2; }

# Exit 2 is "block" in spec 1 §6's shared table.
refuse() { printf 'bootstrap: refusing: %s\n' "$*" >&2; exit 2; }

dry_run() { [ "$BOOTSTRAP_DRY_RUN" -eq 1 ]; }

bootstrap_platform() {
	case "$(uname -s)" in
		Darwin) printf 'darwin\n' ;;
		Linux)  printf 'linux\n' ;;
		*)      refuse "unsupported operating system '$(uname -s)'" ;;
	esac
}

do_dir() {
	target=$1
	if [ -d "$target" ] && [ ! -L "$target" ]; then
		return 0
	fi
	if [ -e "$target" ] || [ -L "$target" ]; then
		refuse "'$target' exists and is not a real directory; move it aside deliberately, then retry"
	fi
	if dry_run; then
		plan "create directory $target"
		return 0
	fi
	mkdir -p "$target"
	did "created directory $target"
}

do_link() {
	link_source=$1
	target=$2
	if [ -L "$target" ]; then
		current=$(readlink "$target") || refuse "cannot read the symlink at '$target'"
		if [ "$current" = "$link_source" ]; then
			return 0
		fi
		refuse "'$target' points to '$current', not '$link_source'; move it aside deliberately, then retry"
	fi
	if [ -e "$target" ]; then
		refuse "'$target' exists and is not a symlink; move it aside deliberately, then retry"
	fi
	do_dir "$(dirname "$target")"
	if dry_run; then
		plan "link $target -> $link_source"
		return 0
	fi
	ln -s "$link_source" "$target"
	did "linked $target -> $link_source"
}

do_seed() {
	seed_source=$1
	target=$2
	if [ -L "$target" ]; then
		refuse "'$target' must be a machine-local regular file but is a symlink; run './bootstrap migrate', or move it aside deliberately, then retry"
	fi
	if [ -f "$target" ]; then
		return 0
	fi
	if [ -e "$target" ]; then
		refuse "'$target' exists and is not a regular file; move it aside deliberately, then retry"
	fi
	[ -f "$seed_source" ] || refuse "seed template '$seed_source' is missing"
	do_dir "$(dirname "$target")"
	if dry_run; then
		plan "seed $target from $seed_source"
		return 0
	fi
	cp "$seed_source" "$target"
	did "seeded $target from $seed_source"
}

do_run() {
	if dry_run; then
		plan "run: $*"
		return 0
	fi
	"$@"
}

do_sudo() {
	if dry_run; then
		plan "run (sudo): $*"
		return 0
	fi
	sudo "$@"
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd bootstrap.d && go test ./...`
Expected: PASS, all seven tests.

- [ ] **Step 7: Commit**

```bash
git add bootstrap.d/go.mod bootstrap.d/lib.sh bootstrap.d/harness_test.go bootstrap.d/lib_test.go
git commit -m "feat(bootstrap): add mutation primitives with a dry-run invariant

lib.sh is the only file permitted to touch the filesystem. Each primitive
no-ops under BOOTSTRAP_DRY_RUN=1, so plan and apply share one code path
and cannot drift.

Tests assert that a dry run leaves the tree byte-identical, and that a
refusal never clobbers the target it refused."
```

---

### Task 2: `manifest.sh` — parsing, platform filter, duplicate detection

**Files:**
- Create: `bootstrap.d/manifest.sh`
- Create: `bootstrap.d/links.manifest`
- Test: `bootstrap.d/manifest_test.go`

**Interfaces:**
- Consumes: `refuse` from `lib.sh` (Task 1).
- Produces: `manifest_rows <manifest-path> <platform>` printing `kind source target` per applicable row; `manifest_duplicate_targets <manifest-path> <platform>` printing repeated targets, one per line.

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/manifest_test.go`:

```go
package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "links.manifest")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestManifestFiltersByPlatform(t *testing.T) {
	f := newFixture(t)
	writeManifest(t, f.home, `# comment line
# kind  source          target              platform
link    starship.toml   .config/starship    *

link    macOS/ghostty   .config/ghostty     darwin
link    linuxonly/foo   .config/foo         linux
`)
	stdout, stderr, code := f.runLib(t, `
. "$BOOTSTRAP_ROOT/bootstrap.d/manifest.sh"
manifest_rows "$HOME/links.manifest" darwin
`)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := strings.TrimSpace(stdout)
	want := "link starship.toml .config/starship\nlink macOS/ghostty .config/ghostty"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestManifestSkipsCommentsAndBlanks(t *testing.T) {
	f := newFixture(t)
	writeManifest(t, f.home, `
   # indented comment
link    a   b   *

`)
	stdout, _, code := f.runLib(t, `
. "$BOOTSTRAP_ROOT/bootstrap.d/manifest.sh"
manifest_rows "$HOME/links.manifest" linux
`)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := strings.TrimSpace(stdout); got != "link a b" {
		t.Errorf("got %q", got)
	}
}

func TestManifestDetectsDuplicateTargets(t *testing.T) {
	f := newFixture(t)
	writeManifest(t, f.home, `
link    one   .config/same   *
link    two   .config/same   darwin
link    three .config/other  *
`)
	stdout, _, code := f.runLib(t, `
. "$BOOTSTRAP_ROOT/bootstrap.d/manifest.sh"
manifest_duplicate_targets "$HOME/links.manifest" darwin
`)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := strings.TrimSpace(stdout); got != ".config/same" {
		t.Errorf("got %q, want .config/same", got)
	}
}

func TestManifestDuplicateIgnoresOtherPlatform(t *testing.T) {
	f := newFixture(t)
	writeManifest(t, f.home, `
link    one   .config/same   linux
link    two   .config/same   darwin
`)
	stdout, _, code := f.runLib(t, `
. "$BOOTSTRAP_ROOT/bootstrap.d/manifest.sh"
manifest_duplicate_targets "$HOME/links.manifest" darwin
`)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := strings.TrimSpace(stdout); got != "" {
		t.Errorf("rows for different platforms are not duplicates, got %q", got)
	}
}

func TestManifestRefusesMissingFile(t *testing.T) {
	f := newFixture(t)
	_, stderr, code := f.runLib(t, `
. "$BOOTSTRAP_ROOT/bootstrap.d/manifest.sh"
manifest_rows "$HOME/nope.manifest" darwin
`)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "nope.manifest") {
		t.Errorf("refusal should name the file: %s", stderr)
	}
}

// The real manifest must parse and be free of duplicate targets on both platforms.
func TestRealManifestIsWellFormed(t *testing.T) {
	f := newFixture(t)
	for _, platform := range []string{"darwin", "linux"} {
		stdout, stderr, code := f.runLib(t, `
. "$BOOTSTRAP_ROOT/bootstrap.d/manifest.sh"
manifest_duplicate_targets "$BOOTSTRAP_ROOT/bootstrap.d/links.manifest" `+platform)
		if code != 0 {
			t.Fatalf("%s: exit %d: %s", platform, code, stderr)
		}
		if got := strings.TrimSpace(stdout); got != "" {
			t.Errorf("%s: duplicate targets in the real manifest: %s", platform, got)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run Manifest`
Expected: FAIL — `manifest.sh: No such file or directory`.

- [ ] **Step 3: Write `manifest.sh`**

Create `bootstrap.d/manifest.sh`:

```bash
# shellcheck shell=bash
#
# links.manifest is whitespace-separated, four columns:
#     kind  source  target  platform
# Blank lines and lines whose first non-space character is '#' are ignored.
# Platform is 'darwin', 'linux', or '*'.

manifest_rows() {
	manifest=$1
	platform=$2
	[ -f "$manifest" ] || refuse "link manifest '$manifest' is missing"
	awk -v want="$platform" '
		{ sub(/^[ \t]+/, "") }
		/^#/  { next }
		NF==0 { next }
		NF!=4 { printf "manifest: line %d has %d columns, want 4\n", NR, NF > "/dev/stderr"; exit 3 }
		($4 == "*" || $4 == want) { print $1, $2, $3 }
	' "$manifest"
}

manifest_duplicate_targets() {
	manifest_rows "$1" "$2" | awk '{ print $3 }' | sort | uniq -d
}
```

- [ ] **Step 4: Write the real manifest**

Create `bootstrap.d/links.manifest`. `git/gitignore_global` and `fish/config.fish.template` do not exist yet — Tasks 8 and 9 create them, and Task 6's checks will flag them until then.

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
link    git/gitignore_global              .gitignore                        *
link    claude/skills                     .claude/skills                    *
link    gemini/skills                     .gemini/skills                    *
link    macOS/ghostty                     .config/ghostty                   darwin
link    macOS/alacritty/alacritty.toml    .config/alacritty/alacritty.toml  darwin
dir     -                                 .config/fish                      *
seed    fish/config.fish.template         .config/fish/config.fish          *
seed    git/gitconfig.local.template      .gitconfig                        *
```

- [ ] **Step 5: Run to verify it passes**

Run: `cd bootstrap.d && go test ./... -run Manifest`
Expected: PASS, six tests.

- [ ] **Step 6: Commit**

```bash
git add bootstrap.d/manifest.sh bootstrap.d/links.manifest bootstrap.d/manifest_test.go
git commit -m "feat(bootstrap): declare managed paths in links.manifest

Each row names a kind -- link, seed, or dir -- which turns spec 1 §8.4's
placement rule into something a check can verify rather than a convention
somebody has to remember.

Duplicate-target detection is platform-aware: the same target under
different platforms is not a conflict."
```

---

### Task 3: Dispatcher, preflight, and profile→phase mapping

**Files:**
- Create: `bootstrap` (executable, repo root)
- Create: `bootstrap.d/phase-00-preflight.sh`
- Test: `bootstrap.d/dispatch_test.go`

**Interfaces:**
- Consumes: `lib.sh`, `manifest.sh`.
- Produces: the CLI contract — `bootstrap plan|apply|check|migrate [arg]`, `--help`. Shell function `phase_preflight <root> <home> <platform> <profile>`. Environment contract: `BOOTSTRAP_ROOT` is exported by the dispatcher for phase files.

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/dispatch_test.go`:

```go
package bootstrap_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runBootstrap invokes the real ./bootstrap with a redirected HOME.
func (f fixture) runBootstrap(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(filepath.Join(f.root, "bootstrap"), args...)
	cmd.Dir = t.TempDir() // prove the root is resolved from $0, not pwd
	cmd.Env = append(os.Environ(), "HOME="+f.home)
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

func TestUnknownVerbIsMalformedInput(t *testing.T) {
	f := newFixture(t)
	_, stderr, code := f.runBootstrap(t, "frobnicate")
	if code != 3 {
		t.Fatalf("exit %d, want 3 (malformed input)", code)
	}
	if !strings.Contains(stderr, "frobnicate") {
		t.Errorf("error should name the verb: %s", stderr)
	}
}

func TestUnknownProfileIsMalformedInput(t *testing.T) {
	f := newFixture(t)
	_, stderr, code := f.runBootstrap(t, "plan", "laptop")
	if code != 3 {
		t.Fatalf("exit %d, want 3", code)
	}
	if !strings.Contains(stderr, "laptop") {
		t.Errorf("error should name the profile: %s", stderr)
	}
}

func TestHelpExitsZero(t *testing.T) {
	f := newFixture(t)
	stdout, _, code := f.runBootstrap(t, "--help")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	for _, want := range []string{"plan", "apply", "check", "migrate", "workstation", "dotfiles"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help omits %q:\n%s", want, stdout)
		}
	}
}

func TestPlanRunsFromAnyDirectory(t *testing.T) {
	f := newFixture(t)
	// cmd.Dir is an unrelated temp dir; if the root came from pwd this fails.
	stdout, stderr, code := f.runBootstrap(t, "plan", "dotfiles")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "preflight") {
		t.Errorf("plan should name its phases:\n%s", stdout)
	}
}

func TestDotfilesProfileSkipsPrivilegedPhases(t *testing.T) {
	f := newFixture(t)
	stdout, stderr, code := f.runBootstrap(t, "plan", "dotfiles")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, forbidden := range []string{"packages", "devtools"} {
		if strings.Contains(stdout, forbidden) {
			t.Errorf("dotfiles profile must not run the %s phase:\n%s", forbidden, stdout)
		}
	}
}

func TestWorkstationProfileRunsAllPhases(t *testing.T) {
	f := newFixture(t)
	stdout, stderr, code := f.runBootstrap(t, "plan", "workstation")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"preflight", "packages", "config", "fish", "devtools", "verify"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("workstation plan omits the %s phase:\n%s", want, stdout)
		}
	}
}

func TestPreflightDeclaresPrivilegeAndNetwork(t *testing.T) {
	f := newFixture(t)
	stdout, _, code := f.runBootstrap(t, "plan", "workstation")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "sudo") || !strings.Contains(stdout, "network") {
		t.Errorf("preflight must declare what needs sudo and network:\n%s", stdout)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run 'Verb|Profile|Help|Plan|Preflight'`
Expected: FAIL — the harness `t.Fatal`s because `./bootstrap` does not exist.

- [ ] **Step 3: Write the preflight phase**

Create `bootstrap.d/phase-00-preflight.sh`:

```bash
# shellcheck shell=bash

phase_preflight() {
	root=$1
	install_home=$2
	platform=$3
	profile=$4

	log "== preflight"
	log "   platform    $platform $(uname -m)"
	log "   repository  $root"
	log "   home        $install_home"
	log "   profile     $profile"

	for tool in uname awk sed sort uniq git; do
		command -v "$tool" >/dev/null 2>&1 ||
			refuse "required stage-zero tool '$tool' is not on PATH"
	done

	if [ "$profile" = workstation ]; then
		log "   this profile needs sudo    (login shell change, /etc/shells)"
		log "   this profile needs network (Homebrew, packages, fisher plugins)"
	else
		log "   this profile needs neither sudo nor network"
	fi

	migrations_require_none "$root" "$install_home"
}
```

`migrations_require_none` is defined in Task 10. Until then, add a temporary definition at the bottom of `phase-00-preflight.sh` so this task is independently testable, and delete it in Task 10:

```bash
# Replaced by bootstrap.d/migrations.sh in Task 10.
if ! command -v migrations_require_none >/dev/null 2>&1; then
	migrations_require_none() { :; }
fi
```

- [ ] **Step 4: Write the dispatcher**

Create `bootstrap` at the repo root:

```bash
#!/usr/bin/env bash
#
# Provision this workstation. See docs/superpowers/specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md
#
# The repository root is resolved from $0, never from pwd: softlinks.sh used
# pwd and silently linked the wrong tree when run from anywhere else.

set -eu

BOOTSTRAP_ROOT=$(cd "$(dirname "$0")" && pwd)
export BOOTSTRAP_ROOT

. "$BOOTSTRAP_ROOT/bootstrap.d/lib.sh"
. "$BOOTSTRAP_ROOT/bootstrap.d/manifest.sh"
for phase_file in "$BOOTSTRAP_ROOT"/bootstrap.d/phase-*.sh; do
	. "$phase_file"
done

usage() {
	cat <<'EOF'
usage: bootstrap <verb> [argument]

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
EOF
}

# Phase lists per profile. Order is significant.
profile_phases() {
	case "$1" in
		workstation) printf 'preflight packages config fish devtools verify\n' ;;
		dotfiles)    printf 'preflight config verify\n' ;;
		*)           printf 'bootstrap: unknown profile "%s"; expected workstation or dotfiles\n' "$1" >&2
		             exit 3 ;;
	esac
}

run_profile() {
	profile=$1
	platform=$(bootstrap_platform)
	for phase in $(profile_phases "$profile"); do
		"phase_$phase" "$BOOTSTRAP_ROOT" "$HOME" "$platform" "$profile"
	done
}

verb=${1:---help}
case "$verb" in
	--help|-h|help)
		usage
		;;
	plan)
		BOOTSTRAP_DRY_RUN=1
		run_profile "${2:-workstation}"
		;;
	apply)
		BOOTSTRAP_DRY_RUN=0
		run_profile "${2:-workstation}"
		;;
	check)
		bootstrap_check "${2:-workstation}"
		;;
	migrate)
		bootstrap_migrate "${2:-}"
		;;
	*)
		printf 'bootstrap: unknown verb "%s"; try --help\n' "$verb" >&2
		exit 3
		;;
esac
```

`bootstrap_check` arrives in Task 6 and `bootstrap_migrate` in Task 10. Until then both verbs exit non-zero with "command not found", which no test in this task exercises.

- [ ] **Step 5: Make it executable and add stub phases**

Phase functions for `packages`, `config`, `fish`, `devtools` and `verify` do not exist yet, so `run_profile` would fail. Create one-line stubs that later tasks replace:

```bash
chmod +x bootstrap
for p in 10-packages 20-config 30-fish 40-devtools 50-verify; do
  name="${p#*-}"
  printf '# shellcheck shell=bash\n\nphase_%s() { log "== %s (not implemented)"; }\n' "$name" "$name" \
    > "bootstrap.d/phase-$p.sh"
done
```

- [ ] **Step 6: Run to verify it passes**

Run: `cd bootstrap.d && go test ./...`
Expected: PASS, all tests from Tasks 1–3.

- [ ] **Step 7: Commit**

```bash
git add bootstrap bootstrap.d/phase-*.sh bootstrap.d/dispatch_test.go
git commit -m "feat(bootstrap): add dispatcher, preflight, and profile phases

The dotfiles profile is preflight + config + verify -- no sudo, no
network, no package manager, no login-shell change -- which is what makes
the tool safe in a container and testable at all.

Tests run ./bootstrap from an unrelated working directory, so a
regression to pwd-based root resolution fails immediately."
```

---

### Task 4: Config phase — the `link` kind

**Files:**
- Modify: `bootstrap.d/phase-20-config.sh` (replaces the Task 3 stub)
- Test: `bootstrap.d/config_test.go`

**Interfaces:**
- Consumes: `manifest_rows`, `manifest_duplicate_targets` (Task 2); `do_link` (Task 1).
- Produces: `phase_config <root> <home> <platform> <profile>`.

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/config_test.go`:

```go
package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runConfig sources the phase and runs it against a caller-supplied manifest.
func (f fixture) runConfig(t *testing.T, manifest string, dryRun bool) (string, string, int) {
	t.Helper()
	manifestPath := writeManifest(t, f.home, manifest)
	mode := "0"
	if dryRun {
		mode = "1"
	}
	return f.runLib(t, `
. "$BOOTSTRAP_ROOT/bootstrap.d/manifest.sh"
. "$BOOTSTRAP_ROOT/bootstrap.d/phase-20-config.sh"
BOOTSTRAP_DRY_RUN=`+mode+`
BOOTSTRAP_MANIFEST="`+manifestPath+`"
phase_config "$BOOTSTRAP_ROOT" "$HOME" darwin workstation
`)
}

func TestConfigLinksRows(t *testing.T) {
	f := newFixture(t)
	_, stderr, code := f.runConfig(t, "link  starship.toml  .config/starship.toml  *\n", false)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got, err := os.Readlink(filepath.Join(f.home, ".config", "starship.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "/starship.toml") {
		t.Errorf("link points to %q", got)
	}
}

func TestConfigCreatesMissingParentDirectories(t *testing.T) {
	f := newFixture(t)
	if _, stderr, code := f.runConfig(t, "link  starship.toml  .config/deep/nested/starship.toml  *\n", false); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Lstat(filepath.Join(f.home, ".config", "deep", "nested", "starship.toml")); err != nil {
		t.Errorf("parent directories were not created: %v", err)
	}
}

func TestConfigIsIdempotent(t *testing.T) {
	f := newFixture(t)
	m := "link  starship.toml  .config/starship.toml  *\n"
	if _, stderr, code := f.runConfig(t, m, false); code != 0 {
		t.Fatalf("first run exit %d: %s", code, stderr)
	}
	after := tree(t, f.home)
	stdout, stderr, code := f.runConfig(t, m, false)
	if code != 0 {
		t.Fatalf("second run exit %d: %s", code, stderr)
	}
	if again := tree(t, f.home); again != after {
		t.Errorf("second run changed the tree:\n%s\n---\n%s", after, again)
	}
	if strings.Contains(stdout, "linked") {
		t.Errorf("second run should report no work: %s", stdout)
	}
}

func TestConfigPlanChangesNothing(t *testing.T) {
	f := newFixture(t)
	before := tree(t, f.home)
	stdout, stderr, code := f.runConfig(t, "link  starship.toml  .config/starship.toml  *\n", true)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if after := tree(t, f.home); after != before {
		t.Errorf("plan mutated the tree:\n%s\n---\n%s", before, after)
	}
	if !strings.Contains(stdout, "plan") {
		t.Errorf("plan produced no plan output: %s", stdout)
	}
}

func TestConfigRefusesUnknownKind(t *testing.T) {
	f := newFixture(t)
	_, stderr, code := f.runConfig(t, "hardlink  a  b  *\n", false)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "hardlink") {
		t.Errorf("refusal should name the kind: %s", stderr)
	}
}

func TestConfigRefusesDuplicateTargets(t *testing.T) {
	f := newFixture(t)
	_, stderr, code := f.runConfig(t, "link  starship.toml  .config/x  *\nlink  tmux/tmux.conf  .config/x  *\n", false)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "more than once") {
		t.Errorf("refusal should explain: %s", stderr)
	}
}

// A refusal inside the row loop must exit the process, not just a subshell.
func TestConfigRefusalStopsTheRun(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.home, "occupied"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := f.runConfig(t,
		"link  starship.toml  occupied      *\n"+
			"link  tmux/tmux.conf  .config/after  *\n", false)
	if code != 2 {
		t.Fatalf("exit %d, want 2: %s", code, stderr)
	}
	if _, err := os.Lstat(filepath.Join(f.home, ".config", "after")); !os.IsNotExist(err) {
		t.Errorf("rows after a refusal must not be processed")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run Config`
Expected: FAIL — the stub `phase_config` prints "not implemented" and creates nothing.

- [ ] **Step 3: Implement the phase**

Replace `bootstrap.d/phase-20-config.sh` entirely:

```bash
# shellcheck shell=bash
#
# Reconcile every applicable row of links.manifest.
#
# The row loop deliberately avoids `manifest_rows | while read`: a pipeline
# puts the loop in a subshell, where refuse's `exit 2` would end only the
# subshell and the run would carry on past a refusal.

phase_config() {
	root=$1
	install_home=$2
	platform=$3
	# shellcheck disable=SC2034  # profile is part of the phase signature
	profile=$4

	manifest=${BOOTSTRAP_MANIFEST:-$root/bootstrap.d/links.manifest}

	log "== config"

	duplicates=$(manifest_duplicate_targets "$manifest" "$platform")
	if [ -n "$duplicates" ]; then
		refuse "the manifest claims these targets more than once: $(printf '%s' "$duplicates" | tr '\n' ' ')"
	fi

	while read -r kind row_source target; do
		[ -n "$kind" ] || continue
		case "$kind" in
			link) do_link "$root/$row_source" "$install_home/$target" ;;
			*)    refuse "unknown manifest kind '$kind' for target '$target'" ;;
		esac
	done < <(manifest_rows "$manifest" "$platform")
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd bootstrap.d && go test ./... -run Config`
Expected: PASS, seven tests.

- [ ] **Step 5: Commit**

```bash
git add bootstrap.d/phase-20-config.sh bootstrap.d/config_test.go
git commit -m "feat(bootstrap): reconcile link rows in the config phase

The row loop uses process substitution rather than a pipeline: a pipeline
would run the loop in a subshell, where refuse's exit 2 ends the subshell
and the run continues past a refusal. A test pins that behaviour."
```

---

### Task 5: Config phase — the `seed` and `dir` kinds

**Files:**
- Modify: `bootstrap.d/phase-20-config.sh`
- Modify: `bootstrap.d/config_test.go`

**Interfaces:**
- Consumes: `do_seed`, `do_dir` (Task 1).
- Produces: no new names; `phase_config` gains two cases.

- [ ] **Step 1: Write the failing tests**

Append to `bootstrap.d/config_test.go`:

```go
func TestConfigSeedsAndDirs(t *testing.T) {
	f := newFixture(t)
	_, stderr, code := f.runConfig(t,
		"dir   -                             .config/fish              *\n"+
			"seed  git/gitconfig.local.template  .config/fish/local.conf   *\n", false)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	info, err := os.Lstat(filepath.Join(f.home, ".config", "fish"))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("dir row should produce a real directory: %v", err)
	}
	seeded, err := os.Lstat(filepath.Join(f.home, ".config", "fish", "local.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if seeded.Mode()&os.ModeSymlink != 0 {
		t.Errorf("seed row must produce a regular file, not a symlink")
	}
}

func TestConfigSeedPreservesLocalEdits(t *testing.T) {
	f := newFixture(t)
	m := "seed  git/gitconfig.local.template  .gitconfig  *\n"
	if _, stderr, code := f.runConfig(t, m, false); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	path := filepath.Join(f.home, ".gitconfig")
	if err := os.WriteFile(path, []byte("[user]\n\tname = edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := f.runConfig(t, m, false); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	content, _ := os.ReadFile(path)
	if !strings.Contains(string(content), "edited") {
		t.Errorf("re-running config overwrote machine-local edits: %q", content)
	}
}

// A seed target that has become a symlink into the repo is the regression
// spec 1 §8.4 fixed for ~/.gitconfig. It must refuse, not silently proceed.
func TestConfigRefusesSeedTargetThatBecameASymlink(t *testing.T) {
	f := newFixture(t)
	if err := os.Symlink(filepath.Join(f.root, "git", "gitconfig.local.template"),
		filepath.Join(f.home, ".gitconfig")); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := f.runConfig(t, "seed  git/gitconfig.local.template  .gitconfig  *\n", false)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "migrate") {
		t.Errorf("refusal should point at the migration: %s", stderr)
	}
}

// A dir target that is a symlink into the repo is the fish regression.
func TestConfigRefusesDirTargetThatIsASymlink(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Join(f.home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.root, "fish"), filepath.Join(f.home, ".config", "fish")); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := f.runConfig(t, "dir  -  .config/fish  *\n", false)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "not a real directory") {
		t.Errorf("refusal should say why: %s", stderr)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run Config`
Expected: FAIL — the four new tests hit `unknown manifest kind 'dir'` / `'seed'` and exit 2 where 0 was expected.

- [ ] **Step 3: Add the two cases**

In `bootstrap.d/phase-20-config.sh`, replace the `case` block:

```bash
		case "$kind" in
			link) do_link "$root/$row_source" "$install_home/$target" ;;
			seed) do_seed "$root/$row_source" "$install_home/$target" ;;
			dir)  do_dir  "$install_home/$target" ;;
			*)    refuse "unknown manifest kind '$kind' for target '$target'" ;;
		esac
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd bootstrap.d && go test ./... -run Config`
Expected: PASS, eleven tests.

- [ ] **Step 5: Commit**

```bash
git add bootstrap.d/phase-20-config.sh bootstrap.d/config_test.go
git commit -m "feat(bootstrap): reconcile seed and dir rows

Two tests pin the regressions this design exists to prevent: a seed target
that has become a symlink into the repo (the ~/.gitconfig defect spec 1
fixed) and a dir target that is a symlink into the repo (the fish defect).
Both refuse and point at migrate."
```

---

### Task 6: `checks.sh`, the `check` verb, and phase 50

**Files:**
- Create: `bootstrap.d/checks.sh`
- Modify: `bootstrap.d/phase-50-verify.sh`
- Test: `bootstrap.d/check_test.go`

**Interfaces:**
- Consumes: `manifest_rows` (Task 2).
- Produces: `bootstrap_check <profile>`; `phase_verify <root> <home> <platform> <profile>`; helper `check_report <status> <name> <detail>` where status is `ok`, `warn`, `fail`, or `n/a`.

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/check_test.go`:

```go
package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckReportsMissingLinks(t *testing.T) {
	f := newFixture(t)
	stdout, _, code := f.runBootstrap(t, "check", "dotfiles")
	if code != 1 {
		t.Fatalf("exit %d, want 1 (advisory) on a bare HOME", code)
	}
	if !strings.Contains(stdout, "manifest") {
		t.Errorf("check should report the manifest rows:\n%s", stdout)
	}
}

func TestCheckPassesAfterApply(t *testing.T) {
	f := newFixture(t)
	if _, stderr, code := f.runBootstrap(t, "apply", "dotfiles"); code != 0 {
		t.Fatalf("apply exit %d: %s", code, stderr)
	}
	stdout, stderr, code := f.runBootstrap(t, "check", "dotfiles")
	if code != 0 {
		t.Fatalf("check exit %d after apply\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

func TestCheckMarksWorkstationOnlyChecksNotApplicable(t *testing.T) {
	f := newFixture(t)
	if _, _, code := f.runBootstrap(t, "apply", "dotfiles"); code != 0 {
		t.Fatalf("apply failed")
	}
	stdout, _, _ := f.runBootstrap(t, "check", "dotfiles")
	for _, name := range []string{"login-shell", "agents", "packages"} {
		line := findLine(stdout, name)
		if line == "" {
			t.Errorf("check omits %q entirely:\n%s", name, stdout)
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "n/a") {
			t.Errorf("%q must be n/a under the dotfiles profile, got: %s", name, line)
		}
	}
}

func TestCheckDetectsStrippedFishSourceLine(t *testing.T) {
	f := newFixture(t)
	if _, _, code := f.runBootstrap(t, "apply", "dotfiles"); code != 0 {
		t.Fatalf("apply failed")
	}
	stub := filepath.Join(f.home, ".config", "fish", "config.fish")
	if err := os.WriteFile(stub, []byte("# someone deleted the source line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := f.runBootstrap(t, "check", "dotfiles")
	if code == 0 {
		t.Fatalf("check must not pass when the fish stub lost its source line:\n%s", stdout)
	}
	if !strings.Contains(stdout, "fish-source") {
		t.Errorf("check should name the failing check:\n%s", stdout)
	}
}

func TestCheckDetectsStaleGitconfigInclude(t *testing.T) {
	f := newFixture(t)
	if _, _, code := f.runBootstrap(t, "apply", "dotfiles"); code != 0 {
		t.Fatalf("apply failed")
	}
	gc := filepath.Join(f.home, ".gitconfig")
	if err := os.WriteFile(gc, []byte("[include]\n\tpath = ~/dotfiles/git/gitconfig.symlink\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := f.runBootstrap(t, "check", "dotfiles")
	if code == 0 {
		t.Fatalf("check must not pass with a stale include:\n%s", stdout)
	}
	if !strings.Contains(stdout, "gitconfig-include") {
		t.Errorf("check should name the failing check:\n%s", stdout)
	}
}

func TestCheckDetectsLinkTargetThatBecameARealFile(t *testing.T) {
	f := newFixture(t)
	if _, _, code := f.runBootstrap(t, "apply", "dotfiles"); code != 0 {
		t.Fatalf("apply failed")
	}
	target := filepath.Join(f.home, ".tmux.conf")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("# hand-written\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := f.runBootstrap(t, "check", "dotfiles")
	if code == 0 {
		t.Fatalf("check must notice a link target that became a real file:\n%s", stdout)
	}
}

func findLine(haystack, needle string) string {
	for _, line := range strings.Split(haystack, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run Check`
Expected: FAIL — `bootstrap_check: command not found`, exit 127.

- [ ] **Step 3: Write `checks.sh`**

Create `bootstrap.d/checks.sh`:

```bash
# shellcheck shell=bash
#
# The eight checks from spec 2 §10, shared by `bootstrap check` and phase 50.
#
# Exit codes follow spec 1 §6: 0 ok, 1 advisory, 2 block.
# Checks 6-8 concern state the dotfiles profile does not manage, so under that
# profile they report n/a rather than producing three false failures on every
# container run.

BOOTSTRAP_CHECK_STATUS=0

check_report() {
	status=$1
	name=$2
	detail=$3
	printf '%-5s %-20s %s\n' "$status" "$name" "$detail"
	case "$status" in
		fail) BOOTSTRAP_CHECK_STATUS=2 ;;
		warn) [ "$BOOTSTRAP_CHECK_STATUS" -eq 0 ] && BOOTSTRAP_CHECK_STATUS=1 ;;
	esac
	return 0
}

check_manifest_rows() {
	root=$1
	install_home=$2
	platform=$3
	manifest=${BOOTSTRAP_MANIFEST:-$root/bootstrap.d/links.manifest}

	duplicates=$(manifest_duplicate_targets "$manifest" "$platform")
	if [ -n "$duplicates" ]; then
		check_report fail manifest-owners "claimed more than once: $(printf '%s' "$duplicates" | tr '\n' ' ')"
	else
		check_report ok manifest-owners "every target has exactly one owner"
	fi

	bad=0
	total=0
	while read -r kind row_source target; do
		[ -n "$kind" ] || continue
		total=$((total + 1))
		full=$install_home/$target
		case "$kind" in
			link)
				want=$root/$row_source
				if [ ! -L "$full" ]; then
					check_report fail "manifest:$target" "expected a symlink to $want"
					bad=$((bad + 1))
				elif [ "$(readlink "$full")" != "$want" ]; then
					check_report fail "manifest:$target" "points to $(readlink "$full"), want $want"
					bad=$((bad + 1))
				fi
				;;
			seed)
				if [ -L "$full" ]; then
					check_report fail "manifest:$target" "must be a machine-local regular file but is a symlink"
					bad=$((bad + 1))
				elif [ ! -f "$full" ]; then
					check_report fail "manifest:$target" "expected a seeded regular file"
					bad=$((bad + 1))
				fi
				;;
			dir)
				if [ -L "$full" ] || [ ! -d "$full" ]; then
					check_report fail "manifest:$target" "expected a real machine-owned directory"
					bad=$((bad + 1))
				fi
				;;
		esac
	done < <(manifest_rows "$manifest" "$platform")

	if [ "$bad" -eq 0 ]; then
		check_report ok manifest-kinds "$total managed paths are present and of the right kind"
	fi
}

check_fish_source_line() {
	install_home=$1
	stub=$install_home/.config/fish/config.fish
	if [ ! -f "$stub" ]; then
		check_report fail fish-source "$stub is missing"
		return 0
	fi
	if grep -q '^source .*/fish/config\.fish$' "$stub"; then
		check_report ok fish-source "the stub still sources the tracked config"
	else
		check_report fail fish-source "the stub has lost its source line; the whole shared config is inert"
	fi
}

check_gitconfig_include() {
	install_home=$1
	config=$install_home/.gitconfig
	if [ ! -f "$config" ]; then
		check_report fail gitconfig-include "$config is missing"
		return 0
	fi
	if grep -q 'git/gitconfig\.shared' "$config"; then
		check_report ok gitconfig-include "includes the current shared config"
	else
		check_report fail gitconfig-include "does not include git/gitconfig.shared; run './bootstrap migrate'"
	fi
}

check_login_shell() {
	profile=$1
	if [ "$profile" != workstation ]; then
		check_report n/a login-shell "not managed by the dotfiles profile"
		return 0
	fi
	fish_path=$(command -v fish 2>/dev/null || true)
	if [ -z "$fish_path" ]; then
		check_report fail login-shell "fish is not on PATH"
		return 0
	fi
	if ! grep -qxF "$fish_path" /etc/shells 2>/dev/null; then
		check_report warn login-shell "$fish_path is absent from /etc/shells"
		return 0
	fi
	case "${SHELL:-}" in
		*fish) check_report ok login-shell "$SHELL" ;;
		*)     check_report warn login-shell "login shell is ${SHELL:-unset}, not fish" ;;
	esac
}

check_agents() {
	profile=$1
	if [ "$profile" != workstation ]; then
		check_report n/a agents "not managed by the dotfiles profile"
		return 0
	fi
	if ! command -v agents >/dev/null 2>&1; then
		check_report fail agents "not on PATH; run './bootstrap apply workstation'"
		return 0
	fi
	if agents doctor >/dev/null 2>&1; then
		check_report ok agents "agents doctor reports no findings"
	else
		check_report warn agents "agents doctor reports findings; run it for detail"
	fi
}

check_packages() {
	root=$1
	profile=$2
	if [ "$profile" != workstation ]; then
		check_report n/a packages "not managed by the dotfiles profile"
		return 0
	fi
	if ! command -v brew >/dev/null 2>&1; then
		check_report fail packages "Homebrew is not on PATH"
		return 0
	fi
	if brew bundle check --file "$root/bootstrap.d/Brewfile" >/dev/null 2>&1; then
		check_report ok packages "every Brewfile entry is installed"
	else
		check_report warn packages "some Brewfile entries are missing; run './bootstrap apply workstation'"
	fi
}

bootstrap_check() {
	profile=${1:-workstation}
	case "$profile" in
		workstation|dotfiles) ;;
		*) printf 'bootstrap: unknown profile "%s"; expected workstation or dotfiles\n' "$profile" >&2; exit 3 ;;
	esac

	BOOTSTRAP_CHECK_STATUS=0
	platform=$(bootstrap_platform)

	check_report ok platform "$platform $(uname -m)"
	check_manifest_rows "$BOOTSTRAP_ROOT" "$HOME" "$platform"
	check_fish_source_line "$HOME"
	check_gitconfig_include "$HOME"
	check_login_shell "$profile"
	check_agents "$profile"
	check_packages "$BOOTSTRAP_ROOT" "$profile"

	case "$BOOTSTRAP_CHECK_STATUS" in
		0) log "healthy" ;;
		1) log "healthy with advisories" ;;
		*) log "unhealthy" ;;
	esac
	exit "$BOOTSTRAP_CHECK_STATUS"
}
```

- [ ] **Step 4: Wire checks into the dispatcher and phase 50**

In `bootstrap`, add after the `manifest.sh` source line:

```bash
. "$BOOTSTRAP_ROOT/bootstrap.d/checks.sh"
```

Replace `bootstrap.d/phase-50-verify.sh`:

```bash
# shellcheck shell=bash
#
# Phase 50 runs the same checks as the `check` verb, but reports rather than
# exits: an advisory finding at the end of an apply must not look like a failed
# apply.

phase_verify() {
	root=$1
	install_home=$2
	platform=$3
	profile=$4

	log "== verify"
	BOOTSTRAP_CHECK_STATUS=0
	check_manifest_rows "$root" "$install_home" "$platform"
	check_fish_source_line "$install_home"
	check_gitconfig_include "$install_home"
	check_login_shell "$profile"
	check_agents "$profile"
	check_packages "$root" "$profile"

	case "$BOOTSTRAP_CHECK_STATUS" in
		0) log "   healthy" ;;
		1) log "   healthy with advisories" ;;
		*) warn "verify found blocking problems; run './bootstrap check' for detail" ;;
	esac
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `cd bootstrap.d && go test ./...`
Expected: `TestCheckPassesAfterApply` FAILS — `fish/config.fish.template`, `git/gitignore_global` and `git/gitconfig.shared` do not exist yet. That is correct and expected; Tasks 8 and 9 create them.

Mark the two dependent tests as skipped for now, with the reason, and remove the skips in Task 9:

```go
	t.Skip("unskip in Task 9: needs fish/config.fish.template and git/gitconfig.shared")
```

Add that line as the first statement of `TestCheckPassesAfterApply`, `TestCheckMarksWorkstationOnlyChecksNotApplicable`, `TestCheckDetectsStrippedFishSourceLine`, `TestCheckDetectsStaleGitconfigInclude` and `TestCheckDetectsLinkTargetThatBecameARealFile`.

Run again: `cd bootstrap.d && go test ./...`
Expected: PASS, with five tests skipped.

- [ ] **Step 6: Commit**

```bash
git add bootstrap bootstrap.d/checks.sh bootstrap.d/phase-50-verify.sh bootstrap.d/check_test.go
git commit -m "feat(bootstrap): add the check verb and verify phase

Checks 6-8 report n/a under the dotfiles profile rather than failing:
they cover state that profile deliberately does not manage, so treating
them as failures would make every container run report three false
problems.

fish-source and gitconfig-include exist because this design introduces
two silent total-failure modes. A design that creates one owes a check."
```

---

### Task 7: The structural test

**Files:**
- Test: `bootstrap.d/structural_test.go`

**Interfaces:**
- Consumes: the phase files from Tasks 3–6.
- Produces: nothing at runtime. This is a guard.

- [ ] **Step 1: Write the test**

Create `bootstrap.d/structural_test.go`:

```go
package bootstrap_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Only lib.sh may mutate. Everything else calls the do_* primitives, which is
// what guarantees plan and apply cannot diverge.
//
// The check inspects the FIRST WORD OF EACH STATEMENT. Two wrong approaches to
// avoid, both of which were tried:
//
//   - Substring matching flags `do_run mkdir -p "$x"` -- the correct form --
//     because the mutating word is preceded by whitespace there exactly as it
//     would be in a bare call.
//   - Matching statement starts with a capture group misses `; do cp "$x"`,
//     because the `;` alternative consumes `do` as its captured word and the
//     `do` alternative never gets to fire.
//
// Splitting on separators and taking each piece's first word has neither
// problem. Verified against sixteen allowed forms and nine violations.
var statementSep = regexp.MustCompile(
	`;|&&|\|\||\||\(|\)|\bthen\b|\bdo\b|\belse\b|\bif\b|\belif\b|\bwhile\b|\buntil\b|\bdone\b|\bfi\b`)

func TestNoBareMutatingCommandsOutsideLib(t *testing.T) {
	f := newFixture(t)
	forbidden := map[string]bool{
		"ln": true, "rm": true, "cp": true, "mv": true,
		"mkdir": true, "chsh": true, "sudo": true, "brew": true,
	}

	dir := filepath.Join(f.root, "bootstrap.d")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := []string{filepath.Join(f.root, "bootstrap")}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sh") && e.Name() != "lib.sh" {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}

	for _, path := range files {
		base := filepath.Base(path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			code := line
			if idx := strings.Index(code, "#"); idx >= 0 {
				code = code[:idx] // strip comments; the prose names these words
			}
			for _, piece := range statementSep.Split(code, -1) {
				fields := strings.Fields(piece)
				if len(fields) == 0 {
					continue
				}
				word := fields[0]
				if idx := strings.LastIndex(word, "/"); idx >= 0 {
					word = word[idx+1:] // /bin/rm counts as rm
				}
				// checks.sh only queries -- `brew bundle check` reads, it does
				// not install. Every other file routes brew through do_run.
				if word == "brew" && base == "checks.sh" {
					continue
				}
				if forbidden[word] {
					t.Errorf("%s:%d calls %q directly; use a do_* primitive instead:\n\t%s",
						base, i+1, word, strings.TrimSpace(line))
				}
			}
		}
	}
}

// Every phase file must define the phase function its name promises.
func TestEveryPhaseFileDefinesItsFunction(t *testing.T) {
	f := newFixture(t)
	matches, err := filepath.Glob(filepath.Join(f.root, "bootstrap.d", "phase-*.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 6 {
		t.Fatalf("expected 6 phase files, found %d", len(matches))
	}
	for _, path := range matches {
		base := strings.TrimSuffix(filepath.Base(path), ".sh") // phase-20-config
		name := base[strings.LastIndex(base, "-")+1:]          // config
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "phase_"+name+"()") {
			t.Errorf("%s does not define phase_%s()", filepath.Base(path), name)
		}
	}
}
```

- [ ] **Step 2: Run it**

Run: `cd bootstrap.d && go test ./... -run 'Structural|NoBare|EveryPhase'`
Expected: PASS. If it fails, a phase file is mutating directly — fix the phase file, not the test.

- [ ] **Step 3: Commit**

```bash
git add bootstrap.d/structural_test.go
git commit -m "test(bootstrap): forbid bare mutating commands outside lib.sh

Enforces the dry-run invariant structurally rather than by discipline:
if a phase file calls ln or rm directly, plan silently stops matching
apply and no behavioural test would catch it.

Same reasoning as spec 1 §3.2, where redaction is guaranteed by the type
having no field to carry a secret."
```

---

### Task 8: Fish inversion

**Files:**
- Create: `fish/config.fish.template`
- Modify: `fish/config.fish`
- Modify: `fish/mypre.fish` (`install_fisher`, `fish_reset_all`)
- Modify: `.gitignore`
- Test: `bootstrap.d/fish_test.go`

**Interfaces:**
- Consumes: `do_seed` via the `seed` manifest row added in Task 2.
- Produces: `fish/config.fish.template` as the seed source. No shell functions.

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/fish_test.go`:

```go
package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateSourcesTrackedConfig(t *testing.T) {
	f := newFixture(t)
	data, err := os.ReadFile(filepath.Join(f.root, "fish", "config.fish.template"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "source $HOME/dotfiles/fish/config.fish") {
		t.Errorf("template must source the tracked config:\n%s", content)
	}
	if !strings.Contains(content, "NOT tracked") {
		t.Errorf("template must warn that edits here are invisible to the repo")
	}
	// The source line must precede the installer-block marker, so appended
	// blocks land after it and therefore override.
	src := strings.Index(content, "source $HOME/dotfiles")
	marker := strings.Index(content, "installer-managed blocks")
	if src < 0 || marker < 0 || src > marker {
		t.Errorf("the source line must come before the installer marker")
	}
}

func TestTrackedConfigUsesRelativeSourcing(t *testing.T) {
	f := newFixture(t)
	data, err := os.ReadFile(filepath.Join(f.root, "fish", "config.fish"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "status dirname") {
		t.Errorf("tracked config must resolve siblings from (status dirname):\n%s", content)
	}
	if strings.Contains(content, "~/.config/fish/") {
		t.Errorf("tracked config must not source through ~/.config/fish")
	}
	if strings.Contains(content, "grok installer") {
		t.Errorf("the grok installer block belongs in the machine-local stub, not here")
	}
}

func TestFishFunctionsDoNotWriteIntoTheRepo(t *testing.T) {
	f := newFixture(t)
	data, err := os.ReadFile(filepath.Join(f.root, "fish", "mypre.fish"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "$HOME/dotfiles/fish") {
		t.Errorf("no fish function may write into the repo by absolute path:\n%s", content)
	}
	if !strings.Contains(content, "__fish_config_dir") {
		t.Errorf("fish_reset_all must target $__fish_config_dir")
	}
}

func TestGitignoreNoLongerHoldsBackFishState(t *testing.T) {
	f := newFixture(t)
	data, err := os.ReadFile(filepath.Join(f.root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{
		"fish/fish_variables", "fish/fish_plugins",
		"fish/functions/", "fish/completions/", "fish/conf.d/",
	} {
		if strings.Contains(string(data), stale) {
			t.Errorf(".gitignore still holds back %q; fisher no longer writes there", stale)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run Fish`
Expected: FAIL — `fish/config.fish.template` does not exist; the other three fail on current content.

- [ ] **Step 3: Create the template**

Create `fish/config.fish.template`:

```fish
# Machine-local fish configuration. NOT tracked by dotfiles.
#
# Shareable settings belong in ~/dotfiles/fish/config.fish -- edits made HERE
# are invisible to the repository and will not follow you to another machine.
#
# This file exists so that installers which append managed blocks to
# config.fish write to a machine-local file instead of into published content.
# fish sources conf.d/*.fish before this file, so anything appended below lands
# last and therefore wins -- the same ordering argument
# git/gitconfig.local.template makes for [include].

source $HOME/dotfiles/fish/config.fish

# --- installer-managed blocks appear below this line ---
```

- [ ] **Step 4: Rewrite the tracked config**

Replace the first line of `fish/config.fish` and delete the grok block at the end. The head becomes:

```fish
# Shared fish configuration, tracked in dotfiles.
#
# Sourced by the machine-local ~/.config/fish/config.fish stub, which is seeded
# from fish/config.fish.template. Siblings resolve from this file's own
# directory, so the clone location appears exactly once on a machine and this
# file works unchanged from a git worktree or a relocated clone.

set -l __dotfiles_fish (status dirname)
source $__dotfiles_fish/alias.fish
source $__dotfiles_fish/mypre.fish
```

and the tail — replacing `source ~/.config/fish/mypost.fish` and the whole
`# >>> grok installer >>>` … `# <<< grok installer <<<` block:

```fish
# sourcing the post scripts
source $__dotfiles_fish/mypost.fish
```

The grok block moves to the machine-local stub during Task 10's fish migration.
It is *not* copied into `config.fish.template`: the template is what a fresh
machine gets, and a fresh machine has no grok install.

- [ ] **Step 5: Fix the two fish functions**

In `fish/mypre.fish`, replace `install_fisher`:

```fish
# >>> fisher plugin manager >>>
function install_fisher
    set --local plugins (read --null <(status dirname)/fishfile)
    curl -sL https://git.io/fisher | source && fisher install jorgebucaran/fisher $plugins
end
# <<< fisher plugin manager <<<
```

and replace `fish_reset_all`:

```fish
# Remove fisher's generated state. It lives under $__fish_config_dir, which is
# machine-local -- never in the repository. It is also not portable between
# machines (fish_variables encodes absolute paths), which is why resetting it
# is a normal operation rather than a repair.
function fish_reset_all
    echo '' >$__fish_config_dir/fish_plugins
    rm -f $__fish_config_dir/fish_variables
    rm -rf $__fish_config_dir/functions $__fish_config_dir/completions $__fish_config_dir/conf.d
    install_fisher
    reload
end
```

- [ ] **Step 6: Drop the five stale `.gitignore` lines**

Delete these from `.gitignore`:

```
fish/fish_variables
fish/fish_plugins
fish/functions/
fish/completions/
fish/conf.d/
```

- [ ] **Step 7: Run to verify it passes**

Run: `cd bootstrap.d && go test ./... -run Fish`
Expected: PASS, four tests.

- [ ] **Step 8: Commit**

```bash
git add fish/config.fish.template fish/config.fish fish/mypre.fish .gitignore bootstrap.d/fish_test.go
git commit -m "refactor(fish): stop installers writing into the repository

~/.config/fish was a symlink to fish/, so fisher wrote functions/,
completions/, conf.d/, fish_plugins and fish_variables into tracked
content, and installers appended managed blocks straight into
config.fish -- the grok block in this diff is one of them.

Per-file symlinks do not fix that: the file that must be tracked is the
same file installers append to. So ~/.config/fish/config.fish becomes a
seeded machine-local stub that sources the tracked config, and the five
.gitignore lines that were holding back those writes are no longer
load-bearing."
```

---

### Task 9: Git file renames and the include repoint

**Files:**
- Rename: `git/gitconfig.symlink` → `git/gitconfig.shared`
- Rename: `git/gitignore_global.symlink` → `git/gitignore_global`
- Modify: `git/gitconfig.local.template`
- Modify: `git/gitconfig.shared` (header comment)
- Modify: `bootstrap.d/check_test.go` (remove the Task 6 skips)

**Interfaces:**
- Consumes: the manifest row `link git/gitignore_global .gitignore *` (Task 2).
- Produces: the path `git/gitconfig.shared`, which `check_gitconfig_include` (Task 6) greps for.

- [ ] **Step 1: Rename both files**

```bash
git mv git/gitconfig.symlink git/gitconfig.shared
git mv git/gitignore_global.symlink git/gitignore_global
```

- [ ] **Step 2: Repoint the template's include**

In `git/gitconfig.local.template`, change:

```
	path = ~/dotfiles/git/gitconfig.symlink
```

to:

```
	path = ~/dotfiles/git/gitconfig.shared
```

and in the same file's prose, change `~/dotfiles/git/gitconfig.symlink` to
`~/dotfiles/git/gitconfig.shared`.

- [ ] **Step 3: Fix the renamed file's own header**

`git/gitconfig.shared` contains a comment referring to itself as a symlink
target. Replace:

```
# If git says "Please tell me who you are", the fix is to set up extras.secret --
# not to run `git config --global user.email ...`. That writes into ~/.gitconfig,
# and if ~/.gitconfig is a symlink to this file, the value lands in this public
# repo and silently overrides the private include below.
```

with:

```
# If git says "Please tell me who you are", the fix is to set up extras.secret --
# not to run `git config --global user.email ...`. ~/.gitconfig is a machine-local
# file that INCLUDES this one (see git/gitconfig.local.template); this file is
# public and must never receive identity.
```

- [ ] **Step 4: Confirm nothing still references the old names**

Run: `grep -rn 'gitconfig\.symlink\|gitignore_global\.symlink' . --exclude-dir=.git --exclude-dir=docs`
Expected: **no output.** `Makefile` line 39 references `gitignore_global.symlink` and must be updated to `git/gitignore_global` — Task 15 removes the target entirely, but leaving a broken path in the interim would make `make dotfiles` fail for anyone who runs it before then.

- [ ] **Step 5: Remove the Task 6 skips**

Delete the five `t.Skip("unskip in Task 9: ...")` lines from `bootstrap.d/check_test.go`.

- [ ] **Step 6: Run the whole suite**

Run: `cd bootstrap.d && go test ./...`
Expected: PASS, nothing skipped.

- [ ] **Step 7: Commit**

```bash
git add -A git/ Makefile bootstrap.d/check_test.go
git commit -m "refactor(git): rename gitconfig.symlink to gitconfig.shared

The suffix stopped carrying information, for two different reasons. This
file is *included* by a machine-local ~/.gitconfig since 37f00a0, not
symlinked to it; gitignore_global is still genuinely symlinked, but the
manifest's kind column now says so.

Existing machines include the old path and will silently lose every
shared git setting until migrated -- ~/.gitconfig is a seed row, so
bootstrap will never rewrite it. The gitconfig-include check catches
that, and the migration in the next commit fixes it."
```

---

### Task 10: `migrate` — reconciling migrations

**Files:**
- Create: `bootstrap.d/migrations.sh`
- Modify: `bootstrap` (source it)
- Modify: `bootstrap.d/phase-00-preflight.sh` (delete the temporary stub)
- Test: `bootstrap.d/migrate_test.go`

**Interfaces:**
- Consumes: `refuse`, `do_run` (Task 1).
- Produces: `bootstrap_migrate <name-or-empty>`; `migrations_require_none <root> <home>` used by preflight; migration names `fish` and `gitconfig`.

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/migrate_test.go`:

```go
package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateFishMovesStateOutOfTheRepo(t *testing.T) {
	f := newFixture(t)
	// Reproduce the legacy layout: ~/.config/fish is a symlink to a directory
	// holding fisher's generated state.
	repoFish := filepath.Join(f.home, "fake-repo", "fish")
	if err := os.MkdirAll(filepath.Join(repoFish, "functions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoFish, "fish_variables"), []byte("SETUVAR x:1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoFish, "functions", "mine.fish"), []byte("function mine\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repoFish, filepath.Join(f.home, ".config", "fish")); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := f.runBootstrap(t, "migrate", "fish")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}

	target := filepath.Join(f.home, ".config", "fish")
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("~/.config/fish should be a real directory after migration")
	}
	for _, want := range []string{"fish_variables", filepath.Join("functions", "mine.fish")} {
		if _, err := os.Stat(filepath.Join(target, want)); err != nil {
			t.Errorf("migration lost %s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(repoFish, "fish_variables")); !os.IsNotExist(err) {
		t.Errorf("fisher state should no longer be in the repo directory")
	}
}

func TestMigrateFishIsIdempotent(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Join(f.home, ".config", "fish"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := f.runBootstrap(t, "migrate", "fish")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "nothing to do") {
		t.Errorf("an already-migrated machine should report nothing to do:\n%s", stdout)
	}
}

func TestMigrateGitconfigRepointsStaleInclude(t *testing.T) {
	f := newFixture(t)
	gc := filepath.Join(f.home, ".gitconfig")
	body := "[include]\n\tpath = ~/dotfiles/git/gitconfig.symlink\n\n[user]\n\tname = keep me\n"
	if err := os.WriteFile(gc, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := f.runBootstrap(t, "migrate", "gitconfig"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, _ := os.ReadFile(gc)
	content := string(data)
	if strings.Contains(content, "gitconfig.symlink") {
		t.Errorf("stale include survived: %s", content)
	}
	if !strings.Contains(content, "gitconfig.shared") {
		t.Errorf("include was not repointed: %s", content)
	}
	if !strings.Contains(content, "keep me") {
		t.Errorf("migration destroyed unrelated machine-local settings: %s", content)
	}
}

func TestBareMigrateRunsReconcilingOnes(t *testing.T) {
	f := newFixture(t)
	gc := filepath.Join(f.home, ".gitconfig")
	if err := os.WriteFile(gc, []byte("[include]\n\tpath = ~/dotfiles/git/gitconfig.symlink\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := f.runBootstrap(t, "migrate"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, _ := os.ReadFile(gc)
	if !strings.Contains(string(data), "gitconfig.shared") {
		t.Errorf("bare migrate should run reconciling migrations")
	}
}

func TestUnknownMigrationIsMalformedInput(t *testing.T) {
	f := newFixture(t)
	_, stderr, code := f.runBootstrap(t, "migrate", "nonsense")
	if code != 3 {
		t.Fatalf("exit %d, want 3", code)
	}
	if !strings.Contains(stderr, "nonsense") {
		t.Errorf("error should name the migration: %s", stderr)
	}
}

func TestPreflightRefusesWhenAMigrationIsPending(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Join(f.home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.root, "fish"), filepath.Join(f.home, ".config", "fish")); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := f.runBootstrap(t, "apply", "dotfiles")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "bootstrap migrate") {
		t.Errorf("preflight must name the remedy: %s", stderr)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run Migrat`
Expected: FAIL — `bootstrap_migrate: command not found`.

- [ ] **Step 3: Write `migrations.sh`**

Create `bootstrap.d/migrations.sh`:

```bash
# shellcheck shell=bash
#
# Declared, idempotent, one-time operations. Kept out of `apply` so that §5's
# refuse-never-clobber invariant stays intact and the code that knows about the
# past is quarantined where it can be deleted once no machine needs it.
#
#   reconciling  moves or rewrites; destroys nothing; runs from a bare `migrate`
#   reclaiming   irreversibly deletes untracked data; runs ONLY when named
#
# Adding one means adding <name>_pending and <name>_migrate, then listing the
# name in BOOTSTRAP_RECONCILING or BOOTSTRAP_RECLAIMING.

BOOTSTRAP_RECONCILING='fish gitconfig'
BOOTSTRAP_RECLAIMING=''

# --- fish: move fisher's state out of the repo -------------------------------

fish_pending() {
	install_home=$1
	[ -L "$install_home/.config/fish" ]
}

fish_migrate() {
	install_home=$1
	target=$install_home/.config/fish
	legacy=$(readlink "$target") || refuse "cannot read the symlink at '$target'"
	[ -d "$legacy" ] || refuse "'$target' points at '$legacy', which is not a directory"

	staging=$target.migrating.$$
	do_run mkdir -p "$staging"
	for item in fish_variables fish_plugins functions completions conf.d; do
		if [ -e "$legacy/$item" ]; then
			# Copy first, remove the source only after every copy succeeded, so
			# an interrupted migration leaves the old state intact.
			do_run cp -R "$legacy/$item" "$staging/$item"
		fi
	done
	do_run rm -f "$target"
	do_run mv "$staging" "$target"
	for item in fish_variables fish_plugins functions completions conf.d; do
		if [ -e "$legacy/$item" ]; then
			do_run rm -rf "$legacy/$item"
		fi
	done
	did "moved fisher state out of '$legacy' into '$target'"
}

# --- gitconfig: repoint a stale include --------------------------------------

gitconfig_pending() {
	install_home=$1
	config=$install_home/.gitconfig
	[ -f "$config" ] && grep -q 'git/gitconfig\.symlink' "$config"
}

gitconfig_migrate() {
	install_home=$1
	config=$install_home/.gitconfig
	scratch=$config.migrating.$$
	do_run sed 's|git/gitconfig\.symlink|git/gitconfig.shared|g' "$config" >"$scratch"
	do_run mv "$scratch" "$config"
	did "repointed the include in '$config' to git/gitconfig.shared"
}

# --- driver ------------------------------------------------------------------

migrations_pending() {
	install_home=$1
	found=''
	for name in $BOOTSTRAP_RECONCILING $BOOTSTRAP_RECLAIMING; do
		if "${name}_pending" "$install_home"; then
			found="$found $name"
		fi
	done
	printf '%s\n' "${found# }"
}

# Called by preflight. Apply must not proceed with a pending migration, because
# the refusals it would hit are unhelpful compared with naming the remedy here.
migrations_require_none() {
	install_home=$2
	pending=$(migrations_pending "$install_home")
	if [ -n "$pending" ]; then
		refuse "this machine has pending migrations ($pending); run 'bootstrap migrate' first"
	fi
}

migration_is_reclaiming() {
	for name in $BOOTSTRAP_RECLAIMING; do
		[ "$name" = "$1" ] && return 0
	done
	return 1
}

bootstrap_migrate() {
	requested=${1:-}

	if [ -n "$requested" ]; then
		known=0
		for name in $BOOTSTRAP_RECONCILING $BOOTSTRAP_RECLAIMING; do
			[ "$name" = "$requested" ] && known=1
		done
		if [ "$known" -eq 0 ]; then
			printf 'bootstrap: unknown migration "%s"; known: %s\n' \
				"$requested" "$BOOTSTRAP_RECONCILING $BOOTSTRAP_RECLAIMING" >&2
			exit 3
		fi
		if ! "${requested}_pending" "$HOME"; then
			log "migrate $requested: nothing to do"
			return 0
		fi
		"${requested}_migrate" "$HOME"
		return 0
	fi

	ran=0
	for name in $BOOTSTRAP_RECONCILING; do
		if "${name}_pending" "$HOME"; then
			"${name}_migrate" "$HOME"
			ran=$((ran + 1))
		fi
	done
	[ "$ran" -gt 0 ] || log "migrate: nothing to do"

	for name in $BOOTSTRAP_RECLAIMING; do
		if "${name}_pending" "$HOME"; then
			log "reclaimable: $name -- run './bootstrap migrate $name' (irreversible)"
		fi
	done
}
```

- [ ] **Step 4: Wire it in**

In `bootstrap`, add after the `checks.sh` source line:

```bash
. "$BOOTSTRAP_ROOT/bootstrap.d/migrations.sh"
```

Migrations must be sourced **before** the phase files, because
`phase-00-preflight.sh` calls `migrations_require_none`. Move the
`. "$BOOTSTRAP_ROOT/bootstrap.d/migrations.sh"` line above the
`for phase_file in ...` loop.

Delete the temporary stub at the bottom of `bootstrap.d/phase-00-preflight.sh`:

```bash
# Replaced by bootstrap.d/migrations.sh in Task 10.
if ! command -v migrations_require_none >/dev/null 2>&1; then
	migrations_require_none() { :; }
fi
```

- [ ] **Step 5: Run to verify it passes**

Run: `cd bootstrap.d && go test ./...`
Expected: PASS. `migrations.sh` calls `mkdir`, `cp`, `rm` and `mv` **through `do_run`**, so the first word of each statement is `do_run` and the structural test does not flag them. If it does flag one, the fix is to route that call through `do_run` — never to weaken the test.

- [ ] **Step 6: Commit**

```bash
git add bootstrap bootstrap.d/migrations.sh bootstrap.d/phase-00-preflight.sh bootstrap.d/migrate_test.go
git commit -m "feat(bootstrap): add reconciling migrations

Two exist: move fisher's untracked state out of the repository, and
repoint a ~/.gitconfig that includes the pre-rename path.

The fish migration copies before it removes, so an interrupted run leaves
the old state intact -- fish_variables and the installed plugin set are
not in git and cannot be recovered if lost.

Preflight refuses when a migration is pending and names the remedy,
rather than letting apply hit a bare refusal that explains nothing."
```

---

### Task 11: `migrate` — the reclaiming kind

**Files:**
- Modify: `bootstrap.d/migrations.sh`
- Modify: `bootstrap.d/migrate_test.go`

**Interfaces:**
- Consumes: the driver from Task 10.
- Produces: migration name `mambaforge`; `BOOTSTRAP_RECLAIMING` becomes non-empty.

- [ ] **Step 1: Write the failing tests**

Append to `bootstrap.d/migrate_test.go`:

```go
func TestBareMigrateListsButDoesNotRunReclaiming(t *testing.T) {
	f := newFixture(t)
	forge := filepath.Join(f.home, "sdk", "mambaforge")
	if err := os.MkdirAll(forge, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(forge, "marker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := f.runBootstrap(t, "migrate")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "mambaforge") || !strings.Contains(stdout, "irreversible") {
		t.Errorf("bare migrate must list reclaiming migrations with a warning:\n%s", stdout)
	}
	if !strings.Contains(stdout, "bootstrap migrate mambaforge") {
		t.Errorf("bare migrate must print the exact command:\n%s", stdout)
	}
	if _, err := os.Stat(forge); err != nil {
		t.Errorf("bare migrate must not delete anything: %v", err)
	}
}

func TestNamedReclaimingMigrationRuns(t *testing.T) {
	f := newFixture(t)
	forge := filepath.Join(f.home, "sdk", "mambaforge")
	if err := os.MkdirAll(forge, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := f.runBootstrap(t, "migrate", "mambaforge"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(forge); !os.IsNotExist(err) {
		t.Errorf("named reclaiming migration should have removed the directory: %v", err)
	}
}

func TestReclaimingMigrationRefusesWhenStillInUse(t *testing.T) {
	f := newFixture(t)
	bin := filepath.Join(f.home, "sdk", "mambaforge", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(bin, "conda")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Put the directory on PATH so `command -v conda` resolves inside it.
	cmdEnv := append(os.Environ(), "HOME="+f.home, "PATH="+bin+":"+os.Getenv("PATH"))
	stdout, stderr, code := f.runBootstrapEnv(t, cmdEnv, "migrate", "mambaforge")
	if code != 2 {
		t.Fatalf("exit %d, want 2\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "PATH") {
		t.Errorf("refusal should explain that something still resolves inside it: %s", stderr)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Errorf("a refused migration must not delete anything: %v", err)
	}
}
```

Add the env-aware runner to `bootstrap.d/harness_test.go`:

```go
func (f fixture) runBootstrapEnv(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(filepath.Join(f.root, "bootstrap"), args...)
	cmd.Dir = t.TempDir()
	cmd.Env = env
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run 'Reclaim|BareMigrate'`
Expected: FAIL — `unknown migration "mambaforge"`, exit 3.

- [ ] **Step 3: Add the migration**

In `bootstrap.d/migrations.sh`, change the declaration:

```bash
BOOTSTRAP_RECLAIMING='mambaforge'
```

and add before the driver section:

```bash
# --- mambaforge: reclaim 3.5 GB of a conda install superseded by uv ----------
#
# RECLAIMING: this deletes untracked data irreversibly, so it never runs from a
# bare `migrate`. Measured 2026-08-10: nothing on PATH resolved inside it, and
# its four environments were named by Python version alone -- exactly what uv
# now provides.

mambaforge_pending() {
	install_home=$1
	[ -d "$install_home/sdk/mambaforge" ]
}

mambaforge_migrate() {
	install_home=$1
	forge=$install_home/sdk/mambaforge
	for tool in conda mamba micromamba python python3 pip; do
		resolved=$(command -v "$tool" 2>/dev/null || true)
		case "$resolved" in
			"$forge"/*)
				refuse "'$tool' resolves to '$resolved', inside '$forge'; it is still on PATH and nothing was deleted"
				;;
		esac
	done
	do_run rm -rf "$forge"
	did "reclaimed '$forge'"
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd bootstrap.d && go test ./...`
Expected: PASS, everything.

- [ ] **Step 5: Commit**

```bash
git add bootstrap.d/migrations.sh bootstrap.d/migrate_test.go bootstrap.d/harness_test.go
git commit -m "feat(bootstrap): add reclaiming migrations, starting with mambaforge

Reclaiming migrations destroy untracked data irreversibly, so a bare
migrate lists them with the exact command and runs none of them. Nothing
has to be rediscovered later, and a routine invocation can never delete
anything.

mambaforge refuses if any of conda, mamba, micromamba, python, python3 or
pip still resolves inside the directory."
```

---

### Task 12: Phase 40 — devtools

**Files:**
- Modify: `bootstrap.d/phase-40-devtools.sh`
- Test: `bootstrap.d/devtools_test.go`

**Interfaces:**
- Consumes: `do_run`, `do_dir` (Task 1).
- Produces: `phase_devtools <root> <home> <platform> <profile>`.

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/devtools_test.go`:

```go
package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The devtools phase must delegate hook installation to git/install-hooks.sh
// rather than reimplementing it. A stub records how it was called.
func TestDevtoolsDelegatesToInstallHooks(t *testing.T) {
	f := newFixture(t)
	record := filepath.Join(f.home, "install-hooks.args")

	stub := filepath.Join(f.home, "stub-install-hooks.sh")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + record + "\"\nexit 0\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := f.runLib(t, `
. "$BOOTSTRAP_ROOT/bootstrap.d/phase-40-devtools.sh"
BOOTSTRAP_INSTALL_HOOKS="`+stub+`"
BOOTSTRAP_SKIP_BUILD=1
phase_devtools "$BOOTSTRAP_ROOT" "$HOME" darwin workstation
`)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}

	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("install-hooks.sh was never invoked: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(args) != 4 {
		t.Fatalf("expected 4 arguments, got %d: %q", len(args), args)
	}
	if args[0] != "install" {
		t.Errorf("arg 0 = %q, want \"install\"", args[0])
	}
	if args[1] != f.root {
		t.Errorf("arg 1 = %q, want the repo root %q", args[1], f.root)
	}
	if args[2] != f.home {
		t.Errorf("arg 2 = %q, want the redirected HOME %q", args[2], f.home)
	}
	if args[3] != filepath.Join(f.home, "bin", "agents") {
		t.Errorf("arg 3 = %q, want $HOME/bin/agents", args[3])
	}
}

func TestDevtoolsPlanInvokesNothing(t *testing.T) {
	f := newFixture(t)
	record := filepath.Join(f.home, "install-hooks.args")
	stub := filepath.Join(f.home, "stub-install-hooks.sh")
	body := "#!/bin/sh\nprintf 'ran\\n' > \"" + record + "\"\nexit 0\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := f.runLib(t, `
. "$BOOTSTRAP_ROOT/bootstrap.d/phase-40-devtools.sh"
BOOTSTRAP_DRY_RUN=1
BOOTSTRAP_INSTALL_HOOKS="`+stub+`"
BOOTSTRAP_SKIP_BUILD=1
phase_devtools "$BOOTSTRAP_ROOT" "$HOME" darwin workstation
`)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Errorf("plan mode must not invoke install-hooks.sh")
	}
	if !strings.Contains(stdout, "install-hooks") {
		t.Errorf("plan should say it would run the installer:\n%s", stdout)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run Devtools`
Expected: FAIL — the stub phase does nothing, so the record file never appears.

- [ ] **Step 3: Implement the phase**

Replace `bootstrap.d/phase-40-devtools.sh`:

```bash
# shellcheck shell=bash
#
# Global Git hooks are NOT reimplemented here. git/install-hooks.sh already
# validates ~/.gitconfig, links ~/.gitattributes, symlinks the four hook names
# and writes core.hooksPath last so a partial install cannot activate an
# incomplete directory -- and it has tests. This phase invokes it.

phase_devtools() {
	root=$1
	install_home=$2
	# shellcheck disable=SC2034
	platform=$3
	# shellcheck disable=SC2034
	profile=$4

	log "== devtools"

	if command -v uv >/dev/null 2>&1; then
		log "   uv already installed"
	else
		do_run brew install uv
	fi

	agents_binary=$install_home/bin/agents
	if [ "${BOOTSTRAP_SKIP_BUILD:-0}" -eq 1 ]; then
		log "   skipping the agents build (BOOTSTRAP_SKIP_BUILD=1)"
	else
		do_dir "$install_home/bin"
		# A subshell rather than `sh -c "cd '$root' && ..."`: interpolating a
		# path into a quoted shell string breaks on any path containing a quote.
		# set -e propagates a failing subshell to the caller.
		( cd "$root/agents" && do_run go build -trimpath -o "$agents_binary" . )
	fi

	installer=${BOOTSTRAP_INSTALL_HOOKS:-$root/git/install-hooks.sh}
	do_run bash "$installer" install "$root" "$install_home" "$agents_binary"
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd bootstrap.d && go test ./... -run Devtools`
Expected: PASS, two tests.

- [ ] **Step 5: Commit**

```bash
git add bootstrap.d/phase-40-devtools.sh bootstrap.d/devtools_test.go
git commit -m "feat(bootstrap): add the devtools phase

Git hooks delegate to git/install-hooks.sh rather than being
reimplemented, so this phase's tests only need to assert it was invoked
with the right arguments -- which is why the bootstrap module needs none
of the agents module's git-specific test helpers."
```

---

### Task 13: Phase 10 — packages and the Brewfile

**Files:**
- Modify: `bootstrap.d/phase-10-packages.sh`
- Create: `bootstrap.d/Brewfile`
- Delete: `super-install-dep.sh`, `user-install-dep.sh`, `brew/`
- Test: `bootstrap.d/packages_test.go`

**Interfaces:**
- Consumes: `do_run`, `do_sudo` (Task 1).
- Produces: `phase_packages <root> <home> <platform> <profile>`.

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/packages_test.go`:

```go
package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackagesPlanNamesStageZeroAndBrewfile(t *testing.T) {
	f := newFixture(t)
	stdout, stderr, code := f.runLib(t, `
. "$BOOTSTRAP_ROOT/bootstrap.d/phase-10-packages.sh"
BOOTSTRAP_DRY_RUN=1
phase_packages "$BOOTSTRAP_ROOT" "$HOME" linux workstation
`)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "brew bundle") {
		t.Errorf("plan should name the Brewfile step:\n%s", stdout)
	}
	if !strings.Contains(stdout, "build-essential") && !strings.Contains(stdout, "base-devel") {
		t.Errorf("plan should name a Linux stage-zero package set:\n%s", stdout)
	}
}

func TestBrewfileIsPlatformSectioned(t *testing.T) {
	f := newFixture(t)
	data, err := os.ReadFile(filepath.Join(f.root, "bootstrap.d", "Brewfile"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, `OS.mac?`) {
		t.Errorf("casks are macOS-only and must be guarded by OS.mac?:\n%s", content)
	}
	for _, gone := range []string{"micromamba", "youtube-dl"} {
		if strings.Contains(content, gone) {
			t.Errorf("Brewfile still lists %q, which the audit removed", gone)
		}
	}
	for _, want := range []string{"fish", "go", "starship", "gitleaks", "uv"} {
		if !strings.Contains(content, want) {
			t.Errorf("Brewfile omits %q, which the design depends on", want)
		}
	}
}

func TestOldInstallerScriptsAreGone(t *testing.T) {
	f := newFixture(t)
	for _, gone := range []string{"super-install-dep.sh", "user-install-dep.sh", "brew"} {
		if _, err := os.Stat(filepath.Join(f.root, gone)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed", gone)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run 'Packages|Brewfile|OldInstaller'`
Expected: FAIL on all three.

- [ ] **Step 3: Write the Brewfile**

The audit: `micromamba` goes (superseded by `uv`); `youtube-dl` goes (unmaintained since 2021, `yt-dlp` is the live fork); `mactex` and `macfuse` are retained but cask-guarded; `gitleaks` and `uv` are **added** because spec 1's `agents guard` and this spec's phase 40 both require them and neither was ever declared.

Create `bootstrap.d/Brewfile`:

```ruby
# Packages for both macOS and Linux. Homebrew serves both; the native package
# manager installs only Homebrew's own prerequisites (see phase-10-packages.sh).
#
# Audited 2026-08-10 against brew/brew-{frameworks,utils,casks}.list:
#   removed  micromamba   superseded by uv
#   removed  youtube-dl   unmaintained since 2021; yt-dlp is the live fork
#   added    gitleaks     agents guard --staged requires it (spec 1 §"Open questions")
#   added    uv           phase 40 requires it

# --- frameworks and languages ---
brew "bazelisk"
brew "boost"
brew "fish"
brew "go"
brew "node"
brew "rustup-init"
brew "tmux"
brew "yarn"

# --- utilities ---
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

# --- macOS only ---
# Casks do not exist on Linux; an unguarded cask line aborts the whole bundle.
if OS.mac?
  cask "macfuse"
  cask "mactex"
  cask "font-symbols-only-nerd-font"
end
```

Note: `font-symbols-only-nerd-font` replaces the vendored `install-font-linux.sh`
removed in Task 15.

- [ ] **Step 4: Implement the phase**

Replace `bootstrap.d/phase-10-packages.sh`:

```bash
# shellcheck shell=bash
#
# Stage zero installs only what Homebrew itself needs. Everything above it
# comes from one Brewfile on both platforms -- per-distro native lists would
# mean two or three manifests kept in sync across distros whose package names
# disagree (fd vs fd-find, bat vs batcat).

phase_packages() {
	root=$1
	# shellcheck disable=SC2034
	install_home=$2
	platform=$3
	# shellcheck disable=SC2034
	profile=$4

	log "== packages"

	if [ "$platform" = linux ]; then
		if command -v apt-get >/dev/null 2>&1; then
			do_sudo apt-get update
			do_sudo apt-get install -y build-essential curl file git
		elif command -v pacman >/dev/null 2>&1; then
			do_sudo pacman -S --needed --noconfirm base-devel curl file git
		else
			refuse "no supported Linux package manager found (looked for apt-get and pacman)"
		fi
	fi

	if command -v brew >/dev/null 2>&1; then
		log "   Homebrew already installed"
	else
		# The only place this tool executes remote code. It is Homebrew's own
		# documented installation method, it runs only when brew is absent, and
		# it is skipped entirely in plan mode. Pin nothing here: the installer
		# URL is the contract Homebrew supports.
		do_run /bin/bash -c \
			"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
	fi

	do_run brew bundle --file "$root/bootstrap.d/Brewfile"
}
```

- [ ] **Step 5: Remove the superseded installers**

```bash
git rm super-install-dep.sh user-install-dep.sh
git rm -r brew/
```

- [ ] **Step 6: Run to verify it passes**

Run: `cd bootstrap.d && go test ./...`
Expected: PASS. `TestNoBareMutatingCommandsOutsideLib` must still pass — every `brew`, `apt-get` and `pacman` call above goes through `do_run`/`do_sudo`.

- [ ] **Step 7: Commit**

```bash
git add bootstrap.d/Brewfile bootstrap.d/phase-10-packages.sh bootstrap.d/packages_test.go
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

### Task 14: Phase 30 — fish

**Files:**
- Modify: `bootstrap.d/phase-30-fish.sh`
- Test: `bootstrap.d/fish_phase_test.go`

**Interfaces:**
- Consumes: `do_run`, `do_sudo` (Task 1).
- Produces: `phase_fish <root> <home> <platform> <profile>`.

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/fish_phase_test.go`:

```go
package bootstrap_test

import (
	"strings"
	"testing"
)

func TestFishPhasePlanCoversShellsChshAndPlugins(t *testing.T) {
	f := newFixture(t)
	stdout, stderr, code := f.runLib(t, `
. "$BOOTSTRAP_ROOT/bootstrap.d/phase-30-fish.sh"
BOOTSTRAP_DRY_RUN=1
SHELL=/bin/bash
phase_fish "$BOOTSTRAP_ROOT" "$HOME" darwin workstation
`)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"/etc/shells", "chsh", "fisher"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("plan omits %q:\n%s", want, stdout)
		}
	}
}

func TestFishPhaseSkipsChshWhenAlreadyFish(t *testing.T) {
	f := newFixture(t)
	stdout, stderr, code := f.runLib(t, `
. "$BOOTSTRAP_ROOT/bootstrap.d/phase-30-fish.sh"
BOOTSTRAP_DRY_RUN=1
SHELL=/opt/homebrew/bin/fish
phase_fish "$BOOTSTRAP_ROOT" "$HOME" darwin workstation
`)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if strings.Contains(stdout, "chsh") {
		t.Errorf("must not plan a shell change when the login shell is already fish:\n%s", stdout)
	}
}

func TestFishPhaseRefusesWithoutFish(t *testing.T) {
	f := newFixture(t)
	// An empty PATH makes `command -v fish` fail deterministically.
	_, stderr, code := f.runLib(t, `
. "$BOOTSTRAP_ROOT/bootstrap.d/phase-30-fish.sh"
BOOTSTRAP_DRY_RUN=1
PATH=/nonexistent
phase_fish "$BOOTSTRAP_ROOT" "$HOME" darwin workstation
`)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "packages phase") {
		t.Errorf("refusal should point at the phase that installs fish: %s", stderr)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run FishPhase`
Expected: FAIL — the stub prints "not implemented".

- [ ] **Step 3: Implement the phase**

Replace `bootstrap.d/phase-30-fish.sh`:

```bash
# shellcheck shell=bash
#
# Plugins are installed explicitly here rather than by a shell-start side
# effect: a provisioning step that only happens if you happen to open a shell
# is not a provisioning step.

phase_fish() {
	root=$1
	# shellcheck disable=SC2034
	install_home=$2
	# shellcheck disable=SC2034
	platform=$3
	# shellcheck disable=SC2034
	profile=$4

	log "== fish"

	fish_path=$(command -v fish 2>/dev/null || true)
	if [ -z "$fish_path" ]; then
		refuse "fish is not on PATH; the packages phase installs it -- run './bootstrap apply workstation'"
	fi

	if grep -qxF "$fish_path" /etc/shells 2>/dev/null; then
		log "   $fish_path is already in /etc/shells"
	else
		# `tee -a` rather than `sudo sh -c "... >> /etc/shells"`: the path never
		# enters a shell string, so a quote in it cannot change what runs.
		printf '%s\n' "$fish_path" | do_sudo tee -a /etc/shells >/dev/null
	fi

	case "${SHELL:-}" in
		*fish)
			log "   login shell is already fish"
			;;
		*)
			do_sudo chsh -s "$fish_path" "$(id -un)"
			;;
	esac

	# fish expands BOOTSTRAP_ROOT itself, so bash never interpolates a path into
	# the command string. The dispatcher exports it; export here too, because
	# the phase is also sourced directly by its test.
	BOOTSTRAP_ROOT=$root
	export BOOTSTRAP_ROOT
	do_run fish -c 'source $BOOTSTRAP_ROOT/fish/mypre.fish; install_fisher'
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd bootstrap.d && go test ./... -run FishPhase`
Expected: PASS, three tests.

- [ ] **Step 5: Commit**

```bash
git add bootstrap.d/phase-30-fish.sh bootstrap.d/fish_phase_test.go
git commit -m "feat(bootstrap): add the fish phase

The Makefile's chsh was guarded by 'sudo -v || if [ -z \$? ]', which skips
on both branches and so has never run -- corroborated by fish being the
login shell while /etc/shells contains no fish entry at all. This phase
adds the entry before changing the shell, and changes nothing when the
login shell is already fish.

Plugins install explicitly rather than through a shell-start side effect."
```

---

### Task 15: The removal campaign

**Files:**
- Delete: `zsh/`, `tools/`, `miniforge/`, `snapshot.sh`, `recover.sh`, `mountcrypt.sh`, `mountsshfs.sh`, `post-install.sh`, `softlinks.sh`, `install-font-linux.sh`, `bin/`, `spacemacs/`, `git/hooks/go.pre-commit`, `gnupg/`, `macOS/iterm2/`
- Modify: `fish/mypost.fish`, `fish/alias.fish`
- Test: `bootstrap.d/removals_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: nothing. Pure deletion.

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/removals_test.go`:

```go
package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemovedPathsAreGone(t *testing.T) {
	f := newFixture(t)
	gone := []string{
		"zsh", "tools", "miniforge", "bin", "spacemacs", "gnupg",
		"macOS/iterm2", "git/hooks/go.pre-commit",
		"snapshot.sh", "recover.sh", "mountcrypt.sh", "mountsshfs.sh",
		"post-install.sh", "softlinks.sh", "install-font-linux.sh",
	}
	for _, path := range gone {
		if _, err := os.Stat(filepath.Join(f.root, path)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed", path)
		}
	}
}

func TestNoConfigReferencesRemovedTooling(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"mypost.fish", "alias.fish", "mypre.fish"} {
		data, err := os.ReadFile(filepath.Join(f.root, "fish", name))
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)
		for _, gone := range []string{"mambaforge", "MAMBA_EXE", "micromamba"} {
			if strings.Contains(content, gone) {
				t.Errorf("fish/%s still references %q", name, gone)
			}
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run 'Removed|NoConfigReferences'`
Expected: FAIL — every listed path still exists.

- [ ] **Step 3: Remove, one commit per group**

```bash
git rm -r zsh/
git commit -m "chore(remove): drop zsh

344 tracked files, mostly vendored oh-my-zsh themes and plugins. The login
shell has been fish for years and ~/.oh-my-zsh no longer exists on this
machine. Recover with: git show HEAD~1:zsh/<path>"
```

```bash
git rm -r tools/
git commit -m "chore(remove): drop tools/

Four scripts for etcd, protoc and a mysql compose file, two of them
#!/usr/bin/env zsh, unreferenced by anything and untouched since 2021.
Recover with: git show HEAD~1:tools/<path>"
```

```bash
git rm -r miniforge/
git commit -m "chore(remove): drop miniforge/ and the mamba wiring

Python environments are managed by uv now. micromamba was already off
PATH, so the mamba init block in fish/mypost.fish was inert.

~/sdk/mambaforge itself (3.5 GB) is reclaimed by './bootstrap migrate
mambaforge'. Recover with: git show HEAD~1:miniforge/<path>"
```

Before that third commit, also edit `fish/mypost.fish` — delete the commented
`# >>> conda initialize >>>` block and the live `# >>> mamba initialize >>>`
block, leaving only the mojo block — and delete the `micromamba` branch from
`fish/alias.fish`:

```fish
if type -q (command -v micromamba)
    alias mamba=micromamba
    alias conda=micromamba
end
```

```bash
git rm snapshot.sh recover.sh mountcrypt.sh mountsshfs.sh post-install.sh
git commit -m "chore(remove): drop unreferenced personal scripts

snapshot.sh and recover.sh use BSD-only 'date -j' and an rclone remote
from 2021. mountcrypt.sh points at the Intel /usr/local encfs path.
mountsshfs.sh has an inverted -d guard and a host that no longer exists.
post-install.sh is not valid bash at all: 'bash -n' reports a syntax
error at line 9 from its empty then-branches.

None is referenced by anything. Recover with: git show HEAD~1:<name>"
```

```bash
git rm softlinks.sh install-font-linux.sh
git commit -m "chore(remove): drop softlinks.sh and the vendored font installer

softlinks.sh is superseded by the config phase, which carries its
alacritty and ghostty links as manifest rows -- and resolves the repo
root from \$0 rather than pwd, which softlinks.sh got wrong.

install-font-linux.sh is an 8.9 KB copy of nerd-fonts' installer pinned
to a 2019 commit and referenced by nothing; the Brewfile now carries
font-symbols-only-nerd-font instead."
```

```bash
git rm -r bin/
git commit -m "chore(remove): drop bin/

Checked rather than assumed: rgr.bin invokes 'e' with ripgrep's flags,
which resolves to /usr/local/plan9/bin/e -- Plan 9's editor -- so it
silently runs the wrong program. git-chdate.bin single-quotes its
--env-filter body, so \$hash and \$proper never expand and it exports
empty dates; it also uses deprecated git filter-branch and BSD-only
'date -v'. git-stats.bin works but is fifteen lines. infernowm.bin
launches a Plan 9 window manager.

They were also linked with the .bin suffix intact, so the commands were
'rgr.bin' and 'git-stats.bin' -- the latter defeating the git- prefix.
Recover with: git show HEAD~1:bin/<name>"
```

```bash
git rm -r spacemacs/ gnupg/ 'macOS/iterm2'
git commit -m "chore(remove): drop unlinked tracked configuration

spacemacs/dotspacemacs was linked by the editors target, which rm -rf'd
~/.emacs.d and cloned spacemacs afresh -- destructive and failing on any
second run. gnupg/ and macOS/iterm2/ were linked by nothing at all.

On an existing machine ~/.spacemacs is now a dangling symlink: rm it.
~/.emacs.d, ~/.vim and ~/.tmux/plugins/tpm stay on disk; dropping a
target stops managing a thing, it does not delete it."
```

```bash
git rm git/hooks/go.pre-commit
git commit -m "chore(remove): drop the Go pre-commit hook

It ran 'go build -n && go test && go fmt && go vet' on every commit in
any repo with .go files at the root, with every command redirected to
/dev/null -- so a failure gave a generic message and no diagnostic. Worse,
'go fmt' rewrote files mid-commit without staging them, so a formatting
fix silently failed to be committed.

This is CI's job. See spec 5."
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd bootstrap.d && go test ./...`
Expected: PASS, everything.

- [ ] **Step 5: Commit the test**

```bash
git add bootstrap.d/removals_test.go
git commit -m "test(bootstrap): pin the removals

Guards against a later commit reintroducing a path this spec deliberately
retired, and against fish config regaining a reference to tooling that is
no longer installed."
```

---

### Task 16: Reduce the Makefile

**Files:**
- Modify: `Makefile`
- Test: `bootstrap.d/makefile_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: the `agents` target only.

- [ ] **Step 1: Write the failing test**

Create `bootstrap.d/makefile_test.go`:

```go
package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMakefileIsDeveloperTargetsOnly(t *testing.T) {
	f := newFixture(t)
	data, err := os.ReadFile(filepath.Join(f.root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, gone := range []string{
		"dotfiles:", "links:", "bins:", "omz:", "editors:", "extra:",
		"fishshell:", "starship:", "dep:", "githooks:",
	} {
		if strings.Contains(content, gone) {
			t.Errorf("Makefile still defines the %q target; ./bootstrap owns provisioning now", gone)
		}
	}
	if !strings.Contains(content, "agents:") {
		t.Errorf("Makefile should keep the agents build target")
	}
	if !strings.Contains(content, "bootstrap") {
		t.Errorf("Makefile should point provisioning at ./bootstrap")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd bootstrap.d && go test ./... -run Makefile`
Expected: FAIL — every listed target is still present.

- [ ] **Step 3: Replace the Makefile**

```makefile
.PHONY: agents

# Provisioning lives in ./bootstrap, not here:
#
#     ./bootstrap plan  workstation
#     ./bootstrap apply workstation
#     ./bootstrap check
#
# What remains is a developer convenience for the agents module during
# inner-loop work. `bootstrap apply workstation` builds the same binary as
# part of its devtools phase.

agents:
	mkdir -p "$(HOME)/bin"
	cd "$(CURDIR)/agents" && go build -trimpath -o "$(HOME)/bin/agents" .
	@echo "built $(HOME)/bin/agents"
```

- [ ] **Step 4: Verify both build paths still work**

Run: `make agents && ls -l ~/bin/agents`
Expected: the binary is rebuilt, `make` reports the path.

Run: `cd bootstrap.d && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add Makefile bootstrap.d/makefile_test.go
git commit -m "refactor: reduce the Makefile to developer targets

make dotfiles is retired rather than aliased: an alias would preserve an
interface we no longer want and leave two apparent entry points, which is
the overlap this work exists to remove.

What remains is the agents build for inner-loop development. It is not a
machine-bootstrap command and no longer presents itself as one."
```

---

### Task 17: Update the specs

**Files:**
- Modify: `docs/superpowers/specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md`
- Modify: `docs/superpowers/specs/agents/README.md`

**Interfaces:**
- Consumes: nothing.
- Produces: nothing.

- [ ] **Step 1: Mark the spec implemented**

In `docs/superpowers/specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md`, change:

```
**Status:** designed — not implemented
```

to:

```
**Status:** implemented
```

- [ ] **Step 2: Record what remains unverified**

Append to the Risks table row for Linux, or add below it, a note stating the
end-to-end status honestly. Replace the Linux risk row with:

```
| Linux support is written but never executed on Linux | **Still true at implementation.** Plan-mode and stub tests cover the logic; no phase has run on a Linux machine. Do not describe the Linux path as supported until it has. |
```

- [ ] **Step 3: Update the README tables**

In `docs/superpowers/specs/agents/README.md`, change spec 2's status to
`implemented`, change the sentence "Spec 2 is designed and awaiting an
implementation plan." to "Spec 2 is implemented.", and add a row to the
implementation-plans table:

```
| 2 | [dotfiles bootstrap](../../plans/2026-08-10-dotfiles-bootstrap.md) | executed |
```

- [ ] **Step 4: Verify every cross-reference still resolves**

Run: `grep -rn 'softlinks\.sh\|gitconfig\.symlink\|make dotfiles\|brew/brew-' --include='*.md' docs/`
Expected: matches only inside spec 2 and this plan, where they are described in
the past tense as things that were removed. Any *instruction* to run them is a
stale reference and must be fixed.

- [ ] **Step 5: Commit**

```bash
git add docs/
git commit -m "docs(spec-2): mark implemented

Linux remains written-but-unexecuted; the risk row says so rather than
letting 'implemented' imply otherwise."
```

---

## Self-Review

**Spec coverage.** Every numbered spec section maps to a task: §1 the rule → Tasks 2, 5, 6 (kinds and their checks); §2 interface → Task 3; §3 phases → Tasks 3, 12, 13, 14; §3.1 Homebrew-on-Linux → Task 13; §4 dry-run invariant → Tasks 1, 7; §5 refuse-never-clobber → Task 1; §6 manifest → Task 2; §6.1 one owner per path → Tasks 2, 6, 12; §7 fish inversion → Task 8; §8 renames → Task 9; §8.1 migrate → Tasks 10, 11; §9 removals → Tasks 13, 15; §9.1 go.pre-commit → Task 15; §10 check → Task 6; §11 testing → Tasks 1, 7 and every task's tests; §12 commit order → the task order itself.

**Known ordering constraint.** Task 6's `TestCheckPassesAfterApply` cannot pass until Tasks 8 and 9 create `fish/config.fish.template` and `git/gitconfig.shared`. Task 6 Step 5 adds explicit `t.Skip` lines naming Task 9; Task 9 Step 5 removes them. This is deliberate, not an oversight — the alternative was a single unreviewable task spanning the manifest, checks, fish and git renames.

**Interim state.** Between Task 9 and Task 15 the Makefile references `git/gitignore_global` (updated in Task 9 Step 4) so `make dotfiles` does not break for anyone who runs it before Task 16 deletes the target.

**Shell-injection surface, reviewed.** Three places construct a command rather than passing arguments, and each is deliberate:

- `bootstrap.d/*_test.go` runs `exec.Command("bash", "-c", …)`. Running shell *is* the thing under test; every script body is a literal in a `_test.go` file and every interpolated path comes from `t.TempDir()`. Noted in the harness so it is not copied into non-test code.
- Phase 40 builds `agents` in a subshell (`( cd … && do_run go build … )`) rather than `sh -c "cd '$root' && …"`, and phase 30 uses `tee -a` and fish's own `$BOOTSTRAP_ROOT` expansion rather than interpolating paths into command strings. Both were `sh -c` in an earlier draft and would have broken on any path containing a quote.
- Phase 10 executes Homebrew's installer via `bash -c "$(curl …)"`. That is Homebrew's documented installation method, it runs only when `brew` is absent, and it is skipped entirely in plan mode. There is no argument-passing form of "install Homebrew".
