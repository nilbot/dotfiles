# Spec 5 — the verification gate

**Date:** 2026-08-10 (scope, as "CI, releases, and binary distribution") /
2026-08-11 (design, narrowed to verification)
**Status:** designed — not implemented
**Depends on:** [spec 1](2026-08-07-agents-repo-context-design.md) for the Go
module, the exit-code table, and the security boundaries CI must not cross.
**Carries obligations from:** [spec 2](2026-08-07-spec-2-dotfiles-hygiene.md),
implemented. Its largest known gap — no phase of it has ever run on Linux — is
closed here or nowhere.
**Split from:** everything about releases, versioning, checksums, Homebrew, and
what a distributed binary stamps now lives in
[spec 6](2026-08-11-spec-6-releases-and-distribution.md), which is scope only.

> **Note added 2026-08-12 from
> [spec 7](2026-08-12-spec-7-capture-and-review.md).** Neither organizing claim
> changes, and both get easier: with the trace index untracked, CI never sees
> machine-local paths in the tracked tree at all.
>
> One scheduling collision. Spec 7 adds `agents review` and
> `agents handoff draft`, which land in **§6**'s command registry and its derived
> help text. Either this spec's registry ships first and spec 7's commands are
> born with help text, or they arrive without it and this spec inherits them.
> A sequencing decision, not a conflict.

---

## What changed from the scope note

The 2026-08-10 scope note was four specs under one number: pull-request
verification, release construction, publishing and installation, and version
drift. Only the first needed no open decisions. The other three were gated on ten
open questions, which is why the document sat at "scope only" — no single
implementation plan could be written against it.

**Spec 5 is now the verification gate and nothing else.** The deferred material
was moved to spec 6 rather than deleted, and spec 6 depends on this one: a
release pipeline built on top of a repository with no automated gate would be
publishing unverified binaries on a schedule.

Two things the scope note recorded as open are answered here because the
evidence it already contained settles them, and one because it was measured
during this design:

- Linux verification was listed as an open choice while the same document
  recorded the two constraints that decide it. See [§7](#7-linux).
- The exit-code table was described as a settled five-code vocabulary shared by
  both binaries. It is not. See [§4](#4-exit-codes-are-a-per-invocation-contract).
- `usage()` in `agents` is a hand-maintained string literal with no link to the
  dispatch switch beside it, and there is no `help` command at any level. See
  [§6](#6-the-command-registry-and-the-documentation-derived-from-it).

## Why it exists

Spec 2 deleted `git/hooks/go.pre-commit` in `c0e3bb1`, with a commit message
ending "This is CI's job. See spec 5." Since that commit, **this repository has
had no automated Go gate anywhere.** Every test run, every `go vet`, every
`gofmt` check has been a human remembering.

The hook is not what is being restored. Recovered with
`git show ac5286a:git/hooks/go.pre-commit`, what it actually did was:

- Ran only where `.go` files sat **at the repository root**
  (`ls | grep '\.go$'`), which is neither of this repository's modules.
- Ran `go build -n`, `go test`, `go fmt`, `go vet` — each without `./...`, so
  each covered exactly one package.
- Redirected every command to `>/dev/null 2>&1` and printed a fixed string on
  failure, so a failure carried **no diagnostic at all**.
- Let `go fmt` rewrite files mid-commit without staging them, so a formatting
  fix silently failed to be committed.
- Was never installed by anything. `git/install-hooks.sh` links four fixed
  names — `pre-commit`, `commit-msg`, `post-merge`, `post-checkout` — and never
  names this file. It had been inert for its entire life.

So this is not a port. It is the first build, test, `gofmt` and `vet` gate this
repository has ever actually run.

## Current baseline

Re-measured on 2026-08-13, on this machine, after spec 7's phases A and B′
landed. Figures from the 2026-08-11 draft that moved are marked.

- No `.github/` directory. No workflows, no PR checks, no releases.
- Two Go modules — `agents/` and `bootstrap.d/` — and deliberately **no
  `go.work`** (spec 2 §11). Two independent build graphs.
- 107 Go files, 54 of them containing tests *(was 98 and 50 on 08-11; spec 7 is
  still adding)*. The suites are fast.
- Go 1.26.5 `darwin/arm64`; `go.mod` says `go 1.26`, which floats.
- gitleaks 8.30.1 on `PATH`, run by hand.
- **Spec 1 §6 defines six shared exit codes** and calls them "identical across
  every subcommand." `agents/internal/exitcode/exitcode.go` implements all six.
  `bootstrap.d/main.go:28` implements five — it has no `5`. See
  [§4](#4-exit-codes-are-a-per-invocation-contract) for the three-way wording
  drift on top of that.
- `agents/main.go:61` dispatches through a `switch`; `agents/main.go:98` is a
  hand-maintained `usage()` literal *(lines moved from 60 and 94)*. Nothing
  connects them. There is no `help` command and no `--help` at any depth;
  `agents --help` reports `unknown command` and exits `3`.
- Four files read the real `$HOME`: `agents/root.go`,
  `agents/internal/doctor/doctor.go`, `agents/internal/machine/machine.go`,
  `bootstrap.d/main.go`. Unchanged since 08-11.
- `agents ls` reports a fleet of one.
- `agents doctor` on this healthy machine exits advisory, over
  `recording:codex` and `pointers:unverified` — both machine-local. *(The
  08-11 draft cited `pointers:local-unreachable`, which spec 7 removed. The
  citation changed; the argument it supports did not, which is the point.)*

## The organizing claim

Every check in this spec asserts one of exactly two properties:

1. **Derived artifacts match their source.** The generated indexes, the help
   text, the exit-code table, the README command reference.
2. **The tools are a function of the repository, not of the machine.** The
   suites under a temp `HOME`; `agents index` as a pure function of tracked
   content.

These are the same defect wearing two coats: *a hand-maintained description of
the code that nothing forces to match the code*. `usage()`, the exit-code table,
a stale `INDEX.md`, and a README naming a command that no longer exists are four
instances. One idea gates all of them.

---

## Design

### 1. Triggers, and why there are no path filters

**Triggers:** `pull_request`, `push` to `master`, `workflow_dispatch`. No `paths`
or `paths-ignore` filter anywhere in the workflow.

The scope note asked for filters "without allowing a docs-only change to
masquerade as source verification." Working it through, the filter is the hazard
rather than the safeguard — and it is the *same* hazard as the Go build cache,
in different clothing.

The obvious filter is `paths: ['agents/**', 'bootstrap.d/**']`. That skips
verification for a change to `Makefile` or `git/gitconfig.shared`. But
`bootstrap.d/makefile_test.go` reads the `Makefile`, and
`agents/internal/doctor/doctor_test.go` reads `git/gitconfig.shared`. Those are
precisely the files whose edits break tests, and precisely the edits a cached
`go test` already fails to notice. A path filter would install a second,
independent route to the same silent green.

**The rule this spec states: no path filter may exclude a file that any test
reads.** The set of such files is not the set under the module directories,
nothing keeps it enumerable, and getting it wrong is invisible. The only filter
that satisfies the rule is none. Runtime is not a counter-argument at this size.

One mechanical trap is recorded so it is not rediscovered: a required check
skipped by a path filter reports "Expected" indefinitely and blocks the pull
request rather than passing it.

### 2. The job graph

| Job | Runner | What it asserts |
|---|---|---|
| `test` (matrix) | `{macos-latest, ubuntu-latest}` × `{agents, bootstrap.d}` | build, `go test -count=1 ./...`, `-race`, `go vet ./...`, `gofmt -l` empty |
| `secrets` | `ubuntu-latest` | gitleaks against the embedded configuration |
| `hygiene` | `ubuntu-latest` | §5 — temp `HOME`, index freshness |
| `docs` | `ubuntu-latest` | §6 — help coverage, README block, backward name check |
| `linux-dotfiles` | container | §7 — the `dotfiles` profile |
| `linux-stage-zero` (matrix) | `{debian, arch}` containers | §7 — stage zero for real |
| `gate` | `ubuntu-latest` | `needs:` every job above; the single required check |

**Four `test` jobs, not one.** Spec 2 §11 chose two modules specifically so a
`bootstrap` failure can never block an `agents` release. Collapsing them into one
job hands that property back for a small saving in configuration.

**`GOWORK=off` is set in the workflow environment.** Spec 2's rejected
alternatives turned down a tracked `go.work` because it alters module resolution
and would hand the release path a `GOWORK=off` footgun. Setting it explicitly
costs one line and means a stray workspace file — a runner's, a contributor's —
can never change what CI resolves.

**`-count=1` is load-bearing, not stylistic.** Several tests here assert against
tracked **non-Go** files, whose edits do not invalidate a cached pass.
`agents/internal/doctor/doctor_test.go:378` records it in the tree:

> Reading a tracked file means the Go build cache does not know when the answer
> changes: run this module's tests with `-count=1` after any rename.

This was watched happening. When spec 2 deleted the Makefile's `githooks`
target, two tests in `agents` began failing while `go test ./...` in that module
reported `ok (cached)`. Only `-count=1` revealed it. The breakage left an
artifact when it was fixed in `06ff3c9`; the stale pass that hid it left none,
which is exactly why the requirement belongs in a gate rather than in a
contributor's memory.

**One required check.** Branch protection names `gate` and nothing else. Adding a
job later is then a workflow change rather than a repository-settings change, and
the set of things that must pass stays visible in the tracked file.

### 3. Toolchain pinning

Go is pinned to the exact patch — `1.26.5` — not to `go-version-file`, because
`go 1.26` in `go.mod` floats to whatever the runner happens to ship. gitleaks is
pinned to `8.30.1` and its download is checksum-verified.

Bumping either is a deliberate pull request whose diff says what changed. A
floating toolchain turns an unrelated upstream release into an unexplained red
gate on someone else's branch.

Testing against the *latest* Go to catch toolchain drift early is a reasonable
thing to want and is **out of scope**: it belongs in a scheduled non-required
job, and adding one now would mean the first thing this repository learns about
required checks is that some of them are allowed to be red.

### 4. Exit codes are a per-invocation contract

The scope note asked CI to "decide which codes fail a job, which annotate, and
which are skips." That question is malformed at the global level, because **the
meaning of a code depends on who is asking.**

For a developer at a terminal, `1` means "look at this." A green CI run has no
reader, so in CI an advisory either fails the build or vanishes; there is no
third behaviour. And `4` inverts outright: for a git hook in an unrelated
repository, "not applicable" is correct and benign, but on a runner we *know*
there is a git checkout and a `.agents/` directory, so a `4` from
`agents guard --staged` means CI invoked it somewhere it does not apply — a bug,
reported as a pass.

`agents doctor` makes the failure of a global table concrete. It exits advisory
on this healthy machine over `recording:codex` and `pointers:unverified`, both
machine-local and both meaningless on a runner. "Advisory fails" makes the gate
permanently red for reasons no pull request can fix; "advisory passes" means
doctor can never fail CI for anything.

Spec 7 then produced the case that settles it. **`agents review --stats` returns
`1` for several readings that are entirely successful** — there, `1` means
"measured, and the reading is not a clean pass", which is not an error at all.
One binary now uses code `1` for *a finding about the repository*, *a warning
about this machine*, and *a successful measurement whose value is low*. No global
verdict can be correct for all three, and no amount of care in choosing one will
make it correct later.

**So every CI call site declares the exit code it expects, and the workflow
asserts equality.**

```
agents guard --staged          → expect 0    (a 4 here means CI ran it wrong)
agents index && git diff       → expect 0    (the diff is the real assertion)
./bootstrap plan  dotfiles     → expect 0    (a 2 is a finding — see §7)
./bootstrap apply dotfiles     → expect 0
./bootstrap check dotfiles     → expect 0    (convergence after apply)
```

This is stronger than "non-zero fails" in one specific way: it catches a tool
that exits `0` where it should have exited `2` — a refusal path that quietly
stopped refusing. "Non-zero fails" cannot catch that at all, and it is the defect
class PR #16 named as *a guard that cannot fail is worse than no guard, because
it reports success*. Each declared code doubles as a live test of the exit-code
contract.

**`agents doctor` does not gate CI.** Its machine-local checks are unassertable
on a runner, and `doctor_test.go` already covers its logic. Gating on it would
mean choosing between a red gate and a toothless one.

**The reconciliation, against spec 1 — not against either binary.** The
08-11 draft of this spec proposed documenting the divergence as a legitimate
asymmetry: `0`–`4` universal, `5` `agents`-only. **That was wrong.** Spec 1 §6
defines all six codes and calls them "identical across every subcommand." `5` is
not an `agents` extension; `bootstrap` is missing a code the foundation defines.
Writing the asymmetry down as truth would have canonised a drift as a decision —
the exact failure this spec exists to prevent, committed by the spec itself.

Because the divergence is wider than two help texts. One table is described in
**three** places, and all three differ:

| Source | code 4 | code 5 |
|---|---|---|
| Spec 1 §6 (canonical) | not applicable / skip | could not record |
| `exitcode.go` constant comments | not applicable here | could not complete the requested operation |
| `agents` help text | skip | could not complete the operation |
| `bootstrap` help text | not applicable | *absent* |

So the work is: **help text renders from the constants** rather than restating
them — which is [§6](#6-the-command-registry-and-the-documentation-derived-from-it)'s
registry doing its job, not a separate mechanism — and a test pins the constants
against spec 1's table. Prose in a spec cannot be generated from code, so that
test is the only thing that can hold the two together.

`bootstrap` is then audited for paths returning `2` (refused) that mean `5` — a
refusal asserts *this machine is in a state I will not write over*, which is a
different claim from *I tried and could not*. Unlike the 08-11 draft, the audit
no longer decides **whether** `bootstrap` gets the code; spec 1 already did. It
decides which existing paths were mislabelled on the way.

The pinning test reads tracked files from outside its own module, which
`bootstrap.d/makefile_test.go` already establishes as this repository's pattern,
and which is one more reason the gate runs `-count=1`.

### 5. The hygiene job — tools as a function of the repository

Two checks that belong together because they assert the same property.

#### The incident that arrived after this section was written

On 2026-08-13, `a26eaa9` fixed exactly the failure this job exists to catch, and
it is recorded here because a hypothetical became a measurement.

`TestInitDoesNotPointAtTheRetiredTrackedTracePath` called `runInit` with no
`t.Chdir`, so **it wired this repository with the ephemeral `agents.test`
binary path** — four times per `go test` run, erroring at session start. The
damage compounded: `stripOurs` only deleted commands whose basename was exactly
`agents`, so the real hook was stripped, the test one added, and nothing could
ever remove it.

**And `agents doctor` reported the wiring exact throughout.** A test escaped
into the machine, corrupted the machine's configuration, and the diagnostic
designed to notice said everything was fine.

`TestMain` now chdirs out of the checkout before any test runs, which closes the
**cwd** escape route. That is a real fix and this spec does not duplicate it.
**It leaves the sibling route open:** a test that reads `$HOME` rather than the
working directory reaches the machine exactly as before, and four files still
read the real `$HOME`. The hygiene job is what closes that one.

One constraint the fix introduced, which matters to anyone writing the tests
this spec asks for: three call sites resolving repository paths from cwd —
`task18RepoRoot` and two `go build` invocations — now use `packageDir` captured
before the chdir. A new test that reads a tracked fixture must do the same.

#### The checks

**The suites under a synthetic environment.** Both modules run with `HOME` and
`XDG_CACHE_HOME` pointed at temporary directories, and `bootstrap.d`
additionally under `umask 077` — the conditions PR #16 verified by hand and
which nothing currently keeps verified. The phase measures what breaks before
deciding what to fix, and this spec does not guess the number.

**`agents index` followed by `git diff --exit-code`.** The `generated-file`
guard rule already blocks a commit that leaves an index stale, but it is enforced
only by a git hook someone has to have installed. A contributor without hooks
lands a stale `INDEX.md` and nothing notices. CI is the enforcement that cannot
be skipped.

This check earns more than freshness: it **asserts that `agents index` is a pure
function of tracked content.** If it ever embeds anything machine-local, this is
the check that says so.

### 6. The command registry, and the documentation derived from it

**This section owns the `cli-help-unification` lane**, whose pending draft
handoff was written on 2026-08-13 for whoever picked this up.

`agents/main.go` dispatches through a `switch` and describes itself through a
separate string literal. Nothing connects them, so adding a command and
forgetting to document it is a silent, ordinary mistake. There is no `help`
command, and `trace cache prune` — a destructive verb — has no help at all.

Spec 7 demonstrated the cost while this spec sat unimplemented. Four usage lines
changed in one workstream — `handoff write|draft|prune` gained a verb, and
`review [--keep|--bin <id>]`, `trace cache prune --retention` and
`trace migrate [--yes]` are new — each one a hand-edit to a literal that
happened to be remembered. Doctor's check set changed in the same period without
any usage line to update, and `wiring:*` gained a `Warn` result it never had.
**The registry is worth landing early for this reason: it is the mechanism that
makes the next workstream's commands document themselves, and there is a next
workstream.**

**A declared command tree replaces both.** Each node carries name, one-line
summary, usage, detail, flags, handler, and subcommands. Dispatch and help walk
the same structure, which makes an undocumented command *structurally*
impossible rather than merely tested. `agents help`, `agents help trace`,
`agents help trace cache prune`, and `--help` at any depth all render from it.
An unknown command still exits `3`.

A coverage check walks the tree and fails on any node with an empty summary,
usage, or detail. It is a belt over the structural braces, and it is what makes
"add a command" fail loudly if the tree is bypassed.

**Documentation is then checked in both directions.**

*Forward* — the full command surface is rendered as markdown into a marked block
in `README.md`, and CI regenerates and diffs it. The prose around the block stays
hand-written, because *when* to reach for a command is judgment and cannot be
generated.

*Backward* — no living document may name an `agents` subcommand the registry
does not define. The check scans inline code spans and fenced blocks for
`agents <subcommand>` and requires each path to resolve. This catches doc rot in
the direction that actually hurts: a document confidently naming a command that
no longer exists.

**The backward check covers `README.md`, both `CLAUDE.md` files — the project
one and the tracked global `claude/CLAUDE.md` — and everything under
`claude/skills/` and `.agents/skills/`. It deliberately excludes
`docs/superpowers/plans/` and `docs/superpowers/specs/`.**
Plans and specs are dated records of what was true when they were written; the
executed bootstrap plan legitimately names `make githooks`, which no longer
exists. Forcing those to match today's command set would destroy the record, and
a record that is silently rewritten to stay true is not a record.

**Harness guidance lives in `claude/skills/`, not in either `CLAUDE.md` and not
in `.agents/skills/`.** The 08-11 draft said `.agents/skills/`; PR #19 shows
that was the wrong shelf.

`bootstrap.d/links.manifest:20-21` symlinks `claude/skills` → `~/.claude/skills`
and `claude/CLAUDE.md` → `~/.claude/CLAUDE.md` on every provisioned machine. So
`claude/skills/` is **fleet-wide**: tracked in this repository, installed
everywhere, loaded in every session in every repository. `.agents/skills/` is
per-repository procedure.

`agents` is a fleet-wide tool — `agents ls` lists registered repositories and
`agents update --all` rewires every one of them. Guidance on when to reach for
it is therefore not specific to this checkout, and putting it in `.agents/skills/`
would mean the one repository that needs it least is the only one that has it.

What that guidance has to carry is judgment rather than syntax: when a finding is
worth `agents handoff draft` rather than a comment, why `agents review` stands
between a draft and the tracked record, when `agents trace show` answers a
question grep cannot, why `agents save` exists separately from `git commit`. The
generated block in §6 gives a harness the command list; this gives it the reason
to use one. Neither `CLAUDE.md` grows into a manual — the project one declares
itself "only the pointer" to `.agents/`, and the global one is kept to rules a
session acts on.

### 7. Linux

Spec 2's [Known gaps](2026-08-07-spec-2-dotfiles-hygiene.md#known-gaps-2026-08-11)
states it without hedging:

> **Linux is untested.** No phase in this design has run on Linux. The stage-zero
> package selection, the Homebrew prefix at `/home/linuxbrew/.linuxbrew`, and `chsh`
> under a different `/etc/shells` convention are all unexercised.

Its risk table records this as **not mitigated**. No Linux machine was available
during implementation and none is expected. CI is the only route, which promotes
a Linux runner from a nice-to-have to the thing that closes spec 2's largest gap.

Three jobs, in increasing order of what they can break.

**`linux-dotfiles`** runs the `dotfiles` profile — preflight, config, verify —
in an unprivileged container. That profile exists precisely so it is
container-safe: no sudo, no network, no package manager, no login-shell change.
`plan`, then `apply`, then `check`, each asserted to exit `0`.

**`linux-stage-zero`**, on `debian` and `archlinux` containers, runs stage zero
for real. Both base images lack `sudo`; it is installed and configured
`NOPASSWD` — not for convenience, but because `Applier.run`
(`bootstrap.d/internal/change/applier.go:335`) sets `cmd.Stdout` and `cmd.Stderr`
and leaves `cmd.Stdin` nil, so any prompt is an immediate "no tty present."
Passwordless is the only shape in which this code can succeed at all.

#### Two named unknowns, with their resolution rules

**Whether `./bootstrap plan dotfiles` exits `0` in a bare container is
unverified.** Spec 2's Known gap 2 says `plan` exits `2` on a machine lacking
Homebrew or fish rather than previewing. If that reaches the `dotfiles` profile,
**it is a finding to fix, not a reason to weaken the job.** A profile designed to
be safe where nothing is installed, which refuses where nothing is installed, is
not doing its job.

**The Arch stage-zero job is expected to fail on its first run, and the fix is
not `-Sy`.** `bootstrap.d/internal/phase/packages.go:76` refreshes the index
before installing on apt, with a comment explaining exactly why: "An install
against an index months out of date does not install an old version, it 404s on
the URL the old index names." The pacman branch at `packages.go:86` goes straight
to `pacman -S --needed --noconfirm`, and the stock `archlinux` image ships an
empty sync database. The asymmetry is unexplained in the code and looks like an
oversight.

The naive repair — adding `-Sy` — is worse than the bug. Arch does not support
partial upgrades: syncing the package database without upgrading installed
packages is a documented way to break a system. The supported form is `-Syu`,
which means stage zero performs a **full system upgrade** in order to install
four packages. That is a real cost and a real decision, and this spec names it
rather than letting the phase discover it while red. The phase decides between
`-Syu` and an explicit precondition that the system is already synced; it does
not ship `-Sy`.

This is the gate paying for itself on its first run, and it is written down in
advance so that the outcome is a confirmation or a surprise — either of which is
informative — rather than a story told afterwards.

### 8. The doctor rider

`make agents` stamps `$(CURDIR)` and writes the single global `~/bin/agents`, so
running it from a linked worktree publishes a binary stamped to a path that will
not survive. Delete the worktree and `githook.go:127` reads the missing extras
directory as "no personal hooks": the chain silently runs none, at exit `0`, and
`AGENTS_DOTFILES_ROOT` cannot rescue it because the stamp deliberately wins.
PR #16 documented this in the Makefile and left the fix for review.

A doctor check — the resolved root exists and contains the expected markers — is
that fix. It **fails** rather than warns, because the consequence is a security
and correctness mechanism silently not running.

**This is labelled a rider, not verification.** It is a spec-1 defect that
belongs to doctor, it needs no CI, and it is here because it would otherwise wait
on a release pipeline it has nothing to do with. It is sequenced last so that
nothing else depends on it.

---

## Phasing

Each phase lands a check **together with the fix that makes it green**, so the
branch is mergeable at every boundary. This ordering was chosen over the two
alternatives in [Rejected alternatives](#rejected-alternatives), both of which
contain a stretch where the work is not trustworthy.

| # | Phase | Ends with |
|---|---|---|
| 1 | Workflow skeleton: `test` matrix, `secrets`, `gate` | The first automated Go gate this repository has run |
| 2 | Hygiene: temp `HOME` and `XDG_CACHE_HOME`, index freshness | Both suites proven independent of the machine |
| 3 | Command registry, `agents help`, coverage check | Every command reachable and documented by construction |
| 4 | Exit-code audit and reconciliation against spec 1 | One vocabulary, rendered from the constants, pinned by a test |
| 5 | Documentation: generated README block, backward name check, `claude/skills/` harness guidance | Docs that cannot drift from the command set |
| 6 | Linux: `dotfiles` profile, then Debian, then Arch | Spec 2's largest known gap closed or precisely narrowed |
| 7 | Doctor stamped-root check | The worktree hazard PR #16 documented is no longer silent |

Phase 1 is expected green on arrival. Phases 2, 3, 4 and 6 each expect red
first — that is the point of pairing each with its fix.

**Phases 3 and 4 were swapped on 2026-08-13.** The 08-11 draft reconciled exit
codes first. Once the help text renders from the constants rather than restating
them ([§4](#4-exit-codes-are-a-per-invocation-contract)), that work *is* a
registry feature and cannot precede the registry. The swap also answers the
pressure spec 7 exposed: four usage lines changed by hand in one workstream, and
another workstream is already open, so every phase the registry waits is more
drift to reconcile when it lands.

## The testing rule

**Every check added by this spec must be demonstrated to fail.** Break the thing
it guards, observe red, restore. A check whose failure has never been seen is
indistinguishable from a check that cannot fail.

This is not a general preference; it is this repository's own finding. Its memory
records that the dominant defect in these tests is *a double that answers
correctly no matter what it is asked*, that every instance was found by mutation
and none by reading, and that two survived to final review still green. A CI
check that cannot fail is that same defect at the largest available scale — and
the hook this spec replaces was, for its entire life, exactly that: never
installed, therefore never able to fail, therefore never noticed.

## Constraints carried from spec 1

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

## Explicitly out of scope

To [spec 6](2026-08-11-spec-6-releases-and-distribution.md): releases, version
and release triggers, checksums, signing, attestations, SBOMs, the Homebrew tap
and formula, bottles, the shim's download-and-verify path, what a distributed
binary stamps as its checkout root, `agents version`, and upgrade and rollback.

Not in either spec: Windows; self-hosted runners; a scheduled job against the
latest Go toolchain; changing what `agents doctor` checks beyond §8.

## Open questions

Two, both narrow, both resolved by running the thing rather than by discussion:

- Does `./bootstrap plan dotfiles` exit `0` in an unprivileged container? §7
  states the answer if it does not.
- Which existing `bootstrap` paths return `2` where they mean `5`? That
  `bootstrap` gets the code is not open — spec 1 §6 defines it. The audit finds
  the call sites that were mislabelled on the way.

## Rejected alternatives

**Path filters with an enumerated exception list.** Enumerate the non-module
files the suites read — `Makefile`, `git/gitconfig.shared`, and whatever is added
later — and filter on the union. Rejected: the list is hand-maintained and its
staleness is invisible, which makes it the same defect this entire spec exists to
gate. A filter that is wrong fails exactly where the build cache also fails.

**Gate first, fixes after** — land the whole workflow immediately with known-red
jobs on `continue-on-error`, removing the flag as each fix arrives. Rejected: a
gate permitted to be red teaches everyone that red is normal, and produces the
condition the deleted `go.pre-commit` died in. Faster signal is not worth
establishing that precedent as the repository's first experience of CI.

**Fixes first, gate last** — reconcile everything locally, then add one workflow
that arrives green. Rejected: the Arch stage-zero fix cannot be verified locally
without Docker, so this fixes blind at precisely the point where CI was the
justification, and leaves the repository unguarded for the whole duration.

**Extending `agents index` to write the README block.** Tempting, because
`index` already owns generated content and the `generated-file` guard rule would
then cover the README for free. Rejected: `agents save` commits `.agents/` paths
and nothing else, and the `mixed-commit` guard exists to keep repository content
and agent context in separate commits. Teaching `index` to write a tracked file
outside `.agents/` would put those two mechanisms in tension for a convenience.
The renderer emits markdown on stdout; CI is the enforcement point.

**~~Adding `NoRecord = 5` to `bootstrap` for symmetry.~~ Reversed 2026-08-13.**
The 08-11 draft rejected this, reasoning that an unused constant documents a
capability the binary does not have. The reasoning was sound and the premise was
false: spec 1 §6 defines six codes as "identical across every subcommand," so
this was never symmetry-for-its-own-sake — `bootstrap` is missing a code the
foundation specifies. Recorded rather than deleted, because the mistake is
instructive: it took the two binaries as the authority on their shared
vocabulary, when the shared vocabulary is defined above both of them.

**Suites only, no container jobs** — run `go test` on `ubuntu-latest` and call
Linux covered. Rejected: it proves the Go code is portable and touches none of
the stage-zero package selection, the linuxbrew prefix, or `chsh`. Spec 2's
Known gap would stay open while appearing closed, which is worse than leaving it
visibly open.
