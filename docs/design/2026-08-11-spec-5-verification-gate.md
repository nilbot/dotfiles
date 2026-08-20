# Spec 5 — the verification gate

**Date:** 2026-08-10 (scope, as "CI, releases, and binary distribution") /
2026-08-11 (design, narrowed to verification) / 2026-08-13 (measured against
master) / 2026-08-14 (restructured phase-first; every verification command
executed, which changed phase 2)
**Status:** designed — not implemented
**Depends on:** [spec 1](2026-08-07-agents-repo-context-design.md) for the Go
module, the exit-code table, and the security boundaries CI must not cross.
**Carries obligations from:** [spec 2](2026-08-07-spec-2-dotfiles-hygiene.md),
implemented. Its largest known gap — no phase of it has ever run on Linux — is
closed here or nowhere.
**Split from:** releases, versioning, checksums, Homebrew, and what a distributed
binary stamps, which live in
[spec 6](2026-08-11-spec-6-releases-and-distribution.md), scope only.

> **One correction worth carrying, 2026-08-13.** A draft of this spec asserted
> that `agents guard --staged` was invoked by nothing, and built an organizing
> principle on it. It is invoked on **every** `pre-commit`:
> `agents/main.go:51-58` calls it and maps `Advisory` to `OK` so a warning does
> not abort the commit. `githook_main_test.go:192` pins the mapping, `:276` pins
> that a hand-edited `INDEX.md` is still blocked.
>
> This is recorded because the false conclusion is easy to re-derive:
> `githook.builtin` (`githook.go:354`) returns `0` for `pre-commit`, which reads
> like an absent guard. It returns `0` because the wiring is in `main.go`,
> exactly as [spec 1's plan](../archive/plans/2026-08-07-agents-repo-context.md)
> Task 17 said it would be. **Anyone auditing this must read `runGitHook` to its
> end; `internal/githook` alone shows the opposite.**

---

## What this spec is

Spec 2 deleted `git/hooks/go.pre-commit` in `c0e3bb1`, with a commit message
ending "This is CI's job. See spec 5." Since then **this repository has had no
automated Go gate anywhere.**

The deleted hook is not what is being restored. Recovered with
`git show ac5286a:git/hooks/go.pre-commit`: it ran only where `.go` files sat at
the repository root (neither of this repository's modules), ran `go build -n`,
`go test`, `go fmt`, `go vet` each without `./...`, redirected everything to
`/dev/null` so failures carried no diagnostic, let `go fmt` rewrite files
mid-commit without staging them — and **was never installed by anything**.
`git/install-hooks.sh` links four fixed hook names and never names that file.

So this is the first build, test, `gofmt` and `vet` gate this repository has ever
actually run. Six phases, each landing a check together with whatever fix makes
it green, so the branch is mergeable at every boundary.

## Current baseline

Measured on this machine, 2026-08-13 and re-verified 2026-08-14, after spec 7's
phases A and B′ landed.

- No `.github/` directory. No workflows, no PR checks, no releases.
- Two Go modules — `agents/` and `bootstrap.d/` — and deliberately **no
  `go.work`** (spec 2 §11). Two independent build graphs.
- 107 Go files, 54 containing tests. The suites are fast.
- Go `1.26.6` `darwin/arm64`; `go.mod` says `go 1.26`, which floats. **The first
  baseline for this spec measured `1.26.5`; re-verifying it a day later measured
  `1.26.6`.** The toolchain moved underfoot mid-design and nothing recorded that
  it had — the argument for pinning, arriving unprompted.
- gitleaks 8.30.1 on `PATH`, run by hand.
- `agents/main.go:61` dispatches through a `switch`; `:98` is a hand-maintained
  `usage()` literal. Nothing connects them. No `help` command, no `--help` at any
  depth; `agents --help` reports `unknown command` and exits `3`.
- Four files read the real `$HOME`: `agents/root.go`,
  `agents/internal/doctor/doctor.go`, `agents/internal/machine/machine.go`,
  `bootstrap.d/main.go`.
- **The exit-code table is described in four places and three of them disagree.**

  | Source | code 4 | code 5 |
  |---|---|---|
  | Spec 1 §6 (design intent) | not applicable / skip | could not record |
  | `exitcode.go` constant comments | not applicable here | could not complete the requested operation |
  | `agents` help text | skip | could not complete the operation |
  | `bootstrap` help text | not applicable | *absent* |

- `bootstrap.d/main.go:29` declares `exitAdvisory`. **No path in the module
  returns it** — verified as the only occurrence, with no bare `return 1` in the
  file.
- `agents ls` reports **56 registered repositories, 40 of them missing.** Two are
  real; the rest are Go test temp directories and capture-experiment repos that
  the test suite registered into the real machine-local registry. See phase 2.
- `agents doctor` exits advisory on this healthy machine, over `recording:codex`
  and `pointers:unverified`. Both machine-local, both meaningless on a runner.

## The organizing claim

**One artifact per fact; everything else computed from it.**

The recurring defect is a hand-maintained description of the code that nothing
forces to match the code. Four instances, all above: `usage()` against the
dispatch switch, the exit-code table against itself, `INDEX.md` against its
frontmatter, and a README with no command reference at all — nothing stale
because nothing is there, and nothing discoverable either.

---

## Decisions that bind every phase

Recorded once here rather than repeated per phase.

### No path filters, anywhere in the workflow

**Triggers:** `pull_request`, `push` to `master`, `workflow_dispatch`. No `paths`
or `paths-ignore`.

The scope note asked for filters "without allowing a docs-only change to
masquerade as source verification." The filter is the hazard, and it is the same
hazard as the Go build cache in different clothing. The obvious filter is
`paths: ['agents/**', 'bootstrap.d/**']` — which skips verification for a change
to `Makefile` or `git/gitconfig.shared`. But `bootstrap.d/makefile_test.go` reads
the `Makefile` and `agents/internal/doctor/doctor_test.go` reads
`git/gitconfig.shared`. Those are exactly the edits that break tests, and exactly
the edits a cached `go test` already fails to notice.

**The rule: no path filter may exclude a file that any test reads.** That set is
not the set under the module directories, nothing keeps it enumerable, and
getting it wrong is invisible. The only filter satisfying the rule is none.

One mechanical trap, recorded so it is not rediscovered: a required check skipped
by a path filter reports "Expected" indefinitely and blocks the pull request
rather than passing it.

### `-count=1` on every test invocation

Not stylistic. Several tests assert against tracked **non-Go** files, whose edits
do not invalidate a cached pass. `agents/internal/doctor/doctor_test.go:450`
records it in the tree:

> Reading a tracked file means the Go build cache does not know when the answer
> changes: run this module's tests with `-count=1` after any rename.

This was watched happening: deleting the Makefile's `githooks` target broke two
tests in `agents` while `go test ./...` reported `ok (cached)`. The breakage left
an artifact when fixed in `06ff3c9`; the stale pass that hid it left none.

### `GOWORK=off` in the workflow environment

Spec 2's rejected alternatives turned down a tracked `go.work` because it alters
module resolution and would hand the release path a `GOWORK=off` footgun. Setting
it explicitly costs one line and means no stray workspace file can change what CI
resolves.

### Toolchain pinned to exact versions

Go `1.26.6`, not `go-version-file` — `go 1.26` in `go.mod` floats to whatever the
runner ships. gitleaks `8.30.1`, checksum-verified on download. Bumping either is
a deliberate pull request whose diff says what changed.

### Exit codes: CI's default is already correct

An exit code carries two things, and only one has an automated consumer:

| | means | who can act on it |
|---|---|---|
| **disposition** | proceed, stop, you-called-me-wrong | git, CI, a harness |
| **attention** | there is output worth reading | a human, an agent |

Git's contract is binary — `pre-commit` and `commit-msg` abort on any non-zero,
`post-merge` and `post-checkout` ignore the code entirely. CI has no reader at
the moment of the call, so it is in the same class. A human or agent reads the
text, and non-zero is what makes them read it: `agents init` exits `1` "so the
state is visible rather than assumed" (spec 1 §9), and `agents review --stats`
returns `1` for readings that are successful but not a clean pass.

**`agents/main.go:51-58` is where the two vocabularies meet, and it already
translates correctly** — `Advisory` becomes `OK`, only `Block` stops the commit.
`internal/githook` speaks git's raw `0`/`1` and does not import `exitcode`, which
is right for a low-level dispatcher. `cmd_hook.go:30` returns literal `0` always,
per spec 1 §6's fail-open rule.

So **CI's default "non-zero fails the step" is the correct disposition reading**,
and needs no wrapping. Verified on this provisioned machine, 2026-08-14:
`agents index`, `./bootstrap plan dotfiles` and `./bootstrap check dotfiles` all
exit `0`. Whether `plan dotfiles` still does inside a bare container is phase 5's
open question, and the answer does not change this reading — it changes what
phase 5 must fix.

Explicit assertion is reserved for **negative paths** — `expect 2` proves a
refusal still refuses, which "non-zero fails" cannot express and which is the
test that catches a guard that quietly stopped guarding.

`agents doctor` does **not** gate CI. Its machine-local checks are unassertable
on a runner, and `doctor_test.go` already covers its logic.

### Every check must be demonstrated to fail

Break the thing it guards, observe red, restore. A check whose failure has never
been seen is indistinguishable from one that cannot fail.

This is the repository's own finding: its memory records that the dominant defect
in these tests is *a double that answers correctly no matter what it is asked*,
that every instance was found by mutation and none by reading, and that two
survived to final review still green. The hook this spec replaces was that defect
at the largest scale — never installed, therefore never able to fail, therefore
never noticed.

### Constraints carried from spec 1

- CI must never approve or bypass Codex, Claude Code, Git, or other trust and
  security prompts.
- No generated binary is committed to the dotfiles tree.
- CI must not publish machine-local paths, registry contents, transcript
  pointers, hook trust state, or user configuration. Runner logs are an output
  surface like any other.
- The record type retains its structural inability to carry assistant messages,
  tool input, or tool response.
- The embedded gitleaks configuration and the tracked canonical configuration
  remain byte-for-byte pinned.

---

## The job graph

Stated once so each phase can say which job it creates or extends.

| Job | Runner | Created by | Extended by |
|---|---|---|---|
| `test` (matrix) | `{macos-latest, ubuntu-latest}` × `{agents, bootstrap.d}` | phase 1 | — |
| `secrets` | `ubuntu-latest` | phase 1 | — |
| `gate` | `ubuntu-latest` | phase 1 | every later phase adds to `needs:` |
| `hygiene` | `ubuntu-latest` | phase 2 | — |
| `docs` | `ubuntu-latest` | **phase 3** (help coverage) | **phase 4** (README block, backward check, skill coverage) |
| `linux-dotfiles` | container | phase 5 | — |
| `linux-stage-zero` (matrix) | `{debian, arch}` containers | phase 5 | — |

**Four `test` jobs, not one.** Spec 2 §11 chose two modules specifically so a
`bootstrap` failure can never block an `agents` release. Collapsing them hands
that property back for a small saving in configuration.

**Branch protection names `gate` and nothing else.** Adding a job later is then a
workflow change rather than a repository-settings change, and the set of things
that must pass stays visible in the tracked file.

---

## Phase 1 — the workflow skeleton

**Builds:** `.github/workflows/verify.yml` with the `test` matrix, `secrets`, and
`gate`. No repository source changes.

Each `test` job runs `go build ./...`, `go test -count=1 ./...`,
`go test -count=1 -race ./...`, `go vet ./...`, and asserts `gofmt -l` is empty.
`secrets` runs gitleaks 8.30.1 against the embedded configuration.

**Expected green on arrival.** This is the only phase that is.

**Verified by:**

```bash
# locally, per module — must already pass before the workflow is written
go build ./... && go test -count=1 ./... && go test -count=1 -race ./... \
  && go vet ./... && test -z "$(gofmt -l .)"
```

and on CI: `gate` reports success, and all four `test` jobs appear beneath it.

**Demonstrated to fail by:** introducing a `gofmt` violation on a scratch branch
and observing `gate` red, then reverting.

---

## Phase 2 — the hygiene job

**Builds:** the `hygiene` job, plus the test isolation its first check demands.

### Check 1 — the suites must not write to `$HOME`

**Containment, not portability.** The distinction is the whole content of this
check, and getting it wrong produces a check that passes while the defect it
targets is live. Measured 2026-08-14 on this machine:

- **Portability holds already.** `HOME=$(mktemp -d) XDG_CACHE_HOME=$(mktemp -d)
  go test -count=1 ./...` in `agents` exits **0**, no failing packages. The suite
  does not depend on this machine.
- **Containment does not.** That same run creates
  `$HOME/.local/state/agents/{registry.json, machine-id, registry.lock}` and
  registers **2 repositories** in it. The suite writes fleet state into whatever
  `HOME` it is handed.
- **Run normally, that `HOME` is the developer's.** This machine's real
  `~/.local/state/agents/registry.json` holds **56 entries, of which 2 are real
  repositories** — 28 are Go test temp directories and 26 are capture-experiment
  repos. `agents ls` reports 40 of them missing.

So "run under a synthetic `HOME` and require green" **passes today and catches
none of this.** It relocates the pollution rather than detecting it. The check
must assert the absence:

```bash
H=$(mktemp -d); X=$(mktemp -d)
HOME="$H" XDG_CACHE_HOME="$X" go test -count=1 ./...      # must pass
test ! -e "$H/.local/state/agents"                        # must hold — RED today
```

Red today with exactly two offenders, both in `agents/cmd_init_test.go`:
`TestInitDoesNotPointAtTheRetiredTrackedTracePath` and
`TestInitLeavesARepositoryThatCanCommit`. Both call `runInit`, which registers
into the fleet via `machine.StateDir()` → `XDG_STATE_HOME`, else
`os.UserHomeDir()`.

The fix is `t.Setenv("XDG_STATE_HOME", t.TempDir())` in the tests that call
`runInit` — the injection point already exists, `StateDir` consults it first, and
52 places in the suite already set `HOME` or `XDG_STATE_HOME` for this reason.
Once green, a developer's real registry is untouched too, because no test reaches
`HOME` at all.

**This is the sibling of the incident that motivated the job, on the same test.**
`a26eaa9` fixed `TestInitDoesNotPointAtTheRetiredTrackedTracePath` for calling
`runInit` with no `t.Chdir` — it had wired this repository with the ephemeral
`agents.test` path, four times per `go test` run, while `agents doctor` reported
the wiring exact. `TestMain` now chdirs out, closing the **cwd** route. That same
test still leaks by the **`$HOME`** route, because the two escape routes were
never enumerated together.

One constraint that fix introduced, for whoever writes these tests: three call
sites resolving repository paths from cwd — `task18RepoRoot` and two `go build`
invocations — now use `packageDir` captured before the chdir. A new test reading
a tracked fixture must do the same.

`bootstrap.d` additionally runs under `umask 077`, the condition PR #16 verified
by hand and which nothing currently keeps verified.

### Check 2 — `agents index` is a pure function of tracked content

Build `agents`, run `agents index`, require `git diff --exit-code` clean. The
`generated-file` guard rule already blocks a stale index at commit time via
`main.go:54` — but it is reached through `core.hooksPath`, global machine
configuration installed by `git/install-hooks.sh`. A contributor whose machine
has not run that has no guard, and nothing tells them. CI does not depend on the
reviewer's machine being provisioned.

**Verified by:**

```bash
H=$(mktemp -d); X=$(mktemp -d)
HOME="$H" XDG_CACHE_HOME="$X" go test -count=1 ./...   # both modules
test ! -e "$H/.local/state/agents"
( umask 077 && cd bootstrap.d && go test -count=1 ./... )
make agents && agents index && git diff --exit-code -- .agents/
```

**`git diff` is scoped to `.agents/` deliberately.** CI runs on a clean checkout
where a bare `git diff --exit-code` is equivalent, but locally it reports every
unrelated edit in the working tree and reads as a failure of this check. Scoping
it makes the command mean the same thing in both places.

**Demonstrated to fail by:** check 1 is already red — reverting the
`XDG_STATE_HOME` isolation on either `cmd_init_test.go` test restores it. For
check 2, hand-edit `.agents/memory/INDEX.md`.

---

## Phase 3 — the command registry

**Builds:** a declared command tree replacing the `switch` at `main.go:61` and
the `usage()` literal at `:98`; `agents help`; the `docs` job carrying the
coverage check; and the exit-code table rendered from the constants.

This is the largest phase and it is what the `cli-help-unification` lane is
waiting for. Spec 7 demonstrated the cost while this spec sat unimplemented: four
usage lines changed by hand in one workstream — `handoff write|draft|prune`
gained a verb, and `review`, `trace cache prune --retention` and `trace migrate`
are new — each a hand-edit that happened to be remembered. `trace cache prune`, a
destructive verb, still has no help at all.

### Central or distributed is the wrong axis

File layout is not what causes the bug; a hand-maintained central registry drifts
exactly as readily as scattered literals. **The property that matters is
derivation.** Three stages, each wanting a different answer:

- **Declare — distributed.** Each command declares itself next to its
  implementation. `review` declares itself in `cmd_review.go`. A command added in
  a new file cannot forget a registry in another file, because there is nothing
  to remember.
- **Assemble — central.** One tree collects the declarations. Dispatch needs one
  entry point; the checks need one thing to walk. No prose is written into it.
- **Render — derived, zero authorship.** `usage()`, `help <path>`, `--help` at
  any depth, and the exit-code table are *outputs*. Nobody edits them, so nobody
  can forget to.

### The declaration

```go
// Command is one node of the tree that dispatch and help both walk. Declaring a
// command and documenting it are the same act, so they cannot diverge.
type Command struct {
	Name     string     // "prune"; the full path comes from traversal
	Summary  string     // one line, shown in the parent's listing
	Usage    string     // "agents trace cache prune --lane <name>"
	Detail   string     // paragraph shown by `agents help <path>`
	Audience []Audience // who invokes this; see below
	Flags    func(*flag.FlagSet)
	Run      func(args []string, io IO) int
	Sub      []*Command
}

// IO is one bundle rather than three parameters because the existing handlers
// have three different signatures and a registry needs one. Measured
// 2026-08-14: most are (args, stdout); runHandoff, runHandoffWrite and
// runHandoffDraft take stdin; runHook takes stdin and writes to *stderr*, not
// stdout, because a harness consumes its stdout. Adapting them to a
// stdout-only signature would silently redirect the hook's diagnostics into a
// channel the harness parses.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type Audience string

// Automated audiences act on the exit code mechanically and cannot read prose.
// Attentional audiences read the output, and a non-zero code is what makes them.
const (
	Git     Audience = "git"     // automated
	Harness Audience = "harness" // automated
	CI      Audience = "ci"      // automated
	Human   Audience = "human"   // attentional
	Agent   Audience = "agent"   // attentional
)
```

Dispatch and help walk the same tree, which makes an undocumented command
*structurally* impossible rather than merely tested. An unknown command still
exits `3`.

**Three things follow from `Audience`; only the first two are checks.**

- **Help visibility.** `hook` is invoked only by a harness and `guard` only by
  git; neither belongs in top-level usage aimed at a person. They move under
  `help --all`. Today both sit in the same flat list as `init`.
- **Skill-doc coverage** — phase 4 consumes this. Every command with an `Agent`
  audience must appear in the harness guidance.
- It **records** that a command with an automated audience must have its
  vocabulary translated at the boundary rather than propagated, as
  `main.go:51-58` already does for `guard`. That is a comment the declaration
  makes unnecessary, not a check.

### The exit-code table, and `bootstrap`

Help renders the table from the constants rather than restating it, which
collapses two of the four descriptions in the baseline. A test pins the constants
against spec 1 §6's table: spec 1 is design intent, the code is the
implementation, and when they disagree the test surfaces it and a human decides
which was wrong.

`bootstrap` then needs `exitAdvisory` resolved — a path that means it, or the
constant deleted. Whether it also wants `5` is decided by whether it has a path
meaning *I tried and could not*, distinct from *this machine is in a state I will
not write over*. That is an audit of existing `exitBlock` returns, not an
argument from spec 1's authority or from matching `agents`.

**Verified by:**

```bash
agents help                    # exit 0, on stdout, rendered from the tree
agents help trace cache prune  # exit 0; non-empty usage and detail for a leaf
agents --help                  # same text as `agents help`, exit 0 (today: exit 3)
agents                         # same text, on stderr, still exit 3
agents nosuchcommand           # exit 3
cd agents && go test -count=1 ./...
```

**`agents` with no arguments keeps exiting `3` on stderr** while `agents help`
exits `0` on stdout. Same rendered text, different disposition: a bare
invocation is a usage error and an explicit `help` request is not. Only the text
is shared, and only the text is what derivation is for.

plus the coverage check in the `docs` job: walk the tree, fail on any node with
an empty `Summary`, `Usage`, or `Detail`.

**Demonstrated to fail by:** blanking one leaf's `Detail` and observing the
coverage check red; changing a constant's comment without changing spec 1's table
and observing the pinning test red.

---

## Phase 4 — documentation derived from the registry

**Builds:** a generated command block in `README.md`, a backward name check, the
harness guidance under `claude/skills/`, and the extensions to the `docs` job
that enforce all three.

**Forward** — the full command surface renders as markdown into a marked block in
`README.md`; CI regenerates and diffs it. The prose around the block stays
hand-written, because *when* to reach for a command is judgment and cannot be
generated.

**Backward** — no living document may name an `agents` subcommand the registry
does not define. The check scans inline code spans and fenced blocks for
`agents <subcommand>` and requires each path to resolve. This catches doc rot in
the direction that hurts: a document confidently naming a command that no longer
exists.

**Scope of the backward check:** `README.md`, both `CLAUDE.md` files — the
project one and the tracked global `claude/CLAUDE.md` — and everything under
`claude/skills/` and `.agents/skills/`. It **excludes**
`docs/archive/plans/` and `docs/design/`: those are dated records
of what was true when written, the executed bootstrap plan legitimately names
`make githooks`, and a record silently rewritten to stay true is not a record.

**Harness guidance lives in `claude/skills/`** — not in either `CLAUDE.md`, and
not in `.agents/skills/`. `bootstrap.d/links.manifest:20-21` symlinks
`claude/skills` → `~/.claude/skills` and `claude/CLAUDE.md` → `~/.claude/CLAUDE.md`
on every provisioned machine, so `claude/skills/` is fleet-wide while
`.agents/skills/` is per-repository procedure. `agents` is a fleet-wide tool —
`agents ls` lists registered repositories, `agents update --all` rewires every
one — so `.agents/skills/` would give the guidance to the one repository that
needs it least.

The two artifacts answer different questions and only one can be generated. The
rendered block answers *what is this command*; the skill answers *which command
is this situation* — when a finding is worth `agents handoff draft` rather than a
comment, why `agents review` stands between a draft and the tracked record, when
`agents trace show` answers what grep cannot. The `Agent`-audience coverage check
is what keeps the second honest as the first grows: without it a new command
lands, gets a generated reference entry, and no agent ever reaches for it — the
shape spec 7 measured when `CLAUDE.md` said *how* to write a handoff and never
*that* one should, and twenty sessions produced none.

Neither `CLAUDE.md` becomes a manual. The project one declares itself "only the
pointer" to `.agents/`; the global one is kept to rules a session acts on.

**Verified by:**

```bash
agents help --render=markdown > /tmp/surface.md
# regenerate the marked block in README.md from /tmp/surface.md, then:
git diff --exit-code README.md
cd agents && go test -count=1 -run 'TestDocumentedCommandsExist' ./...
```

**Demonstrated to fail by:** adding a command to the registry without
regenerating the README block; and naming `agents nosuchverb` in a code span in
`CLAUDE.md`.

---

## Phase 5 — Linux

**Builds:** `linux-dotfiles` and the `linux-stage-zero` matrix, plus the Arch
repair.

Spec 2's [Known gaps](2026-08-07-spec-2-dotfiles-hygiene.md#known-gaps-2026-08-11)
states the problem without hedging:

> **Linux is untested.** No phase in this design has run on Linux. The stage-zero
> package selection, the Homebrew prefix at `/home/linuxbrew/.linuxbrew`, and `chsh`
> under a different `/etc/shells` convention are all unexercised.

Its risk table records this as **not mitigated**. No Linux machine was available
during implementation and none is expected, so CI is the only route.

**`linux-dotfiles`** runs the `dotfiles` profile — preflight, config, verify — in
an unprivileged container. That profile exists precisely so it is container-safe:
no sudo, no network, no package manager, no login-shell change. `plan`, `apply`,
`check`, each asserted to exit `0`.

All three exit `0` on this provisioned machine (verified 2026-08-14). **Whether
`plan` still does in a bare container is unverified**, and spec 2's Known gap 2
says `plan` exits `2` on a machine lacking Homebrew or fish rather than
previewing. If that reaches the `dotfiles` profile, **it is a finding to fix, not
a reason to weaken the job**: a profile designed to be safe where nothing is
installed, which refuses where nothing is installed, is not doing its job.

**`linux-stage-zero`**, on `debian` and `archlinux`, runs stage zero for real.
Both base images lack `sudo`; it is installed and configured `NOPASSWD` — not for
convenience, but because `Applier.run`
(`bootstrap.d/internal/change/applier.go:335`) sets `cmd.Stdout` and `cmd.Stderr`
and leaves `cmd.Stdin` nil, so any prompt is an immediate "no tty present."

### Arch stage zero is broken — measured, not predicted

`packages.go:76` refreshes the index before installing on apt, with a comment
explaining why: "An install against an index months out of date does not install
an old version, it 404s on the URL the old index names." The pacman branch at
`packages.go:86` goes straight to `pacman -S --needed --noconfirm`, with no
equivalent step and no comment about its absence.

```
$ docker run --rm --platform linux/amd64 archlinux:base \
    pacman -S --needed --noconfirm base-devel curl file git
error: target not found: base-devel
error: target not found: curl
error: target not found: file
error: target not found: git
                                                        → exit 1
```

**Stage zero cannot work on a fresh Arch machine.** The stock image ships an empty
sync database, so `-S` resolves nothing — and this is not a container artefact:
any Arch install whose database has not been synced behaves the same way. Spec 2
has always listed Arch/Manjaro as supported, so **that support has never worked**,
not merely never been tested.

The repair is **not** `-Sy`. Arch does not support partial upgrades — syncing the
database without upgrading installed packages is a documented way to break a
system — so the supported form is `-Syu`, which means stage zero performs a full
system upgrade to install four packages. That cost is real, and the phase decides
between accepting it and requiring an already-synced system as an explicit
precondition. It does not ship `-Sy`.

**`-Syu` was verified in the same session: same image, same four packages, exit
0.** The verifying run needed the package sandbox disabled. **That belongs to
the container and stage zero must not ship it** — disabling pacman's sandbox on
a real machine is a security regression working around a problem that machine
does not have.

> **Corrected 2026-08-17.** Two claims in the paragraphs above were wrong when
> written, and the corrections change what the Arch job must do.
>
> **It is not an emulation artefact.** Re-measured on `menci/archlinuxarm:base`
> running natively on aarch64, no emulation anywhere: `pacman -Syu` fails with
> `restricting filesystem access failed because the Landlock ruleset could not
> be applied: Operation not permitted`, then `switching to sandbox user 'alpm'
> failed`. It is Landlock under Docker's default restrictions, not seccomp
> under emulation — and `--security-opt seccomp=unconfined` does **not** fix it,
> which is the measurement that disproves the original diagnosis outright.
>
> The fix is `DisableSandbox` under `[options]` in the container's
> `/etc/pacman.conf`, which takes plain `pacman -Syu` to exit 0 with the full
> install. It must be placed under `[options]`: appended at the end of the file
> it lands inside a repo section, where it clears the Landlock error and leaves
> the sandbox-user failure — measured both ways. Configuring the container is
> also why the flag stays out of the command, so the original conclusion
> survives its reasoning being wrong.
>
> **It is not "CI or nowhere".** `archlinux` publishes no arm64 manifest, but
> `menci/archlinuxarm` does, and every measurement in this correction was taken
> locally on Apple Silicon. The Arch job is locally reproducible; the belief
> that it was not is what left the original diagnosis unchecked.

**Verified by:** the three container jobs green, with the `dotfiles` profile
asserted at exit `0` for `plan`, `apply` and `check`, and both stage-zero
containers reaching a state where `build-essential`/`base-devel`, `curl`, `file`
and `git` are present.

**Demonstrated to fail by:** the Arch job is red before the repair and green
after — the one phase whose failure has already been observed.

---

## Phase 6 — the doctor rider

**Builds:** one `agents doctor` check that the stamped checkout root exists.

`make agents` stamps `$(CURDIR)` and writes the single global `~/bin/agents`, so
running it from a linked worktree publishes a binary stamped to a path that will
not survive. Delete the worktree and `githook.go:127` reads the missing extras
directory as "no personal hooks": the chain silently runs none, at exit `0`, and
`AGENTS_DOTFILES_ROOT` cannot rescue it because the stamp deliberately wins.
PR #16 documented this in the Makefile and left the fix for review.

The check **fails** rather than warns, because the consequence is a correctness
mechanism silently not running.

**What is verified, and what this phase must establish first.** Two things were
measured on 2026-08-14 and one deliberately was not.

`doctor.go:436` compares `globalValues[0].Value != deps.HooksDir` — **a string
comparison with no `os.Stat`.** Two paths that agree with each other and both
point at a deleted directory therefore pass, which is the mechanism PR #16
described, now confirmed in the code rather than taken on trust.

But a binary stamped to a nonexistent root fails **three** checks today —
`git-hooks:global`, `git-hooks:links` and `git-attributes` — because that stamp
*disagrees* with the real `core.hooksPath`. The worktree scenario is the agreeing
case, and `git-hooks:links` looks for the four links under the stamped root, so
**it may already catch the silent case.** Whether it does was not established.

**So the phase begins by reproducing the worktree scenario end to end** — build
from a worktree, point `core.hooksPath` at that worktree, delete it, run doctor
— and only adds `root:exists` if nothing already reports it. If something does,
the finding is that PR #16's note is stale and the phase is a documentation fix.
Adding a check that duplicates an existing one is the same defect as adding a
constant nobody returns.

**This is a rider, not verification.** It is a spec-1 defect belonging to doctor,
it needs no CI, and it is here because it would otherwise wait on a release
pipeline it has nothing to do with. Sequenced last so nothing depends on it.

**Verified by:**

```bash
# NOT `AGENTS_DOTFILES_ROOT=... agents doctor` -- the installed binary is
# stamped, the stamp deliberately beats the environment, and that command is
# therefore a no-op that reports success. Measured: identical output with and
# without it. Stamp a throwaway build instead.
cd agents && go build -ldflags "-X main.dotfilesRoot=/nonexistent-root" -o /tmp/agents-badroot .
/tmp/agents-badroot doctor    # must report the bad root, not merely disagree about hooks
agents doctor                 # the real binary stays green
```

**Demonstrated to fail by:** the throwaway build above. Its predecessor in this
spec — setting `AGENTS_DOTFILES_ROOT` — was a check that could not fail, in the
phase whose entire subject is a check that could not fail.

---

## Explicitly out of scope

To [spec 6](2026-08-11-spec-6-releases-and-distribution.md): releases, version
and release triggers, checksums, signing, attestations, SBOMs, the Homebrew tap
and formula, bottles, the shim's download-and-verify path, what a distributed
binary stamps as its checkout root, `agents version`, upgrade and rollback, and
migrating spec 7's machine-local store schema across binary versions.

Not in either spec: Windows; self-hosted runners; a scheduled job against the
latest Go toolchain; changing what `agents doctor` checks beyond phase 6.

## Open questions

Three, all narrow, all resolved by running the thing rather than by discussion:

- ~~Does `./bootstrap plan dotfiles` exit `0` in an unprivileged container?~~
  **Answered 2026-08-17: yes.** `plan`, `apply` and `check` all exit 0 in
  `debian:stable-slim`. Spec 2's Known gap 2 does not reach this profile —
  `plan` reports the missing links as the state it intends to change. That gap
  is now scoped to `workstation` in spec 2.
- ~~Does plain `-Syu` suffice on a native x86_64 runner, without the
  `--disable-sandbox` the emulated verification needed?~~ **The question was
  built on a wrong diagnosis** (see the correction above): the sandbox failure
  is Landlock under Docker, not seccomp under emulation, and it reproduces on
  native arm64. The answerable form is *does the runner's container permit
  Landlock*, and the job does not depend on the answer, because it configures
  `DisableSandbox` in the container either way.
- Which `bootstrap` paths return `2` where they mean `5`, and does `exitAdvisory`
  have a path that should return it, or should it be deleted?

## Rejected alternatives

**Path filters with an enumerated exception list** — enumerate the non-module
files the suites read and filter on the union. Rejected: the list is
hand-maintained and its staleness is invisible, which is the defect this spec
exists to gate. A filter that is wrong fails exactly where the build cache also
fails.

**Gate first, fixes after** — land the whole workflow with known-red jobs on
`continue-on-error`, removing the flag as each fix arrives. Rejected: a gate
permitted to be red teaches everyone that red is normal, which is the condition
the deleted `go.pre-commit` died in. Faster signal is not worth establishing that
precedent as this repository's first experience of CI.

**Fixes first, gate last** — reconcile everything locally, then add one workflow
that arrives green. Rejected: it leaves the repository unguarded for the whole
duration.

> **Corrected 2026-08-17.** This rejection originally carried a second reason —
> that local Docker could not stand in for the runner, since `archlinux` has no
> arm64 manifest and pacman needed an emulation-only flag. Both halves were
> wrong (see the correction in the Arch section), and the leg has been removed
> rather than quietly left standing. The rejection holds on the first reason
> alone, which is the one that decided it.
>
> The execution bore this out in the opposite direction to the deleted claim:
> local Docker caught the Arch defects, and CI caught a defect local Docker
> could not — a linked worktree's `.git` is a pointer file, so a `git config
> --global` in the container silently did nothing and the `dotfiles` profile
> appeared to converge when part of the setup had not run. Neither substitutes
> for the other; the argument for the gate is that they fail differently.

**Extending `agents index` to write the README block** — tempting, because
`index` already owns generated content and the `generated-file` guard rule would
cover the README for free. Rejected: `agents save` commits `.agents/` paths and
nothing else, and the `mixed-commit` guard exists to keep repository content and
agent context in separate commits. Teaching `index` to write a tracked file
outside `.agents/` puts those two mechanisms in tension for a convenience. The
renderer emits markdown on stdout; CI is the enforcement point.

**Adding `NoRecord = 5` to `bootstrap` to match `agents`** — rejected. Neither
binary nor spec 1 is the authority on whether a code is needed; a path that means
it is. Adding one without such a path produces exactly what
`bootstrap.d/main.go:29` already is: a constant nobody returns, describing a
behaviour the binary does not have, with no way for a reader to tell it from one
that works.

**A CI reachability check** — assert that a command's declared invoker really
invokes it. Rejected: the instance that motivated it was `guard`, and `guard` is
wired (see the note at the top). What remains is one historical hook and one dead
constant, which does not justify a job.

**Suites only, no container jobs** — run `go test` on `ubuntu-latest` and call
Linux covered. Rejected: it proves the Go code is portable and touches none of
the stage-zero package selection, the linuxbrew prefix, or `chsh`. Spec 2's Known
gap would stay open while appearing closed, which is worse than leaving it
visibly open.
