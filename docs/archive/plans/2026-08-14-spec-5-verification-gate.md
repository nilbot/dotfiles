# Spec 5 — the verification gate: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give this repository its first automated build, test, `vet` and `gofmt`
gate, plus the four invariants it enforces: test containment, a command registry
that dispatch and help both walk, documentation derived from that registry, and
Linux coverage that closes spec 2's largest known gap.

**Architecture:** One GitHub Actions workflow whose only required check is a
`gate` job depending on every other. Each task lands a check together with
whatever fix makes it green, so the branch is mergeable at every boundary. The
`agents` CLI gains a declared command tree — commands declare themselves beside
their implementations, one tree assembles them, and every description is
rendered from it.

**Tech Stack:** Go 1.26.6 (stdlib only), GitHub Actions, Docker containers for
Linux, gitleaks 8.30.1.

**Spec:** [`docs/superpowers/specs/agents/2026-08-11-spec-5-verification-gate.md`](../../design/2026-08-11-spec-5-verification-gate.md)
(committed `ddb3f03`). Read it alongside this plan — the plan argues from it, and
its "Decisions that bind every phase" section is normative here.

## Global Constraints

- **Stdlib only.** No new dependencies in either Go module.
- **Test command:** `cd agents && go test -count=1 ./...` and
  `cd bootstrap.d && go test -count=1 ./...`. **`-count=1` is mandatory** —
  several tests read tracked non-Go files and a cached pass has already hidden a
  real break.
- **Run the `bootstrap.d` suite under `umask 077` as well as your own.**
- **`internal/phase` must import no I/O package** — not `os`, `os/exec`,
  `io/fs`, `net`. Enforced by an existing architecture test.
- **Exit codes:** `0` ok, `1` advisory, `2` block/refuse, `3` malformed input,
  `4` not applicable/skip, `5` could not complete. `agents` implements all six;
  `bootstrap` implements `0`–`4`.
- **Go pinned to `1.26.6`**, gitleaks pinned to `8.30.1`. Never
  `go-version-file` — `go 1.26` in `go.mod` floats.
- **`GOWORK=off`** in the workflow environment. No `go.work` is ever added.
- **No `paths` or `paths-ignore` filter** anywhere in the workflow. See the
  spec's "No path filters" decision — `bootstrap.d/makefile_test.go` reads the
  `Makefile` and `agents/internal/doctor/doctor_test.go` reads
  `git/gitconfig.shared`.
- **Every check must be demonstrated to fail** before its task is complete.
  Break the thing it guards, observe red, restore.
- **Commit messages:** no AI attribution, no `Co-Authored-By` trailer.
- **Staging:** exact paths per commit. Never `git add -A`.
- **No test may execute** `fish`, `brew`, `sudo`, `chsh`, `dscl`, `getent`,
  `apt-get`, `pacman`, or `make agents` against the real machine.

---

## File Structure

| Path | Responsibility | Task |
|---|---|---|
| `.github/workflows/verify.yml` | the whole gate; one file, jobs added incrementally | 1, 3, 7, 10, 12, 13 |
| `agents/cmd_init_test.go` | *modified* — two tests stop writing to the real fleet registry | 2 |
| `agents/command.go` | **new** — `Command`, `Audience`, `IO`, tree walk, help rendering | 4 |
| `agents/command_test.go` | **new** — tree traversal, coverage, rendering | 4 |
| `agents/commands.go` | **new** — the assembled tree; the one central artifact | 5 |
| `agents/main.go` | *modified* — dispatch through the tree, `usage()` deleted | 5 |
| `agents/cmd_help.go` | **new** — `agents help`, `--help`, `--render=markdown` | 6 |
| `agents/exitcode_doc_test.go` | **new** — pins the constants against spec 1 §6 | 6 |
| `bootstrap.d/main.go` | *modified* — resolve the never-returned `exitAdvisory` | 8 |
| `agents/docs_test.go` | **new** — backward name check over living documents | 10 |
| `README.md` | *modified* — generated command block between markers | 9 |
| `claude/skills/agents-tool/SKILL.md` | **new** — fleet-wide harness guidance | 11 |
| `bootstrap.d/internal/phase/packages.go` | *modified* — the Arch `-Syu` repair | 13 |

`agents/command.go` holds mechanism (types, traversal, rendering) and
`agents/commands.go` holds data (the tree). They are split because the mechanism
is tested against synthetic trees while the data is tested against the real
binary's behaviour, and because a single file mixing both grows past the size
where edits stay reliable.

---

## Task 1: The workflow skeleton

**Files:**
- Create: `.github/workflows/verify.yml`

**Interfaces:**
- Produces: a `gate` job that later tasks extend via `needs:`. Job ids created
  here: `test`, `secrets`, `gate`.

Phase 1 of the spec. Expected **green on arrival** — it is the only task that is.

- [ ] **Step 1: Confirm the suites are green locally before writing any YAML**

```bash
cd ~/dotfiles/agents      && go build ./... && go vet ./... && test -z "$(gofmt -l .)" && go test -count=1 ./...
cd ~/dotfiles/bootstrap.d && go build ./... && go vet ./... && test -z "$(gofmt -l .)" && go test -count=1 ./...
```

Expected: all pass. If anything fails, stop and fix that first — this task must
not be the thing that discovers a pre-existing break.

- [ ] **Step 2: Record the gitleaks checksum you are about to pin**

```bash
curl -sL https://github.com/gitleaks/gitleaks/releases/download/v8.30.1/gitleaks_8.30.1_linux_x64.tar.gz | shasum -a 256
```

Copy the 64-character hex value it prints. It goes into the workflow in Step 3
as `GITLEAKS_SHA256`. Pinning the version without pinning the artifact leaves
the supply chain unpinned, which is the half that matters.

- [ ] **Step 3: Write the workflow**

`.github/workflows/verify.yml`:

```yaml
# The verification gate. See docs/superpowers/specs/agents/2026-08-11-spec-5-verification-gate.md
#
# There are deliberately no `paths:` filters. bootstrap.d/makefile_test.go reads
# the Makefile and agents/internal/doctor/doctor_test.go reads
# git/gitconfig.shared, so a filter keyed on the module directories would skip
# verification for exactly the edits a cached `go test` already fails to notice.
# A required check skipped by a path filter also reports "Expected" forever and
# blocks the PR rather than passing it.
name: verify

on:
  pull_request:
  push:
    branches: [master]
  workflow_dispatch:

permissions:
  contents: read

env:
  # A stray go.work -- a runner's or a contributor's -- must never change what
  # CI resolves. Spec 2 §11 chose two modules with no workspace deliberately.
  GOWORK: off

jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        os: [macos-latest, ubuntu-latest]
        module: [agents, bootstrap.d]
    runs-on: ${{ matrix.os }}
    defaults:
      run:
        working-directory: ${{ matrix.module }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          # Exact patch, never go-version-file: `go 1.26` in go.mod floats to
          # whatever the runner happens to ship.
          go-version: '1.26.6'
          cache-dependency-path: ${{ matrix.module }}/go.sum
      - name: build
        run: go build ./...
      - name: gofmt
        run: test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }
      - name: vet
        run: go vet ./...
      # -count=1 is load-bearing. Several tests assert against tracked non-Go
      # files, whose edits do not invalidate a cached pass.
      - name: test
        run: go test -count=1 ./...
      - name: test -race
        run: go test -count=1 -race ./...

  secrets:
    runs-on: ubuntu-latest
    env:
      GITLEAKS_VERSION: '8.30.1'
      GITLEAKS_SHA256: 'PASTE_THE_VALUE_FROM_STEP_2_HERE'
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - name: install gitleaks, checksum-verified
        run: |
          set -euo pipefail
          url="https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz"
          curl -sSfL "$url" -o /tmp/gitleaks.tar.gz
          echo "${GITLEAKS_SHA256}  /tmp/gitleaks.tar.gz" | sha256sum -c -
          tar -xzf /tmp/gitleaks.tar.gz -C /tmp gitleaks
          sudo install /tmp/gitleaks /usr/local/bin/gitleaks
          gitleaks version
      - name: scan
        run: gitleaks git --config git/gitleaks.toml --redact --exit-code 1

  # The single required check. Branch protection names this job and nothing
  # else, so adding a job later is a workflow change rather than a
  # repository-settings change.
  gate:
    if: always()
    needs: [test, secrets]
    runs-on: ubuntu-latest
    steps:
      - name: require every dependency to have succeeded
        run: |
          echo "test:    ${{ needs.test.result }}"
          echo "secrets: ${{ needs.secrets.result }}"
          [ "${{ needs.test.result }}"    = "success" ] || exit 1
          [ "${{ needs.secrets.result }}" = "success" ] || exit 1
```

Replace `PASTE_THE_VALUE_FROM_STEP_2_HERE` with the hex value from Step 2.

- [ ] **Step 4: Validate the YAML parses before pushing**

```bash
cd ~/dotfiles && ruby -ryaml -e 'd=YAML.load_file(".github/workflows/verify.yml"); puts "parses, jobs: #{d["jobs"].keys.join(", ")}"' 
```

Expected: `parses`. A malformed workflow silently never runs, which is the
"unreachable mechanism" failure this whole spec exists to prevent.

- [ ] **Step 5: Commit and push to a branch, confirm the gate runs green**

```bash
cd ~/dotfiles
git add .github/workflows/verify.yml
git commit -m "ci: add the verification gate -- build, test, race, vet, gofmt, secrets"
git push -u origin HEAD
gh pr create --fill
gh pr checks --watch
```

Expected: four `test` jobs, one `secrets`, one `gate`, all green.

- [ ] **Step 6: Demonstrate the gate can fail**

```bash
cd ~/dotfiles/agents
printf '\n\nfunc  badlyFormatted( ) {}\n' >> root.go
cd ~/dotfiles && git add agents/root.go && git commit -m "temp: prove gofmt fails the gate" && git push
gh pr checks --watch
```

Expected: the `gofmt` step red, `gate` red. Then revert:

```bash
cd ~/dotfiles && git revert --no-edit HEAD && git push && gh pr checks --watch
```

Expected: green again. **Do not skip this step** — a gate whose failure has
never been observed is indistinguishable from one that cannot fail.

- [ ] **Step 7: Set branch protection to require `gate` only**

Repository settings → Branches → `master` → require status checks → add `gate`.
Add nothing else, so later tasks never touch repository settings.

---

## Task 2: Stop the test suite writing to the real fleet registry

**Files:**
- Modify: `agents/cmd_init_test.go`
- Test: `agents/cmd_init_test.go` (the same file — the fix is in the tests)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing importable. Establishes the invariant Task 3 asserts in CI.

This is phase 2's red half, landing before the check that catches it. Measured
2026-08-14: a full `go test -count=1 ./...` in `agents` writes
`$HOME/.local/state/agents/{registry.json,machine-id,registry.lock}` and
registers **two repositories** in it. On a developer machine that is the real
registry — this machine accumulated 56 entries of which 2 were real.

`machine.StateDir()` resolves `XDG_STATE_HOME` first and `os.UserHomeDir()`
otherwise, so the injection point already exists and 52 places in the suite
already use it.

- [ ] **Step 1: Reproduce, and record the number**

```bash
cd ~/dotfiles/agents
H=$(mktemp -d); X=$(mktemp -d)
HOME="$H" XDG_CACHE_HOME="$X" go test -count=1 ./... >/dev/null
find "$H/.local/state/agents" -type f
python3 -c "import json;print('repos leaked:',len(json.load(open('$H/.local/state/agents/registry.json'))['repos']))"
```

Expected: three files listed, `repos leaked: 2`.

- [ ] **Step 2: Add the assertion as a failing test**

Append to `agents/cmd_init_test.go`:

```go
// A test that registers into the machine's real fleet registry is a test that
// escaped its sandbox. machine.StateDir() reads XDG_STATE_HOME first and
// os.UserHomeDir() otherwise, so a test that sets neither writes to the
// developer's own ~/.local/state/agents/registry.json -- measured 2026-08-14 at
// 56 entries on this machine, of which 2 were real repositories.
//
// This asserts containment, not portability. The suite already PASSES under a
// synthetic HOME; passing was never the question.
func TestInitDoesNotTouchTheAmbientStateDirectory(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	root := newRepo(t)
	t.Chdir(root)
	var out bytes.Buffer
	if code := runInit(nil, &out); code != exitcode.OK && code != exitcode.Advisory {
		t.Fatalf("runInit = %d; %s", code, out.String())
	}

	registry := filepath.Join(state, "agents", "registry.json")
	if _, err := os.Stat(registry); err != nil {
		t.Fatalf("init did not use XDG_STATE_HOME; registry absent at %s: %v", registry, err)
	}
	data, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), root) {
		t.Errorf("the registry under XDG_STATE_HOME does not name %s:\n%s", root, data)
	}
}
```

- [ ] **Step 3: Run it, confirm it passes — then confirm it is not the whole fix**

```bash
cd ~/dotfiles/agents && go test -count=1 -run TestInitDoesNotTouchTheAmbientStateDirectory ./...
```

Expected: PASS. This test proves `XDG_STATE_HOME` is honoured; it does **not**
prove the other two tests use it. That is Step 4.

- [ ] **Step 4: Isolate the two tests that leak**

In `agents/cmd_init_test.go`, add `t.Setenv("XDG_STATE_HOME", t.TempDir())` as
the first line of the body of both `TestInitDoesNotPointAtTheRetiredTrackedTracePath`
and `TestInitLeavesARepositoryThatCanCommit`:

```go
func TestInitLeavesARepositoryThatCanCommit(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newRepo(t)
	t.Chdir(root)
	// ...unchanged from here
```

Apply the identical one-line addition to
`TestInitDoesNotPointAtTheRetiredTrackedTracePath`.

- [ ] **Step 5: Verify containment now holds**

```bash
cd ~/dotfiles/agents
H=$(mktemp -d); X=$(mktemp -d)
HOME="$H" XDG_CACHE_HOME="$X" go test -count=1 ./...
test ! -e "$H/.local/state/agents" && echo "CONTAINED" || { echo "STILL LEAKING:"; find "$H/.local/state/agents" -type f; }
```

Expected: tests pass, then `CONTAINED`.

- [ ] **Step 6: Demonstrate the check can fail**

Remove the `t.Setenv` line you added to `TestInitLeavesARepositoryThatCanCommit`,
re-run Step 5, confirm `STILL LEAKING`, then restore the line and confirm
`CONTAINED` again.

- [ ] **Step 7: Commit**

```bash
cd ~/dotfiles
git add agents/cmd_init_test.go
git commit -m "$(cat <<'EOF'
test(agents): stop init tests writing to the real fleet registry

runInit registers through machine.StateDir(), which reads XDG_STATE_HOME and
falls back to os.UserHomeDir(). Two tests set neither, so a plain `go test
./...` registered two repositories in the developer's own
~/.local/state/agents/registry.json. Measured on one machine: 56 entries, of
which 2 were real repositories and the rest Go test temp directories and
throwaway experiment repos.

a26eaa9 fixed TestInitDoesNotPointAtTheRetiredTrackedTracePath for the cwd
route by adding t.Chdir. This is the sibling route on the same test: the two
ways a test reaches the machine were never enumerated together.

The new test asserts containment rather than portability. The suite already
passed under a synthetic HOME -- passing was never the question, and a check
that only asserts it would relocate the pollution rather than detect it.
EOF
)"
```

---

## Task 3: The hygiene job

**Files:**
- Modify: `.github/workflows/verify.yml`

**Interfaces:**
- Consumes: Task 2's containment fix (this job is red without it).
- Produces: job id `hygiene`, added to `gate`'s `needs:`.

- [ ] **Step 1: Add the job**

Insert before the `gate` job in `.github/workflows/verify.yml`:

```yaml
  # Two checks, one property: the tools are a function of the repository, not of
  # the machine. See the spec's phase 2 -- the first check asserts CONTAINMENT
  # (the suite must not write to $HOME), not portability. The suite already
  # passes under a synthetic HOME; that was never the question.
  hygiene:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26.6'
          cache-dependency-path: agents/go.sum
      - name: suites must not write to $HOME
        run: |
          set -euo pipefail
          H=$(mktemp -d); X=$(mktemp -d)
          for module in agents bootstrap.d; do
            ( cd "$module" && HOME="$H" XDG_CACHE_HOME="$X" go test -count=1 ./... )
          done
          if [ -e "$H/.local/state/agents" ]; then
            echo "a test wrote fleet state into \$HOME:" >&2
            find "$H/.local/state/agents" -type f >&2
            exit 1
          fi
      - name: bootstrap.d under a restrictive umask
        run: ( umask 077 && cd bootstrap.d && go test -count=1 ./... )
      - name: agents index is a pure function of tracked content
        run: |
          set -euo pipefail
          mkdir -p "$HOME/bin"
          ( cd agents && go build -trimpath -ldflags "-X main.dotfilesRoot=$GITHUB_WORKSPACE" -o "$HOME/bin/agents" . )
          "$HOME/bin/agents" index
          # Scoped to .agents/ so the command means the same thing here as it
          # does on a developer's dirty working tree.
          git diff --exit-code -- .agents/
```

- [ ] **Step 2: Add `hygiene` to the gate**

In the `gate` job, change `needs:` and add the assertion:

```yaml
    needs: [test, secrets, hygiene]
```

```yaml
          echo "hygiene: ${{ needs.hygiene.result }}"
          [ "${{ needs.hygiene.result }}" = "success" ] || exit 1
```

- [ ] **Step 3: Validate and push**

```bash
cd ~/dotfiles
ruby -ryaml -e 'd=YAML.load_file(".github/workflows/verify.yml"); puts "parses, jobs: #{d["jobs"].keys.join(", ")}"' 
git add .github/workflows/verify.yml
git commit -m "ci: assert the suites do not write to \$HOME, and that agents index is pure"
git push && gh pr checks --watch
```

Expected: `hygiene` green.

- [ ] **Step 4: Demonstrate it can fail, both checks**

Containment: temporarily remove one `t.Setenv("XDG_STATE_HOME", ...)` from
`agents/cmd_init_test.go`, push, confirm `hygiene` red with the leaked path
named, revert, confirm green.

Index freshness: temporarily append a line to `.agents/memory/INDEX.md`, push,
confirm the `agents index` step red, revert, confirm green.

---

## Task 4: The command registry types

**Files:**
- Create: `agents/command.go`
- Test: `agents/command_test.go`

**Interfaces:**
- Produces: `type Command struct{...}`, `type Audience string`, `type IO struct{...}`,
  `func (c *Command) Find(path []string) (*Command, []string)`,
  `func (c *Command) Walk(fn func(path []string, cmd *Command))`,
  `func RenderUsage(root *Command, w io.Writer, all bool)`,
  `func RenderHelp(cmd *Command, path []string, w io.Writer)`,
  `func (a Audience) Automated() bool`.
  Tasks 5, 6, 7, 9 and 10 all consume these.

Mechanism only — no real commands are declared here. Task 5 declares them.

- [ ] **Step 1: Write the failing tests**

`agents/command_test.go`:

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

// A synthetic tree, so these tests exercise the mechanism rather than the
// binary's real command set. The real tree is asserted in Task 5.
func fixtureTree() *Command {
	leaf := &Command{
		Name: "prune", Summary: "remove copies", Usage: "agents trace prune --lane <n>",
		Detail: "Removes cached copies for one lane. Never touches the index.",
		Audience: []Audience{Human},
	}
	trace := &Command{
		Name: "trace", Summary: "query records", Usage: "agents trace <sub>",
		Detail: "Reads the machine-local trace store.",
		Audience: []Audience{Human, Agent},
		Sub:      []*Command{leaf},
	}
	hook := &Command{
		Name: "hook", Summary: "harness entrypoint", Usage: "agents hook <event>",
		Detail: "Invoked by a harness. Not for people.",
		Audience: []Audience{Harness},
	}
	return &Command{Name: "agents", Sub: []*Command{trace, hook}}
}

func TestFindResolvesANestedPath(t *testing.T) {
	cmd, rest := fixtureTree().Find([]string{"trace", "prune", "--lane", "x"})
	if cmd == nil || cmd.Name != "prune" {
		t.Fatalf("Find returned %v", cmd)
	}
	if len(rest) != 2 || rest[0] != "--lane" {
		t.Fatalf("remaining args = %v, want [--lane x]", rest)
	}
}

func TestFindStopsAtTheDeepestMatch(t *testing.T) {
	cmd, rest := fixtureTree().Find([]string{"trace", "nosuch"})
	if cmd == nil || cmd.Name != "trace" {
		t.Fatalf("Find returned %v; an unknown subcommand must resolve to its parent", cmd)
	}
	if len(rest) != 1 || rest[0] != "nosuch" {
		t.Fatalf("remaining args = %v, want [nosuch]", rest)
	}
}

func TestWalkVisitsEveryNodeWithItsFullPath(t *testing.T) {
	seen := map[string]bool{}
	fixtureTree().Walk(func(path []string, _ *Command) { seen[strings.Join(path, " ")] = true })
	for _, want := range []string{"trace", "trace prune", "hook"} {
		if !seen[want] {
			t.Errorf("Walk never visited %q; saw %v", want, seen)
		}
	}
	if seen["agents"] {
		t.Error("Walk visited the root; only commands are nodes")
	}
}

// The coverage check that makes an undocumented command fail loudly.
func TestWalkFindsAnEmptyDetail(t *testing.T) {
	tree := fixtureTree()
	tree.Sub[0].Sub[0].Detail = ""
	var missing []string
	tree.Walk(func(path []string, c *Command) {
		if c.Summary == "" || c.Usage == "" || c.Detail == "" {
			missing = append(missing, strings.Join(path, " "))
		}
	})
	if len(missing) != 1 || missing[0] != "trace prune" {
		t.Fatalf("missing = %v, want [trace prune]", missing)
	}
}

// Audience decides help visibility: a command no person invokes does not belong
// in the usage text a person reads.
func TestRenderUsageHidesNonHumanCommandsUnlessAllIsSet(t *testing.T) {
	var narrow, wide bytes.Buffer
	RenderUsage(fixtureTree(), &narrow, false)
	RenderUsage(fixtureTree(), &wide, true)

	if strings.Contains(narrow.String(), "hook") {
		t.Errorf("default usage shows a harness-only command:\n%s", narrow.String())
	}
	if !strings.Contains(narrow.String(), "trace") {
		t.Errorf("default usage hides a human command:\n%s", narrow.String())
	}
	if !strings.Contains(wide.String(), "hook") {
		t.Errorf("--all usage hides a harness-only command:\n%s", wide.String())
	}
}

func TestRenderHelpPrintsUsageAndDetail(t *testing.T) {
	cmd, _ := fixtureTree().Find([]string{"trace", "prune"})
	var out bytes.Buffer
	RenderHelp(cmd, []string{"trace", "prune"}, &out)
	for _, want := range []string{"agents trace prune --lane <n>", "Never touches the index"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help omitted %q:\n%s", want, out.String())
		}
	}
}

func TestAutomatedAudiences(t *testing.T) {
	for _, a := range []Audience{Git, Harness, CI} {
		if !a.Automated() {
			t.Errorf("%s must be automated: it acts on the exit code and cannot read prose", a)
		}
	}
	for _, a := range []Audience{Human, Agent} {
		if a.Automated() {
			t.Errorf("%s must be attentional: it reads the output", a)
		}
	}
}
```

- [ ] **Step 2: Run, confirm it fails**

```bash
cd ~/dotfiles/agents && go test -count=1 -run 'TestFind|TestWalk|TestRender|TestAutomated' ./...
```

Expected: FAIL — `undefined: Command`.

- [ ] **Step 3: Implement**

`agents/command.go`:

```go
package main

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Command is one node of the tree that dispatch and help both walk. Declaring a
// command and documenting it are the same act, so they cannot diverge -- which
// is the whole reason this type exists. The previous arrangement was a switch
// in main.go and an unrelated string literal beside it, and spec 7 changed four
// lines of that literal by hand in a single workstream.
type Command struct {
	Name     string     // "prune"; the full path comes from traversal
	Summary  string     // one line, shown in the parent's listing
	Usage    string     // "agents trace cache prune --lane <name>"
	Detail   string     // paragraph shown by `agents help <path>`
	Audience []Audience // who invokes this
	Flags    func(*flag.FlagSet)
	Run      func(args []string, io IO) int
	Sub      []*Command
}

// IO is one bundle rather than three parameters because the existing handlers
// have three different signatures and a registry needs one. Most are
// (args, stdout); runHandoff, runHandoffWrite and runHandoffDraft take stdin;
// runHook takes stdin and writes to *stderr*, not stdout, because a harness
// consumes its stdout. Adapting them all to a stdout-only signature would
// silently redirect the hook's diagnostics into a channel the harness parses.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type Audience string

// Automated audiences act on the exit code mechanically and cannot read prose,
// so for them the code must be disposition: proceed, stop, or you-called-me-
// wrong. Attentional audiences read the output, and a non-zero code is what
// makes them read it -- which is why `agents init` exits 1 so its state is
// visible, and why `agents review --stats` returns 1 for a successful reading
// that is not a clean pass.
const (
	Git     Audience = "git"
	Harness Audience = "harness"
	CI      Audience = "ci"
	Human   Audience = "human"
	Agent   Audience = "agent"
)

func (a Audience) Automated() bool {
	return a == Git || a == Harness || a == CI
}

// visibleToPeople reports whether this command belongs in the usage text a
// person reads. `hook` is invoked only by a harness and `guard` only by git;
// both sat in the same flat list as `init` before this existed.
func (c *Command) visibleToPeople() bool {
	for _, a := range c.Audience {
		if a == Human {
			return true
		}
	}
	return false
}

// Find walks as deep as the arguments match and returns the deepest command
// plus the arguments that are not part of its path. An unknown subcommand
// resolves to its parent with the unknown token still in rest, so the parent
// reports it -- which is how `agents trace nosuch` keeps exiting 3.
func (c *Command) Find(args []string) (*Command, []string) {
	cur := c
	i := 0
	for i < len(args) {
		next := cur.child(args[i])
		if next == nil {
			break
		}
		cur = next
		i++
	}
	if cur == c {
		return nil, args
	}
	return cur, args[i:]
}

func (c *Command) child(name string) *Command {
	for _, s := range c.Sub {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// Walk visits every command in the tree with its full path, excluding the root.
// It is what the coverage check and the README renderer both traverse.
func (c *Command) Walk(fn func(path []string, cmd *Command)) {
	var descend func(prefix []string, node *Command)
	descend = func(prefix []string, node *Command) {
		for _, s := range node.Sub {
			path := append(append([]string{}, prefix...), s.Name)
			fn(path, s)
			descend(path, s)
		}
	}
	descend(nil, c)
}

// RenderUsage writes the top-level listing. With all=false it shows only
// commands a person invokes; with all=true it shows everything.
func RenderUsage(root *Command, w io.Writer, all bool) {
	fmt.Fprint(w, "usage: agents <command> [flags]\n\n")
	width := 0
	var rows []*Command
	for _, c := range root.Sub {
		if !all && !c.visibleToPeople() {
			continue
		}
		rows = append(rows, c)
		if n := len(c.Usage); n > width {
			width = n
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	for _, c := range rows {
		fmt.Fprintf(w, "  %-*s  %s\n", width, c.Usage, c.Summary)
	}
	if !all {
		fmt.Fprint(w, "\n  agents help --all            include commands invoked by git and harnesses\n")
	}
	fmt.Fprint(w, "\n")
	RenderExitCodes(w)
}

// RenderHelp writes one command's own page.
func RenderHelp(cmd *Command, path []string, w io.Writer) {
	fmt.Fprintf(w, "agents %s -- %s\n\n", strings.Join(path, " "), cmd.Summary)
	fmt.Fprintf(w, "usage: %s\n\n", cmd.Usage)
	fmt.Fprintf(w, "%s\n", cmd.Detail)
	if len(cmd.Sub) > 0 {
		fmt.Fprint(w, "\nsubcommands:\n")
		width := 0
		for _, s := range cmd.Sub {
			if n := len(s.Name); n > width {
				width = n
			}
		}
		for _, s := range cmd.Sub {
			fmt.Fprintf(w, "  %-*s  %s\n", width, s.Name, s.Summary)
		}
	}
}
```

`RenderExitCodes` is written in Task 6. Until then this file will not compile —
add this temporary stub at the bottom of `command.go` and delete it in Task 6
Step 3:

```go
// TEMPORARY: replaced by the real renderer in Task 6.
func RenderExitCodes(w io.Writer) {}
```

- [ ] **Step 4: Run, confirm the tests pass**

```bash
cd ~/dotfiles/agents && go test -count=1 -run 'TestFind|TestWalk|TestRender|TestAutomated' ./... && gofmt -l . && go vet ./...
```

Expected: PASS, `gofmt -l` silent, vet clean.

- [ ] **Step 5: Demonstrate each test can fail**

Mutate and confirm red, restoring after each:
- In `Find`, `break` on the first iteration → `TestFindResolvesANestedPath` red.
- In `Walk`, drop the recursive `descend(path, s)` → `TestWalkVisitsEveryNode…` red.
- In `visibleToPeople`, `return true` unconditionally → `TestRenderUsageHides…` red.
- In `Automated`, drop `a == CI` → `TestAutomatedAudiences` red.

**A test that stays green under its mutation is undiscriminating and must be
rewritten** — this repository's memory records that as its dominant test defect,
found by mutation and never by reading.

- [ ] **Step 6: Commit**

```bash
cd ~/dotfiles
git add agents/command.go agents/command_test.go
git commit -m "feat(agents): add the command tree that dispatch and help both walk"
```

---

## Task 5: Declare the real commands and dispatch through the tree

**Files:**
- Create: `agents/commands.go`
- Modify: `agents/main.go`
- Test: `agents/commands_test.go`

**Interfaces:**
- Consumes: `Command`, `Audience`, `IO`, `Find` from Task 4.
- Produces: `func rootCommand() *Command`. Tasks 6, 7, 9 and 10 consume it.

Every existing handler keeps its current signature; the tree adapts them. That
keeps this task a re-wiring rather than a rewrite of fifteen commands.

- [ ] **Step 1: Write the failing test**

`agents/commands_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

// Every command reachable by dispatch must carry help. This is the belt over
// the structural braces: the tree makes an undocumented command hard, and this
// makes it loud.
func TestEveryCommandIsDocumented(t *testing.T) {
	var missing []string
	rootCommand().Walk(func(path []string, c *Command) {
		switch {
		case c.Summary == "", c.Usage == "", c.Detail == "":
			missing = append(missing, strings.Join(path, " "))
		case len(c.Audience) == 0:
			missing = append(missing, strings.Join(path, " ")+" (no audience)")
		}
	})
	if len(missing) > 0 {
		t.Errorf("commands missing help or audience:\n  %s", strings.Join(missing, "\n  "))
	}
}

// A leaf must be runnable and a branch must not pretend to be.
func TestEveryLeafHasARunner(t *testing.T) {
	rootCommand().Walk(func(path []string, c *Command) {
		if len(c.Sub) == 0 && c.Run == nil {
			t.Errorf("%s is a leaf with no Run", strings.Join(path, " "))
		}
	})
}

// The commands that existed before the tree must all still be reachable.
func TestTheKnownCommandSetIsPresent(t *testing.T) {
	present := map[string]bool{}
	rootCommand().Walk(func(path []string, _ *Command) { present[strings.Join(path, " ")] = true })
	for _, want := range []string{
		"init", "wire", "doctor", "index", "save",
		"handoff write", "handoff draft", "handoff prune",
		"review",
		"trace ls", "trace show", "trace cache", "trace cache prune", "trace migrate",
		"ls", "update", "guard", "hook",
	} {
		if !present[want] {
			t.Errorf("command %q disappeared in the move to the tree", want)
		}
	}
}
```

- [ ] **Step 2: Run, confirm it fails**

```bash
cd ~/dotfiles/agents && go test -count=1 -run 'TestEveryCommand|TestEveryLeaf|TestTheKnown' ./...
```

Expected: FAIL — `undefined: rootCommand`.

- [ ] **Step 3: Declare the tree**

`agents/commands.go`. Each entry's `Run` adapts the existing handler, so the
handlers themselves are untouched:

```go
package main

// rootCommand assembles the declarations. This is the one central artifact in
// the design; everything else about a command is declared beside its
// implementation and rendered from here. Nothing writes prose into this file
// beyond the declarations themselves.
func rootCommand() *Command {
	return &Command{Name: "agents", Sub: []*Command{
		{
			Name: "init", Summary: "create .agents/, triggers, wiring, fleet entry",
			Usage:    "agents init [--local]",
			Detail:   "Scaffolds .agents/, writes harness wiring, and registers this repository in the machine-local fleet. Prints the remaining trust steps and exits 1 (advisory) so the state is visible rather than assumed. --local keeps .agents/ git-ignored.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runInit(a, io.Out) },
		},
		{
			Name: "wire", Summary: "regenerate harness configs (merges, never overwrites)",
			Usage:    "agents wire",
			Detail:   "Regenerates the generated harness configuration for every harness this repository is wired for. Merges into files that hold unrelated settings rather than owning them.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runWire(a, io.Out) },
		},
		{
			Name: "doctor", Summary: "report wiring, trust evidence, reachability, and lane health",
			Usage:    "agents doctor",
			Detail:   "Reports what is wired, what the harnesses trust, which pointers are reachable, and how healthy each lane is. Observes; never changes state. Exits 1 when any check is advisory.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runDoctor(a, io.Out) },
		},
		{
			Name: "index", Summary: "regenerate memory and handoff indexes",
			Usage:    "agents index",
			Detail:   "Regenerates .agents/memory/INDEX.md and .agents/reports/handoff/INDEX.md from the frontmatter of the files they describe. A pure function of tracked content.",
			Audience: []Audience{Human, Agent, CI},
			Run:      func(a []string, io IO) int { return runIndex(a, io.Out) },
		},
		{
			Name: "save", Summary: "commit .agents/ paths and nothing else (escape hatch)",
			Usage:    "agents save [-m msg]",
			Detail:   "Commits .agents/ paths and nothing else. An escape hatch for a hand-edited memory entry or a batch of promotions -- the normal path is `agents review --keep`, which commits as part of promoting.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runSave(a, io.Out) },
		},
		{
			Name: "handoff", Summary: "lane-scoped handoff management",
			Usage:    "agents handoff write|draft|prune",
			Detail:   "Writes, queues and prunes lane-scoped handoff notes. `draft` queues an unreviewed note outside the tracked tree; `write` writes a reviewed one into it.",
			Audience: []Audience{Human, Agent},
			Sub: []*Command{
				{
					Name: "write", Summary: "write a reviewed note into the tracked tree",
					Usage:    "agents handoff write --lane <name> --session <id>",
					Detail:   "Reads the note body on stdin and writes it into .agents/reports/handoff/<lane>/. --session is required: it is what keeps concurrent agents on one branch from clobbering each other.",
					Audience: []Audience{Human, Agent},
					Run:      func(a []string, io IO) int { return runHandoffWrite(a, io.In, io.Out) },
				},
				{
					Name: "draft", Summary: "queue an unreviewed note outside the tracked tree",
					Usage:    "agents handoff draft --lane <name> --session <id>",
					Detail:   "Reads the note body on stdin and queues it in the machine-local store. Drafts are untracked until `agents review --keep` promotes one, so drafting costs nothing and commits you to nothing.",
					Audience: []Audience{Human, Agent},
					Run:      func(a []string, io IO) int { return runHandoffDraft(a, io.In, io.Out) },
				},
				{
					Name: "prune", Summary: "bound the number of notes per lane",
					Usage:    "agents handoff prune --keep <n>",
					Detail:   "Removes the oldest tracked handoff notes for a lane, keeping the most recent n.",
					Audience: []Audience{Human},
					Run:      func(a []string, io IO) int { return runHandoffPrune(a, io.Out) },
				},
			},
		},
		{
			Name: "review", Summary: "read pending drafts; promote one, or bin it",
			Usage:    "agents review [--show|--keep|--bin|--edit <id>] [--stats]",
			Detail:   "Lists pending drafts, prints one, or promotes it. --keep writes the note into .agents/, regenerates the affected index, and commits, in one act. There is deliberately no --keep --all: promotion is where a human decides.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runReview(a, io.Out) },
		},
		{
			Name: "trace", Summary: "query records; read one back; copy reachable ones",
			Usage:    "agents trace ls|show|cache|migrate",
			Detail:   "Reads the machine-local trace store: the pointer index, the transcript cache, and the migration from the retired tracked location.",
			Audience: []Audience{Human, Agent},
			Sub: []*Command{
				{
					Name: "ls", Summary: "query records",
					Usage:    "agents trace ls [--lane <n>] [--since <d>] [--machine <m>]",
					Detail:   "Filters the trace index mechanically by lane, module, machine, harness and time. Choosing among the survivors is semantic and falls back to matching on description.",
					Audience: []Audience{Human, Agent},
					Run:      func(a []string, io IO) int { return runTraceLS(a, io.Out) },
				},
				{
					Name: "show", Summary: "read one transcript back",
					Usage:    "agents trace show <id> [--path]",
					Detail:   "Resolves a record to content: the harness's own copy if it still exists, otherwise ours. Reports on stderr which one answered. Exits 5 when neither holds it, and 3 when the id prefix is ambiguous.",
					Audience: []Audience{Human, Agent},
					Run:      func(a []string, io IO) int { return runTraceShow(a, io.Out) },
				},
				{
					Name: "cache", Summary: "copy reachable transcripts into the store",
					Usage:    "agents trace cache [--lane <n>] [--since <d>]",
					Detail:   "Copies transcripts that are still on disk into the machine-local cache. The subagent-stop hook already does this at the earliest moment a finished transcript exists; this is the manual sweep.",
					Audience: []Audience{Human},
					Run:      func(a []string, io IO) int { return runTraceCache(a, io.Out) },
					Sub: []*Command{
						{
							Name: "prune", Summary: "remove cached copies, never the records",
							Usage:    "agents trace cache prune --lane <name> | --retention",
							Detail:   "Removes cached transcript copies. The index is never touched: it is the record that a transcript existed at all. Dry run unless --yes. Prunability is never inferred from git -- a deleted branch is usually a merged one, and a throwaway worktree is often where the interesting work happened.",
							Audience: []Audience{Human},
							Run:      func(a []string, io IO) int { return runTraceCachePrune(a, io.Out) },
						},
					},
				},
				{
					Name: "migrate", Summary: "move a tracked index into the machine-local store",
					Usage:    "agents trace migrate [--yes]",
					Detail:   "Copies a tracked trace index into the machine-local store, unstages it, and drops the merge=union attribute. Dry run unless --yes.",
					Audience: []Audience{Human},
					Run:      func(a []string, io IO) int { return runTraceMigrate(a, io.Out) },
				},
			},
		},
		{
			Name: "ls", Summary: "list the fleet on this machine",
			Usage:    "agents ls [--prune]",
			Detail:   "Lists every repository registered on this machine and reports drift in both directions. Drift is normal news, not an error. --prune forgets only entries confirmed missing.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runFleetLS(a, io.Out) },
		},
		{
			Name: "update", Summary: "rewire every registered repo (dry run by default)",
			Usage:    "agents update --all [--apply]",
			Detail:   "Regenerates harness wiring across the whole fleet. Dry run unless --apply, because this touches many repositories at once.",
			Audience: []Audience{Human},
			Run:      func(a []string, io IO) int { return runFleetUpdate(a, io.Out) },
		},
		{
			Name: "guard", Summary: "pre-commit checks (the only command that blocks)",
			Usage:    "agents guard --staged",
			Detail:   "Scans staged .agents/ content for secrets, regenerates the generated indexes and compares them byte-for-byte, and warns on a commit mixing agent context with code. Invoked automatically on every pre-commit; main.go maps its advisory result to success so a warning does not abort the commit.",
			Audience: []Audience{Git, CI},
			Run:      func(a []string, io IO) int { return runGuard(a, io.Out) },
		},
		{
			Name: "hook", Summary: "harness hook entrypoint",
			Usage:    "agents hook <event> --harness <name>",
			Detail:   "Records one harness lifecycle event. Reads the payload on stdin and writes diagnostics to stderr, because the harness consumes stdout. Exits 0 on every path: a failed record must never disrupt a dispatch.",
			Audience: []Audience{Harness},
			Run:      func(a []string, io IO) int { return runHook(a, io.In, io.Err) },
		},
	}}
}
```

- [ ] **Step 4: Dispatch through the tree**

Replace the whole `run` function and delete `usage()` in `agents/main.go`:

```go
func run(args []string) int {
	root := rootCommand()
	if len(args) == 0 {
		// Still exit 3 on stderr: a bare invocation is a usage error, while an
		// explicit `agents help` is not. Same text, different disposition.
		RenderUsage(root, os.Stderr, false)
		return exitcode.Malformed
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		return runHelp(args[1:], os.Stdout)
	}
	cmd, rest := root.Find(args)
	if cmd == nil {
		fmt.Fprintf(os.Stderr, "agents: unknown command %q\n", args[0])
		RenderUsage(root, os.Stderr, false)
		return exitcode.Malformed
	}
	if cmd.Run == nil {
		if len(rest) > 0 {
			fmt.Fprintf(os.Stdout, "agents %s: unknown subcommand %q\n", cmd.Name, rest[0])
		} else {
			fmt.Fprintf(os.Stdout, "usage: %s\n", cmd.Usage)
		}
		return exitcode.Malformed
	}
	return cmd.Run(rest, IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
}
```

Delete `func usage()` entirely. `runHelp` arrives in Task 6; until then add a
temporary stub at the bottom of `main.go` and delete it in Task 6 Step 3:

```go
// TEMPORARY: replaced in Task 6.
func runHelp(args []string, w io.Writer) int { RenderUsage(rootCommand(), w, false); return exitcode.OK }
```

- [ ] **Step 5: Run the full suite**

```bash
cd ~/dotfiles/agents && go build ./... && go vet ./... && test -z "$(gofmt -l .)" && go test -count=1 ./...
```

Expected: PASS. If a test asserted against the old `usage()` text, update it to
call `RenderUsage` — do not reintroduce a literal.

- [ ] **Step 6: Confirm behaviour by hand**

```bash
cd ~/dotfiles && make agents
agents; echo "no-args exit=$?"          # usage on stderr, exit 3
agents nosuchcommand; echo "exit=$?"    # exit 3
agents trace nosuch; echo "exit=$?"     # exit 3
agents doctor >/dev/null; echo "exit=$?"
```

Expected: `3`, `3`, `3`, and doctor's usual `0` or `1`.

- [ ] **Step 7: Demonstrate the coverage test can fail**

Blank one command's `Detail` in `commands.go`, run
`go test -count=1 -run TestEveryCommandIsDocumented ./...`, confirm it names
that command, restore.

- [ ] **Step 8: Commit**

```bash
cd ~/dotfiles
git add agents/commands.go agents/commands_test.go agents/main.go
git commit -m "$(cat <<'EOF'
feat(agents): dispatch through the command tree, and delete usage()

main.go dispatched through a switch and described itself through a separate
string literal with nothing connecting them. Spec 7 changed four lines of that
literal by hand in one workstream, and `trace cache prune` -- a destructive verb
-- had no help at all.

Commands now declare name, summary, usage, detail and audience in one place, and
dispatch and help walk the same tree, so an undocumented command is structurally
impossible rather than merely discouraged. Handlers keep their existing
signatures; the tree adapts them through an IO bundle, because runHook writes to
stderr rather than stdout and flattening that would redirect its diagnostics
into the channel the harness parses.

A bare `agents` still exits 3 on stderr. `agents help` exits 0 on stdout. Same
text, different disposition.
EOF
)"
```

---

## Task 6: `agents help`, and the exit-code table rendered from the constants

**Files:**
- Create: `agents/cmd_help.go`
- Create: `agents/exitcode_doc_test.go`
- Modify: `agents/command.go` (delete the `RenderExitCodes` stub), `agents/main.go` (delete the `runHelp` stub)

**Interfaces:**
- Consumes: `rootCommand`, `RenderUsage`, `RenderHelp` from Tasks 4-5.
- Produces: `func runHelp(args []string, w io.Writer) int`, `func RenderExitCodes(w io.Writer)`,
  `func RenderMarkdown(root *Command, w io.Writer)`. Task 9 consumes `RenderMarkdown`.

The exit-code table is described in four places and three disagree. Rendering it
from the constants collapses two of them; a test pins the rest against spec 1 §6.

- [ ] **Step 1: Write the failing tests**

`agents/exitcode_doc_test.go`:

```go
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

// Spec 1 §6 defines the shared vocabulary. The code is the implementation and
// the spec is design intent; when they disagree this test is what surfaces it,
// and a human decides which was wrong. Prose cannot be generated from code, so
// a pinning test is the only thing that can hold the two together.
func TestExitCodeTableMatchesSpecOne(t *testing.T) {
	want := []struct {
		code int
		name string
	}{
		{exitcode.OK, "ok"},
		{exitcode.Advisory, "advisory"},
		{exitcode.Block, "block"},
		{exitcode.Malformed, "malformed"},
		{exitcode.Skip, "skip"},
		{exitcode.NoRecord, "could not complete"},
	}
	for i, w := range want {
		if w.code != i {
			t.Fatalf("%s must be %d, is %d; spec 1 §6 fixes these values", w.name, i, w.code)
		}
	}

	var out bytes.Buffer
	RenderExitCodes(&out)
	for _, w := range want {
		if !strings.Contains(out.String(), w.name) {
			t.Errorf("the rendered table omits %q:\n%s", w.name, out.String())
		}
	}
}

// The table must be rendered, not restated. If someone reintroduces a literal,
// changing a constant stops changing the help text and the drift returns.
func TestExitCodeTableIsRenderedFromTheConstants(t *testing.T) {
	var out bytes.Buffer
	RenderExitCodes(&out)
	for _, digit := range []string{"0", "1", "2", "3", "4", "5"} {
		if !strings.Contains(out.String(), digit) {
			t.Errorf("code %s missing from the rendered table:\n%s", digit, out.String())
		}
	}
}

func TestHelpForALeafExitsZeroAndNamesTheCommand(t *testing.T) {
	var out bytes.Buffer
	if code := runHelp([]string{"trace", "cache", "prune"}, &out); code != exitcode.OK {
		t.Fatalf("help exit = %d, want 0", code)
	}
	for _, want := range []string{"agents trace cache prune", "never the records"} {
		if !strings.Contains(strings.ToLower(out.String()), strings.ToLower(want)) {
			t.Errorf("help omitted %q:\n%s", want, out.String())
		}
	}
}

func TestHelpForAnUnknownPathExitsThree(t *testing.T) {
	var out bytes.Buffer
	if code := runHelp([]string{"nosuchcommand"}, &out); code != exitcode.Malformed {
		t.Fatalf("help for an unknown command exit = %d, want 3", code)
	}
}

// --all is what makes a harness-only or git-only command discoverable without
// putting it in the list a person reads.
func TestHelpAllIncludesTheAutomatedCommands(t *testing.T) {
	var narrow, wide bytes.Buffer
	runHelp(nil, &narrow)
	runHelp([]string{"--all"}, &wide)
	if strings.Contains(narrow.String(), "agents hook") {
		t.Errorf("default help lists a harness-only command:\n%s", narrow.String())
	}
	if !strings.Contains(wide.String(), "agents hook") {
		t.Errorf("--all omits a harness-only command:\n%s", wide.String())
	}
}
```

- [ ] **Step 2: Run, confirm it fails**

```bash
cd ~/dotfiles/agents && go test -count=1 -run 'TestExitCode|TestHelp' ./...
```

Expected: FAIL — the stubbed `RenderExitCodes` writes nothing and the stubbed
`runHelp` ignores its arguments.

- [ ] **Step 3: Delete both stubs, then implement**

Delete the temporary `RenderExitCodes` from `agents/command.go` and the
temporary `runHelp` from `agents/main.go`.

`agents/cmd_help.go`:

```go
package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

// exitCodeMeanings is the single description of the shared vocabulary. Help
// renders from it rather than restating it, which is what stops the four
// descriptions of this one table from drifting -- spec 1 §6, these comments,
// and each binary's help text disagreed on code 4 before this existed.
var exitCodeMeanings = []struct {
	Code int
	Text string
}{
	{exitcode.OK, "ok"},
	{exitcode.Advisory, "advisory -- finished, but read the output"},
	{exitcode.Block, "block -- the only code that stops work"},
	{exitcode.Malformed, "malformed input"},
	{exitcode.Skip, "skip -- not applicable here"},
	{exitcode.NoRecord, "could not complete the operation"},
}

func RenderExitCodes(w io.Writer) {
	fmt.Fprint(w, "exit codes:\n")
	for _, m := range exitCodeMeanings {
		fmt.Fprintf(w, "  %d  %s\n", m.Code, m.Text)
	}
}

// runHelp answers `agents help`, `agents help <path>`, and `--help` at any
// depth. It exits 0 -- an explicit request for help is not a usage error, which
// is the one way it differs from a bare `agents`.
func runHelp(args []string, w io.Writer) int {
	root := rootCommand()

	all := false
	var path []string
	for _, a := range args {
		switch a {
		case "--all":
			all = true
		case "--render=markdown":
			RenderMarkdown(root, w)
			return exitcode.OK
		default:
			path = append(path, a)
		}
	}

	if len(path) == 0 {
		RenderUsage(root, w, all)
		return exitcode.OK
	}

	cmd, rest := root.Find(path)
	if cmd == nil || len(rest) > 0 {
		fmt.Fprintf(w, "agents help: no such command %q\n", strings.Join(path, " "))
		return exitcode.Malformed
	}
	RenderHelp(cmd, path, w)
	return exitcode.OK
}

// RenderMarkdown emits the whole command surface for the generated README
// block. It writes to stdout rather than to the file: `agents save` commits
// .agents/ paths and nothing else, and the mixed-commit guard exists to keep
// repository content and agent context in separate commits, so teaching this
// binary to write a tracked file outside .agents/ would put those two
// mechanisms in tension.
func RenderMarkdown(root *Command, w io.Writer) {
	fmt.Fprint(w, "| Command | What |\n|---|---|\n")
	root.Walk(func(path []string, c *Command) {
		fmt.Fprintf(w, "| `agents %s` | %s |\n", strings.Join(path, " "), c.Summary)
	})
}
```

- [ ] **Step 4: Run, confirm the tests pass**

```bash
cd ~/dotfiles/agents && go test -count=1 ./... && go vet ./... && test -z "$(gofmt -l .)"
```

- [ ] **Step 5: Confirm the spec's verification commands by hand**

```bash
cd ~/dotfiles && make agents
agents help; echo "exit=$?"                     # 0, on stdout
agents help trace cache prune; echo "exit=$?"   # 0, usage and detail
agents --help >/dev/null; echo "exit=$?"        # 0
agents help --all | grep -c "agents hook"       # 1
agents help | grep -c "agents hook"             # 0
agents; echo "no-args exit=$?"                  # 3, on stderr
agents help nosuchcommand; echo "exit=$?"       # 3
```

Expected exactly those values.

- [ ] **Step 6: Demonstrate the pinning test can fail**

Change `exitcode.Skip` to `= 6` in
`agents/internal/exitcode/exitcode.go`, run
`go test -count=1 -run TestExitCodeTableMatchesSpecOne ./...`, confirm it fails
naming `skip`, then restore `= 4`.

- [ ] **Step 7: Commit**

```bash
cd ~/dotfiles
git add agents/cmd_help.go agents/exitcode_doc_test.go agents/command.go agents/main.go
git commit -m "$(cat <<'EOF'
feat(agents): add `agents help`, and render the exit-code table from the constants

One table was described in four places and three of them disagreed on code 4:
spec 1 §6 says "not applicable / skip", the constants' comments say "not
applicable here", agents' help said "skip", and bootstrap's says "not
applicable". Help now renders from the constants, which collapses two of the
four, and a test pins the constants against spec 1's table so the remaining two
cannot drift silently.

`agents help <path>` works at any depth, so `trace cache prune` has help for the
first time. --all includes the commands only git and harnesses invoke; the
default list is the one a person reads.
EOF
)"
```

---

## Task 7: The docs job — help coverage in CI

**Files:**
- Modify: `.github/workflows/verify.yml`

**Interfaces:**
- Consumes: `TestEveryCommandIsDocumented` from Task 5.
- Produces: job id `docs`, extended by Task 10.

- [ ] **Step 1: Add the job**

Insert before `gate` in `.github/workflows/verify.yml`:

```yaml
  # Derived-artifact checks. Task 7 adds help coverage; Task 10 adds the README
  # block and the backward name check to this same job.
  docs:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26.6'
          cache-dependency-path: agents/go.sum
      - name: every command carries help and an audience
        run: cd agents && go test -count=1 -run 'TestEveryCommandIsDocumented|TestEveryLeafHasARunner|TestTheKnownCommandSetIsPresent' ./...
```

- [ ] **Step 2: Add `docs` to the gate**

```yaml
    needs: [test, secrets, hygiene, docs]
```

```yaml
          echo "docs:    ${{ needs.docs.result }}"
          [ "${{ needs.docs.result }}" = "success" ] || exit 1
```

- [ ] **Step 3: Validate, push, confirm green**

```bash
cd ~/dotfiles
ruby -ryaml -e 'd=YAML.load_file(".github/workflows/verify.yml"); puts "parses, jobs: #{d["jobs"].keys.join(", ")}"' 
git add .github/workflows/verify.yml
git commit -m "ci: require every command to carry help and an audience"
git push && gh pr checks --watch
```

- [ ] **Step 4: Demonstrate it can fail**

Blank a `Detail` in `agents/commands.go`, push, confirm `docs` red naming the
command, revert, confirm green.

---

## Task 8: Resolve `bootstrap`'s never-returned `exitAdvisory`

**Files:**
- Modify: `bootstrap.d/main.go`
- Test: `bootstrap.d/main_internal_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: nothing importable.

`bootstrap.d/main.go:29` declares `exitAdvisory = 1`. Verified 2026-08-14: it is
the only occurrence in the module and there is no bare `return 1` in the file. A
constant nobody returns describes a behaviour the binary does not have, and a
reader cannot tell it from one that works.

- [ ] **Step 1: Confirm the finding still holds**

```bash
cd ~/dotfiles/bootstrap.d
grep -rn "exitAdvisory" . ; grep -n "return 1$" main.go ; echo "---"
```

Expected: exactly one line, the declaration, and no bare `return 1`.

- [ ] **Step 2: Audit for a path that means "could not complete"**

```bash
cd ~/dotfiles/bootstrap.d && grep -n "exitBlock" main.go
```

Read each site and classify it. A **refusal** asserts *this machine is in a
state I will not write over*. `5` means *I tried and could not*. Record the
classification in the commit message.

- [ ] **Step 3: Write the failing test**

Append to `bootstrap.d/main_internal_test.go`:

```go
// A constant no path returns documents a behaviour this binary does not have.
// exitAdvisory sat here unreturned; either a path means it, or it goes. The
// same reasoning is why bootstrap does not gain a code 5 to match agents unless
// it has a path that means "I tried and could not", distinct from "this machine
// is in a state I will not write over".
func TestEveryDeclaredExitCodeHasAProducer(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, name := range []string{"exitOK", "exitBlock", "exitMalformed", "exitNotApplicable"} {
		if strings.Count(body, name) < 2 {
			t.Errorf("%s is declared but never returned", name)
		}
	}
	if strings.Contains(body, "exitAdvisory") {
		t.Error("exitAdvisory is declared and returned by no path; delete it or give it a producer")
	}
}
```

- [ ] **Step 4: Run, confirm it fails**

```bash
cd ~/dotfiles/bootstrap.d && go test -count=1 -run TestEveryDeclaredExitCodeHasAProducer ./...
```

Expected: FAIL on the `exitAdvisory` assertion.

- [ ] **Step 5: Delete the constant**

Remove the `exitAdvisory = 1` line from the `const` block in
`bootstrap.d/main.go`. If Step 2 found a real advisory path, add the return
instead and invert the test's final assertion — record which you did and why.

Update the usage text's exit-code line in the same edit if the deletion changes
it.

- [ ] **Step 6: Run both suites**

```bash
cd ~/dotfiles/bootstrap.d && go build ./... && go vet ./... && test -z "$(gofmt -l .)" && go test -count=1 ./...
( umask 077 && cd ~/dotfiles/bootstrap.d && go test -count=1 ./... )
```

- [ ] **Step 7: Commit**

```bash
cd ~/dotfiles
git add bootstrap.d/main.go bootstrap.d/main_internal_test.go
git commit -m "$(cat <<'EOF'
fix(bootstrap): delete the exit code no path returns

exitAdvisory was declared and returned by nothing -- verified as the only
occurrence in the module, with no bare `return 1` in the file. A constant nobody
returns describes a behaviour the binary does not have, and a reader has no way
to tell that from one it does.

Whether bootstrap should gain a code 5 to match agents is decided by whether it
has a path meaning "I tried and could not", distinct from "this machine is in a
state I will not write over" -- an audit, not an argument from symmetry.
EOF
)"
```

---

## Task 9: The generated README command block

**Files:**
- Modify: `README.md`
- Test: `agents/docs_test.go` (created here, extended in Task 10)

**Interfaces:**
- Consumes: `RenderMarkdown` from Task 6.
- Produces: `func readmeBlock(t *testing.T) (before, block, after string)` used by Task 10.

README currently contains **zero** mentions of `agents` commands — verified
2026-08-14. This block is new content, not a replacement.

- [ ] **Step 1: Add the markers to `README.md`**

Insert a new section immediately before `## Layout`:

```markdown
## The `agents` tool

`agents` maintains the tracked `.agents/` directory in this and every other
repository on the machine — the memory, handoffs, and the harness wiring that
feeds them. `agents help <command>` explains any of these in full; **when** to
reach for one is in the skill under `claude/skills/agents-tool/`.

<!-- BEGIN GENERATED: agents help --render=markdown -->
<!-- END GENERATED -->
```

The prose stays hand-written because *when* to reach for a command is judgment
and cannot be generated. Only what is between the markers is derived.

- [ ] **Step 2: Write the failing test**

`agents/docs_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	beginMarker = "<!-- BEGIN GENERATED: agents help --render=markdown -->"
	endMarker   = "<!-- END GENERATED -->"
)

// repoRootForDocs resolves the checkout from this package's directory rather
// than from the working directory: TestMain chdirs out of the checkout before
// any test runs, so cwd is not the repository. Task 2's fix introduced that
// constraint and three existing call sites already honour it.
func repoRootForDocs(t *testing.T) string {
	t.Helper()
	return filepath.Dir(packageDir)
}

func readmeBlock(t *testing.T) (before, block, after string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRootForDocs(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	i := strings.Index(text, beginMarker)
	j := strings.Index(text, endMarker)
	if i < 0 || j < 0 || j < i {
		t.Fatalf("README.md is missing the generated-block markers")
	}
	return text[:i+len(beginMarker)], text[i+len(beginMarker) : j], text[j:]
}

// The README block is derived. If it drifts, the fix is to regenerate it, not
// to edit it -- which is the whole reason it is generated.
func TestReadmeCommandBlockIsCurrent(t *testing.T) {
	_, block, _ := readmeBlock(t)
	var want bytes.Buffer
	RenderMarkdown(rootCommand(), &want)
	if strings.TrimSpace(block) != strings.TrimSpace(want.String()) {
		t.Errorf("README command block is stale. Regenerate it:\n"+
			"  agents help --render=markdown\n\ngot:\n%s\nwant:\n%s", block, want.String())
	}
}
```

- [ ] **Step 3: Confirm `packageDir` is still the right handle**

```bash
cd ~/dotfiles/agents && sed -n '28,34p' main_test.go
```

Expected: `packageDir` is declared there, described as this package's own
directory, and captured before `TestMain` chdirs out of the checkout.
`cmd_doctor_test.go:315` already uses it for exactly this reason. No new
variable is needed — `repoRootForDocs` in Step 2 builds on it.

- [ ] **Step 4: Run, confirm it fails**

```bash
cd ~/dotfiles/agents && go test -count=1 -run TestReadmeCommandBlockIsCurrent ./...
```

Expected: FAIL — the block is empty and the rendered table is not.

- [ ] **Step 5: Generate the block**

```bash
cd ~/dotfiles && make agents
python3 - <<'PY'
import subprocess, pathlib
begin = "<!-- BEGIN GENERATED: agents help --render=markdown -->"
end   = "<!-- END GENERATED -->"
p = pathlib.Path("README.md"); text = p.read_text()
block = subprocess.run(["agents","help","--render=markdown"], capture_output=True, text=True, check=True).stdout
i = text.index(begin) + len(begin); j = text.index(end)
p.write_text(text[:i] + "\n" + block + text[j:])
print("regenerated")
PY
```

- [ ] **Step 6: Run, confirm it passes**

```bash
cd ~/dotfiles/agents && go test -count=1 -run TestReadmeCommandBlockIsCurrent ./...
```

- [ ] **Step 7: Demonstrate it can fail**

Delete one table row from inside the markers in `README.md`, re-run the test,
confirm it reports the block stale, regenerate with Step 5, confirm green.

- [ ] **Step 8: Commit**

```bash
cd ~/dotfiles
git add README.md agents/docs_test.go agents/main_test.go
git commit -m "docs: generate the agents command reference into README from the registry"
```

---

## Task 10: The backward name check, and the `docs` job extension

**Files:**
- Modify: `agents/docs_test.go`, `.github/workflows/verify.yml`

**Interfaces:**
- Consumes: `rootCommand`, `readmeBlock`, `repoRootForDocs` from Tasks 5 and 9.

No living document may name an `agents` subcommand the registry does not define.

- [ ] **Step 1: Write the failing test**

Append to `agents/docs_test.go`:

```go
// Living documents must not name a command that does not exist. Plans and specs
// are deliberately excluded: they are dated records of what was true when they
// were written -- the executed bootstrap plan legitimately names `make
// githooks`, which no longer exists -- and a record silently rewritten to stay
// true is not a record.
func TestLivingDocumentsNameOnlyRealCommands(t *testing.T) {
	root := repoRootForDocs(t)
	known := map[string]bool{"help": true}
	rootCommand().Walk(func(path []string, _ *Command) { known[strings.Join(path, " ")] = true })

	targets := []string{"README.md", "CLAUDE.md", filepath.Join("claude", "CLAUDE.md")}
	for _, dir := range []string{filepath.Join("claude", "skills"), filepath.Join(".agents", "skills")} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
				rel, _ := filepath.Rel(root, p)
				targets = append(targets, rel)
			}
			return nil
		})
	}

	// Only inline code spans. Prose mentions of the word "agents" are not
	// command references, and matching them would produce false positives
	// nobody would act on.
	span := regexp.MustCompile("`agents ([a-z][a-z -]*)`")
	for _, rel := range targets {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue // an optional document that does not exist yet
		}
		for _, m := range span.FindAllStringSubmatch(string(data), -1) {
			words := strings.Fields(m[1])
			for n := len(words); n > 0; n-- {
				if known[strings.Join(words[:n], " ")] {
					goto next
				}
			}
			t.Errorf("%s names `agents %s`, which the registry does not define", rel, m[1])
		next:
		}
	}
}
```

Add `"regexp"` to the file's imports.

- [ ] **Step 2: Run it**

```bash
cd ~/dotfiles/agents && go test -count=1 -run TestLivingDocumentsNameOnlyRealCommands ./...
```

Expected: PASS if every document is already accurate. **If it fails, the
document is wrong, not the test** — fix the document.

- [ ] **Step 3: Demonstrate it can fail**

```bash
cd ~/dotfiles && printf '\n`agents nosuchverb` is not real.\n' >> CLAUDE.md
cd agents && go test -count=1 -run TestLivingDocumentsNameOnlyRealCommands ./...
```

Expected: FAIL naming `CLAUDE.md`. Then:

```bash
cd ~/dotfiles && git checkout CLAUDE.md
```

- [ ] **Step 4: Extend the `docs` job**

Add to the `docs` job's steps in `.github/workflows/verify.yml`:

```yaml
      - name: documentation matches the command set, both directions
        run: cd agents && go test -count=1 -run 'TestReadmeCommandBlockIsCurrent|TestLivingDocumentsNameOnlyRealCommands|TestHarnessSkillCoversAgentCommands' ./...
```

`TestHarnessSkillCoversAgentCommands` arrives in Task 11; this step is red until
then, so **commit this step together with Task 11** rather than pushing it now.

- [ ] **Step 5: Commit the test only**

```bash
cd ~/dotfiles
git add agents/docs_test.go
git commit -m "test(agents): no living document may name a command the registry lacks"
```

---

## Task 11: The fleet-wide harness skill

**Files:**
- Create: `claude/skills/agents-tool/SKILL.md`
- Modify: `agents/docs_test.go`, `.github/workflows/verify.yml`

**Interfaces:**
- Consumes: `rootCommand`, `repoRootForDocs`.

`bootstrap.d/links.manifest:20-21` symlinks `claude/skills` → `~/.claude/skills`
on every provisioned machine, so this is fleet-wide. `.agents/skills/` would give
the guidance to the one repository that needs it least.

- [ ] **Step 1: Write the failing test**

Append to `agents/docs_test.go`:

```go
// A command an agent may invoke must appear in the fleet-wide guidance, or a
// harness never learns to reach for it. Spec 7 measured this exact shape: the
// instruction said HOW to write a handoff and never THAT one should, and twenty
// sessions produced none. A generated reference answers "what is this command";
// only the skill answers "which command is this situation".
func TestHarnessSkillCoversAgentCommands(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRootForDocs(t), "claude", "skills", "agents-tool", "SKILL.md"))
	if err != nil {
		t.Fatalf("the fleet-wide agents skill is missing: %v", err)
	}
	text := string(data)

	var missing []string
	rootCommand().Walk(func(path []string, c *Command) {
		for _, a := range c.Audience {
			if a != Agent {
				continue
			}
			if !strings.Contains(text, "`agents "+strings.Join(path, " ")+"`") {
				missing = append(missing, strings.Join(path, " "))
			}
			return
		}
	})
	if len(missing) > 0 {
		t.Errorf("agent-facing commands absent from the skill:\n  %s", strings.Join(missing, "\n  "))
	}
}
```

- [ ] **Step 2: Run, confirm it fails**

```bash
cd ~/dotfiles/agents && go test -count=1 -run TestHarnessSkillCoversAgentCommands ./...
```

Expected: FAIL — the file does not exist.

- [ ] **Step 3: Write the skill**

`claude/skills/agents-tool/SKILL.md`:

```markdown
---
name: agents-tool
description: Use when working in a repository that has a tracked .agents/ directory - deciding when to record a finding, read a past session back, or promote a draft into the repository's memory.
---

# The `agents` tool

`agents` keeps durable context in the repository instead of in a harness
directory that does not travel. This skill is about **when** to reach for each
command. For **what** each one does, run `agents help <command>`.

## Start of a session

Run `agents doctor` early and report any warnings before relying on repository
context. A hook cannot install itself and a missing hook fails silently, so an
empty or stale `.agents/` means the setup is broken rather than that there is
nothing to say.

Read `.agents/memory/INDEX.md` and `.agents/reports/handoff/INDEX.md` before
assuming. `reviewed` was written deliberately; `draft` was written at session
end and has not been checked by anyone. Weigh them differently.

## When a stretch of work concludes

A bug understood, a decision made, an approach abandoned: record it with
`agents handoff draft` before moving on. At most three bullets, covering what a
future agent could not get from the code or the git log.

**The test is not "was this hard" but "does the diff carry it".** A fix's
justification — what was ruled out, why this fix and not another, what was
deliberately left alone — is invisible in the change itself. That is worth a
draft even when the fix explains itself. A mechanical edit with no conclusion is
not.

Drafts are untracked until reviewed, so drafting costs nothing and commits you
to nothing. That is the point: the cost of a wrong draft is a `--bin`.

## When the material is worth keeping

`agents review` lists what is pending; `agents review --keep <id>` promotes one,
which writes it, regenerates the affected index, and commits, in a single act.
Promotion is where a human decides — never promote in bulk, and prefer to show
the drafts and ask.

## When a question needs evidence rather than recall

`agents trace ls` finds the sessions that touched a lane or a module.
`agents trace show <id>` reads one transcript back — the harness's own copy if it
still exists, otherwise the cached one. Reach for it when the answer is *what
actually happened* and grep over the current tree cannot say, because the tree
no longer contains the attempt that failed.

## Recording and committing

`agents index` regenerates the generated indexes; it is also run by the
pre-commit guard, so a stale index blocks a commit rather than landing.
`agents save` is an escape hatch for a hand-edited memory entry — the normal
path is promotion, which commits on its own.

Keep repository content and agent context in separate commits. The guard warns
when one commit touches both.
```

- [ ] **Step 4: Run, confirm it passes**

```bash
cd ~/dotfiles/agents && go test -count=1 -run TestHarnessSkillCoversAgentCommands ./...
```

If it names a missing command, add a sentence about *when* to use it — do not
add a bare mention to satisfy the check.

- [ ] **Step 5: Demonstrate it can fail**

Delete the `agents trace show` paragraph from the skill, re-run, confirm it is
named, restore.

- [ ] **Step 6: Run the whole docs set, then push with Task 10's workflow step**

```bash
cd ~/dotfiles/agents && go test -count=1 ./...
cd ~/dotfiles
ruby -ryaml -e 'd=YAML.load_file(".github/workflows/verify.yml"); puts "parses, jobs: #{d["jobs"].keys.join(", ")}"' 
git add claude/skills/agents-tool/SKILL.md agents/docs_test.go .github/workflows/verify.yml
git commit -m "$(cat <<'EOF'
docs: add the fleet-wide agents skill, and require it to cover agent commands

links.manifest symlinks claude/skills to ~/.claude/skills on every provisioned
machine, so this reaches every session in every repository. .agents/skills/
would have given the guidance to the one repository that needs it least.

The generated README block answers "what is this command"; the skill answers
"which command is this situation", which is judgment and cannot be generated.
The coverage check keeps the second honest as the first grows -- without it a
command lands, gets a generated reference entry, and no agent ever reaches for
it. Spec 7 measured that exact shape.
EOF
)"
git push && gh pr checks --watch
```

Expected: `docs` green.

---

## Task 12: Linux — the `dotfiles` profile in a container

**Files:**
- Modify: `.github/workflows/verify.yml`

**Interfaces:**
- Produces: job id `linux-dotfiles`.

Spec 2's largest known gap: no phase of it has ever run on Linux. The `dotfiles`
profile exists precisely so it is container-safe — no sudo, no network, no
package manager, no login-shell change.

- [ ] **Step 1: Reproduce locally first**

```bash
cd ~/dotfiles
docker run --rm -v "$PWD":/src:ro -w /tmp debian:stable-slim bash -c '
  set -x
  apt-get update -qq && apt-get install -y -qq golang-go git >/dev/null
  cp -r /src /work && cd /work
  ./bootstrap plan dotfiles;  echo "plan=$?"
  ./bootstrap apply dotfiles; echo "apply=$?"
  ./bootstrap check dotfiles; echo "check=$?"
'
```

**Record all three exit codes.** Spec 2's Known gap 2 says `plan` exits `2` on a
machine lacking Homebrew or fish. If that happens here, **it is a finding to fix,
not a reason to weaken the job**: a profile designed to be safe where nothing is
installed, which refuses where nothing is installed, is not doing its job. Fix
it in this task and record what you changed.

- [ ] **Step 2: Add the job**

```yaml
  # The dotfiles profile is preflight + config + verify: no sudo, no network, no
  # package manager, no login-shell change. That is what makes it safe here, and
  # it is the first time any phase of spec 2 has run on Linux.
  linux-dotfiles:
    runs-on: ubuntu-latest
    container: debian:stable-slim
    steps:
      - name: minimal prerequisites
        run: apt-get update -qq && apt-get install -y -qq golang-go git ca-certificates
      - uses: actions/checkout@v4
      - name: git trusts the workspace
        run: git config --global --add safe.directory "$GITHUB_WORKSPACE"
      - name: plan
        run: ./bootstrap plan dotfiles
      - name: apply
        run: ./bootstrap apply dotfiles
      - name: check converges after apply
        run: ./bootstrap check dotfiles
```

- [ ] **Step 3: Add to the gate, validate, push**

```yaml
    needs: [test, secrets, hygiene, docs, linux-dotfiles]
```

```yaml
          echo "linux-dotfiles: ${{ needs.linux-dotfiles.result }}"
          [ "${{ needs.linux-dotfiles.result }}" = "success" ] || exit 1
```

```bash
cd ~/dotfiles
ruby -ryaml -e 'd=YAML.load_file(".github/workflows/verify.yml"); puts "parses, jobs: #{d["jobs"].keys.join(", ")}"' 
git add .github/workflows/verify.yml
git commit -m "ci: run the dotfiles profile on Linux, closing part of spec 2's largest gap"
git push && gh pr checks --watch
```

- [ ] **Step 4: Demonstrate it can fail**

Temporarily add a `link` row to `bootstrap.d/links.manifest` naming a source that
does not exist, push, confirm `linux-dotfiles` red, revert, confirm green.

- [ ] **Step 5: Update spec 2's Known gaps**

In `docs/superpowers/specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md`, amend
the "Linux is untested" gap to state precisely what now runs on Linux and what
still does not. Do not delete the gap — narrow it, and say which job narrowed it.

```bash
cd ~/dotfiles
git add docs/superpowers/specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md
git commit -m "docs(spec-2): narrow the Linux gap to what CI does not yet cover"
```

---

## Task 13: The Arch stage-zero repair

**Files:**
- Modify: `bootstrap.d/internal/phase/packages.go`, `bootstrap.d/internal/phase/packages_test.go`, `.github/workflows/verify.yml`

**Interfaces:**
- Produces: job id `linux-stage-zero`.

**Measured 2026-08-13: Arch stage zero has never worked.** `packages.go:86` runs
`pacman -S --needed --noconfirm` with no prior database sync, and a fresh Arch
system ships an empty one:

```
$ docker run --rm --platform linux/amd64 archlinux:base \
    pacman -S --needed --noconfirm base-devel curl file git
error: target not found: base-devel        → exit 1
```

`packages.go:76` syncs first on the apt branch and explains why; the pacman
branch has neither the step nor a comment about its absence.

- [ ] **Step 1: Write the failing test**

In `bootstrap.d/internal/phase/packages_test.go`, add:

The package's existing double is `fakeChange` (`preflight_test.go:246`). Its
`Sudo` records into `Ops` as `"sudo <argv>"` via `record`, so the assertions read
`Ops` rather than a dedicated field.

```go
// Measured 2026-08-13 on archlinux:base: `pacman -S` with no prior sync exits 1
// with "target not found" for all four packages, because a fresh Arch system
// ships an empty sync database. The repair is -Syu and NOT -Sy: Arch does not
// support partial upgrades, and syncing the database without upgrading
// installed packages is a documented way to break a system.
func TestArchStageZeroSynchronizesBeforeInstalling(t *testing.T) {
	f := &fakeChange{lookPathOnly: map[string]bool{"pacman": true}}
	if err := stageZero(Context{Platform: "linux", Change: f}); err != nil {
		t.Fatalf("stageZero: %v", err)
	}

	var pacman string
	for _, op := range f.Ops {
		if strings.HasPrefix(op, "sudo pacman") {
			pacman = op
		}
	}
	if pacman == "" {
		t.Fatalf("stage zero ran no pacman command; Ops = %v", f.Ops)
	}
	if !strings.Contains(pacman, "-Syu") {
		t.Errorf("pacman must sync and upgrade before installing; got %q", pacman)
	}
	if strings.Contains(pacman, "-Sy ") || strings.HasSuffix(pacman, "-Sy") {
		t.Errorf("-Sy alone is a partial upgrade and is unsupported on Arch; got %q", pacman)
	}
	if strings.Contains(pacman, "--disable-sandbox") {
		t.Errorf("--disable-sandbox is an emulation artefact and must never ship; got %q", pacman)
	}
}
```

`lookPathOnly` rather than `lookPathErr` is deliberate and the fixture's own
comment says why: it expresses "pacman and nothing else", which is the shape that
keeps this case honest when a name is added to a required list later.

- [ ] **Step 2: Run, confirm it fails**

```bash
cd ~/dotfiles/bootstrap.d && go test -count=1 -run TestArchStageZero ./internal/phase/
```

Expected: FAIL — the command is `-S`, not `-Syu`.

- [ ] **Step 3: Implement the repair**

In `bootstrap.d/internal/phase/packages.go`, replace the pacman branch:

```go
	if _, err := c.Change.LookPath("pacman"); err == nil {
		c.logf("   stage zero  pacman: base-devel curl file git")
		// -Syu, not -S and not -Sy. A fresh Arch system ships an empty sync
		// database, so a bare -S resolves nothing: measured on archlinux:base,
		// "target not found" for all four packages, exit 1. Arch does not
		// support partial upgrades either, so syncing the database without
		// upgrading installed packages is a documented way to break a system.
		//
		// The honest cost: installing four packages performs a full system
		// upgrade. That is the supported shape, and the alternative -- requiring
		// an already-synced system -- is a precondition nothing can enforce on a
		// machine being provisioned for the first time.
		//
		// --needed still skips what is already present, so a re-apply does not
		// reinstall four packages.
		return c.Change.Sudo("pacman", "-Syu", "--needed", "--noconfirm",
			"base-devel", "curl", "file", "git")
	}
```

- [ ] **Step 4: Run, confirm it passes**

```bash
cd ~/dotfiles/bootstrap.d && go test -count=1 ./... && go vet ./... && test -z "$(gofmt -l .)"
( umask 077 && cd ~/dotfiles/bootstrap.d && go test -count=1 ./... )
```

- [ ] **Step 5: Commit the fix on its own**

```bash
cd ~/dotfiles
git add bootstrap.d/internal/phase/packages.go bootstrap.d/internal/phase/packages_test.go .github/workflows/verify.yml
git commit -m "$(cat <<'EOF'
fix(bootstrap): sync the pacman database before installing, and verify on Linux

Arch stage zero has never worked. packages.go ran `pacman -S --needed
--noconfirm` with no prior database sync, and a fresh Arch system ships an empty
one -- measured on archlinux:base: "target not found" for all four packages,
exit 1. The apt branch syncs first and says why; the pacman branch had neither
the step nor a comment about its absence.

The repair is -Syu, not -Sy. Arch does not support partial upgrades, so syncing
the database without upgrading installed packages is a documented way to break a
system. The honest cost is that installing four packages now performs a full
system upgrade.

Verified locally under amd64 emulation, where the run additionally needed
--disable-sandbox because pacman's seccomp sandbox fails there. That flag is an
emulation artefact and is deliberately not shipped: disabling the sandbox on a
real machine is a security regression working around a problem that machine does
not have. A test asserts it never appears in the command.
EOF
)"
```

---

## Task 14: Linux — stage zero for real, on Debian and Arch

**Files:**
- Modify: `.github/workflows/verify.yml`

**Interfaces:**
- Consumes: Task 13's `-Syu` repair (the Arch job is red without it).
- Produces: job id `linux-stage-zero`.

**Measured before writing this task, and it decides the job's shape.**
`phase.For` (`internal/phase/phase.go:94`) gives the `dotfiles` profile only
`{preflight, config, verify}`; `stageZero` is called from `Packages`
(`packages.go:38`), which that profile excludes. `bootstrap` has **no per-phase
flag** — the verbs take a profile and nothing narrower. So `apply dotfiles`
never reaches stage zero, and `apply workstation` is the only path to the real
code.

That makes this job broader than its name: `workstation` also runs Homebrew,
fish and devtools. **This is in scope rather than an accident** — spec 2's Known
gaps list the Homebrew prefix at `/home/linuxbrew/.linuxbrew` and `chsh` under a
different `/etc/shells` convention as equally unexercised, and CI is the only
route to those too.

**Expect this job to be red more than once, and treat each failure as a
finding.** Task 13's unit test asserts the command string; only this job asserts
the string *works*. Neither is worth much alone.

- [ ] **Step 1: Reproduce locally before writing YAML**

```bash
cd ~/dotfiles
docker run --rm --platform linux/amd64 -v "$PWD":/src:ro archlinux:base bash -c '
  set -x
  pacman -Syu --needed --noconfirm sudo go git >/dev/null 2>&1
  cp -r /src /work && cd /work
  git config --global --add safe.directory /work
  ./bootstrap apply workstation; echo "apply=$?"
  command -v gcc curl file git
'
```

Record the exit code and which phase it stopped in. Repeat with
`debian:stable-slim`, substituting
`apt-get update -qq && apt-get install -y -qq sudo golang-go git ca-certificates`.

- [ ] **Step 2: Add the job**

```yaml
  # Stage zero for real. `apply workstation` rather than `apply dotfiles`,
  # because phase.For gives the dotfiles profile only {preflight, config,
  # verify} and stageZero is called from Packages -- there is no per-phase flag,
  # so this is the only path to the real code. That also drags in Homebrew, fish
  # and devtools, which spec 2's Known gaps list as equally unexercised on
  # Linux; those are in scope here rather than accidental.
  #
  # Both base images lack sudo. It is installed and configured NOPASSWD because
  # Applier.run (internal/change/applier.go:335) sets Stdout and Stderr and
  # leaves Stdin nil, so any prompt is an immediate "no tty present" --
  # passwordless is the only shape in which this code can succeed at all.
  linux-stage-zero:
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        include:
          - image: debian:stable-slim
            prereqs: apt-get update -qq && apt-get install -y -qq sudo golang-go git ca-certificates
          - image: archlinux:base
            prereqs: pacman -Syu --needed --noconfirm sudo go git
    container:
      image: ${{ matrix.image }}
      options: --user root
    steps:
      - name: prerequisites and passwordless sudo
        run: |
          set -eux
          ${{ matrix.prereqs }}
          echo "root ALL=(ALL) NOPASSWD: ALL" > /etc/sudoers.d/ci
          chmod 0440 /etc/sudoers.d/ci
      - uses: actions/checkout@v4
      - name: git trusts the workspace
        run: git config --global --add safe.directory "$GITHUB_WORKSPACE"
      - name: provision
        run: ./bootstrap apply workstation
      - name: stage zero delivered its four prerequisites
        run: |
          set -eux
          command -v gcc
          command -v curl
          command -v file
          command -v git
```

- [ ] **Step 3: Add to the gate, validate, push**

```yaml
    needs: [test, secrets, hygiene, docs, linux-dotfiles, linux-stage-zero]
```

```yaml
          echo "linux-stage-zero: ${{ needs.linux-stage-zero.result }}"
          [ "${{ needs.linux-stage-zero.result }}" = "success" ] || exit 1
```

```bash
cd ~/dotfiles
ruby -ryaml -e 'd=YAML.load_file(".github/workflows/verify.yml"); puts "parses, jobs: #{d["jobs"].keys.join(", ")}"' 
git add .github/workflows/verify.yml
git commit -m "ci: run stage zero for real on Debian and Arch"
git push && gh pr checks --watch
```

- [ ] **Step 4: Demonstrate the Arch job proves Task 13's fix**

Revert `packages.go` to `-S`, push, confirm the Arch job red with
`target not found`, restore, confirm green. This is the only check in the plan
whose failure has already been observed off-CI; observing it on-CI is what proves
the job runs at all rather than passing vacuously.

- [ ] **Step 5: Record what the job could not close**

If a later phase fails and you narrow the job rather than fixing it — for
instance skipping the fish phase in a container — say so in
`docs/superpowers/specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md`'s Known
gaps rather than leaving the gap looking closed. A gap that appears closed while
open is worse than one visibly open.

```bash
cd ~/dotfiles
git add docs/superpowers/specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md
git commit -m "docs(spec-2): record what the Linux jobs verify and what they do not"
```

---

## Task 15: Reproduce the worktree hazard before adding a doctor check

**Files:**
- Modify: `agents/internal/doctor/doctor.go`, `agents/internal/doctor/doctor_test.go` (only if Step 2 shows a check is needed)

**Interfaces:**
- Consumes: nothing.

Phase 6 of the spec, and it **begins by establishing whether the defect is
already reported**. `doctor.go:436` compares `core.hooksPath` against `HooksDir`
as strings with no `os.Stat`, which is the mechanism PR #16 described — but a
binary stamped to a nonexistent root fails three checks today because that stamp
*disagrees* with the real config, and `git-hooks:links` looks under the stamped
root, so it may already catch the agreeing case.

- [ ] **Step 1: Reproduce the actual scenario**

```bash
cd ~/dotfiles
git worktree add /tmp/wt-probe -b wt-probe
cd /tmp/wt-probe/agents
go build -trimpath -ldflags "-X main.dotfilesRoot=/tmp/wt-probe" -o /tmp/agents-wt .
# Make core.hooksPath agree with the stamp, as install-hooks.sh would from here:
git config --global core.hooksPath /tmp/wt-probe/git/hooks.d
/tmp/agents-wt doctor; echo "exit BEFORE deletion=$?"
cd ~/dotfiles && git worktree remove --force /tmp/wt-probe && git branch -D wt-probe
/tmp/agents-wt doctor; echo "exit AFTER deletion=$?"
```

**Record the full output of both runs.** Restore your real configuration
immediately afterwards:

```bash
git config --global core.hooksPath "$HOME/dotfiles/git/hooks.d"
agents doctor    # confirm your machine is healthy again
rm -f /tmp/agents-wt
```

- [ ] **Step 2: Decide from what you observed**

- **If some check already fails** after the deletion, PR #16's note is stale.
  Add no check. Instead amend the Makefile comment and PR #16's claim to name
  the check that does report it, commit that, and stop. Record which check.
- **If nothing fails** — exit `0` with every check green — continue to Step 3.

- [ ] **Step 3 (only if needed): Write the failing test**

In `agents/internal/doctor/doctor_test.go`:

```go
// A binary stamped to a checkout that no longer exists runs none of the personal
// git hooks and says nothing about it: githook treats a missing extras directory
// as "no personal hooks" and carries on at exit 0. Every path-comparing check
// passes, because the paths agree with each other -- they simply both name
// nothing.
func TestDoctorFailsWhenTheStampedRootIsGone(t *testing.T) {
	deps := DependenciesFor(filepath.Join(t.TempDir(), "deleted-worktree"))
	checks := rootChecks(deps)
	var found *Check
	for i := range checks {
		if checks[i].Name == "root:exists" {
			found = &checks[i]
		}
	}
	if found == nil {
		t.Fatal("no root:exists check was produced")
	}
	if found.Status != Fail {
		t.Errorf("root:exists = %v, want Fail: the consequence is a correctness mechanism silently not running", found.Status)
	}
}
```

- [ ] **Step 4 (only if needed): Implement**

`Dependencies` (`doctor.go:62`) has **no `Root` field** — verified 2026-08-14.
`DependenciesFor(root)` receives the root and derives `HooksDir` and
`AttributesSource` from it without keeping it. Add the field:

```go
type Dependencies struct {
	LookPath              func(string) (string, error)
	// ...existing fields unchanged...
	SharedGitConfig       string
	// Root is the checkout this binary was stamped to. Kept rather than only
	// derived from, because every other path here is built by joining onto it,
	// so nothing existing can report that the root itself is gone.
	Root                  string
}
```

and set it in `DependenciesFor`:

```go
	return Dependencies{
		// ...existing fields unchanged...
		Root: root,
	}
```

Then add the check and call it from the check assembly:

```go
// rootChecks reports whether the checkout this binary was stamped to still
// exists. Nothing else does: git-hooks:global compares core.hooksPath against
// HooksDir as strings, so two paths that agree with each other and both name a
// deleted directory pass. This fails rather than warns, because the consequence
// is that the git hook chain runs none of the personal hooks, at exit 0.
func rootChecks(deps Dependencies) []Check {
	info, err := os.Stat(deps.Root)
	switch {
	case err != nil:
		return []Check{{
			Name: "root:exists", Status: Fail,
			Detail: fmt.Sprintf("the stamped checkout %s does not exist", deps.Root),
			Remedy: "rebuild from the main checkout: cd <checkout> && make agents",
		}}
	case !info.IsDir():
		return []Check{{
			Name: "root:exists", Status: Fail,
			Detail: fmt.Sprintf("the stamped checkout %s is not a directory", deps.Root),
			Remedy: "rebuild from the main checkout: cd <checkout> && make agents",
		}}
	}
	return []Check{{Name: "root:exists", Status: OK, Detail: "the stamped checkout exists"}}
}
```

- [ ] **Step 5: Verify**

```bash
cd ~/dotfiles/agents && go test -count=1 ./... && go vet ./... && test -z "$(gofmt -l .)"
cd ~/dotfiles && make agents && agents doctor | grep root:exists
cd agents && go build -ldflags "-X main.dotfilesRoot=/nonexistent-root" -o /tmp/agents-badroot .
/tmp/agents-badroot doctor | grep root:exists    # must say FAIL
rm -f /tmp/agents-badroot
```

Note: **do not** try to demonstrate this with `AGENTS_DOTFILES_ROOT=/nonexistent
agents doctor`. The installed binary is stamped and the stamp deliberately beats
the environment — measured, the output is byte-identical with and without it.
That command is a check that cannot fail.

- [ ] **Step 6: Commit**

```bash
cd ~/dotfiles
git add agents/internal/doctor/doctor.go agents/internal/doctor/doctor_test.go
git commit -m "fix(agents): report a stamped checkout root that no longer exists"
```

---

## Task 16: Close out — enable the gate and record what changed

**Files:**
- Modify: `docs/superpowers/specs/agents/README.md`

**Interfaces:**
- Consumes: every preceding task. Produces nothing later tasks rely on.

- [ ] **Step 1: Confirm the whole gate is green on a fresh run**

```bash
cd ~/dotfiles && gh pr checks --watch
```

Expected: `test` ×4, `secrets`, `hygiene`, `docs`, `linux-dotfiles`,
`linux-stage-zero` ×2, `gate` — all green.

- [ ] **Step 2: Confirm `gate` is the only required check**

Repository settings → Branches → `master`. Exactly one required status check:
`gate`.

- [ ] **Step 3: Update the spec catalog**

In `docs/superpowers/specs/agents/README.md`, change spec 5's status from
`designed` to `implemented`, and add a row to the implementation-plans table:

```markdown
| 5 | [the verification gate](2026-08-14-spec-5-verification-gate.md) | executed |
```

```bash
cd ~/dotfiles
git add docs/superpowers/specs/agents/README.md
git commit -m "docs: spec 5 is implemented"
```

- [ ] **Step 4: Draft a handoff**

```bash
agents handoff draft --lane spec-5-verification-gate --session <this session id>
```

Three bullets covering what a future agent could not get from the diff — for
instance which open questions the Linux jobs actually answered, and whether the
worktree scenario needed a new doctor check or turned out already reported.

- [ ] **Step 5: Merge**

```bash
cd ~/dotfiles && gh pr merge --squash
```

---

## Self-Review

**1. Spec coverage.** Every phase maps to tasks: phase 1 → Task 1; phase 2 →
Tasks 2-3; phase 3 → Tasks 4-8; phase 4 → Tasks 9-11; phase 5 → Tasks 12-14;
phase 6 → Task 15; close-out → Task 16. The spec's three open questions are each
answered by a task step that records the result: `plan dotfiles` in a container
(Task 12 Step 1), plain `-Syu` on a native runner (Task 14 Step 1), and
`bootstrap`'s exit-code audit (Task 8 Step 2).

**2. Placeholder scan.** One deliberate parameter remains: `GITLEAKS_SHA256` in
Task 1, whose value Step 2 computes with an exact command — pinning a checksum
requires the artifact.

Task 15 is conditional by design: Steps 3-5 run only if Step 1 shows nothing
already reports the defect, which is the spec's own instruction not to add a
check that duplicates an existing one.

**Four hedges present in the first draft were resolved by reading the code
rather than left to the implementer**, and each changed the plan:

- `apply dotfiles` does **not** reach `stageZero`. `phase.For` gives the
  `dotfiles` profile only `{preflight, config, verify}` and `stageZero` is
  called from `Packages`; there is no per-phase flag. Task 14 therefore uses
  `apply workstation` and says why that widens its scope.
- The phase test double is `fakeChange` (`preflight_test.go:246`), not
  `fakeMachine`, and records privileged calls into `Ops` as `"sudo <argv>"`.
  Task 13's test reads `Ops`.
- `packageDir` already exists (`main_test.go:33`) and is already used for this
  purpose at `cmd_doctor_test.go:315`. Task 9 uses it rather than adding one.
- `doctor.Dependencies` has **no** `Root` field. Task 15 adds it explicitly
  rather than saying "add one if absent".

**3. Task sizing.** Task 13 was split out of the original Linux task. The
`-Syu` repair is small, certain and valuable on its own; the container job that
proves it is larger and may find more. A reviewer can accept the fix and reject
the job.

**4. Type consistency.** `Command`, `Audience`, `IO`, `Find`, `Walk`,
`RenderUsage`, `RenderHelp`, `RenderExitCodes`, `RenderMarkdown`, `rootCommand`,
`runHelp`, `readmeBlock`, `repoRootForDocs`, `packageDir` are each defined once
and used with matching signatures. Two temporary stubs are introduced with an
explicit deletion step: `RenderExitCodes` (Task 4 Step 3 → deleted Task 6 Step 3)
and `runHelp` (Task 5 Step 4 → deleted Task 6 Step 3). Task 10's workflow step
names `TestHarnessSkillCoversAgentCommands`, which Task 11 creates; Task 10 Step
5 therefore commits only the test and Task 11 Step 6 pushes the workflow change.
