# Spec 5 — the verification gate

**Date:** 2026-08-10 (scope, as "CI, releases, and binary distribution") /
2026-08-11 (design, narrowed to verification) / 2026-08-13 (measured against
master) / 2026-08-14 (every verification command executed)
**Status:** in force. The gate is `.github/workflows/verify.yml`, which cites
this document; what follows is what the gate is and the decisions that bind it,
not the order it was built in. The construction phases, the pre-gate baseline,
the deleted check and the rejected alternatives are gone — the first three are
in that file's history, and the rejections are not needed to use the gate.
**Depends on:** the two-module layout and the exit-code table as built, and the
security boundaries CI must not cross.

**Spec 1**, cited below for the placement rule, the exit-code table and the
fail-open boundary, is the repo-tracked context design: deleted 2026-10-12 with
the system it described. Where this document cites it, the citation is to a
design no longer in the tree; its content is in git history and its plan is in
the archive.
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
> exactly as spec 1's plan
> Task 17 said it would be. **Anyone auditing this must read `runGitHook` to its
> end; `internal/githook` alone shows the opposite.**

---

## Amendment 1 — 2026-09-20

**The `test` matrix is no longer a cross product.** It is four deliberate legs:
`agents` on `{ubuntu-latest, ubuntu-24.04-arm, macos-latest}`, and `bootstrap.d`
on `{ubuntu-latest}`. A fifth job, `macos-dotfiles`, runs the real program on
macOS.

Why the matrix changed: macOS was 27.7% of the job-minutes in the window that
built and released v0.6.0 (59.3 of 213.6), and one of its two legs bought
coverage that Linux already had in another form. `bootstrap.d`'s darwin
behaviour is parameterised rather than `runtime.GOOS`-derived —
`loginShell(..., "darwin", ...)` and `packagesCtx("darwin", "brew")` take the
platform as an argument, so those branches already run on Linux. That suite also
never executed the macOS commands even on macOS: `brew`, `chsh` and `dscl` are
recorded into a fake planner and asserted, not run. What the dropped leg did do
for real was run the suite's integration tests, which execute the compiled
`./bootstrap` in a temporary HOME.

That part is replaced rather than dropped, by a job that tests more.
**`macos-dotfiles` runs the real `./bootstrap plan dotfiles` on macOS.** The
dropped leg never ran the program against a real machine profile; this does, and
it plans rows no other job reaches — the `macOS/` subtree of `links.manifest`
(`~/.config/ghostty`, `~/.config/alacritty/alacritty.toml`) is planned only when
the platform resolves to `darwin`. Measured on its first run (`35535717117`): 24
seconds against 103 for the leg it replaces, exiting 0 with `preflight` reporting
`platform darwin` and `needs neither sudo nor network`. `plan` writes nothing and
the `dotfiles` profile is preflight + config + verify, so it is safe on a
disposable runner.

**What is still not covered on macOS:** the `packages` and `fish` phases. No job
runs `apply workstation` on macOS, which is what would install Homebrew formulae,
change the login shell with `chsh`, and read it back with `dscl`. Linux covers
the equivalent code path for real against Linuxbrew in `linux-stage-zero`; the
macOS half of it is exercised on a real machine, or not at all. That gap is older
than this amendment and is not closed by it.

In exchange for the dropped leg, `ubuntu-24.04-arm` also runs `agents` on arm64
Linux. `linux/arm64` is a shipped release target (spec 6 §4.1) that no leg had
ever run on; the `macos-latest` leg only approximated arm64 through a different
operating system, and it stays. macOS remains for `agents` because host behaviour
is the entire point of that leg: it is what caught the git background-maintenance
race in the tree snapshot (run `35470984407`, fixed in `d05a43e`), which no Linux
leg surfaced. The 2026-08-29 latency work targeted `test (macos-latest,
bootstrap.d)` at "< 45s"; that job no longer exists. Those measurements are the
record of that day and are not rewritten.

**Adding a leg is not only a matrix edit.** The gitleaks install in the `test`
job is checksum-pinned per runner and fails closed on an unlisted one
(`unhandled runner os`), so a new runner needs its asset and digest pinned in the
same change: `ubuntu-24.04-arm` takes `gitleaks_8.30.1_linux_arm64.tar.gz` at
`e4a487ee…`. All four pinned digests are the ones gitleaks publishes in
`gitleaks_8.30.1_checksums.txt`.

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

## The organizing claim

**One artifact per fact; everything else computed from it.**

The recurring defect is a hand-maintained description of the code that nothing
forces to match the code. Four instances, all above: `usage()` against the
dispatch switch, the exit-code table against itself, `INDEX.md` against its
frontmatter, and a README with no command reference at all — nothing stale
because nothing is there, and nothing discoverable either.

---

## Decisions that bind the gate

Recorded once here rather than repeated per phase.

### No path filters, anywhere in the workflow

**Triggers:** `pull_request`, `push` to `master`, `workflow_dispatch`. No `paths`
or `paths-ignore`.

The scope note asked for filters "without allowing a docs-only change to
masquerade as source verification." The filter is the hazard, and it is the same
hazard as the Go build cache in different clothing. The obvious filter is
`paths: ['agents/**', 'bootstrap.d/**']` — which skips verification for a change
to `Makefile` or `git/gitconfig.shared`. The `Makefile` half went with the file on
2026-10-09, and took `bootstrap.d/makefile_test.go` with it;
`agents/internal/doctor/doctor_test.go` still reads `git/gitconfig.shared`. Those
are exactly the edits that break tests, and exactly the edits a cached `go test`
already fails to notice.

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
state is visible rather than assumed" (spec 1 §9), and a reading that is successful but not a clean pass still returns `1`.

**`agents/main.go:51-58` is where the two vocabularies meet, and it already
translates correctly** — `Advisory` becomes `OK`, only `Block` stops the commit.
`internal/githook` speaks git's raw `0`/`1` and does not import `exitcode`, which
is right for a low-level dispatcher. `cmd_hook.go:30` returns literal `0` always,
per spec 1 §6's fail-open rule.

So **CI's default "non-zero fails the step" is the correct disposition reading**,
and needs no wrapping. Verified on this provisioned machine, 2026-08-14:
`./bootstrap plan dotfiles` and `./bootstrap check dotfiles` both exit `0`. Whether `plan dotfiles` still exits `0` inside a bare container is unverified,
and the answer does not change this reading — it changes what the container
jobs must cover.

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
| `test` (matrix) | `agents` on `{ubuntu-latest, ubuntu-24.04-arm, macos-latest}`; `bootstrap.d` on `{ubuntu-latest}` | phase 1 | **amended 2026-09-20** (Amendment 1) |
| `secrets` | `ubuntu-latest` | phase 1 | — |
| `gate` | `ubuntu-latest` | phase 1 | every later phase adds to `needs:` |
| `hygiene` | `ubuntu-latest` | phase 2 | **amended 2026-09-24** — three legs: containment for `agents`, containment for `bootstrap.d`, and bootstrap.d under a restrictive umask. The job id and the gate's `needs:` are unchanged; the two properties stopped sharing a job because it cost their sum (75s) instead of their longest (39s) |
| `docs` | `ubuntu-latest` | **phase 3** (help coverage) | **phase 4** (README block, backward check, skill coverage) |
| `linux-dotfiles` | container | phase 5 | — |
| `macos-dotfiles` | `macos-latest` | **amended 2026-09-20** (Amendment 1) | — |
| `linux-stage-zero` (matrix) | `{debian, arch}` containers | phase 5 | — |

**Four `test` jobs, not one.** Spec 2 §11 chose two modules specifically so a
`bootstrap` failure can never block an `agents` release. Collapsing them hands
that property back for a small saving in configuration.

**Branch protection names `gate` and nothing else.** Adding a job later is then a
workflow change rather than a repository-settings change, and the set of things
that must pass stays visible in the tracked file.

---

## The hygiene job

### Containment: the suites must not write to `$HOME`

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
  repos. The since-deleted `agents ls` reported 40 of them missing.

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

## The command registry

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
collapses descriptions the baseline carried separately. A test pins the constants
against the table written inside `agents/exitcode_doc_test.go`
(`TestExitCodeTableMatchesSpecOne`): spec 1 §6 defines the vocabulary, the code is
the implementation, and when they disagree the test surfaces it while a human
decides which was wrong.

**`bootstrap` and the exit-code vocabulary it shares with `agents`.** `exitAdvisory`
needed a producer, not a deletion: `bootstrap check` returns `1` when checks warn
and none fails, but the `1` was produced inside `bootstrap.d/internal/check` as a
bare literal while the name stayed in `bootstrap.d/main.go`, so a grep of
`bootstrap.d/main.go` reported the behaviour as absent when it had always been
there. `runCheck` (`bootstrap.d/main.go:300`) now maps each number to its name in
one place, with a default that fails closed to `exitBlock`.

`TestEveryDeclaredExitCodeHasAProducer` (`bootstrap.d/main_internal_test.go`) fails
if any declared code is returned by no path, and that is the whole of it: it counts
identifier uses, so it does not pin the mapping itself, and swapping the advisory
arm's return value leaves every package green. `bootstrap` declares five codes and
stops at `4` — `exitOK` 0, `exitAdvisory` 1, `exitBlock` 2, `exitMalformed` 3,
`exitNotApplicable` 4 — and takes no `5`.

**Verified by:**

```bash
agents help                    # exit 0, on stdout, rendered from the tree
agents help githook  # exit 0; non-empty usage and detail for a leaf
agents --help                  # same text as `agents help`, exit 0 (today: exit 3)
agents                         # same text, on stderr, still exit 3
agents nosuchcommand           # exit 3
cd agents && go test -count=1 ./...
```

and by the coverage check in the `docs` job: walk the tree, fail on any node
with an empty `Summary`, `Usage`, or `Detail`.

**`agents` with no arguments keeps exiting `3` on stderr** while `agents help`
exits `0` on stdout. Same rendered text, different disposition: a bare
invocation is a usage error and an explicit `help` request is not. Only the text
is shared, and only the text is what derivation is for.


**Demonstrated to fail by:** blanking one leaf's `Detail` and observing the
coverage check red; changing a constant's comment without changing the test's own table
and observing the pinning test red.

---

## Documentation derived from the registry

**Forward** — the full command surface renders as markdown into a marked block in
`README.md`; CI regenerates and diffs it. The prose around the block stays
hand-written, because *when* to reach for a command is judgment and cannot be
generated.

**Backward** — no living document may name an `agents` subcommand the registry
does not define. The check scans inline code spans and fenced blocks for
`agents <subcommand>` and requires each path to resolve. This catches doc rot in
the direction that hurts: a document confidently naming a command that no longer
exists.

**The backward check is two tests in `agents/docs_test.go` over the same documents,
with different scopes.** `TestLivingDocumentsNameOnlyRealCommands` fails when an
`agents` subcommand named in a living document is not in the registry, which is how
a rename or a typo in a skill, a README or an instruction file gets caught. It reads
`README.md`, `agents/README.md`, `git/README.md`, the project `CLAUDE.md`, the
tracked global `global/AGENTS.md`, and every `.md` under `.agents/skills/`.

`TestLivingDocumentsNameNoDeletedCommand` covers a wider list — those roots **plus**
`docs/qna/` and `docs/design/`, the two stores the root `AGENTS.md` sends an agent to
before it asserts — and fails when a document names one of the verbs the tool has
deleted, unless the same line says the command is gone. Plans and specifications are
outside the first test because they are dated records: an executed plan legitimately
names a target that no longer exists, and a record silently rewritten to stay true is
not a record. `agents/docs_test.go` states that rule in the same words, and names the
`make githooks` it protects.

Where the guard walks a directory, a missing root is an error: a `claude/skills/`
that had been deleted went on being walked, and reporting nothing, for eighteen
days. The listed files are not held to that — a missing one is skipped, so deleting
`git/README.md` or `CLAUDE.md` would narrow the guard in silence.

**Harness guidance lived in `claude/skills/`** — not in either `CLAUDE.md`, and
not in `.agents/skills/`. `bootstrap.d/links.manifest` used to symlink
`claude/skills` → `~/.claude/skills` on every provisioned machine, so
`claude/skills/` was fleet-wide while `.agents/skills/` is per-repository
procedure. `agents` was a fleet-wide tool: the
deleted `agents ls` listed registered repositories, and the
deleted `agents update --all` rewired every one of them — so `.agents/skills/`
would give the guidance to the one repository that
needs it least. *(The fleet machinery is deleted 2026-09-21; `agents` is now a
per-repository tool with no fleet scope, so the argument now rests only on which
tier the guidance belongs to. `claude/` went with it, and the manifest's three
global rows are `:22-24` today.)*

The two artifacts answer different questions and only one can be generated. The
rendered block answers *what is this command*; the skill answers *which command
is this situation* — judgment, which is why it cannot be generated. The
`Agent`-audience coverage check is what keeps the second honest as the first
grows: without it a new command lands, gets a generated reference entry, and no
agent ever reaches for it — the shape spec 7 measured when `CLAUDE.md` said *how*
to write a handoff and never *that* one should, and twenty sessions produced none.

Neither `CLAUDE.md` becomes a manual. The project one declares itself "only the
pointer" to `.agents/`; the global one is kept to rules a session acts on.

**Verified by:**

```bash
agents help --render=markdown > /tmp/surface.md
# regenerate the marked block in README.md from /tmp/surface.md, then:
git diff --exit-code README.md
cd agents && go test -count=1 -run 'TestLivingDocumentsNameOnlyRealCommands' .
```

*(The test named here was `TestDocumentedCommandsExist` when this phase was
written. It is now `TestLivingDocumentsNameOnlyRealCommands`, which covers the
same invariant plus the instruction files and both skill trees; the README block
itself is guarded by `TestReadmeCommandBlockIsCurrent`.)*

**Demonstrated to fail by:** adding a command to the registry without
regenerating the README block; and naming `agents nosuchverb` in a code span in
`CLAUDE.md`.

---

## Linux

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

> **Amended 2026-10-09 — those four probes cannot see the failure provisioning is
> about to start having.** `linux-stage-zero` records `apply`'s exit code without
> asserting it and gates on `grep -q "stage zero"`, and the packages phase prints
> that line *before* `brew bundle` runs. So a `brew bundle` that installs nothing
> leaves the job green, and the failure surfaces afterwards, in devtools, where it
> is recorded rather than asserted too. That was survivable while devtools built
> `agents` from the checkout. It stops being survivable when provisioning consumes
> the tap, because `agents` then arrives only from `nilbot/tap/agents` and nothing
> in the four probes names it. **The probe list gains `command -v agents`**, which
> is what turns "the tap formula did not install" into something the job reports
> rather than something a reader infers from a later phase's silence. **Landed
> 2026-10-09**: the job's step is now "stage zero delivered its four prerequisites,
> and the Brewfile its agents", with `PATH="/home/linuxbrew/.linuxbrew/bin:$PATH"
> command -v agents` — the prefix spelled out because a `run:` step is not a login
> shell and Homebrew's `shellenv` has not been read, which is the same defect
> `resolveBrew` exists for. See
> the 2026-10-09 decision that nothing builds `agents` from a checkout.

**Demonstrated to fail by:** the Arch job is red before the repair and green
after — the one phase whose failure has already been observed.

---

## Explicitly out of scope

To [spec 6](2026-08-11-spec-6-releases-and-distribution.md): releases, version
and release triggers, checksums, signing, attestations, SBOMs, the Homebrew tap
and formula, bottles, the shim's download-and-verify path, what a distributed
binary stamps as its checkout root, `agents version`, upgrade and rollback, and
migrating spec 7's machine-local store schema across binary versions.

Not in either spec: Windows; self-hosted runners; a scheduled job against the
latest Go toolchain; changing what `agents doctor` checks beyond phase 6.
