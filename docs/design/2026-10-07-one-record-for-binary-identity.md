# One record for binary identity

**Date:** 2026-10-07
**Status:** proposed. No code changed; §9 lists what would have to be true before
this is in force, and §10–§11 what has to move to get there.
**Supersedes in part:** [binary identity and standalone resolution](2026-08-28-binary-identity-and-standalone-resolution.md)
— its mode matrix and its `AGENTS_DOTFILES_ROOT` rule are replaced; its link-time
stamp survives with a narrower job (§4.4).
**Depends on:** [the `agents`/`bootstrap` boundary](2026-09-20-agents-and-bootstrap-boundary.md)
§2–§6, spec 1 §8 (Go-backed git hooks), spec 6 (releases and distribution).

---

## 1. The question, and what is wrong with the answer

*Which binary, and which checkout?* has three answers, three owners, and no
statement that ties them together:

| the question, as asked in code | answered by | owner |
|---|---|---|
| which file will git exec for a hook? | the binary argument to the installer, recorded as four symlinks | `git/install-hooks.sh` |
| which checkout does this binary belong to? | `main.dotfilesRoot`, else `AGENTS_DOTFILES_ROOT`, else `""` | `agents/root.go` |
| is the `agents` on `PATH` the running one? | `os.Executable()` compared with `PATH` by file identity | `agents/internal/doctor` |

On this machine they disagree, in the shape [the boundary document §4a](2026-09-20-agents-and-bootstrap-boundary.md)
already recorded as a defect: the four links name `/opt/homebrew/bin/agents`, the
mode comes from a line in `~/.config/fish/config.fish`, and the checkout
`bootstrap` builds for (`~/bin/agents`) does not exist.

Three consequences, each measured rather than argued.

**1. Behaviour depends on how a process was launched, and says nothing when it
changes.** Same binary, same repository, one variable apart:

```text
$ agents doctor                          → 18 checks
$ env -u AGENTS_DOTFILES_ROOT agents doctor → 13 checks
```

The five that disappear are exactly the machine-level ones: `root:exists`,
`git-hooks:global`, `git-hooks:effective`, `git-hooks:links`,
`git-hooks:unmanaged`. No line of output says a mode was entered or left.

**2. The binding is invisible and owned by nobody.** `AGENTS_DOTFILES_ROOT` is
set by a hand-written line in `~/.config/fish/config.fish`, which
`bootstrap.d/links.manifest` declares as `seed` — copied once, never rewritten.
No tracked file has ever written that variable: `git log -S AGENTS_DOTFILES_ROOT
-- bootstrap.d fish` is empty.

**3. The machine's record lives where git can delete it.** `core.hooksPath` names
`~/dotfiles/git/hooks.d`, a directory that is entirely untracked (only its
`.gitignore` is committed). A `core.hooksPath` naming a directory that does not
exist is silent — measured 2026-10-07, a commit in a fresh repository with
`core.hooksPath` aimed at a missing directory exits 0 and prints nothing, the
same way a dangling hook does.

The defect is not that three mechanisms exist. It is that **one fact is answered
by more than one of them**, and that behaviour is read from an ambient source
rather than from a record.

## 2. The model

Three scopes — the boundary document's, unchanged — and now exactly one record per
scope, with exactly one writer:

| scope | record | writer | readers |
|---|---|---|---|
| the machine | the hook chain: `core.hooksPath`, the entries under it, and the facts they state | `git/install-hooks.sh` | git; the dispatcher; `agents doctor` |
| one checkout | the link-time stamp: which checkout built this binary | `make agents`; `bootstrap`'s devtools phase | `agents version`; `agents doctor` (provenance only) |
| one repository | `.agents/`, the doc stores, the harness configs | `agents init`, `agents wire` | the harnesses; `agents doctor` |

The modes are not replaced by better modes. They are deleted: what a run does is
determined by which chain invoked it and which repository it is standing in.

## 3. The rules

1. **One writer per fact.** A fact with two writers is a fact that will
   eventually disagree with itself.
2. **Readers never infer.** A reader takes the fact from its record. It does not
   reconstruct it from `PATH`, from the environment, from the name of the file it
   happens to be running as, or from the value it is about to verify.
3. **No ambient behaviour.** Nothing in the process environment changes what the
   tool does. An environment variable may point *at* a person's shell; it must
   not point *at* a decision.
4. **Ambiguity refuses.** Where a fact is missing, unreadable or contradictory,
   say so and stop. Never fall back to a default that behaves differently from
   the stated intent.
5. **State bindings; do not imply them.** A binding is a line a person can read
   and a test can assert. A symlink is an implicit statement whose failure is
   silence.

## 4. The design

### 4.1 The chain is the machine record

The chain is one directory, machine-owned, named by `core.hooksPath`. It holds
two kinds of thing:

`chain.env` — the facts, one `key=value` per line, with the writer named in the
file:

```sh
# written by git/install-hooks.sh; re-run the installer to change it
format=1
binary=/opt/homebrew/bin/agents
checkout=/Users/nilbot/dotfiles
```

and four executable entries named `pre-commit`, `commit-msg`, `post-merge`,
`post-checkout`. `binary` is the program git execs. `checkout` names the
directory whose `git/hooks/` supplies the personal stages, or `-` for none.

The chain is deliberately **not** inside a checkout. It is machine state, it is
named by a machine-global Git setting, and a checkout is a directory a person
moves and deletes.

### 4.2 An entry states the binding, then verifies it

An entry is a generated POSIX `sh` script. Symlinks are the thing being replaced:

```sh
#!/bin/sh
# Written by git/install-hooks.sh for /Users/nilbot/dotfiles.
# Re-run the installer to change the binary or the checkout.
chain=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || exit 1
[ -r "$chain/chain.env" ] || {
  echo "agents: $chain/chain.env is missing; the commit guard is not running" >&2
  exit 1
}
. "$chain/chain.env"
[ -x "$binary" ] || {
  echo "agents: the commit guard is not installed: $binary is missing or not executable" >&2
  echo 'agents: repair with: bash /Users/nilbot/dotfiles/git/install-hooks.sh install --adopt-owned /Users/nilbot/dotfiles "$HOME" "$(command -v agents)"' >&2
  exit 1
}
exec "$binary" githook pre-commit --checkout "$checkout" "$@"
```

Three properties follow, and each one is the reason for the shape.

- **The binding is readable.** `cat` the chain and you know which binary will
  run and which checkout supplies the personal stages. No mode, no environment,
  no inference from a path's shape.
- **Failure is loud, and closed where it must be.** A missing binary prints what
  is wrong and the command that repairs it. The guard-bearing names
  (`pre-commit`, `commit-msg`) exit non-zero, so a broken guard blocks the commit
  instead of permitting it. The observational names (`post-merge`,
  `post-checkout`) print and exit 0, because a missing banner must not fail a
  pull.
- **The guard outlives the checkout.** With `checkout` unreadable, `pre-commit`
  still runs the binary's built-in stage and reports the personal stages as
  missing. Today, deleting the checkout deletes the guard.

### 4.3 The dispatcher is told, not asked

`agents githook <name> --checkout <path>` runs one hook's chain: the repository's
own hook, the personal stages under `<path>/git/hooks/` when `<path>` is not `-`,
then the built-in stages. It reads no record and derives nothing. The stamp and
the environment are not inputs.

This replaces the multicall, which dispatches on `filepath.Base(os.Args[0])`.
That mechanism makes identity depend on the name of the file that happens to be
exec'd — exactly the implicit binding rule 5 rejects — and it is the reason an
entry cannot be a script today. One mechanism replaces another; the tool does not
gain a way to be a hook, it changes its one way.

This is **not** the `agents hook` command deleted on 2026-09-21. That was the
harness-lifecycle record entry point — same word, different job, different
payload — and it stays deleted.

### 4.4 The stamp is provenance, not mode

`main.dotfilesRoot` keeps one job: saying which checkout built this binary.

- `agents version` prints it: `built from /Users/nilbot/dotfiles`, or
  `release build` when the binary is unstamped.
- `agents doctor` reports it as a fact, and warns when the named directory no
  longer exists — the linked-worktree hazard `Makefile` documents.

It never changes behaviour. A stamped binary and an unstamped binary, given the
same chain, do the same thing.

### 4.5 `doctor` reports the machine; it does not hold a root

`doctor` asks the machine one question — *what is installed here?* — and answers it
from `core.hooksPath` and the chain, not from a stamped root:

| check | reads | replaces |
|---|---|---|
| `chain:hooks-path` | `git config --global core.hooksPath` | `git-hooks:global`, `git-hooks:effective` |
| `chain:entries` | each of the four names: an executable regular file carrying the generated header | `git-hooks:links` |
| `chain:record` | `chain.env` parses, and its `binary` is executable | new |
| `chain:unmanaged` | executable look-alikes under other names in the chain | `git-hooks:unmanaged` |
| `chain:local` | a repository-local `core.hooksPath` override, which bypasses the chain by choice | `git-hooks:local` |
| `chain:running` | which binary the chain names, and which binary is running, reported as two facts | the old `binary` check's implicit "the" binary |
| `attributes:global` | `~/.gitattributes` is a link whose target exists | `git-attributes` |
| `provenance:checkout` | the stamped directory, if any, still exists | `root:exists` |

Every one of these is answerable without knowing "the" root, and none of them
compares a value against itself. `~/.gitattributes` needs no root either: the
question is whether the link's target exists, not whether it is the one a
particular checkout would have written.

## 5. Resolution, stated once

| consumer | the fact it needs | reads it from | must never read it from |
|---|---|---|---|
| git | which entries to run | `core.hooksPath` | — |
| a chain entry | what to exec | `chain.env` | `PATH`, the environment, `argv[0]` |
| the dispatcher | where the personal stages are | the `--checkout` its entry passed | the stamp, the environment |
| `agents doctor` | what is installed on this machine | `core.hooksPath`, the entries, `chain.env` | a stamped root; the environment |
| `agents version` | what built this binary | the link-time stamp | — |
| `agents init`, `agents wire` | which repository this is | the repository under the working directory | anything machine-level |

## 6. Failure modes, all of them loud

| what is wrong | what happens | who says so |
|---|---|---|
| the named binary is missing or not executable | guard hooks name it and exit 1; observational hooks name it and exit 0 | the entry |
| `chain.env` is missing or malformed | every entry names the file and exits 1 | the entry |
| the checkout is gone | the guard still runs; the personal stages are reported missing | the entry, and `doctor` |
| `core.hooksPath` names a directory that does not exist | nothing runs — **the one remaining silence** | `doctor`, if it is run |
| the chain names a binary other than the running one | both are printed as facts | `doctor` |
| a repository sets its own `core.hooksPath` | the chain is bypassed; that is a deliberate local choice | `doctor` (`chain:local`) |

The fourth row is the residual: a deleted chain directory cannot announce itself,
because the only thing that could announce it is inside it. Machine-owned state,
not touched by package upgrades, is the mitigation; `doctor` is the detector.

## 7. What this deletes

- `AGENTS_DOTFILES_ROOT`, and with it every behaviour that depended on the
  process environment.
- The mode matrix: there is no Operator Mode and no Standalone Mode. There is a
  chain on this machine, or there is not.
- The `argv[0]` multicall, replaced by `agents githook`.
- The four symlinks in `~/dotfiles/git/hooks.d/`, replaced by four generated
  entries in the chain directory.
- `DotfilesRoot()` as a decision input. What survives is a provenance reading
  used by `version` and reported by `doctor`.

Nothing is added to the repository scope: `init`, `wire`, `guard` and their
records are untouched.

## 8. Rejected alternatives

**8.1 `$HOME/dotfiles` existence as the default.** Removed 2026-08-28: on any
machine with a directory of that name it silently activated the operator
behaviour. It is the ambient-source defect in its purest form.

**8.2 `AGENTS_DOTFILES_ROOT`.** Kept as "the explicit override" and rejected now
for three measured reasons: it is unmanaged (§1.2); its presence depends on
launch ancestry, so the same binary behaves two ways on one machine (§1.1); and
it makes behaviour a property of the environment rather than of the installation.
An override that is needed to make the common case work is not an override, it is
the mechanism.

**8.3 `realpath $(which agents)`** — the question that produced this document.
Rejected on measurement, not on taste. `realpath` resolves Homebrew's stable
`bin/agents` to `Cellar/agents/<version>/bin/agents`, which the next
`brew upgrade` deletes; the hooks then dangle and git runs a dangling hook as if
no hook existed. Recorded with the incident in
[why a brew upgrade stops my commit guard](../qna/why-does-a-brew-upgrade-stop-my-commit-guard.md).
Two further reasons: a `PATH` lookup at hook time is ambient (cron and Dock
launches resolve differently), and the path of an installed binary carries no
information about which checkout it came from — `~/bin/agents` and
`/opt/homebrew/bin/agents` are both outside every checkout, so no derivation can
recover the checkout from either.

**8.4 `command -v agents` resolved inside an entry.** The stable path is the right
*thing to record*; resolving it at exec time is not. A record states a decision;
a lookup re-takes it every time, against an environment the record cannot see.

**8.5 Deriving behaviour from the thing being verified.** Recorded in
`agents/root.go`: a root derived from `core.hooksPath` would make a check that
compares the two pass by construction. The same argument rules out deriving the
checkout from the entry's own path shape.

**8.6 A second config file beside `core.hooksPath`.** Two writers for one fact,
which rule 1 forbids. `core.hooksPath` is already the machine's statement of
where the chain is; the chain states the rest.

**8.7 Keeping the chain inside the checkout.** Measured §1.3: deleting or moving
the checkout switches the guard off in silence, and the Makefile already warns
that a binary built in a linked worktree points at a temporary directory.

**8.8 Making the entries symlinks and adding a check elsewhere.** A check that
runs later cannot help the commit that already passed. The only process that can
report a broken chain at commit time is a process git actually executes.

## 9. What would prove it

Each of these is written to fail on the mutation it names, which is this
repository's standard for a guard.

Where each test lives is itself a contract. Contracts about prose, the command
tree and the generated README block belong in `agents/doctests.txt`, which the CI
`docs` job runs by name and fails if a listed test stops existing. Behaviour of
one package belongs in the ordinary `test` job, which runs everything. The chain
tests are behaviour, so they belong in the ordinary suite — and that is what puts
them under the `hygiene` job's synthetic-`HOME` guard (§10).

1. **Delete the recorded binary; commit.** The commit is refused, and the output
   names the missing path and the repair command. Mutation: make the entry exit
   0, and watch the commit succeed.
2. **Empty `chain.env`.** Every entry refuses. Mutation: default `checkout` to
   `.` and watch a wrong directory be consulted.
3. **Delete the checkout; commit.** The built-in guard still runs. Mutation: make
   the entry require the checkout, and watch the guard disappear with it.
4. **Set `AGENTS_DOTFILES_ROOT` to a second checkout.** Nothing changes.
   Mutation: read it, and watch the check count move between 18 and 13.
5. **Point the chain at a keg path.** The installer refuses and names the stable
   path. Mutation: accept it, and watch the next upgrade dangle.
6. **Install two binaries and run the other one.** `doctor` prints both, naming
   which is wired and which is running, and reports neither as a failure.
7. **`git config --global --unset core.hooksPath`.** `doctor` reports that no
   chain is installed, and the repository-level checks still run.

## 10. What enforces the old behaviour today, and what happens to it

A design that ignores its own guard rails does not land. These are the artifacts
that pin today's behaviour, what each asserts, and what it becomes. Two rows are
the reason the section exists: the `hygiene` jobs already enforce this design's
second rule over the test suite, and `bootstrap.d/makefile_test.go` enforces the
stamp with an explanation this design deletes.

| surface | pins today | becomes | verdict |
|---|---|---|---|
| `agents/root_test.go`, `TestDotfilesRoot*` (3 tests) | unstamped → `""`; the environment is respected; the stamp beats the environment | provenance tests: `version` prints the stamped path or `release build`, and neither changes what a run does | replaced |
| `agents/githook_main_test.go` (4 sites) | points the dispatcher at a fixture checkout by setting `AGENTS_DOTFILES_ROOT` in a child process | passes `--checkout` to `githook`; the child-process harness stays | rewritten |
| `agents/install_hooks_test.go` | the symlink model — four links, keg refusal, `--adopt-owned`, the `core.hooksPath` preflight, `core.hooksPath` written last | the same policies against generated entries, plus: an entry verifies its binary; guard names exit 1; observational names exit 0; `chain.env` is rewritten on install | rewritten in shape, policies kept |
| `agents/doctests.txt` + the CI `docs` job | every listed name exists and passes; the job fails naming the offender | mechanism unchanged; the list follows renames and gains the identity contract tests | survives |
| `agents/commands_test.go`, `TestTheCommandSetIsExactlyThis` | exactly six commands | seven, with `githook` added on purpose | edited deliberately |
| `TestReadmeCommandBlockIsCurrent` + the generated block in `agents/README.md` | the block is derived from `agents help --render=markdown` | regenerated once `githook` exists | survives |
| `TestLivingDocumentsNameOnlyRealCommands`, `…NameNoDeletedCommand`, `…NameOnlyResolvablePaths` | living documents name only real commands, and only paths the checkout contains | the rewritten Operating Modes section must satisfy all three | survives, and does real work |
| `bootstrap.d/makefile_test.go` | exactly one `go build` line in the Makefile, and that it carries `-X main.dotfilesRoot` | the assertion is unchanged and **the comment is rewritten**: it justifies the stamp by behaviour ("doctor fails three checks that are fine", "the git hook chain runs no personal hooks and reports nothing"), which is the theory this design deletes | survives, reason replaced |
| `bootstrap.d/internal/phase/devtools.go` + tests | build `~/bin/agents` stamped, preflight, install hooks at that path, `core.hooksPath` last | build, then install a chain naming that binary; the ordering discipline is kept | rewritten |
| `bootstrap.d/internal/check` | does not verify the hook chain | **still does not.** The installer owns the chain and `doctor` reports it; two verifiers for one fact is the two-owners defect again | decision, recorded |
| CI `hygiene (agents, home)`, `hygiene (bootstrap.d, home)` | the whole suite runs with `HOME=$H`, and `$H` must be **completely empty** afterwards | unchanged — this is rule 2, already in force over the tests. New chain tests therefore take their directory and their home as arguments | survives |
| CI `linux-dotfiles` | real `bootstrap plan/apply/check dotfiles` against the runner's home, then assert it stays converged | unchanged — and it stays unchanged only while `check` does not learn about the chain | survives, conditionally |
| CI `macos-dotfiles` | `./bootstrap plan dotfiles` | unaffected | unaffected |
| CI `linux-stage-zero` | `apply workstation` drags in devtools, so the installer runs for real on a runner | it will create a real chain there; nothing in the design may name a path a bare runner lacks | exercised |
| `agents/README.md` Operating Modes; `git/README.md`; `git/hooks.d/.gitignore` | the two modes, the symlinks, `~/bin/agents` — already stale on this machine, whose links name Homebrew | rewritten; `git/hooks.d/` retires, and its `.gitignore` with it | rewritten |
| [binary identity and standalone resolution](2026-08-28-binary-identity-and-standalone-resolution.md), and the design index | the mode matrix and the environment variable | an amendment note, which is this store's convention for a partly superseded design | amended |
| `docs/qna/` — three entries describe the current mechanism | how the installer accepts a Homebrew symlink; why a `brew upgrade` stops the guard; why an unstamped binary skips dotfiles checks | records are not rewritten. The first two describe mechanisms that survive in changed form and get an amendment; the third becomes the record of a deleted behaviour | amended, or historical |

Two things in that table are worth saying outside it.

**The `hygiene` jobs are rule 2, already applied to the tests.** They run the
suite with `HOME` pointed at an empty directory and fail if anything appears in
it. A test that cannot reach the code under test without faking the environment
is telling you the code has an ambient input: the current suite has eight such
sites, in two files. The design does not fight this guard, it extends it — after
stage 1 the machine paths are arguments, so the tests need no ambient at all.

**`makefile_test.go` is a guard whose assertion is still right and whose stated
reason is now false.** It exists because "the agents binary cannot work out which
checkout it came from, so whoever builds it has to say", and it spells out the
consequences in mode terms. That reason is the thing being deleted. Leaving the
comment is how a deleted theory stays alive: the next reader finds the old
explanation where the guard is, and the new design looks like the violation.

## 11. Landing it

Two stages. Each is independently verifiable, and they are separable in the way
§12.5 asks.

**Stage 1 — told, not asked.** The whole identity fix, in place.

- `DotfilesRoot()` becomes `BuildCheckout()`, read by `version` and reported by
  `doctor`; behaviour stops reading it.
- `AGENTS_DOTFILES_ROOT` is deleted, not deprecated.
- `main.go`'s `argv[0]` multicall is replaced by
  `agents githook <name> --checkout <path>`.
- `install-hooks.sh` generates four executable `sh` entries and `chain.env`,
  still inside the checkout's `git/hooks.d/`, and `doctor` reports the machine
  from `core.hooksPath` and the chain instead of from a root.
- Tests: `root_test.go` replaced; `githook_main_test.go`'s four environment sites
  become arguments; `install_hooks_test.go` rewritten in shape with the §10
  policies kept; the exact command set gains `githook`; the README block is
  regenerated; and a new end-to-end test — temporary home, temporary repository,
  real `git`, real chain — which the `hygiene` job then runs under its
  synthetic-home guard.
- CI: no workflow change. The `docs` job's names follow the renames through
  `doctests.txt`; the `hygiene` jobs fail if a new test writes into `HOME`.
- Rollback: remove the chain directory, then re-run the previous installer. The
  previous installer refuses entries that are not symlinks, so the chain has to
  go first — by design, and worth saying in the runbook.
- Upgrade on a machine that already has the symlinks: the installer recognises
  them as its own (they resolve into an agents keg, or name the binary it was
  handed) and replaces them under `--adopt-owned`. This machine's four links
  qualify.

**Stage 2 — machine-owned.** Small once stage 1 exists.

- The chain moves out of the checkout to the directory §12.1 decides.
- `core.hooksPath` is repointed, and the entries name the checkout that supplies
  the personal stages.
- `git/hooks.d/` retires; bootstrap's devtools phase, `git/README.md` and the
  installer's argument list follow.
- The guard now survives a moved or deleted checkout, and a missing binary is
  loud at commit time rather than silent.

Stage 1 does not fix the failure measured in §1.3: the chain still lives where
git can delete it, so a deleted checkout still switches the guard off in silence.

## 12. Open decisions

1. **The chain directory's path.** It must be machine-owned, survive package
   upgrades, and not collide with another writer. `~/.config/agents/` is taken by
   unrelated `.env` files on this machine, so it is out. `$XDG_STATE_HOME/agents`
   (default `~/.local/state/agents`) is the recommendation: state, not config,
   because the installer generates it. Decide once and let `bootstrap`'s manifest
   name it, so the path has one owner.
2. **The subcommand's name.** `agents githook` is used above. It must not be
   `hook`, which names the deleted harness-lifecycle entry point.
3. **`~/.gitattributes`.** It is machine-global and points into a checkout, so it
   has the same shape of problem as the chain. This document leaves it as it is —
   a link into the checkout, reported by `doctor` — and records the question
   rather than answering it.
4. **Compatibility.** Whether to accept `AGENTS_DOTFILES_ROOT` for one release
   with a printed deprecation, or refuse it immediately. Refusing is cleaner and
   this is a one-machine tool; accepting hides the change from the one person who
   needs to see it.
5. **Scope.** §11 already takes it in two stages: stage 1 is the identity fix and
   lands on its own; stage 2 is the location change and is small once stage 1
   exists. The sequence is a decision, so it is argued there and not restated
   here.
