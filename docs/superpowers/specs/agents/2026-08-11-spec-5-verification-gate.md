# Spec 5 — the verification gate

**Date:** 2026-08-10 (scope, as "CI, releases, and binary distribution") /
2026-08-11 (design, narrowed to verification) / 2026-08-13 (reworked around
audience and reachability; Arch stage zero measured)
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
> [spec 7](2026-08-12-spec-7-capture-and-review.md), amended 08-13.** Nothing in
> the organizing claim changes, and it gets easier: with the trace index
> untracked, CI never sees machine-local paths in the tracked tree at all.
>
> One scheduling collision was flagged — spec 7 adds `agents review` and
> `agents handoff draft`, which land in **§6**'s registry and its derived help
> text — and it has since resolved itself in the less good direction. **They
> shipped without help text, and this spec inherits them.** That is now the
> argument for landing the registry early rather than a question about it: the
> collision is not hypothetical and the next workstream will do the same.

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
  both binaries. It is not. See [§4](#4-exit-codes-audience-and-reachability).
- `usage()` in `agents` is a hand-maintained string literal with no link to the
  dispatch switch beside it, and there is no `help` command at any level. See
  [§6](#6-the-command-registry-and-the-documentation-derived-from-it).

**Reworked 2026-08-13, from first principles rather than from spec 1's
authority.** Two questions were treated as separate — what the exit-code table
should be, and whether help should be central or distributed — and both dissolved
under measurement. Nothing branches on the specific exit-code value, while the
*sign* is consumed everywhere; and central-vs-distributed turned out to be about
file layout when the property that matters is derivation. What the evidence
actually pointed at is [the organizing claim](#the-organizing-claim), which is
neither of those and subsumes both. The 08-11 positions this replaced are kept
under [Rejected alternatives](#rejected-alternatives) rather than deleted.

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
  [§4](#4-exit-codes-audience-and-reachability) for the three-way wording
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

**Every mechanism must have a path by which its absence becomes visible.**

The recurring defect in this repository is not that descriptions drift. It is
that mechanisms become unreachable, and *unreachable is indistinguishable from
working*. Four measured instances:

- **`git/hooks/go.pre-commit`** — nothing ever installed it. Inert for its entire
  life, and deleted without anyone noticing it had never run.
- **The wiring incident** (`a26eaa9`, 2026-08-13) — a test wired this repository
  with the ephemeral `agents.test` path, four times per `go test` run, while
  `agents doctor` reported the wiring exact.
- **`agents guard --staged`** — usage calls it "the only command that blocks" and
  spec 1 calls it the sole deliberate exception to fail-open. **Nothing invokes
  it.** See [§4](#4-exit-codes-audience-and-reachability).
- **`bootstrap`'s `exitAdvisory`** — declared at `main.go:29` and never returned
  by any path. The same disease from the other end: a value with no producer,
  where the others are mechanisms with no caller.

Drift is a special case of this, not a separate idea. `usage()` diverging from
the dispatch switch beside it happens because the description is *unreachable
from the code* — nothing forces the two into contact. A stale `INDEX.md`, a
README naming a deleted command, and two binaries describing one vocabulary
differently are the same shape.

So the checks in this spec fall into two families, and the second is the one that
has never existed here:

1. **Derivation** — one artifact per fact, everything else computed from it.
   Help, the exit-code table, the generated indexes, the README block.
2. **Reachability** — a declared mechanism is actually reached by the thing
   declared to reach it. Nothing in this repository checks this today, and it is
   the only family that would have caught all four instances above.

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
| `docs` | `ubuntu-latest` | §6 — help coverage, README block, backward name check, `agent`-audience skill coverage |
| `reachability` | `ubuntu-latest` | §6 — every declared audience actually reaches its command |
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

### 4. Exit codes: audience and reachability

The scope note asked CI to "decide which codes fail a job, which annotate, and
which are skips." The question is malformed, but not for the reason the 08-11
draft of this section gave. It is malformed because **an exit code carries two
different things, and only one of them has a consumer that can act.**

#### What was measured

- **Nothing branches on `1` vs `4` vs `5`.** No consumer, in either module,
  selects behaviour on the specific value.
- **The two paths that run automatically bypass the vocabulary entirely.**
  `agents/internal/githook/githook.go` does not import `exitcode` and returns
  raw `0`/`1`; `agents/cmd_hook.go:30` returns literal `0` unconditionally.
- **The sign, however, is consumed everywhere.** Git aborts `pre-commit` and
  `commit-msg` on non-zero and ignores the code entirely for `post-merge` and
  `post-checkout`. CI fails a step on non-zero. A human or an agent reads output
  that came with a non-zero exit and skims output that did not.

So the byte carries **disposition** — what an automated caller must do — and
**attention** — what makes a reader read the text. The value distinguishes
neither; only the sign does.

#### Audience decides which one applies

| Class | Members | What the exit code may mean |
|---|---|---|
| **Automated** | `git`, `harness`, `ci` | disposition only — non-zero *is* failure |
| **Attentional** | `human`, `agent` | non-zero is the trigger that gets the text read |

An automated consumer cannot read prose and decide. CI in particular has no
reader at the moment of the call: the log is consulted later, and only if
something already failed. **There is no attention to hijack**, which puts CI in
the same class as git rather than in a class of its own.

**The binding rule: if any automated consumer invokes a command, its exit code
must be disposition-only.** Attentional consumers lose nothing by this — git
prints hook output regardless of exit status, and so does a terminal, so the
text still arrives. Where a command has no automated audience it may hijack
freely, which is what `agents init` already does when it exits `1` "so the state
is visible rather than assumed" (spec 1 §9).

This is not a new rule imposed on the code. It is the rule the code already
follows in two of three places — `githook.go` maps to git's two-value
disposition, `cmd_hook.go` fails open for the harness — and follows nowhere by
declaration, so nothing stops the third place from being wrong. It is.

#### `guard` is the third place, and its two defects mask each other

`agents/cmd_guard.go:61` returns `Advisory` when findings exist but none are
blocking. Its audience is `git` and `ci` — both automated. Git cannot see the
`Blocking` field the code carefully computes; it sees a non-zero and aborts.

**So wiring the guard today would abort every commit carrying a warning-level
finding.** The distinction the command exists to draw would be erased by its own
consumer.

That has never fired, because **nothing invokes `agents guard --staged`.** The
four hook symlinks route to `githook.Run`, which runs repository hooks and then
personal extras, and finally `builtin` — which returns `0` for every name except
`commit-msg` (`githook.go:354`). The extras directory holds `recent.post-merge`
and one symlink to it. `.git/hooks/` holds only samples. No harness configuration
references it. It runs when a human types it, and otherwise never.

The two defects hide each other, which is why **they must land in one phase.**
Fix reachability alone and commits begin failing on warnings; fix the exit code
alone and nothing changes, because nothing calls it.

It also corrects [§5](#5-the-hygiene-job--tools-as-a-function-of-the-repository):
the `generated-file` guard rule is not "enforced only by a git hook someone has
to have installed." It is enforced by nothing at all, which makes CI the only
enforcement point rather than the second one.

#### What CI does, and what it does not

Because the commands CI invokes will have disposition-only semantics, **CI's
default "non-zero fails the step" is already correct and needs no wrapping.**
Checked against the actual call sites: `agents index` and
`./bootstrap plan|apply|check` all exit `0` nominally. `guard` is the only
hijacker, and it is the one being fixed.

Explicit assertion survives for one purpose: **negative paths.** `expect 2`
proves a refusal still refuses — the test that catches a guard which quietly
stopped guarding, and which "non-zero fails" cannot express. Those are a handful
of steps and they are tests, not workarounds.

The 08-11 draft required *every* call site to declare its expected code. That was
over-general, and worse, it would have put a copy of a fact the code owns into
the workflow file — the drift this spec exists to remove, reintroduced by the
spec.

#### Spec 1's table, and `bootstrap`'s dead constant

Spec 1 §6 defines six codes and calls them "identical across every subcommand."
That is design intent, and this spec treats it as such: **the code is
authoritative and the table is checked against it**, not the reverse. A design
document that must be edited whenever code changes is drift wearing a spec's
clothes. When the two disagree the check surfaces it and a human decides which
was wrong.

The wording has already diverged three ways — spec 1 says code 4 is "not
applicable / skip", the constant comment says "not applicable here", `agents`
help prints "skip", `bootstrap` help prints "not applicable". The fix is that
**help renders the table from the constants rather than restating it**, which is
[§6](#6-the-command-registry-and-the-documentation-derived-from-it)'s registry
doing its job rather than a separate mechanism.

`bootstrap` then needs two things, and neither is the symmetry argument the 08-11
draft made:

- **Delete `exitAdvisory`** (`bootstrap.d/main.go:29`). It is declared and never
  returned by any path — a value with no producer. Its audience includes `ci` and
  `human`, so under the binding rule it should never hijack, and it does not. The
  constant documents a behaviour the binary does not have.
- **Audit `exitBlock` for paths meaning "could not complete."** A refusal asserts
  *this machine is in a state I will not write over*, which is a different claim
  from *I tried and could not*. Whether that warrants adding code `5` is decided
  by whether such paths exist, not by matching `agents`.

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
guard rule is written to block a commit that leaves an index stale. It has never
blocked one: the rule lives in `agents guard --staged`, and
[§4](#4-exit-codes-audience-and-reachability) establishes that **nothing invokes
that command.** So this is not CI adding a second enforcement point to a hook
someone might not have installed — as the 08-11 draft claimed. It is CI becoming
the *first* one, and until phase 4 wires the guard, the only one.

This check earns more than freshness: it **asserts that `agents index` is a pure
function of tracked content.** If it ever embeds anything machine-local, this is
the check that says so.

### 6. The command registry, and the documentation derived from it

**This section owns the `cli-help-unification` lane**, whose pending draft
handoff was written on 2026-08-13 for whoever picked this up.

`agents/main.go` dispatches through a `switch` at `:61` and describes itself
through a string literal at `:98`. Nothing connects them, so adding a command and
forgetting to document it is a silent, ordinary mistake. There is no `help`
command, and `trace cache prune` — a destructive verb — has no help at all.

Spec 7 demonstrated the cost while this spec sat unimplemented. Four usage lines
changed in one workstream — `handoff write|draft|prune` gained a verb, and
`review [--keep|--bin <id>]`, `trace cache prune --retention` and
`trace migrate [--yes]` are new — each a hand-edit to a literal that happened to
be remembered. Doctor's check set changed in the same period with no usage line
to update, and `wiring:*` gained a `Warn` result it never had.

#### Central or distributed is the wrong axis

The question that presents itself — should help live in one place or beside each
command — is about **file layout**, and file layout is not what causes the bug.
A hand-maintained central registry drifts exactly as readily as scattered
literals. **The property that matters is derivation:** whether every appearance
of a fact is computed from one artifact, or authored separately.

Once that is the question, the answer has three stages and the tension dissolves,
because each stage wants a different answer:

- **Declare — distributed.** Each command declares its name, summary, flags,
  detail, and audience *next to its implementation*. `review` declares itself in
  `cmd_review.go`. Authorship belongs where the knowledge is and where it
  changes; a command added in a new file cannot forget a registry in another
  file, because there is nothing to remember.
- **Assemble — central.** One tree collects the declarations. Dispatch needs a
  single entry point and the checks need a single thing to walk. This is the only
  central artifact, and no prose is written into it.
- **Render — derived, zero authorship.** `usage()`, `help <path>`, `--help` at
  any depth, the exit-code table, and the README block are all *outputs*. Nobody
  edits them, so nobody can forget to.

Dispatch and help walk the same tree, which makes an undocumented command
*structurally* impossible rather than merely tested. An unknown command still
exits `3`. A coverage check walks the tree and fails on any node with an empty
summary, usage, or detail — a belt over the structural braces, and what makes
"add a command" fail loudly if the tree is ever bypassed.

#### `audience` is the field that carries the rest

Each node declares who invokes it: `human`, `agent`, `git`, `harness`, `ci`.
[§4](#4-exit-codes-audience-and-reachability) groups these into automated and
attentional classes. Four things derive from the field, and none of them is a
rule anyone has to remember:

- **The exit-code regime.** Any automated audience forces disposition-only
  semantics. This is checkable, and `guard` fails it today.
- **Help visibility.** `hook` is invoked only by a harness, so it does not belong
  in top-level usage; it belongs under `help --all`.
- **Skill-doc coverage.** Every command with an `agent` audience must appear in
  the guidance, or a harness never learns to reach for it. Without this, a new
  command lands, gets a generated reference entry, and no agent ever uses it.
- **Reachability.** A command declaring `audience: git` must be reachable from
  the git hook chain; `audience: harness` must appear in generated wiring.

**Reachability is the family this repository has never had**, and it is the only
one of the four that would have caught all of [the organizing
claim](#the-organizing-claim)'s four instances. It is red today on `guard`, whose
fix is paired with its exit-code defect in one phase because either alone makes
things worse.

#### Documentation, checked in both directions

*Forward* — the full command surface is rendered as markdown into a marked block
in `README.md`, and CI regenerates and diffs it. The prose around the block stays
hand-written, because *when* to reach for a command is judgment and cannot be
generated.

*Backward* — no living document may name an `agents` subcommand the registry does
not define. The check scans inline code spans and fenced blocks for
`agents <subcommand>` and requires each path to resolve. This catches doc rot in
the direction that hurts: a document confidently naming a command that no longer
exists.

**The backward check covers `README.md`, both `CLAUDE.md` files — the project one
and the tracked global `claude/CLAUDE.md` — and everything under `claude/skills/`
and `.agents/skills/`. It deliberately excludes `docs/superpowers/plans/` and
`docs/superpowers/specs/`.** Plans and specs are dated records of what was true
when they were written; the executed bootstrap plan legitimately names
`make githooks`, which no longer exists. Forcing those to match today's command
set would destroy the record, and a record silently rewritten to stay true is not
a record.

#### Where harness guidance lives

**`claude/skills/`, not either `CLAUDE.md` and not `.agents/skills/`.** The 08-11
draft said `.agents/skills/`; PR #19 shows that was the wrong shelf.

`bootstrap.d/links.manifest:20-21` symlinks `claude/skills` → `~/.claude/skills`
and `claude/CLAUDE.md` → `~/.claude/CLAUDE.md` on every provisioned machine. So
`claude/skills/` is **fleet-wide**: tracked here, installed everywhere, loaded in
every session in every repository. `.agents/skills/` is per-repository procedure.

`agents` is a fleet-wide tool — `agents ls` lists registered repositories and
`agents update --all` rewires every one. Guidance on when to reach for it is
therefore not specific to this checkout, and `.agents/skills/` would give it to
the one repository that needs it least.

The two artifacts answer different questions and only one can be generated. The
rendered block answers *what is this command*; the skill answers *which command
is this situation*. That second question is judgment — when a finding is worth
`agents handoff draft` rather than a comment, why `agents review` stands between
a draft and the tracked record, when `agents trace show` answers what grep
cannot. The `agent`-audience coverage check above is what keeps the second
artifact honest as the first one grows.

Neither `CLAUDE.md` becomes a manual. The project one declares itself "only the
pointer" to `.agents/`; the global one is kept to rules a session acts on.

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

#### One named unknown, with its resolution rule

**Whether `./bootstrap plan dotfiles` exits `0` in a bare container is
unverified.** Spec 2's Known gap 2 says `plan` exits `2` on a machine lacking
Homebrew or fish rather than previewing. If that reaches the `dotfiles` profile,
**it is a finding to fix, not a reason to weaken the job.** A profile designed to
be safe where nothing is installed, which refuses where nothing is installed, is
not doing its job.

#### Arch stage zero is broken — measured 2026-08-13, not predicted

`bootstrap.d/internal/phase/packages.go:76` refreshes the index before installing
on apt, with a comment explaining exactly why: "An install against an index
months out of date does not install an old version, it 404s on the URL the old
index names." The pacman branch at `packages.go:86` goes straight to
`pacman -S --needed --noconfirm`, with no equivalent step and no comment about
its absence.

The 08-11 draft recorded this as a prediction. Docker settled it:

```
$ docker run --rm --platform linux/amd64 archlinux:base \
    pacman -S --needed --noconfirm base-devel curl file git
error: target not found: base-devel
error: target not found: curl
error: target not found: file
error: target not found: git
                                                        → exit 1
```

**Stage zero cannot work on a fresh Arch machine.** The stock image ships an
empty sync database, so `-S` resolves nothing. This is not a container artefact:
any Arch install whose database has not been synced behaves the same way, and
spec 2 has always listed Arch/Manjaro as supported. The asymmetry with the apt
branch was an oversight, and it means **spec 2's Arch support has never worked**,
not merely never been tested.

The repair is not `-Sy`. Arch does not support partial upgrades — syncing the
database without upgrading installed packages is a documented way to break a
system — so the supported form is `-Syu`, which means stage zero performs a
**full system upgrade** to install four packages. That cost is real and the phase
decides between accepting it and requiring an already-synced system as an
explicit precondition. It does not ship `-Sy`.

**The `-Syu` repair was verified in the same session** — same image, same four
packages, **exit 0**. So the fix is known to work, not merely reasoned about.

One caveat belongs with that result. The verifying run needed
`--disable-sandbox`, because pacman's seccomp sandbox fails under amd64
emulation (`error restricting syscalls via seccomp: 22`, `switching to sandbox
user 'alpm' failed`). That flag is an artefact of emulation on Apple Silicon, not
part of the repair, and **stage zero must not ship it** — disabling pacman's
sandbox on a real machine is a security regression to work around a problem that
machine does not have. On CI's native x86_64 runners plain `-Syu` is expected to
suffice, and the Arch job is what confirms it.

That emulation detour is itself worth recording: `archlinux` publishes **no
arm64 manifest**, and under emulation pacman needs a flag production must not
use. The Arch job is therefore not locally reproducible on Apple Silicon — it
runs on CI or nowhere. That sharpens rather than weakens the case for it, and
saves the next person an afternoon.

This is the gate paying for itself before it exists.

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
| 3 | Command registry with `audience`: declare/assemble/render, `agents help`, coverage check, exit-code table rendered from the constants | Dispatch and description cannot diverge; one vocabulary with one author |
| 4 | Reachability, and `guard`'s two defects **together**: the per-audience reachability check, wiring the guard, and replacing its `Advisory` return | The first reachability check this repository has had, and a guard that guards |
| 5 | Documentation: generated README block, backward name check, `claude/skills/` guidance with `agent`-audience coverage | Docs that cannot drift from the command set |
| 6 | Linux: `dotfiles` profile, then Debian, then Arch with the `-Syu` fix | Spec 2's largest known gap closed or precisely narrowed |
| 7 | Doctor stamped-root check | The worktree hazard PR #16 documented is no longer silent |

Phase 1 is expected green on arrival. Phases 2, 3 and 4 expect red first — that
is the point of pairing each with its fix.

**Phase 6 is no longer a discovery.** Arch stage zero is now known broken and the
`-Syu` repair is known to work ([§7](#7-linux)), so the phase lands the job and
the fix together like every other. What it still discovers is whether
`plan dotfiles` survives a bare container, and whether plain `-Syu` suffices on a
native x86_64 runner without the emulation flag.

**Phase 4 cannot be split.** Wiring the guard without fixing its `Advisory`
return makes every warning-level finding abort a commit; fixing the return
without wiring it changes nothing, because nothing calls it. The two defects have
been masking each other and must stop together.

**Phases 3 and 4 were reworked on 2026-08-13.** The 08-11 draft had a standalone
"exit-code reconciliation" phase. Under the audience model there is no such
thing: the table is one of the registry's rendered outputs (phase 3), and the
disposition rule is enforced by the reachability work (phase 4). `bootstrap`'s
cleanup — deleting the never-returned `exitAdvisory` and auditing `exitBlock` for
paths meaning "could not complete" — rides in phase 4 for the same reason.

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

Four, all narrow, all resolved by running the thing rather than by discussion:

- Does `./bootstrap plan dotfiles` exit `0` in an unprivileged container? §7
  states the answer if it does not.
- Does plain `-Syu` suffice on a native x86_64 runner, without the
  `--disable-sandbox` the emulated verification needed? §7.
- Which existing `bootstrap` paths return `2` where they mean `5`? An audit, not
  an authority question — see [Rejected alternatives](#rejected-alternatives).
- **Where does `guard` get wired?** Phase 4 has to answer this, and the options
  are not equivalent. As a `builtin` for `pre-commit` it is installed wherever the
  four symlinks are, which is the whole fleet, and it cannot be opted out of. As a
  personal extras hook it is opt-in per machine and would have stayed unwired here
  by the same accident that hid it. The first is what "the only command that
  blocks" implies; the second is what the current architecture makes easy.

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
that arrives green. Still rejected, though the 08-11 reasoning has weakened: that
draft said the Arch fix could not be verified without CI, and on 2026-08-13
Docker verified both the defect and the repair locally. What survives is the rest
of the argument — it leaves the repository unguarded for the entire duration, and
local Docker cannot stand in for the runner anyway, since `archlinux` has no
arm64 manifest and pacman needs a flag under emulation that production must not
ship.

**Extending `agents index` to write the README block.** Tempting, because
`index` already owns generated content and the `generated-file` guard rule would
then cover the README for free. Rejected: `agents save` commits `.agents/` paths
and nothing else, and the `mixed-commit` guard exists to keep repository content
and agent context in separate commits. Teaching `index` to write a tracked file
outside `.agents/` would put those two mechanisms in tension for a convenience.
The renderer emits markdown on stdout; CI is the enforcement point.

**Adding `NoRecord = 5` to `bootstrap` for symmetry — rejected twice, reversed
once in between.** Worth recording in full, because the round trip is the most
instructive thing in this document.

*08-11:* rejected, on the grounds that an unused constant documents a capability
the binary does not have. *08-13 morning:* reversed, on the grounds that spec 1
§6 defines six codes as "identical across every subcommand," so `bootstrap` was
simply missing one. *08-13, after measuring:* **both arguments were wrong, and
the first was wrong for the right reason.**

Neither the binaries nor spec 1 is the authority. The question is empirical:
does `bootstrap` have a path that means *I tried and could not*, distinct from
*this machine is in a state I will not write over*? If yes it gets the code
because it needs it; if no, adding it produces exactly the artefact the 08-11
draft feared — and that artefact already exists. `bootstrap.d/main.go:29`
declares `exitAdvisory` and **no path returns it.** A value with no producer,
sitting in the file, describing a behaviour the binary does not have.

So the 08-11 instinct was right and its reason was incomplete. The reason is not
"unused constants are untidy"; it is the organizing claim — a declaration with no
path connecting it to reality is indistinguishable from one that works, and that
is the defect this whole spec is about. `exitAdvisory` is deleted in phase 4.

**Suites only, no container jobs** — run `go test` on `ubuntu-latest` and call
Linux covered. Rejected: it proves the Go code is portable and touches none of
the stage-zero package selection, the linuxbrew prefix, or `chsh`. Spec 2's
Known gap would stay open while appearing closed, which is worse than leaving it
visibly open.
