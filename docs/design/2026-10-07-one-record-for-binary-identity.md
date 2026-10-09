# Binary identity: one record, no modes

**Date:** 2026-10-07, rewritten 2026-10-09
**Status:** version three. Attacked by an adversarial reviewer; the migration surface
measured on a wired machine. No code changed.
**Replaces:** the two-mode resolution in
[binary identity and standalone resolution](2026-08-28-binary-identity-and-standalone-resolution.md).
**Depends on:** the boundary between `agents` and `bootstrap`
([the boundary document](2026-09-20-agents-and-bootstrap-boundary.md)).

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
- **Entry** — one file in the chain, named after one hook event. Git executes it.
- **Record** — the file `chain.env` inside the chain: a comment line, then
  `format`, `binary` and `checkout`. It states which binary to run and which
  checkout supplies the extra stages.
- **Personal stage** — an extra program in `<checkout>/git/hooks/`, named
  `<anything>.<hook>` (for example `recent.post-merge`). The tool runs these after
  the repository's own hook.
- **Repository hook** — the hook file a repository keeps in its own hooks directory
  for an event. The dispatcher runs it before the personal stages, and skips it only
  when that file *is* the dispatcher.
- **Dispatcher** — the program a chain entry execs: `agents githook <name>`. It runs
  the repository's own hook, then the personal stages, then the built-in stages.
  Before this change the dispatcher was the `agents` binary invoked under the hook's
  own name; a **retired dispatcher shim** is an older dispatcher file that a
  repository's hooks directory may still hold, and `doctor` reports one as
  `git-hooks:legacy` today and `chain:legacy` after §3.4's rename.
- **Installer mode** — `git/install-hooks.sh` runs as `preflight` or `install`.
  `preflight` reports and exits before the binary is built, and is deliberately
  blind to it; `install` runs after the build and validates the binary it is handed.
- **`--adopt-owned`** — the installer flag that permits replacing a hook entry this
  installer previously wrote. Without it, anything already at a hook name is a
  refusal.
- **The stamp** — the checkout path compiled into a personal build, written by the
  linker flag `-X main.dotfilesRoot=<path>`. It answers "what built me".
- **Mode** — today's word for the two behaviours: "Operator Mode" (a personal build
  that also runs the machine-level checks and the personal stages) and "Standalone
  Mode" (a released build that does not). This design deletes the modes.
- **Keg** — Homebrew's name for its versioned install directory,
  `Cellar/agents/<version>/`. Homebrew deletes the old keg when it upgrades, so a
  chain that names a keg path breaks on the next upgrade.
- **Multicall** — today's dispatch: one binary behaving as different programs
  according to the name it was invoked under (`agents/main.go:24`).
- **Harness hook entry** — a different mechanism from the chain: an entry in a
  repository's `.claude/settings.json`, `.codex/hooks.json` or `.agents/hooks.json`
  naming a command for one harness's lifecycle event. §3.5 records what happens to
  them.

## 2. What we cannot change

Five items decide the shape of this design: four facts about Git, Homebrew and
this repository's own configuration, and one rule this repository holds itself to.
Each item states the fact, the evidence for it, and the consequence it forces. The
table at the end of this section records the alternatives this rejected; it is a
record of decisions, not a constraint.

**The version we would prefer.** Hook installation would be a single command with
no hooks directory, and the tool would learn which checkout it belongs to from
where it is installed. Neither is available. Section 5 describes the full picture.

### Facts about the platform and this repository

**F1. Git runs hooks by looking in a directory.** `core.hooksPath` names a path,
and Git executes whatever file it finds at each hook name inside that directory —
`git help config` documents the setting as a path and names no sibling that takes a
program. The directory is therefore not optional: `git/install-hooks.sh` must keep
four files correct at those names, and the chain names one binary through what
those four files contain, not through any setting.

**F2. An installed binary's path does not say which checkout built it.** The
personal build is installed at `~/bin/agents`, the released build at
`/opt/homebrew/bin/agents`. A checkout appears in neither path, so `realpath`,
`command -v` and every other derivation fail to recover one. The checkout has to be
recorded when the binary is built (`Makefile:40`,
`bootstrap.d/internal/phase/devtools.go:88`), or stated when it is installed.

**F3. An upgrade deletes the binary the hooks point at, and Git reports nothing.**
On 2026-09-20 the four installed links named `Cellar/agents/0.5.1/bin/agents`.
`brew upgrade` deleted that directory, and the next `git commit` exited 0 with no
warning while the commit guard was gone
([the incident](../qna/why-does-a-brew-upgrade-stop-my-commit-guard.md)). Anything
recorded must therefore name Homebrew's stable `bin/agents` symlink, which is
repointed on every upgrade.

The installer handles this in two pieces, and only one of them is a refusal. With a
stable path already installed, it refuses a resolved keg path
(`git/install-hooks.sh:158-160`). With nothing installed, it accepts the keg path,
records it, and prints a note telling the reader to use the stable path
(`:281-287`). The first repair anyone reaches for, `realpath $(which agents)`, is
what the refusal exists to stop, and the table below records it.

**F4. `AGENTS_DOTFILES_ROOT` is set by hand, and no test supplies it honestly.**
The variable is a hand-written line in `~/.config/fish/config.fish:19`, inside a
file that `bootstrap` copies once and never rewrites
(`bootstrap.d/links.manifest:29` declares it a `seed` row — a file `bootstrap`
writes once from a tracked template and never rewrites). The tracked template
`fish/config.fish.template` does not contain the line, so **no automated path
writes this variable or could rewrite it** — it is the only on-disk statement
binding this machine's installed binary to this checkout. No tracked file writes it
into the environment: `git log -S AGENTS_DOTFILES_ROOT -- bootstrap.d fish` returns
nothing, and `Makefile:35` mentions the variable in a comment, which installs
nothing. The only tracked mentions are documentation and tests, and the tests show
the cost: eight lines across two test files set the variable, or spawn a child with
a fabricated `HOME`, to reach the code under test. Measured 2026-10-07: with the
variable set, `agents doctor` reports 18 checks; with it unset, 13. The five that
disappear are `root:exists`, `git-hooks:global`, `git-hooks:effective`,
`git-hooks:links` and `git-hooks:unmanaged`, and no line of output says which
behaviour is in force.

### A rule we hold ourselves to

**R1. A check must not be derived from the value it compares against
(`core.hooksPath`).** `agents/root.go:12` records the argument: the machine-level
check compares `core.hooksPath` against the known root, so a root derived from
`core.hooksPath` would pass by construction. The design pays for the rule twice. It
keeps the build-time stamp, the one statement of the binary's origin that the hooks
path cannot influence. It gives up the machine-level check that compared the stamp
against the filesystem, and §4 accounts for that as a cost.

### Alternatives we rejected

| alternative | the fact that rules it out |
|---|---|
| `realpath $(which agents)` | F3: it resolves to the keg the next upgrade deletes |
| `command -v agents` inside a chain entry | F4: a lookup re-decides on every commit, against an environment the record cannot see |
| keep `AGENTS_DOTFILES_ROOT` as an override | F4: an override the common case depends on is the mechanism, not an override |
| `$HOME/dotfiles` when it exists | removed 2026-08-28 for F4's reason: it switched on the operator behaviour on any machine with that directory |
| keep the chain inside the checkout | F3's mechanism in a new place: when `core.hooksPath` names a missing directory, Git commits at exit 0, so a deleted checkout switches the guard off silently |
| a second config file beside `core.hooksPath` | `bootstrap.d/links.manifest:10` states the rule: "one owner per path" |

## 3. What we will build — the shape both stages end at

Section 6 stages the work; this section describes the end state. One part of it is
not true after stage 1 alone, and it is the part a reader is most likely to trip
on: **where the chain lives.** Stage 1 keeps it inside the checkout — the shape
§2's table records as an alternative this design rejects, and rejects because a
deleted checkout then switches the guard off silently — and stage 2 moves it to the
machine, which is what makes the guard outlive the checkout. The record, the
entries, the `githook` subcommand and the `doctor` checks are all built by stage 1.

### 3.1 The chain holds a record and four entries

The chain lives, once stage 2 has run, at **`~/.config/agents/hooks.d/`** —
machine-owned, outside any checkout, and not a cache directory, because a cleaner
must never be able to delete the commit guard. Git reaches it through
`core.hooksPath`.

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
# The checkout is read from our own header, so a broken record is still repairable.
{ IFS= read -r _shebang; IFS= read -r _header; } < "$0"
repair_root=${_header#*for }
repair_root=${repair_root%.}
fail() {
  printf 'agents: %s\n' "$1" >&2
  if [ -n "$repair_root" ]; then
    printf 'agents: repair with: bash %s/git/install-hooks.sh install --adopt-owned %s "$HOME" "$(command -v agents)"\n' "$repair_root" "$repair_root" >&2
  fi
  exit 1
}
chain=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || fail "cannot locate the chain directory"
[ -n "$repair_root" ] || repair_root=$chain
format=; binary=; checkout=
while IFS='=' read -r key value; do
  case "$key" in
    ''|\#*) ;;
    format)   format=$value ;;
    binary)   binary=$value ;;
    checkout) checkout=$value ;;
    *) fail "unknown key '$key' in $chain/chain.env" ;;
  esac
done < "$chain/chain.env" || fail "cannot read $chain/chain.env"
[ "$format" = 1 ] || fail "$chain/chain.env is not format 1"
case "$binary" in /*) ;; *) fail "binary in $chain/chain.env is not an absolute path" ;; esac
[ -f "$binary" ] && [ -x "$binary" ] || fail "the commit guard is not installed: $binary is missing or not executable"
case "$checkout" in -|/*) ;; *) fail "checkout in $chain/chain.env is neither - nor an absolute path" ;; esac
exec "$binary" githook pre-commit --checkout "$checkout" "$@"
```

Every rejection goes through `fail`: the chain directory cannot be resolved, the
record cannot be read, an unknown key, a wrong `format`, a relative or missing
`binary`, a bad `checkout`. All of them print the repair command and not just a
diagnosis, which is what the ownership rule below depends on — the command the
entry prints has to be able to repair the state the entry is reporting, and the
only failure a hand-run installer can fix is one it can recognise as its own.

Each entry passes its own hook name and **forwards Git's arguments unchanged**.
That matters for `commit-msg`, which Git calls with the path of the message file:
the entry's last line becomes
`exec "$binary" githook commit-msg --checkout "$checkout" "$@"`, and the file is
what the tool reads. `pre-commit` gets no arguments, and `post-merge` and
`post-checkout` get Git's.

**The record must never be sourced.** `. "$chain/chain.env"` would run the file as
shell code at every commit, with the committing user's privileges, so a record
carrying `: > /tmp/pwned` would execute it — and it would let a record rewrite
`PATH` or `IFS` inside the process that is about to decide whether the commit is
allowed. A symbolic link cannot execute anything; this can. The allow-list above
exists for that reason, and every rejection exits non-zero. The rule it enforces
belongs here rather than in §2, because it is about this file and not about the
platform: **a value the record does not define is a value the entry must not guess
at.** A truncated record — which is what a concurrent write leaves — must refuse
rather than forward an empty `--checkout`.

An entry replaces a symbolic link, and buys three things a symlink cannot:

1. **It states the binding.** `cat` the chain and you know which binary will run
   and which checkout supplies the personal stages. No mode, no environment, no
   inference from the shape of a path.
2. **It fails loudly, and closed where it must.** `pre-commit` and `commit-msg`
   exit non-zero when the binary is missing, so a broken guard blocks the commit.
   `post-merge` and `post-checkout` report and exit 0, because a missing banner
   must not fail a `git checkout` or a `git switch`.
3. **It outlives the checkout.** With `checkout` unreadable, `pre-commit` still
   runs the built-in check and reports the personal stages as missing. Today,
   deleting the checkout deletes the guard.

### 3.2 The binary is told, not asked

`agents githook <name> --checkout <path>` runs one hook in three parts: the
repository's own hook — the file the repository keeps in its own hooks directory,
which the dispatcher runs unless it is the dispatcher itself
(`agents/internal/githook/githook.go:110-119`, `:156-172`) — then the personal
stages under `<path>/git/hooks/` when `<path>` is not `-`, then the built-in
stages. It reads no record and derives nothing. This replaces the
multicall, which is the implicit binding this design removes. The tool does not
gain a way to be a hook; it changes its one way.

The name `githook` is chosen so that it cannot collide with the retired `hook`
subcommand: `agents/internal/harness/harness.go:120-145` still recognises the old
shape `<binary> hook <semantic> --harness <name>` so that `agents wire` can remove
it, and a new command reusing that word would be indistinguishable from an entry it
is supposed to delete.

`AGENTS_ACTIVE_GIT_HOOKS` stays, and this is deliberate. It is not a configuration
input: the tool sets it for its own child processes so that a repository hook which
calls `agents` again cannot recurse forever
(`agents/internal/githook/githook.go:30`). It is a marker the tool writes for
itself, and it will be documented and tested as one.

### 3.3 The stamp becomes provenance

The linker stamp keeps one job: saying which checkout built this binary.
`agents version` prints it (`built from /Users/nilbot/dotfiles`, or `release build`
for a released binary — the exact text is an open question, §9.1). `agents doctor`
reports it, and warns when the named directory no longer exists, which is the
hazard `Makefile` documents for a binary built in a linked worktree — a second
checkout of the same repository created by `git worktree add`, whose directory can
be removed while a binary compiled from it survives and still names it.

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
| `chain:entries` | each of the four names is a regular file carrying the generated header **and is executable** — Git skips a non-executable hook with a hint and exit 0 (measured 2026-10-09, Git 2.54) | `git-hooks:links` (which today *requires* a symlink, `doctor.go:632`) |
| `chain:record` | `chain.env` parses under the allow-list: `format=1`, `binary` an absolute path to an executable regular file, `checkout` `-` or absolute | new |
| `chain:unmanaged` | look-alikes under other names, **and dangling links** an earlier installer left behind | `git-hooks:unmanaged` |
| `chain:local` | a repository-local `core.hooksPath` override, which bypasses the chain by choice | `git-hooks:local` |
| `chain:legacy` | the exact retired dispatcher, if one is still installed | `git-hooks:legacy` |
| `chain:running` | the binary the record names beside the binary that is running; **fails when they are not the same file** | the old `binary` check's implicit "the" binary |
| `attributes:global` | `~/.gitattributes` is a link whose target exists | `git-attributes` |
| `provenance:checkout` | the stamped directory, if any, still exists (a warning, where `root:exists` was a failure) | `root:exists` |

**Path shape is not identity.** Today the binary is recognised in six different
ways: the compiled stamp, the environment variable, the name the process was
invoked as (`agents/main.go:24`), the basename in `isOwnedBinary`
(`agents/internal/harness/harness.go:174-176`, which accepts `agents`,
`agents-test-bin` and `agents-standalone`), the shape `*/Cellar/agents/*/bin/agents`
(`git/install-hooks.sh:191-208`), and content fingerprints of two retired shims
(`agents/internal/githook/githook.go:151-215`).

Two of those decide **which binary runs** — the stamp and the environment variable
— and this design replaces both, in different ways. The environment variable is
deleted. The stamp is not: it is still compiled in, `doctor` still reports it, and
no decision reads it (§3.3). That is why the two sections can both be right when
one says the stamp is replaced and the other says it is kept — what is replaced is
its authority over behaviour, not the value.

Three of the remaining four are kept deliberately, each for its own job: the
basename is how `agents wire` decides which harness entries are its own, the Cellar
path shape is how the installer decides what it may adopt, and the fingerprints are
how a dispatcher skips a retired shim and how `doctor`'s `git-hooks:legacy` warns
about one, with the removal itself left manual. The invoked name is not kept: §6
replaces the multicall, and the `argv[0]` branch survives only as the one-release
shim.

### 3.5 Who reads what

| consumer | the fact it needs | reads it from | must never read it from |
|---|---|---|---|
| Git | which entries to run | `core.hooksPath` | — |
| an entry | what to execute | `chain.env` | `PATH`, the environment, its own name |
| `agents githook` | where the personal stages are | the `--checkout` its entry passed | the stamp, the environment |
| `agents doctor` | what is installed | `core.hooksPath`, the entries, `chain.env` | a stamped root as the *location* of the install; the environment. The stamp answers a different question — what built this binary — and §3.4's `provenance:checkout` reports it as provenance |
| `agents version` | what built this binary | the stamp | — |
| `init`, `wire` | which repository this is | the working directory | anything machine-level |

The harness hook entries are a **separate contract**, and this design leaves them
alone. Nothing writes them any more — `Adapter.Wire` stopped doing so
(`agents/internal/harness/harness.go:71-79`) and `agents init` only calls `Wire`
(`agents/cmd_init.go:89`) — while `StripHooks` still removes them
(`harness.go:600`), so a repository wired before 2026-09-21 keeps working and
`agents wire` cleans the entries up. Nothing here breaks that, because the new
subcommand is `githook` and the removal matcher keys on `hook`.

### 3.6 What failure looks like

| what is wrong | what happens | who says so |
|---|---|---|
| the named binary is missing or not executable | guard entries name it and exit 1; observational entries name it and exit 0 | the entry |
| `chain.env` is missing or malformed | every entry names the file and exits 1 | the entry |
| the checkout is gone | the guard still runs; the personal stages are reported missing | the entry, and `doctor` |
| the chain names a binary other than the running one | **`doctor` fails and prints both paths** | `doctor` (`chain:running`) |
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
the record names, and the executable that is running. Reporting them as two facts
with no failure would make `doctor` print `ok` while the binary you are running is
not the binary your commits run, which is the disagreement this whole design exists
to surface. It stays a failure, and it prints both paths.

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
both are constrained only by F1–F4 and R1.

## 6. What changes, in what order — stage 1, then stage 2

**Stage 1 — told, not asked.** The identity fix, in place. It deliberately builds
one shape §2's table rejects — the chain inside the checkout — because the entries
do not care where they live, and moving the directory is a separate risk with its
own refusal to unpick (§3's opening states this too). Stage 2 removes that shape.

- `DotfilesRoot()` becomes `BuildCheckout()`, read only by `version` and reported by
  `doctor`; `AGENTS_DOTFILES_ROOT` is deleted and never read.
- `main.go`'s `argv[0]` multicall is replaced by `agents githook <name> --checkout
  <path>`, registered with the `Git`/`CI` audience and hidden from the human
  listing, exactly as `guard` is today. The `argv[0]` branch itself survives for one
  release, as the compatibility shim described under "The transition window" below —
  that shim is a change to this bullet, not an exception to it.
- `git/install-hooks.sh` generates four entries and `chain.env` instead of four
  symlinks, and still writes `core.hooksPath` last so a partial install cannot
  activate an incomplete chain.
- `doctor` reports the machine per §3.4 instead of holding a root.
- The chain stays in `git/hooks.d/` for this stage; the entries do not care where
  they live.

**Stage 2 — machine-owned.** Small once stage 1 exists.

- The chain moves to `~/.config/agents/hooks.d/`, and the installer creates that
  directory (it currently refuses unless the directory already exists, which a
  fresh machine would not satisfy — `git/install-hooks.sh:234` requires "an
  existing real directory").
- `core.hooksPath` is repointed; `git/hooks.d/` retires, and its `.gitignore` with
  it. **The installer's own guard has to move with it**: `install-hooks.sh:133-135`
  refuses when `core.hooksPath` is already set to something other than the
  directory it is installing into, and after stage 1 every wired machine's config
  names `<root>/git/hooks.d`. `inspect_global_hooks_path` therefore gains the old
  chain path as an accepted **value** of `core.hooksPath`. That refusal has two
  conditions and this touches one: `install-hooks.sh:133-135` refuses when the
  configured value is not the directory being installed into, and when the configured
  value did not come from `file:$HOME/.gitconfig`. The second is what stops a
  `core.hooksPath` arriving through an `include`, and it stays.
- The guard now survives a moved or deleted checkout. Stage 1 does not achieve
  this.

### What the installer finds, and what it does

Stage 1 turns four symlinks into four entries in a directory that already holds
four symlinks, so the installer needs a statement of what it does with each shape
it can find at a hook name. `--adopt-owned` keeps its meaning from today: it is the
flag that lets the installer replace something that is already there. What changes
is what counts as **owned**. For an entry, ownership is decided from the entry
alone, by three things that are all inside the file: line 1 is exactly `#!/bin/sh`;
line 2 matches the generated form `# Written by git/install-hooks.sh for <path>.`
with the path taken as a pattern and compared against nothing; and the last line
execs `agents githook` for the hook name the file itself carries. The path is not
compared with the installer's own checkout, because the table's fourth row has to
recognise an entry generated from a different checkout and replace it.

It must not be decided from `chain.env`, because a missing or malformed record is
one of the states the repair command exists for, and a rule that read the record
first would refuse the installer's own entries as foreign. The same install that
writes the entries writes the record, and the entry reads its own header to name
the checkout in the repair line it prints.

This is a **widening** worth naming. Today a regular file at a hook name is never
adopted — `install-hooks.sh:165-167` has no adopt arm for one — and under a
two-line prefix test a hand-written hook that happens to open with those two
comment lines becomes replaceable under the flag. The third test above is what
keeps that from being cheap: a file that merely mimics two comment lines does not
carry the `exec … githook <hook-name>` line for the name it sits at.

One consequence belongs in the open: **an exact symlink stops being a success
state.** Today `check_exact_symlink_or_absent` (`git/install-hooks.sh:143-147`)
returns success when the link already names the binary the installer was handed,
which is the state this machine is in. Under this design that state is what the
installer must convert, so it becomes a refusal naming `--adopt-owned` — otherwise
nothing would ever turn four symlinks into four entries.

| what is at the hook name | without `--adopt-owned` | with `--adopt-owned` |
|---|---|---|
| nothing | write the entry | write the entry |
| a symlink this installer wrote — its target is the binary the installer was handed, or resolves into a Homebrew keg for `agents` (`:143-163`, `:202-208`) | refuse, naming the symlink and the flag | replace it with the entry |
| a symlink to something else, or a regular file this installer did not write | refuse, naming the file | refuse: it is not ours, and the flag does not make it ours |
| an entry this installer wrote, whose header names a different checkout or an older generated form | refuse, naming the file and the flag | replace it |

The third row is the one that matters: `chain:entries` will report a leftover
symlink as wrong forever, and the person repairing it by hand must be able to. The
installer's message names the file and the flag, and `doctor`'s `chain:unmanaged`
check reports the complementary case — a look-alike under another name, or a
dangling link an earlier installer left behind — because it skips every managed hook
name (`doctor.go:698`).

### The transition window, and how it is closed

The binary and the chain update through different channels: `brew upgrade agents`
replaces the binary, while `git pull` plus re-running the installer replaces the
chain. Both orders break, and both must be handled rather than hoped away:

| order | what happens |
|---|---|
| new binary first, old symlinks installed | the symlinks still resolve, so Git runs the new binary as `pre-commit`, `commit-msg`, `post-merge` or `post-checkout`, with Git's arguments and no `githook` subcommand. Every commit **and every `git checkout` and `git switch`** fails: with the multicall gone the binary prints a usage error (measured 2026-10-09: exit 3, `agents: unknown command "…/COMMIT_EDITMSG"`), and Git propagates it as exit 1 — measured on Git 2.54, `git checkout -b other` switches the branch and then reports failure, so anything chaining on git's status breaks |
| installer first, old binary on `PATH` | the entries call `agents githook`, which the old release does not have; the same machine-wide failure, but the installer reports success first |

Two changes close it, and both belong in stage 1:

1. **The installer probes the binary, in `install` mode, after `validate_binary`
   and before the first write.** It runs the binary with a known argument and
   refuses if the binary cannot answer `githook`. The placement is the whole
   mechanism: `preflight` runs *before* the build and is deliberately binary-blind
   (`bootstrap.d/internal/phase/devtools.go:52-67` records the reason), so a probe
   placed there would refuse on the stale `~/bin/agents` that the very next step
   rebuilds — or on no binary at all, on the machine type most likely to be running
   `bootstrap apply workstation` for the first time. A mismatched pair then fails at
   install time, with a message that says so, instead of at the first commit.
2. **The `argv[0]` branch stays for one release, and answers all four names with
   Git's arguments.** The old symlinks invoke the binary as `pre-commit` with no
   arguments and as `commit-msg` with the message file; a shim that answered only a
   bare `pre-commit` would leave every commit failing at `commit-msg` with
   `agents: unknown command "…/COMMIT_EDITMSG"`. It carries a version and a removal
   date in its comment (§9.2), and it is a compatibility shim, not a second design.

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
previous installer while a new binary is on `PATH` records that binary into four
symlinks, which it cannot answer **once the compatibility shim is removed**; while
the shim lives, the install succeeds and the commits keep working.

**Entries are published atomically.** A hook name that exists but is not executable
is skipped by Git with a hint and **exit 0** — silence, and the hint can be turned
off with `advice.ignoredHook=false`. Measured on 2026-10-09 with Git 2.54: a
non-executable `pre-commit` prints the hint on stderr and the commit succeeds. No
entry may be visible in a partial or non-executable state: each is written to a
temporary file in the same directory with its final mode and `mv`ed into place, and
`chain.env` the same way. `ln -sfn` was atomic; its replacement has to be too.

**One caution for the repair text.** On this machine "re-run the installer" is not
one action: `bootstrap`'s devtools phase installs the chain against
`~/bin/agents`, a checkout build (`bootstrap.d/internal/phase/devtools.go:50`), so
following that advice also changes *which binary Git runs*. The repair line should
name the binary it is reinstalling, or say that it will switch to the checkout
build.

### What the change touches

The change is not local to `agents`. Grouped by the job each surface does, with
what the change makes of it:

| surface | what the change does to it |
|---|---|
| **The builders** — `script/package-release.sh:7-10` (version and commit), `Makefile:40` and `bootstrap.d/internal/phase/devtools.go:86-89` (the stamp) | the stamp's *reader* changes; both builders keep passing `-X main.dotfilesRoot=<checkout>`, and the exact-command pins in `devtools_test.go:22,153` and `makefile_test.go:110,165` are edited only if the flag changes, which this design does not |
| **The release path** — `release.yml:57-97` (version and SHA), `:120` (`package-release.sh` is passed the version, not the commit, which is why the script derives it a second time), `:134-138` (archive name and the version-output glob), `:190-201` (the tap) | one fact — the commit — is derived twice today. This design does not change that, and whoever sequences this work should decide whether to fix it here or separately |
| **The installer** — `git/install-hooks.sh` in full: `:61` (the four names), `:139-168` (the symlink checks), `:191-208` (keg path shape), `:259-279` (the writes), `:281-287` (the keg note) | rewritten to write entries, with the table above as its specification |
| **The consumers** — `agents/cmd_version.go:12`, `agents/root.go:15-30`, `agents/cmd_doctor.go:71-80`, `agents/internal/doctor/doctor.go:149-173` and `:621-644`, `agents/main.go:24-58` | the stamp becomes provenance; the multicall becomes `githook`; the link comparison becomes `chain:running` |
| **`bootstrap`** — `bootstrap.d/internal/check/checks.go:317-324` (`agentsOnPath` is `LookPath("agents")`) | unchanged, and worth knowing: the provisioner's whole opinion of the binary stays "a name resolves somewhere" |
| **The documentation still in force** — `2026-08-11-spec-6-releases-and-distribution.md:90-97`, `2026-08-11-spec-5-verification-gate.md:734-790`, `2026-08-07-agents-repo-context-design.md` §8, `2026-09-20-agents-and-bootstrap-boundary.md` §3, `2026-08-28-contributor-guardrails-and-scaffold-decoupling.md:17` | each restates the deleted contract and must be amended in the same change |
| **`git/README.md`** | states the install and upgrade rule, and is **not** in `livingDocuments` (`agents/docs_test.go:80-84`), unlike `agents/README.md`, which is checked three ways. The file most likely to be left stating the old rule is the one with nothing watching it |

Three facts about the repository's own guards belong here, because they shape how
the work must be done:

- `agents/doctests.txt` is enforced **by name** by the CI `docs` job: every listed
  test must exist and pass, so a renamed test must be edited there in the same
  commit or the job fails naming it.
- The `hygiene (agents, home)` job runs the suite with `HOME` pointed at an empty
  directory and fails if anything appears in it. F4 is the fact that ambient inputs
  decide this tool's behaviour today; that job is the same suspicion, already
  enforced over the tests.
- `docs/design/` **is** covered by `docsStores` (`agents/docs_test.go:533-547`), but
  only by `TestLivingDocumentsNameNoDeletedCommand`. What is untested is whether a
  design document still *describes the contract in force*: a document restating the
  old resolution passes as long as it names no deleted command.

### Migration: what an already-wired machine experiences

Measured on this machine, 2026-10-09. Every item is a thing the change must handle,
and the general rule is that a machine carries at least two independent statements
of "which binary" and "which checkout".

| what is on the machine | what the change does |
|---|---|
| `git/hooks.d/pre-commit`, `commit-msg`, `post-merge`, `post-checkout` — four symlinks to `/opt/homebrew/bin/agents` | replaced by four entries, per the table above; `--adopt-owned` is the flag that permits it, and the installer must recognise them as ours |
| `~/.gitconfig` → `[core] hooksPath = /Users/nilbot/dotfiles/git/hooks.d` | stage 1 leaves it; stage 2 repoints it, through the relaxed `inspect_global_hooks_path` |
| `/opt/homebrew/bin/agents` → `../Cellar/agents/0.7.0/bin/agents` | read, never written: Homebrew owns it |
| `~/.config/fish/config.fish:19` → `set -x AGENTS_DOTFILES_ROOT /Users/nilbot/dotfiles` | **the one item no automated path owns.** It is inside a `seed` row, so `bootstrap` never rewrites it, and the tracked template does not contain it. Deleting the variable from the code leaves this line behind, harmless but misleading; the change must say so, and the person must delete it by hand |
| `~/bin/agents` | absent here. Where it exists, it is a second owner of the chain: the devtools phase installs against it, so re-running `bootstrap apply workstation` switches which binary Git runs |
| the Homebrew keg's `INSTALL_RECEIPT.json` | a third party's record of origin (`source.tap`, `tap_git_head`). The change coexists with it and cannot convert it |
| `.claude`, `.codex` and `.agents` hook entries in repositories wired before 2026-09-21 | untouched: `agents wire` removes them and nothing writes them |
| retired dispatcher shims in a repository's `.git/hooks` | untouched: recognised by size and SHA-256 (`agents/internal/githook/githook.go:151-215`) and reported by the check that is `git-hooks:legacy` today and `chain:legacy` after §3.4's rename |
| `~/.cache/dotfiles-bootstrap/<cksum>/bootstrap` | `bootstrap`'s own cached binary, keyed by a checksum of the checkout path. Adjacent, not affected, and the same failure mode — a cached artifact bound to a path that can move |

**What has and has not been exercised here.** The dangling-link failure itself was
measured on this machine: F3's incident of 2026-09-20 is the qna's "upgrading this
machine from agents v0.5.1 to v0.6.0". What is untested here is the *stable-path*
upgrade. The four links have named `/opt/homebrew/bin/agents` since 2026-09-25, and
no upgrade has happened since, so nothing has yet exercised Homebrew repointing
that symlink underneath a wired chain. That is a reason to keep the rollback
sequences tested rather than reasoned about.

## 7. What enforces the old behaviour today

Each of these asserts the contract this design deletes, or pins a string it
changes. They are the work a plan must schedule, and the reason renaming
`DotfilesRoot()` to `BuildCheckout()` cannot be done by editing one file.

| site | what it pins |
|---|---|
| `agents/cmd_version_test.go:11-51`, `agents/main_test.go:61-90` | the exact `agents version` line, stamped and unstamped, for `version`, `--version` and `-v` |
| `agents/root_test.go:22-63` | all three branches of `DotfilesRoot()`: unstamped, environment, and the stamp beating the environment |
| `agents/githook_main_test.go:384-436` | one case per `DotfilesRoot()` branch, asserting which checkout's personal hooks run |
| `agents/install_hooks_test.go:1260-1420` | the keg path shape, the stable path that must be recorded, the refusal, and `--adopt-owned` on either side of the mode |
| `agents/internal/doctor/doctor_test.go:293-353`, `:512`, `:599-715`, `:664`, `:764` | the binary comparison, the stamped-checkout check, the hooks-path checks, and the remedies that must name `install-hooks.sh` and `--adopt-owned` |
| `bootstrap.d/internal/phase/devtools_test.go:22`, `:153`; `bootstrap.d/makefile_test.go:110,165` | the exact build command and the stamp's value, in both builders |
| `release.yml:138` | the prefix of `agents version`'s output, matched in a glob |
| `agents/doctests.txt:28` | `TestTheCommandSetIsExactlyThis`: a new subcommand has to be declared there, or the `docs` job fails |
| `agents/doctests.txt:42-50` | the document guards the `docs` job runs by name — the README command block, and the three living-document tests over `README.md`, `agents/README.md`, `CLAUDE.md`, `global/AGENTS.md` and the two skill trees |
| `agents/README.md:88-121`, `:164-180`; `README.md:49-62`; `git/README.md:19-96`; the five `docs/qna/` answers that state the old rule — `why-does-an-unstamped-or-homebrew-agents-binary-skip-dotfiles-checks.md`, `why-is-agents-still-in-dotfiles-and-when-should-it-split.md`, `can-this-check-actually-fail.md`, `why-does-a-brew-upgrade-stop-my-commit-guard.md`, `why-does-install-hooks-refuse-symlinks-like-opt-homebrew-bin-agents.md`; and the five design documents listed under "What the change touches" | the prose that states the old rule, each guarded by a different mechanism or by none |

## 8. How we would know it works

Each test states the mutation that should make it fail, because a guard that has
only ever been green is not known to be a guard. Tests 1–2, 4–13 and 15–18 are
stage 1 subjects. Test 3 is not: it asserts that the guard survives a deleted
checkout, which stage 1 does not achieve, so it is written with stage 2. Test 14
needs two installed binaries, which is easiest to arrange once the chain is
machine-owned.

| # | test | mutation that must make it fail |
|---|---|---|
| 1 | the recorded binary is deleted, then a commit is attempted: the commit is refused, the output names the missing path and the repair command, and the exit status is non-zero | make the entry exit 0; the commit then succeeds |
| 2 | `chain.env` is emptied: every entry refuses, naming the file | default `checkout` to `.` and watch the wrong directory be consulted |
| 3 | the checkout is deleted: the built-in guard still runs and the personal stages are reported missing | make the entry require the checkout; the guard disappears with it |
| 4 | `AGENTS_DOTFILES_ROOT` set to a second checkout: nothing changes | read the variable; the check count moves between 18 and 13 |
| 5 | the installer is handed a binary that cannot answer `githook`, in `install` mode: it refuses and writes nothing | remove the probe; the chain is installed and the next commit fails |
| 6 | the probe is reached in `preflight` mode with no `~/bin/agents`: `bootstrap apply workstation` still builds and installs | move the probe into `preflight`; the run refuses before the build that would satisfy it |
| 7 | the installer is handed a keg path while a stable path is installed: it refuses and names the stable path. Handed a keg path with no stable path, it installs and prints a note | delete the refusal; the next upgrade dangles. Delete the note too, and the only person who has one `agents` cannot install a guard at all |
| 8 | a record whose line is a shell command: the command does **not** run, and the entry refuses | source the record instead of parsing it; the command runs at commit time |
| 9 | a record truncated after `binary`: every entry refuses, naming the missing key | default the absent keys; an empty `--checkout` is forwarded and the wrong directory, or none, is consulted |
| 10 | a record whose `binary` names a directory: the entry refuses | test `[ -x ]` alone, which is true for a directory; `exec` then fails with exit 126 |
| 11 | a record naming a binary whose path contains a space: it runs | quote-strip or word-split the value; the space path fails with `not found` |
| 12 | the compatibility shim: invoked as `commit-msg` with a message-file argument, it still runs the guard | narrow the shim to a bare `pre-commit`; every commit fails at `commit-msg` with an unknown-command error |
| 13 | an entry whose mode is not executable: `doctor` reports it, and a commit **succeeds** — Git prints its hint and runs nothing, so the guard is off and only the hint says so | write the entry and `chmod` after; the same thing happens, which is why publication is atomic (§6) |
| 14 | the chain names a binary other than the running one: `doctor` **fails** and prints both paths | report both as facts with no failure; the machine looks healthy while commits run a different binary |
| 15 | `core.hooksPath` is unset: `doctor` reports that no chain is installed, and the repository-level checks still run | make the absence an error; `doctor` fails on a machine that never had a chain |
| 16 | four symlinks this installer wrote, converted with `--adopt-owned`: four entries and a `chain.env` appear, and none of the symlinks remains | make the conversion a no-op; the chain keeps four symlinks and `chain:entries` reports them forever |
| 17 | the same four symlinks, without the flag: the installer refuses, naming the file and `--adopt-owned`, and writes nothing | let the exact-link success case through (`install-hooks.sh:143-147` returns 0 today); the conversion never happens |
| 18 | a foreign regular file at a hook name, with and without the flag: it is refused both times, and the file is left alone | treat the flag as "replace anything"; a repository's own hook is overwritten |

Placement is itself a contract. Contracts about prose, the command tree and the
generated README block belong in `agents/doctests.txt`, which the CI `docs` job
runs by name. Package behaviour belongs in the ordinary `test` job — which also
puts these tests under the `hygiene` job's synthetic-`HOME` guard.

Four things this design introduces have no test here yet, and each needs one before
the work is planned: `provenance:checkout`'s warning; `chain:record`'s allow-list,
which no row exercises because every row writes its record through the installer;
`chain:legacy`; and the installer creating the chain directory on a machine that
has none. The checks that replace today's machine checks are otherwise covered by
the rows above — test 13 asserts `chain:entries` and test 15 asserts
`chain:hooks-path` — and what is untested in them is the mapping from the five old
names to the four new ones.

## 9. Open questions

1. **The exact text `agents version` prints.** `release.yml:138` matches
   `"agents ${VERSION} (commit: ${SHA},"*` in a shell `case`, so everything after
   the comma is free: `built: <date>`, `built from <checkout>` and `release build`
   all match. What breaks the release is dropping or moving the comma, reordering a
   field before `(commit:`, or losing the `v`. A mismatch fails that job **before**
   the release object is created, so the cost is a pushed tag with no release rather
   than a published release with wrong bytes. Recommendation: append the checkout
   after the existing fields, and treat the comma as the part that is pinned.
2. **How long the compatibility shim lives.** It should carry a version and a
   removal date in its comment, or it becomes permanent. The removal is what makes
   the rollback paragraph's "cannot answer" true again, so the two belong in the
   same change.
3. **`~/.gitattributes`.** Its content is deliberately empty of rules
   (`git/gitattributes` says so), and `core.attributesFile` is unset, so it marks
   nothing. Recommendation: stop managing it — delete the link, the file, the
   installer's step and `doctor`'s `attributes:global` check — and leave the
   per-repository rule, which `agents init` writes and the CI `context` job checks.
   Note that `git/gitattributes` itself claims `core.attributesFile` points at it,
   which the configuration contradicts; whichever way this question is answered,
   that file needs the same edit.
4. **Who creates the chain directory on a fresh machine.** The installer must, but
   `bootstrap`'s manifest states that `core.hooksPath` is deliberately absent from
   it because the installer owns it. Creating the directory is therefore the
   installer's job, and the manifest's comment stays true.
5. **Whether the commit's double derivation belongs in this change.**
   `release.yml:81-87` resolves the SHA from the API, and
   `script/package-release.sh:9` derives it again because the workflow passes only
   the version. It is the same defect this design removes, one layer up, and it is
   the sequencing agent's to place.
6. **Two defects found while reviewing this, both outside its scope.** The
   `livingDocuments` list in `agents/docs_test.go` walks `claude/skills`, which no
   longer exists, and discards the walk error — so that guard covers one skill tree
   where it claims two. And `AGENTS_ACTIVE_GIT_HOOKS` is undocumented; the design
   keeps it deliberately (§3.2) and it should be written down wherever the hook
   contract is.
