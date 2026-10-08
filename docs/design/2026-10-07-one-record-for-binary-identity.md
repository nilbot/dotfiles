# Binary identity: one record, no modes

**Date:** 2026-10-07
**Status:** draft for review. No code changed.
**Replaces:** the two-mode resolution in [binary identity and standalone resolution](2026-08-28-binary-identity-and-standalone-resolution.md).
**Depends on:** the boundary between `agents` and `bootstrap` ([the boundary document](2026-09-20-agents-and-bootstrap-boundary.md)).

---

## 1. What this is meant to do

`agents` is a small command-line tool that prepares a Git repository for AI coding
agents: it writes the repository's context files, and it installs a **commit
guard** — a check that runs before a commit and refuses it if the staged changes
contain a secret.

Git runs those checks through *hooks*. A hook is a program that Git runs at a fixed
moment, named after that moment: `pre-commit`, `commit-msg`, `post-merge`,
`post-checkout`. Git finds them by looking in one directory, which the setting
`core.hooksPath` names. There is no way to tell Git "run this program for hooks" —
only "look in this directory".

There are two builds of `agents` on a machine like this one:

- a **personal build**, compiled from a checkout of this repository, used by the
  person who owns the checkout;
- a **released build**, installed by Homebrew, used by anyone.

Both are the same program. The personal build additionally knows *which checkout
it came from*, and uses that to run the checkout's own extra hook stages.

**The situation this document fixes.** That knowledge is currently expressed three
different ways, with three different owners, and the three can disagree:

| the question | who answers it today |
|---|---|
| which file will Git run for a hook? | the shell installer, which records the answer as four symbolic links |
| which checkout does this binary belong to? | the binary itself, from a value compiled into it, or from an environment variable |
| is the `agents` on `PATH` the one that is running? | `agents doctor`, by comparing files |

**What we want to be true instead.** A run's behaviour should be decided by *how
the machine is wired* — which directory Git is told to look in, and what that
directory says — and not by which process launched `agents`, not by an environment
variable, and not by where the binary happens to sit. A person should be able to
read one file and know exactly which binary Git will run and which checkout
supplies the extra stages. If that wiring is broken, the failure should be loud,
and for a guard it should fail **closed**: a missing guard must block the commit,
not silently permit it.

### Glossary

- **Chain** — the directory `core.hooksPath` names, together with the files in it.
  This document uses "the chain" for that directory and its contents, and nothing
  else.
- **Entry** — one file in the chain, named after one hook event. Git executes it.
- **Record** — the two-line file inside the chain that states which binary to run
  and which checkout supplies the extra stages.
- **Personal stage** — an extra program in `<checkout>/git/hooks/`, named
  `<anything>.<hook>` (for example `recent.post-merge`). The tool runs these after
  the repository's own hook.
- **The stamp** — the checkout path compiled into a personal build, written by the
  linker flag `-X main.dotfilesRoot=<path>`. It exists to answer "what built me".
- **Mode** — today's word for the two behaviours: "Operator Mode" (a personal build
  that also runs the machine-level checks and the personal stages) and "Standalone
  Mode" (a released build that does not). This design deletes the modes.
- **Keg** — Homebrew's name for its versioned install directory,
  `Cellar/agents/<version>/`. Homebrew deletes the old keg when it upgrades, so a
  chain that names a keg path breaks on the next upgrade.

## 2. What we cannot change

Five items decide the shape of this design: four facts about Git, Homebrew and
this repository's own configuration, and one rule this repository holds itself to.
Each item states the fact, the evidence for it, and the consequence it forces. The table at the end
records decisions, not constraints.

**The version we would prefer.** Hook installation would be a single command with
no launcher directory, and the tool would learn which checkout it belongs to from
where it is installed. Neither is available. Section 5 describes the full picture.

### Facts about the platform and this repository

**F1. Git runs hooks by looking in a directory.** `core.hooksPath` names one
directory, and Git executes the file in it whose name matches the hook event. Git has no setting that names a program. `git/install-hooks.sh`, the shell
installer, must therefore create a directory and keep four executable files
correct inside it. The launcher directory is the
only shape Git supports, and it is not a choice this design made.

**F2. An installed binary's path does not say which checkout built it.** The
personal build is installed at `~/bin/agents`, the released build at
`/opt/homebrew/bin/agents`. A checkout appears in neither path, so `realpath`,
`command -v` and every other derivation fail to recover one. The checkout has to be
recorded when the binary is built (`Makefile:38-41`,
`bootstrap.d/internal/phase/devtools.go:88`), or stated when it is installed.

**F3. An upgrade deletes the binary the hooks point at, and Git reports
nothing.** On 2026-09-20 the four installed links named
`Cellar/agents/0.5.1/bin/agents`. `brew upgrade` deleted that directory, and the
next `git commit` exited 0 with no warning while the commit guard was gone
([the incident](../qna/why-does-a-brew-upgrade-stop-my-commit-guard.md)). Anything
recorded must therefore name Homebrew's stable `bin/agents` symlink, which is
repointed on every upgrade. The installer refuses a resolved keg path for this
reason ([why it refuses
one](../qna/why-does-install-hooks-refuse-symlinks-like-opt-homebrew-bin-agents.md)).
The first repair anyone reaches for, `realpath $(which agents)`, fails here, and
the table below records it.

**F4. `AGENTS_DOTFILES_ROOT` is set by hand, and no test supplies it honestly.**
The variable is a hand-written line in `~/.config/fish/config.fish:19`, inside a
file that `bootstrap` copies once and never rewrites
(`bootstrap.d/links.manifest:29`). No tracked file writes it into the
environment: `git log -S AGENTS_DOTFILES_ROOT -- bootstrap.d fish` returns nothing,
and `Makefile:35` mentions the variable in a comment, which installs nothing. The
only tracked mentions are documentation and tests, and the tests show the cost.
Eight lines across two test files set the variable, or spawn a child with a
fabricated `HOME`, to reach the code under test. Measured 2026-10-07: with the
variable set, `agents doctor` reports 18 checks; with it unset, 13. The five that disappear are
`root:exists`, `git-hooks:global`, `git-hooks:effective`, `git-hooks:links` and
`git-hooks:unmanaged`, and no line of output says which behaviour is in force.

### A rule we hold ourselves to

**R1. A check must not be derived from the value it compares against
(`core.hooksPath`).** `agents/root.go:12`
records the argument: the machine-level check compares `core.hooksPath` against the
known root, so a root derived from `core.hooksPath` would pass by construction. The
design pays for the rule twice. It keeps the build-time stamp, the one statement of
the binary's origin that the hooks path cannot influence. It gives up the
machine-level check that compared the stamp against the filesystem, and section 4
accounts for that as a cost.

### Alternatives we rejected

The items above rule out several designs that look simpler. This table records each
decision with the fact behind it, so that nobody proposes the alternative again
without the reason.

| alternative | the fact that rules it out |
|---|---|
| `realpath $(which agents)` | F3: it resolves to the keg the next upgrade deletes |
| `command -v agents` inside a chain entry | F4: a lookup re-decides on every commit, against an environment the record cannot see |
| keep `AGENTS_DOTFILES_ROOT` as an override | F4: an override the common case depends on is the mechanism, not an override |
| `$HOME/dotfiles` when it exists | removed 2026-08-28 for F4's reason: it switched on the operator behaviour on any machine with that directory |
| keep the chain inside the checkout | F3's mechanism in a new place: when `core.hooksPath` names a missing directory, Git commits at exit 0, so a deleted checkout switches the guard off silently |
| a second config file beside `core.hooksPath` | `bootstrap.d/links.manifest:10` states the rule: "one owner per path" |


## 3. What we will build

### 3.1 The chain holds a record and four entries

The chain lives at **`~/.config/agents/hooks.d/`** — machine-owned, outside any
checkout, and not a cache directory, because a cleaner must never be able to delete
the commit guard. Git reaches it through `core.hooksPath`.

It holds a record, `chain.env`, and the record is **data, not code**:

```sh
# Written by git/install-hooks.sh. Re-run the installer to change it.
format=1
binary=/opt/homebrew/bin/agents
checkout=/Users/nilbot/dotfiles
```

`binary` is the program Git will run. `checkout` names the directory whose
`git/hooks/` holds the personal stages, or `-` when there are none. Values are
taken literally: one `key=value` per line, no quoting, no expansion, and nothing
after the first `=` is interpreted. That is what lets a path containing a space be
recorded at all, which the repository already supports elsewhere
(`agents/install_hooks_test.go` installs twice with space-containing paths).

And four entries — `pre-commit`, `commit-msg`, `post-merge`, `post-checkout` —
each an executable POSIX shell script generated by the installer:

```sh
#!/bin/sh
# Written by git/install-hooks.sh for /Users/nilbot/dotfiles.
chain=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || exit 1
format=; binary=; checkout=
while IFS='=' read -r key value; do
  case "$key" in
    ''|\#*) ;;
    format)   format=$value ;;
    binary)   binary=$value ;;
    checkout) checkout=$value ;;
    *) echo "agents: unknown key '$key' in $chain/chain.env" >&2; exit 1 ;;
  esac
done < "$chain/chain.env" || exit 1
[ "$format" = 1 ] || { echo "agents: $chain/chain.env is not format 1" >&2; exit 1; }
case "$binary" in /*) ;; *) echo "agents: binary in $chain/chain.env is not an absolute path" >&2; exit 1 ;; esac
[ -f "$binary" ] && [ -x "$binary" ] || {
  echo "agents: the commit guard is not installed: $binary is missing or not executable" >&2
  echo 'agents: repair with: bash /Users/nilbot/dotfiles/git/install-hooks.sh install --adopt-owned /Users/nilbot/dotfiles "$HOME" "$(command -v agents)"' >&2
  exit 1
}
case "$checkout" in -|/*) ;; *) echo "agents: checkout in $chain/chain.env is neither - nor an absolute path" >&2; exit 1 ;; esac
exec "$binary" githook pre-commit --checkout "$checkout" "$@"
```

**The record must never be sourced.** `. "$chain/chain.env"` would run the file as
shell code at every commit, with the committing user's privileges, so a record
carrying `: > /tmp/pwned` would execute it — and it would let a record rewrite
`PATH` or `IFS` inside the process that is about to decide whether the commit is
allowed. A symbolic link cannot execute anything; this can. The allow-list above
exists for that reason, and every rejection exits non-zero: rule 4 of §2 says
ambiguity refuses, and a truncated record (which is what a concurrent write leaves)
must refuse rather than forward an empty `--checkout`.

An entry replaces a symbolic link, and buys three things a symlink cannot:

1. **It states the binding.** `cat` the chain and you know which binary will run
   and which checkout supplies the personal stages. No mode, no environment, no
   inference from the shape of a path.
2. **It fails loudly, and closed where it must.** `pre-commit` and `commit-msg`
   exit non-zero when the binary is missing, so a broken guard blocks the commit.
   `post-merge` and `post-checkout` report and exit 0, because a missing banner
   must not fail a pull.
3. **It outlives the checkout.** With `checkout` unreadable, `pre-commit` still
   runs the built-in check and reports the personal stages as missing. Today,
   deleting the checkout deletes the guard.

### 3.2 The binary is told, not asked

`agents githook <name> --checkout <path>` runs one hook: the repository's own hook
first, then the personal stages under `<path>/git/hooks/` when `<path>` is not `-`,
then the built-in stages. It reads no record and derives nothing. This replaces the
current *multicall* — one binary behaving as different programs according to the
name it was invoked under (`agents/main.go:24`) — which is the implicit binding
this design removes. The tool does not gain a way to be a hook; it changes its one
way.

`AGENTS_ACTIVE_GIT_HOOKS` stays, and this is deliberate. It is not a
configuration input: the tool sets it for its own child processes so that a
repository hook which calls `agents` again cannot recurse forever
(`agents/internal/githook/githook.go:30`). It is a marker the tool writes for
itself, and it will be documented and tested as one.

### 3.3 The stamp becomes provenance

The linker stamp keeps one job: saying which checkout built this binary.
`agents version` prints it (`built from /Users/nilbot/dotfiles`, or `release build`
for a released binary — the exact format is an open question, §8.1). `agents
doctor` reports it, and warns when the named directory no longer exists, which is
the hazard `Makefile` documents for a binary built in a linked worktree.

It never changes a decision. A stamped binary and an unstamped binary, given the
same chain, do the same thing.

The linker variable keeps its current name, `main.dotfilesRoot`. Renaming it would
touch four files that hardcode the string (`Makefile:40`,
`bootstrap.d/internal/phase/devtools.go:88`, `bootstrap.d/makefile_test.go:110,165`,
`bootstrap.d/internal/phase/devtools_test.go`) for no behavioural gain.

### 3.4 `doctor` reports the machine; it does not hold a root

`doctor` answers one question — *what is installed here?* — from `core.hooksPath`
and the chain, instead of from a compiled-in root:

| check | reads | replaces |
|---|---|---|
| `chain:hooks-path` | `git config --global core.hooksPath` | `git-hooks:global`, `git-hooks:effective` |
| `chain:entries` | each of the four names is a regular file carrying the generated header **and is executable** — Git skips a non-executable hook with a hint and exit 0 | `git-hooks:links` (which today *requires* a symlink, `doctor.go:632`) |
| `chain:record` | `chain.env` parses under the allow-list: `format=1`, `binary` an absolute path to an executable regular file, `checkout` `-` or absolute | new |
| `chain:unmanaged` | look-alikes under other names, **and dangling links** an earlier installer left behind | `git-hooks:unmanaged` |
| `chain:local` | a repository-local `core.hooksPath` override, which bypasses the chain by choice | `git-hooks:local` |
| `chain:legacy` | the exact retired dispatcher, if one is still installed | `git-hooks:legacy` |
| `chain:running` | the binary the record names beside the binary that is running; **fails when they are not the same file** | the old `binary` check's implicit "the" binary |
| `attributes:global` | `~/.gitattributes` is a link whose target exists | `git-attributes` |
| `provenance:checkout` | the stamped directory, if any, still exists (a warning, where `root:exists` was a failure) | `root:exists` |

### 3.5 Who reads what

| consumer | the fact it needs | reads it from | must never read it from |
|---|---|---|---|
| Git | which entries to run | `core.hooksPath` | — |
| an entry | what to execute | `chain.env` | `PATH`, the environment, its own name |
| `agents githook` | where the personal stages are | the `--checkout` its entry passed | the stamp, the environment |
| `agents doctor` | what is installed | `core.hooksPath`, the entries, `chain.env` | a stamped root; the environment |
| `agents version` | what built this binary | the stamp | — |
| `init`, `wire` | which repository this is | the working directory | anything machine-level |

### 3.6 What failure looks like

| what is wrong | what happens | who says so |
|---|---|---|
| the named binary is missing or not executable | guard entries name it and exit 1; observational entries name it and exit 0 | the entry |
| `chain.env` is missing or malformed | every entry names the file and exits 1 | the entry |
| the checkout is gone | the guard still runs; the personal stages are reported missing | the entry, and `doctor` |
| the chain names a binary other than the running one | both printed as facts | `doctor` |
| a repository sets its own `core.hooksPath` | the chain is bypassed; that is a deliberate local choice | `doctor` (`chain:local`) |
| the chain **directory** is gone | nothing runs; Git reports nothing | `doctor` — the one remaining silence (§4) |

## 4. What we accept today

| what we give up | what we get | what would let us revisit it |
|---|---|---|
| Four shell scripts instead of four symlinks: one extra process on every hook run, and a `/bin/sh` dependency | a binding a person can read, a loud failure, and a guard that fails closed | nothing soon; the cost is milliseconds against a guard that runs a secret scanner |
| A new machine-level directory (`~/.config/agents/hooks.d`) that the installer must create | the guard survives moving or deleting the checkout | if Git ever accepts a program instead of a directory |
| An operator can no longer point a binary at a checkout with an environment variable | no ambient modes, and a test suite that needs no fabricated environment | never, by design — this is the defect being removed |
| `root:exists` was a **failure** when the stamped checkout was missing; `provenance:checkout` is a **warning** | one honest check instead of one that compares two values that the same change can move together | a genuinely independent second statement of identity, which does not exist once the chain is the only record |
| The chain's correctness on a machine where `doctor` is never run | — | nothing: a deleted chain directory cannot announce itself, because the only thing that could announce it lives inside it |

Two of these are worth saying outside the table.

**The independence we lose is narrow, and one independence we must keep.** Today
`root:exists` compares a value compiled into the binary against the filesystem —
two statements that can disagree, which is why it is a real guard. Under this
design the chain is the only record of which checkout supplies the personal
stages, so nothing can independently confirm that part; the stamp survives as
provenance and `doctor` prints the two side by side.

What must **not** be softened is `chain:running`. The check that failed during the
`brew upgrade` incident — "the installed hook does not resolve to the current
binary" (`doctor.go:640`) — compares two independently obtained facts: the binary
the record names, and the executable that is running. Replacing it with a report
that "neither is a failure" would make `doctor` print `ok` while the binary you
are running is not the binary your commits run, which is the disagreement this
whole design exists to surface. It stays a failure, and it prints both paths.

**The remaining silence is a Git property, not a design choice.** If the chain
directory is deleted, Git looks in a directory that is not there and runs nothing,
at exit 0. Measured 2026-10-07. The mitigation is that the directory is
machine-owned and no package manager touches it; the detector is `doctor`.

## 5. The ideal world

If the constraints in §2 did not exist, this is what we would have:

- **One record, one writer, one reader each.** The machine's wiring states which
  binary Git runs and which checkout it belongs to; the binary asks nothing of its
  environment; `doctor` reports what the record says and never reconstructs it.
- **No stamp at all.** The checkout would be a property of the installation rather
  than of the build, so a released binary and a personal build would differ only in
  what the record names — not in what they contain or how they behave.
- **No shell in the chain.** Git would name a program; the guard would be a single
  statement instead of four generated files.
- **The guard could not be absent.** Installing the tool would install the guard,
  and removing it would be an explicit act with a visible consequence.
- **One model in the documentation.** "Mode" would not appear anywhere, because
  behaviour would follow from the wiring rather than from the build.

The two-stage plan in §6 is the path from here to there. Stage 1 removes the modes
and makes the binding explicit. Stage 2 makes the chain machine-owned and the guard
independent of any checkout. Neither stage needs a Git feature that does not exist;
both are constrained only by C1–C5.

## 6. What changes, in what order

**Stage 1 — told, not asked.** The identity fix, in place.

- `DotfilesRoot()` becomes `BuildCheckout()`, read only by `version` and reported by
  `doctor`; `AGENTS_DOTFILES_ROOT` is deleted and never read.
- `main.go`'s `argv[0]` multicall is replaced by `agents githook <name> --checkout
  <path>`, registered with the `Git`/`CI` audience and hidden from the human
  listing, exactly as `guard` is today.
- `git/install-hooks.sh` generates four entries and `chain.env` instead of four
  symlinks, and still writes `core.hooksPath` last so a partial install cannot
  activate an incomplete chain.
- `doctor` reports the machine per §3.4 instead of holding a root.
- The chain stays in `git/hooks.d/` for this stage; the entries do not care where
  they live.

**Stage 2 — machine-owned.** Small once stage 1 exists.

- The chain moves to `~/.config/agents/hooks.d/`, and the installer creates that
  directory (it currently refuses unless the directory already exists, which a
  fresh machine would not satisfy — `git/install-hooks.sh` requires "an existing
  real directory").
- `core.hooksPath` is repointed; `git/hooks.d/` retires, and its `.gitignore` with
  it.
- The guard now survives a moved or deleted checkout. Stage 1 does not achieve
  this.

**The transition window, and how it is closed.** The binary and the chain update
through different channels: `brew upgrade agents` replaces the binary, while
`git pull` plus re-running the installer replaces the chain. Both orders break, and
both must be handled rather than hoped away:

| order | what happens |
|---|---|
| new binary first, old symlinks installed | the symlinks still resolve, so Git runs the new binary as `pre-commit` with no arguments; with the multicall gone it prints a usage error and exits non-zero, so **every commit on the machine fails** — loud, but with a misleading message |
| installer first, old binary on `PATH` | the entries call `agents githook`, which the old release does not have; the same machine-wide failure, but the installer reports success first |

Two changes close it, and both belong in stage 1:

1. **The installer probes the binary before writing anything.** It runs the binary
   with a known argument and refuses if the binary cannot answer `githook`. A
   mismatched pair then fails at install time, with a message that says so, instead
   of at the first commit.
2. **The new binary keeps answering a bare `pre-commit` invocation for one
   release**, so an upgrade that lands before the installer re-runs still commits.
   This is a compatibility shim with a stated removal date, not a second design.

**Rollback, measured.** "Remove the chain directory, then re-run the previous
installer" does not work, and the state it leaves is this design's own remaining
silence: with the directory gone, `core.hooksPath` names nothing, every commit
succeeds at exit 0 with no output, and the previous installer then refuses because
it requires the hooks directory to be an existing real directory
(`git/install-hooks.sh:234`). The working sequence is:

- **Stage 1** (chain still inside the checkout): delete the four entries and
  `chain.env` from `git/hooks.d/`, but keep the directory and its tracked
  `.gitignore`, then run the previous installer. Measured: with the names absent it
  succeeds, and the guard is off for zero commits.
- **Stage 2**: recreate the four symlinks in `<root>/git/hooks.d/` **first**, then
  point `core.hooksPath` back at that directory, and only then delete the chain
  directory. Repointing before the symlinks exist is the silent state again.

Rollback reverses both halves, not one — the chain *and* the binary. Re-running the
previous installer while the new binary is on `PATH` records that binary into four
symlinks, which it cannot answer.

**Entries are published atomically.** A hook name that exists but is not executable
is skipped by Git with a hint and **exit 0** — silence, and the hint can be turned
off with `advice.ignoredHook=false`. No entry may be visible in a partial or
non-executable state: each is written to a temporary file in the same directory
with its final mode and `mv`ed into place, and `chain.env` the same way. `ln -sfn`
was atomic; its replacement has to be too.

**One caution for the repair text.** On this machine "re-run the installer" is not
one action: `bootstrap`'s devtools phase installs the chain against
`~/bin/agents`, a checkout build (`bootstrap.d/internal/phase/devtools.go:50`), so
following that advice also changes *which binary Git runs*. The repair line should
name the binary it is reinstalling, or say that it will switch to the checkout
build.

**The map of the change** — every artifact, with what it pins and what it becomes —
is a checklist for whoever does the work, and lives in the implementation plan
(`docs/plans/`) rather than here: it will be stale the moment the first rename
lands, and it names test functions that mean nothing to a reader of this design.
Four facts from it belong here, because they shape the work:

- The change reaches **five design documents still in force** that restate the
  deleted contract (`docs/design/2026-08-11-spec-6-…:90-97`,
  `2026-08-11-spec-5-…:734-790`, `2026-08-07-agents-repo-context-design.md` §8,
  `2026-09-20-agents-and-bootstrap-boundary.md` §3, and
  `2026-08-28-contributor-guardrails-…:17`), and nothing tests `docs/design/`, so
  they drift in silence unless the same change amends them.
- **`release.yml:138` pins the prefix of `agents version`'s output** in a glob, so
  changing that line can fail a release *after* it is published.
- `agents/doctests.txt` is enforced **by name** by the CI `docs` job: every listed
  test must exist and pass, so a renamed test must be edited there in the same
  commit or the job fails naming it.
- The `hygiene (agents, home)` job runs the suite with `HOME` pointed at an empty
  directory and fails if anything appears in it. That is §2's rule about ambient
  inputs, already enforced over the tests — the design extends it to the tool.

## 7. How we would know it works

Each test states the mutation that should make it fail, because a guard that has
only ever been green is not known to be a guard.

| # | test | mutation that must make it fail |
|---|---|---|
| 1 | the recorded binary is deleted, then a commit is attempted: the commit is refused, the output names the missing path and the repair command, and the exit status is non-zero | make the entry exit 0; the commit then succeeds |
| 2 | `chain.env` is emptied: every entry refuses, naming the file | default `checkout` to `.` and watch the wrong directory be consulted |
| 3 | the checkout is deleted: the built-in guard still runs and the personal stages are reported missing | make the entry require the checkout; the guard disappears with it |
| 4 | `AGENTS_DOTFILES_ROOT` set to a second checkout: nothing changes | read the variable; the check count moves between 18 and 13 |
| 5 | the installer is handed a binary that cannot answer `githook`: it refuses and writes nothing | remove the probe; the chain is installed and the next commit fails |
| 6 | the installer is handed a keg path while a stable path is installed: it refuses and names the stable path. Handed a keg path with no stable path, it installs and prints a note | delete the refusal; the next upgrade dangles. Delete the note too, and the only person who has one `agents` cannot install a guard at all |
| 7 | a record whose line is a shell command: the command does **not** run, and the entry refuses | source the record instead of parsing it; the command runs at commit time |
| 8 | a record truncated after `binary`: every entry refuses, naming the missing key | default the absent keys; an empty `--checkout` is forwarded and the wrong directory, or none, is consulted |
| 9 | a record whose `binary` names a directory: the entry refuses | test `[ -x ]` alone, which is true for a directory; `exec` then fails with exit 126 |
| 10 | a record naming a binary whose path contains a space: it runs | quote-strip or word-split the value; the space path fails with `not found` |
| 11 | an entry whose mode is not executable: `doctor` reports it, and a commit is refused while the guard cannot run | write the entry and `chmod` after; Git skips it with a hint and the commit succeeds |
| 12 | the chain names a binary other than the running one: `doctor` **fails** and prints both paths | report both as facts with no failure; the machine looks healthy while commits run a different binary |
| 7 | two binaries are installed and the other one is run: `doctor` prints both, naming which is wired and which is running, and reports neither as a failure | derive the running binary from the chain; the two facts collapse into one |
| 8 | `core.hooksPath` is unset: `doctor` reports that no chain is installed, and the repository-level checks still run | make the absence an error; `doctor` fails on a machine that never had a chain |

Placement is itself a contract. Contracts about prose, the command tree and the
generated README block belong in `agents/doctests.txt`, which the CI `docs` job
runs by name. Package behaviour belongs in the ordinary `test` job — which also
puts these tests under the `hygiene` job's synthetic-`HOME` guard.

Two of these cannot pass in stage 1 and should not be written as though they could:
test 3 needs the chain to be outside the checkout, and test 12's two binaries are
most easily distinguished once the chain is machine-owned. Tests 7–11 are stage 1
tests, because they are about the entry and the record, not about where the chain
lives.

## 8. Open questions

1. **The exact text `agents version` prints.** `release.yml:138` matches
   `"agents ${VERSION} (commit: ${SHA},"*`, so appending the checkout after the
   existing fields keeps the glob matching, while reordering or replacing `built:`
   with `built from` breaks a release after publication. Recommendation: append,
   and change the glob only when changing it is deliberate.
2. **How long the compatibility shim of §6 lives.** It should carry a version and
   a removal date in its comment, or it becomes permanent.
3. **`~/.gitattributes`.** Its content is deliberately empty of rules
   (`git/gitattributes` says so), and `core.attributesFile` is unset, so it marks
   nothing. Recommendation: stop managing it — delete the link, the file, the
   installer's step and `doctor`'s `attributes:global` check — and leave the
   per-repository rule, which `agents init` writes and the CI `context` job checks.
4. **Who creates the chain directory on a fresh machine.** The installer must, but
   `bootstrap`'s manifest states that `core.hooksPath` is deliberately absent from
   it because the installer owns it. Creating the directory is therefore the
   installer's job, and the manifest's comment stays true.
5. **Two defects found while reviewing this, both outside its scope.** The
   `livingDocuments` list in `agents/docs_test.go` walks `claude/skills`, which no
   longer exists, and discards the walk error — so that guard covers one skill tree
   where it claims two. And `AGENTS_ACTIVE_GIT_HOOKS` is undocumented; the design
   keeps it deliberately (§3.2) and it should be written down wherever the hook
   contract is.
