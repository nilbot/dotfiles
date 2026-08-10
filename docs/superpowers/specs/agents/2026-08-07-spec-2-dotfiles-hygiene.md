# Spec 2 — dotfiles hygiene: a phased, cross-platform bootstrap

**Date:** 2026-08-07 (scope) / 2026-08-10 (design; language reversed same day)
**Status:** designed — not implemented
**Implementation language:** Go, with a shell shim for stage zero (§2.1). The
first implementation was shell throughout and was reversed after one task; the
evidence is in [Measured facts](#the-six-bash-defects-that-decided-1-2026-08-10)
and the reasoning under [Rejected alternatives](#rejected-alternatives).
**Depends on:** [spec 1](2026-08-07-agents-repo-context-design.md) §8, which has landed.

---

## What changed from the scope note

The original scope treated this as cleanup: delete `zsh/`, consolidate two
overlapping link mechanisms, remove dead scripts. Reviewing it surfaced a larger
framing error, and the design follows from correcting it.

**"Bootstrap" must mean reaching a usable workstation, not creating symlinks.**
`make dotfiles` is retired completely rather than kept as an alias — an alias
would preserve an interface we no longer want and leave two apparent entry
points, which is the exact overlap this spec exists to remove.

Three scope items are already closed and are recorded here so they are not
attempted twice:

- The dead `claude/` scripts (`commit-msg`, `check-commits.sh`,
  `setup-protection.sh`, `update-repo-hooks.sh`) were removed with spec 1 §8 in
  `b87810f`. `claude/` now holds only `CLAUDE.md` and `skills/`.
- No `.DS_Store` is tracked. `git ls-files` finds none.
- `git/hooks/go.pre-commit`'s fate is decided below: it is removed.

---

## 1. The rule this spec is built on

Spec 1 §8.4 fixed `~/.gitconfig` and cautioned that `~/.claude` and `~/.codex`
are harness-owned. Those were two instances of one rule, never stated. Stating
it is most of this design:

> **A well-known path that some other program writes to must be a machine-local
> regular file, seeded once from a tracked template, that *references* tracked
> content. Never a symlink into the repo.**
>
> Contrapositive: a well-known path nothing else writes to may be a symlink.

`~/.gitconfig` was the first instance. `~/.config/fish/config.fish` is the
second, and it is currently broken in exactly the way `~/.gitconfig` was — see
§6.

The rule's value is that it is **checkable**. Once each managed path declares
which kind it is, `bootstrap check` can verify that the path on disk is still
that kind, and a regression becomes a test failure instead of a discovery.

## 2. Interface

```sh
./bootstrap plan  workstation      # what would change; writes nothing
./bootstrap apply workstation      # converge this machine
./bootstrap apply dotfiles         # narrow profile
./bootstrap check                  # is this machine healthy?
./bootstrap migrate                # reconciling migrations; lists reclaiming ones
./bootstrap migrate <name>         # run one named migration, incl. reclaiming (§8.1)
```

`./bootstrap` resolves the repository root from `$0`, not from `pwd`.
`softlinks.sh` uses `pwd` today and silently links the wrong tree when run from
anywhere else.

For a new machine, `./bootstrap apply workstation` performs the complete setup.
For an existing machine the same command converges whatever is missing.

### 2.1 The shim, and the one manual step

`./bootstrap` is a ~55-line shell shim. It is the only shell in the design, and
it has exactly one job: get to Go, then hand over.

1. `go` on `PATH` → use it.
2. Otherwise **refuse**, naming the exact command for the detected platform.

**It installs nothing.** An earlier draft ran `brew install go` for you. That
branch was removed rather than repaired, because it produced the design's worst
measured defect: `set -e` does not apply inside `$( )`, so a failed or
off-`PATH` install was swallowed, leaving an empty path that reached the build
as `"" build …` — exit 127, or worse, a silent `exec` of a stale cached binary.
See [the shim defects](#five-shim-defects-measured-2026-08-10). Deleting the
branch removes the failure mode; patching it would have left the shim carrying
exactly the bash semantics that disqualified shell everywhere else here.

It then builds `bootstrap.d/` to a binary under
`${XDG_CACHE_HOME:-~/.cache}/dotfiles-bootstrap/<key>/`, where **`<key>` is
derived from the repository root** — without that, a main checkout and a git
worktree share one binary and whichever built first wins, running old code
against a new tree. It rebuilds when any `.go` file, `go.mod`, or a source
directory is newer than the binary (directories, so a deletion counts), then
`exec`s it with the original arguments. Every shim failure exits `2`.

**Building from the checkout, not installing a release.** The binary always
matches the tree you cloned: no release-version coordination, no drift between
an installed tool and the configuration it applies, and no dependency on
[spec 5](2026-08-10-spec-5-ci-release-distribution.md), which is unwritten.

**The honest cost:** a machine without Go needs one manual command before
`./bootstrap` runs. That is deliberate, and it is the same call spec 1 §9 made
about Codex hook trust: the design's job is to make a required manual step *one
obvious step* rather than a silent failure.

### 2.2 The seam where spec 5 removes Go as a dependency

Recorded now because it is the actual answer to §2.1's manual step, and because
a future reader will otherwise re-litigate the auto-install branch.

**If [spec 5](2026-08-10-spec-5-ci-release-distribution.md) publishes verified
release binaries, the shim stops needing Go at all.** Its structure becomes:

1. A released binary matching this checkout is cached and verified → `exec` it.
2. Otherwise, if a release exists for this platform → download, verify checksum,
   cache, `exec`.
3. Otherwise `go` is on `PATH` → build from source as today.
4. Otherwise refuse.

Steps 3 and 4 are exactly today's shim, so this is an extension, not a rewrite,
and **spec 2 still does not depend on spec 5** — build-from-source remains the
floor. The design constraint that matters is the one already in place: the shim
must never `exec` a binary it cannot attribute to the current checkout. Today
that is the cache key; under spec 5 it becomes the checksum plus a version the
binary reports back. Whichever mechanism, the property is the same, and it is
the property finding 2 below was raised against.

## 3. Phases

Ordered, independently repeatable.

| # | Phase | Does | `workstation` | `dotfiles` |
|---|---|---|---|---|
| 00 | preflight | detect OS and architecture, check stage-zero tools, declare what needs `sudo` and network | ✓ | ✓ |
| 10 | packages | native stage-zero → Homebrew → `Brewfile` | ✓ | — |
| 20 | config | reconcile every row of `links.manifest` | ✓ | ✓ |
| 30 | fish | `/etc/shells`, login shell, fisher plugins | ✓ | — |
| 40 | devtools | `uv`, build `agents`, global Git hooks | ✓ | — |
| 50 | verify | the same checks `check` runs | ✓ | ✓ |

**The `dotfiles` profile is preflight + config + verify: no sudo, no network, no
package manager, no login-shell change.** That is what makes it safe inside a
container, on a machine whose packages are managed elsewhere, and inside a test.
It is a phase filter, not a second code path.

### 3.1 Packages, and why Homebrew on Linux

One `Brewfile` serves both platforms. The native package manager installs only
what Homebrew itself requires, and nothing else:

- Debian/Ubuntu — `build-essential curl file git`
- Arch/Manjaro — `base-devel curl file git`

Everything above stage zero comes from the `Brewfile`, on both platforms. The
alternative — per-distro native package lists, which `super-install-dep.sh` does
today — needs two or three manifests kept in sync across distros whose package
names disagree (`fd` vs `fd-find`, `bat` vs `batcat`), and several tools in use
here are not consistently packaged natively at all.

The PATH assumption already exists: `fish/mypre.fish` adds
`/home/linuxbrew/.linuxbrew/bin` on Linux and `/opt/homebrew/bin` on macOS.

The `Brewfile` carries platform sections, because casks are macOS-only. The
current `brew/*.list` files are **audited, not mechanically translated** — see
§9 for what does not survive the audit.

### 3.2 Fish

Confirm fish was installed by phase 10; add its resolved path to `/etc/shells`
if absent; change the login shell only when it is not already fish; install
fisher plugins explicitly rather than relying on a shell-start side effect.

This phase has never actually run — see [Measured facts](#measured-facts-2026-08-10).

### 3.3 Devtools

`uv`, the `agents` binary built to `~/bin/agents`, and global Git hooks. The
Git-hook step **delegates to `git/install-hooks.sh`**, which already performs
this carefully and has tests. It is not reimplemented.

## 4. The dry-run invariant

`plan` and `apply` are the same code. **All machine access — reads included —
goes through one interface**, `change.Interface`, with two implementations:
`Applier` performs the operation, `Planner` records the intent and touches
nothing.

```go
type Interface interface {
	// queries
	Lstat(path string) (FileInfo, error)
	Readlink(path string) (string, error)
	LookPath(name string) (string, error)
	ReadFile(path string) ([]byte, error)

	// mutations
	Dir(path string) error
	Link(source, target string) error
	Seed(source, target string) error
	Run(name string, args ...string) error
	Sudo(name string, args ...string) error
}
```

**The phase package imports nothing capable of I/O.** Not `os`, not `os/exec`.
It receives a `change.Interface` and can reach the machine no other way, so "a
phase cannot mutate outside dry-run control" is a property of the **import
graph** — exact, complete, and machine-checked by one test that asserts the
package's import set.

This is the same move spec 1 §3.2 makes for redaction, and the reason it had to
be this rather than a lexical scan is recorded in
[Measured facts](#the-six-bash-defects-that-decided-1-2026-08-10): the shell
version's grep-based equivalent was written wrong twice — once flagging the
*correct* form, once missing a real violation.

**Queries go through the interface too, which is what makes `Planner` honest.**
It overlays planned changes on what it reads, so a `Link` into a directory the
plan just created reports that directory as present. The shell design could not
do this: `do_link` called `do_dir` unconditionally and re-emitted a
`create directory` line for every link into a shared parent — output `apply`
would never produce.

A separate `plan` implementation was rejected. Two implementations of one
behaviour drift, which is precisely the failure the `softlinks.sh` / `Makefile`
overlap already demonstrates in this repo.

## 5. Refuse, never clobber

Inherited from `git/install-hooks.sh`, which does this well already.

A target that exists and is not the exact intended thing produces a **refusal
naming the remediation**, never a deletion. Idempotence comes from "already
correct → no-op", not from "delete and redo".

This is a change in kind. The current Makefile does `rm -rf $HOME/.vim`,
`rm -rf $HOME/.emacs.d`, `rm -rf $HOME/.config/fish`, and
`rm -f $HOME/.config/starship.toml` — and its `editors` and `tmux` targets fail
outright on a second run, because `git clone` into an existing directory is an
error. Neither property is acceptable in something meant to converge an existing
machine.

## 6. `links.manifest`

One tracked table. Three kinds, which are §1's rule made mechanical:

| kind | meaning | use when |
|---|---|---|
| `link` | symlink target → repo source | nothing else writes to this path |
| `seed` | copy source → target **once**, never overwrite | another program writes here |
| `dir` | real machine-owned directory | a program owns the whole directory |

```
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

`check` verifies each target is the **right kind**. A `link` target that has
become a real file is a finding. A `seed` target that has become a symlink into
the repo is a finding — that second one is the `~/.gitconfig` regression spec 1
fixed and the fish regression §7 fixes.

**Harness skill directories are ordinary rows.** Today `claude/skills` is linked
by a hand-written Makefile line and `gemini/skills` by nothing at all — two
harnesses already diverging, the same drift spec 1 opened with. A third harness
becomes one row.

This has to be the bootstrap's job rather than `agents`', for a hard reason:
**`agents` is a Go binary built by phase 40, so it cannot be a dependency of
phase 20.** The trigger for revisiting, stated the way spec 4 states its
triggers:

> Move global harness assets into `agents` when they need **generation** rather
> than **linking** — when a global config must be merged into a file the harness
> co-owns, as `~/.claude/settings.json` already is per-repo, rather than
> symlinked wholesale.

Linking is declarative and a table suffices. Merging needs a program that
understands the format. The boundary is observable, so it will be obvious when
it is crossed.

### 6.1 One owner per path

`~/.gitattributes` and the `core.hooksPath` wiring belong to
`git/install-hooks.sh`. The manifest therefore does **not** list them.

One split is deliberate: `~/.gitconfig` is *seeded* by phase 20 (the dying
Makefile target does this today; `install-hooks.sh` validates the file but never
creates it), then *validated* by `install-hooks.sh` in phase 40.

`check` asserts no path is claimed twice. Two owners is how `softlinks.sh` and
the Makefile drifted apart in the first place.

## 7. Fish: zero symlinks under `~/.config/fish`

`~/.config/fish` is a symlink to `dotfiles/fish/`, so fisher writes `functions/`,
`completions/`, `conf.d/`, `fish_plugins` and `fish_variables` into the tracked
repo, held back only by `.gitignore`.

Per-file symlinking does **not** fix this, and that is the important part.
Third-party installers write to `config.fish` by appending managed blocks, so
the one file that must be tracked is also the one file that gets written to. The
evidence is in the repo: tracked `fish/config.fish` carries a
`# >>> grok installer >>>` block, and tracked `fish/mypost.fish` carries a block
marked *"managed by 'mamba init'"*. Both arrived because the well-known path was
a link into the repo.

The fix is §1's rule, and fish supports it because — measured, not assumed —
`conf.d/*.fish` is sourced **before** `config.fish`, giving the same last-wins
ordering that makes git's `[include]` work.

| Path | Kind | Owner |
|---|---|---|
| `~/.config/fish/` | real directory | machine |
| `~/.config/fish/config.fish` | seeded once, never overwritten | machine + installers |
| `conf.d/`, `functions/`, `completions/`, `fish_plugins`, `fish_variables` | real, machine-local | fisher |
| `dotfiles/fish/*.fish` | tracked | repo — reached by `source` only |

The seeded stub:

```fish
# Machine-local fish config. NOT tracked by dotfiles. Shareable settings belong
# in ~/dotfiles/fish/config.fish -- edits here are invisible to the repo.
source $HOME/dotfiles/fish/config.fish
# --- installer-managed blocks appear below this line ---
```

Installer blocks append below the `source` line, so they land machine-local
**and** correctly override — the identical ordering argument
`git/gitconfig.local.template` already makes in its own header comment.

The stub necessarily names the clone location, and that is correct: it is
machine-local and seeded once, so it is the right place for the one fact that
varies per machine. Everything downstream of it is relative — tracked
`fish/config.fish` sources `mypre`, `alias` and `mypost` from its own
`(status dirname)`, so the clone path appears exactly once on a machine and the
tracked files work unchanged from a worktree or a relocated clone.

Three consequences:

1. **Five `.gitignore` lines are deleted** — `fish/fish_variables`,
   `fish/fish_plugins`, `fish/functions/`, `fish/completions/`, `fish/conf.d/`.
   They exist only to hold back writes that will no longer arrive. `.gitignore`
   stops being load-bearing.
2. **Two tracked functions are built around the old layout and are rewritten,
   not merely relinked.** `fish_reset_all` does
   `rm -rf $HOME/dotfiles/fish/{functions,completions,conf.d}/` and must target
   `$__fish_config_dir`; `install_fisher` reads `$HOME/dotfiles/fish/fishfile`
   and must resolve it from `(status dirname)`.
3. **`check` gains a real check.** If anything strips the `source` line from the
   stub, the entire shared configuration goes dark with no error at all. That is
   a silent total failure and it is cheap to detect.

The `# >>> grok installer >>>` block is lifted out of tracked `config.fish` into
the local stub. The mamba block in `mypost.fish` goes entirely (§9).

## 8. The `.symlink` rename, and the migration hazard inside it

The suffix has stopped carrying information, and for two different reasons:

- `git/gitconfig.symlink` → **`git/gitconfig.shared`**. Since `37f00a0` it is
  *included* by a machine-local `~/.gitconfig`, not symlinked to it.
- `git/gitignore_global.symlink` → **`git/gitignore_global`**. Still genuinely
  symlinked, but the manifest's `kind` column now carries what the suffix used
  to.

**The first rename is a live hazard.** Every existing machine's `~/.gitconfig`
contains `path = ~/dotfiles/git/gitconfig.symlink`. It is a `seed` row, so
bootstrap will never overwrite it — the include would silently point at a
missing file and *every shared git setting would disappear with no error*.

Hence `migrate`, and hence a `check` for it.

### 8.1 `./bootstrap migrate`

Declared, idempotent, one-time operations. Each refuses unless its preconditions
hold. Keeping them out of `apply` preserves §5 intact: `apply` never clobbers,
and the code that knows about the past is quarantined where it can be pruned
once no machine needs it.

Migrations come in two kinds, and the kind determines whether a bare
`./bootstrap migrate` will run it:

| kind | destroys untracked data | run by bare `migrate` |
|---|---|---|
| **reconciling** | no — moves or rewrites | yes |
| **reclaiming** | yes, irreversibly | no — must be named |

**Reconciling migrations** run by default:

1. **fish** — move fisher's untracked state out of the repo into a real
   `~/.config/fish`, then replace the symlink. This moves data that is *not in
   git* (`fish_variables`, the installed plugin set), which is exactly the
   fumble-prone step worth automating rather than printing as five hand-run
   commands. The move completes before the source is released, so a failure
   leaves the old state intact.
2. **gitconfig include** — repoint a `~/.gitconfig` that includes the old
   `gitconfig.symlink` path.

**Reclaiming migrations** run only when named — `./bootstrap migrate mambaforge`:

3. **mambaforge** — remove `~/sdk/mambaforge` (3.5 GB). Precondition: refuse if
   anything on `PATH` resolves inside it. Measured on this machine: nothing
   does, and its four environments are named by Python version alone
   (`3_9`–`3_12`), which is the job `uv` now performs.

A bare `./bootstrap migrate` **lists** the reclaiming migrations it is eligible
to run, with the exact command for each, and performs none of them. So nothing
has to be remembered or rediscovered later — the tool says what is reclaimable
and how — while a routine invocation can never destroy untracked data. This is
the same shape as the rest of the design: declare the kind, and behaviour
follows from the kind.

## 9. What is removed

Each group gets **its own commit naming what was removed and why**. Git history
is the archive — `git show <sha>:bin/rgr.bin` recovers any of this byte-for-byte
forever — so the only real obligation is findability, and that is a
commit-message obligation, not a working-tree one. Keeping dead files in the
tree as "reference" is the mechanism by which a dotfiles repo rots: every future
reader must re-derive which files are live.

| Group | Removed |
|---|---|
| zsh | `zsh/` (344 tracked files), the `omz` target, zsh from `super-install-dep.sh` |
| tools | `tools/` |
| conda/mamba | `miniforge/`, `micromamba` from the package list, the mamba block in `fish/mypost.fish`. The `conda`/`mamba` aliases in `alias.fish` are `type -q`-guarded and go quietly with it. `~/sdk/mambaforge` itself is reclaimed by §8.1's third migration |
| stale scripts | `snapshot.sh`, `recover.sh`, `mountcrypt.sh`, `mountsshfs.sh`, `post-install.sh` |
| superseded installers | `super-install-dep.sh`, `user-install-dep.sh`, `softlinks.sh` — content carried into phases 10 and 20 |
| editors | the `editors`, `tmux` and `extra` targets, and `spacemacs/` |
| fonts | `install-font-linux.sh` |
| bins | `bin/` (all four) and the `bins` target |
| go hook | `git/hooks/go.pre-commit` |
| unlinked | `gnupg/`, `macOS/iterm2/` |
| Makefile | everything except the `agents` target |

`macOS/filebrowser/` stays: a self-contained opt-in launchd setup with its own
script, claimed by no phase.

Two side effects the removal commits must state: `~/.spacemacs` becomes a
dangling symlink on this machine, and `~/.tmux/plugins/tpm` and `~/.vim` remain
on disk. Dropping a target stops *managing* a thing; it does not delete it.

### 9.1 Why `go.pre-commit` goes

It runs `go build -n && go test && go fmt && go vet` on every commit in any repo
with `.go` files at the root. Three defects, any one of which is disqualifying:

- Every command is `>/dev/null 2>&1`, so a failure yields a generic message and
  **no diagnostic**.
- `go fmt` **rewrites files mid-commit without staging them**, so a formatting
  fix silently fails to be committed.
- `go test` on a large module makes every commit slow.

This is CI's job, and CI is [spec 5](2026-08-10-spec-5-ci-release-distribution.md).

### 9.2 Why the `bin/` scripts have no reference value

Asked explicitly, and checked rather than assumed:

- **`rgr.bin` has never worked.** It invokes `e`, not `rg`, with ripgrep's
  flags. On this machine `e` resolves to `/usr/local/plan9/bin/e` — Plan 9's
  editor — because `$PLAN9/bin` is on `PATH`. So it does not fail cleanly; it
  silently runs the wrong program.
- **`git-chdate.bin` has never worked.** Its `--env-filter` body is
  single-quoted, so `$hash` and `$proper` never expand: the comparison tests
  against an empty string and it exports empty dates. It also uses deprecated
  `git filter-branch` and BSD-only `date -v`.
- **`git-stats.bin` works** and is the only one with content, but it is fifteen
  lines you would rewrite from the idea rather than the code.
- `infernowm.bin` is a two-line Plan 9 launcher.

`bins` also links with the `.bin` suffix intact, so the commands are `rgr.bin`
and `git-stats.bin` — the latter defeating the `git-` prefix's whole purpose.

## 10. `bootstrap check`

1. Platform supported.
2. Every manifest row: target present, **right kind**, pointing at the right source.
3. No path claimed by two rows.
4. `~/.config/fish/config.fish` still contains its `source` line.
5. `~/.gitconfig` includes the current shared-config path.
6. Login shell is fish; fish is present in `/etc/shells`. *(workstation only)*
7. `agents` on `PATH`, with `agents doctor`'s result folded in. *(workstation only)*
8. `Brewfile` packages present. *(workstation only)*

`check` takes the same profile argument as `apply` and defaults to
`workstation`. Checks 6–8 concern state the `dotfiles` profile deliberately does
not manage, so under that profile they report **not applicable** rather than a
finding — otherwise every container run would report three false failures.

Output is a concise healthy / advisory / failure summary.

**Exit codes come from [spec 1 §6](2026-08-07-agents-repo-context-design.md#6-binary-surface)'s
table**, with the same meanings: `0` ok, `1` advisory, `2` block, `3` malformed
input, `4` not applicable. (`5`, "could not record", has no analogue here.) One
vocabulary across both tools in this repo.

Checks 4 and 5 are the two silent-total-failure modes, and both exist *because*
this design introduces them. A design that creates a new silent failure mode
owes a check for it.

## 11. Module layout and testing

A second Go module at `bootstrap.d/`, module path
`github.com/nilbot/dotfiles/bootstrap`. It is a **sibling** of `agents/`;
nothing moves into `agents/`, which is unchanged.

```
bootstrap                       the shim (§2.1) -- the only shell in the design
bootstrap.d/
  go.mod                        module github.com/nilbot/dotfiles/bootstrap
  main.go                       verbs, profiles, dispatch
  links.manifest  Brewfile
  internal/change/              Interface, Applier, Planner -- owns ALL I/O
  internal/manifest/            row parsing, the Kind type
  internal/phase/               the six phases -- imports no I/O package
  internal/check/               the eight checks
  internal/migrate/             reconciling and reclaiming migrations
```

The directory keeps its `.d` suffix: the module *path* is what Go cares about,
and renaming buys nothing. Verified rather than assumed — a Go module in a
dot-suffixed directory builds and tests correctly, and the module path need not
match the directory name.

**No `go.work`, and no shared helper package with `agents/`.** Two independent
`go test ./...` invocations mean a bootstrap failure can never block an `agents`
binary release, which matters because spec 5 builds release CI around that
module specifically. Phase 40 *delegates* git-hook installation to the
already-tested `git/install-hooks.sh`, so its test only asserts the invocation
arguments — which is why none of `agents`' git-specific test helpers are needed.

Tests come in two layers, and the split is the point:

**Logic tests** run against a fake `change.Interface`. No temp `HOME`, no
filesystem, no subprocess — phases are pure functions over an interface:

- Every manifest kind, and refusal on an unknown one.
- Duplicate-target detection, platform filtering.
- Refusal paths: a pre-existing wrong-kind target is refused, not clobbered.
- Kind regressions: a `link` target that became a real file; a `seed` target
  that became a symlink into the repo.
- `Planner` records the right operations, in order, for each profile.

**Integration tests** use a real `Applier` with `HOME` redirected under
`t.TempDir()`:

- `plan` changes nothing — a tree snapshot before and after is identical,
  compared by content and link target, not merely by name and kind.
- `apply dotfiles` twice: the second run is entirely no-ops. Idempotence is the
  property the Makefile never had.
- Phase 40 invokes `install-hooks.sh` with the correct arguments.

**Architecture test:** `internal/phase`'s import set contains no I/O package.
This is the §4 invariant, checked exactly.

**Honest limits.** Phases 10, 30 and 40 need network, `sudo`, and a package
manager; their logic is covered by fakes and their real execution is not
exercised end-to-end. **No Linux machine is available to verify against**, so
Linux support ships written and unit-tested but unverified in practice. That is
recorded in [Risks](#risks), not claimed as working. The shim itself is shell
and gets a small integration test, but its Go-absent branch cannot be tested on
a machine that has Go.

## 12. Commit order

Bootstrap lands **before** anything is removed, so the repo is never in a state
where a machine cannot be provisioned:

1. `change.Interface` with both implementations, `manifest`, the shim, and the
   dispatcher. Nothing removed; both paths work.
2. Fish inversion (§7) and the `migrate` verb (§8.1).
3. Phase 20 reaches parity with the Makefile's link targets → those targets go.
4. Phase 10 and the `Brewfile` → `super-install-dep.sh` and
   `user-install-dep.sh` go.
5. The removal commits (§9), one per group.
6. Makefile reduced to the `agents` target.
7. This document's status, and the README table row, updated.

---

## Measured facts (2026-08-10)

Observed on this machine, not read from documentation.

### The Makefile's privileged steps have never run

`make dep` is guarded by `sudo -v || if [ -z $$? ]; then sudo ./super-install-dep.sh; fi`.
If `sudo -v` succeeds, `||` short-circuits and the branch is skipped. If it
fails, `[ -z 1 ]` is false — `$?` is a non-empty string — and the branch is
skipped too. **Neither path can ever run it.**

The identical construct guards `chsh` in both the `omz` and `fishshell` targets,
so **`make` has never set the login shell either.** Corroborated: the login
shell is `/opt/homebrew/bin/fish`, yet `/etc/shells` contains **no fish entry**
at all. It was set by hand with `sudo`, which bypasses the `/etc/shells` check.

### The installers are broken independently of that

- `user-install-dep.sh` reads `brew/brew-cask.list`; the file is
  `brew/brew-casks.list`.
- It calls `brew cask install`, which is no longer a Homebrew command.
- It installs Homebrew via the retired ruby `master`-branch URLs, for both
  Homebrew and Linuxbrew.
- `post-install.sh` is **not valid bash**: `bash -n` reports
  `syntax error near unexpected token 'elif'` at line 9 (empty `then` branches).
  It also calls `$(name -v)` for `uname -v`.
- `make extra` links `$HOME/crypt/extras.secret` into the repo. `~/crypt` does
  not exist; the real location is `~/etc/extras.secret`, which is what
  `gitconfig.symlink` includes. No `extras.secret` link exists in the repo. The
  target produces a dangling symlink.

### Fish

- `~/.config/fish` has been a symlink to `~/dotfiles/fish` since April 2021.
- Tracked `fish/config.fish` carries a `# >>> grok installer >>>` block; tracked
  `fish/mypost.fish` carries a block marked *"managed by 'mamba init'"*. Both
  are third-party writes that reached tracked content through that symlink.
- fish 4.8.1, probed with a redirected `XDG_CONFIG_HOME`: `conf.d/*.fish` is
  sourced **before** `config.fish`.
- `$__fish_config_dir` is `~/.config/fish`.

### Dead things confirmed dead

- `~/.oh-my-zsh` does not exist. zsh is fully out of use.
- `micromamba` is not on `PATH`, so the mamba block in `mypost.fish` is already
  inert. `~/sdk/mambaforge` survives from May 2024 at 3.5 GB. Its four
  environments are named by Python version only (`3_9`, `3_10`, `3_11`, `3_12`),
  and none of `conda`, `mamba`, `micromamba`, `python`, `python3` or `pip`
  resolves inside it.
- `gnupg/`, `gemini/skills/` and `macOS/iterm2/` are tracked and referenced by
  no target.
- `bin/rgr.bin` and `bin/git-chdate.bin` have never worked (§9.2).

### Go module layout

A Go module in a dot-suffixed sibling directory (`bootstrap.d/`) builds and
tests correctly, and its module path need not match the directory name.

### The six bash defects that decided §1 (2026-08-10)

The first implementation was shell. One task — roughly 100 lines of `lib.sh`
plus its tests — produced six defects, every one a property of the language
rather than a mistake about the design. Recorded because the decision to switch
rests on them, and because a future reader will otherwise wonder why a dotfiles
bootstrap is a Go program.

1. `do_dir` assigned `target` as a global. `do_link` calls
   `do_dir "$(dirname "$target")"` *before* using `$target`, so every symlink
   would have been created at its parent directory's path. Caught in review.
2. `local current=$(readlink "$target") || refuse "…"` never fires. `local` is a
   builtin whose own exit status is 0, so it masks the command substitution's.
   The fix for defect 1 introduced this one.
3. `dry_run() { [ "$BOOTSTRAP_DRY_RUN" -eq 1 ]; }` fails **open**. `-eq` is
   numeric: `BOOTSTRAP_DRY_RUN=true` and `BOOTSTRAP_DRY_RUN=` each print
   `integer expression expected` and then take the *mutate* branch. The one
   variable whose purpose is preventing mutation treated anything it could not
   parse as permission to proceed.
4. `manifest_rows | while read …` puts the loop in a subshell, where `refuse`'s
   `exit 2` ends only the subshell and the run continues past a refusal.
   Required process substitution instead.
5. Whether `[ "$X" -eq 0 ] && X=1` inside a `case` arm aborts under `set -e` had
   to be settled by running a probe. It does not — but the reasoning is not
   something a reader can check by inspection.
6. The lexical scan enforcing §4 was written wrong twice. Version one flagged
   `do_run mkdir -p "$x"`, the correct form, because the mutating word sits
   after whitespace there exactly as in a bare call. Version two missed
   `; do cp "$x" /tmp` because the `;` alternative consumed `do` as its captured
   word before the `do` alternative could match. Only a
   split-on-separators-and-take-each-first-word approach, verified against
   sixteen allowed forms and nine violations, was correct.

Defects 1 and 2 are impossible in Go. Defect 3 is a `bool`. Defect 4 is an early
`return`. Defect 5 does not exist. Defect 6 becomes an import-set assertion.

### Five shim defects, measured (2026-08-10)

The shim was granted shell's only exception in this design on the grounds that
its job — find Go, build, exec — is small enough to get right. Review measured
five defects in that job. Recorded because the premise turned out to be wrong,
and because anyone proposing to grow the shim should read this first.

1. **`set -e` does not apply inside `$( )`.** Confirmed on bash 3.2.57 and
   5.3.15. A `find_go` helper called as `go_bin=$(find_go)` swallowed a failed
   `brew install go` and returned empty; the shim then ran `"" build …`
   (exit 127) or silently `exec`ed a stale cached binary. The `exit 2` refusal
   was only reachable when Homebrew was *absent*. **Fixed by deleting the
   auto-install branch** (§2.1), not by patching it.
2. **One cache for every checkout.** The cache path had no component derived
   from the repository root, so a main clone and a git worktree shared one
   binary. A worktree whose files predate the other's build rebuilt nothing and
   `exec`ed the wrong binary while exporting the right root: old code, new tree,
   silently. Fixed by keying the cache on the root. An mtime-only scan also
   missed deletions; fixed by including directories in the scan.
3. **Shim exit codes escaped the shared table.** A Go compile error exited `1`
   — "advisory" — which is the most likely real failure and the one CI would
   read as non-blocking. `mkdir` failure likewise. Every shim failure now
   exits `2`.
4. **`CDPATH` could point the root at the wrong tree.** Reproduced:
   `cd "$(dirname "$0")"` searches `CDPATH` for a relative `$0` and echoes the
   resolved path, so the root came back as a same-named decoy directory *and*
   as two lines. This is precisely the "silently linked the wrong tree" failure
   the [six bash defects](#the-six-bash-defects-that-decided-1-2026-08-10) say
   this redesign exists to prevent, reproduced inside the replacement. Fixed
   with `CDPATH= cd -- …`.
5. **Preflight validated `PATH` tools but not its two load-bearing inputs.**
   `HOME` unset with `XDG_CACHE_HOME` set — a normal container shape, and
   containers are why the `dotfiles` profile exists — yielded an empty `Home`,
   which would make the config phase resolve every managed path against `/`.
   Fixed by refusing on an empty `Root` or `Home`.

Defect 4 is the one worth carrying: a design whose stated purpose is preventing
a class of failure reproduced that exact failure in the first shell file it
allowed itself.

---

## Rejected alternatives

**Rebuilding the Makefile as the bootstrap interface** (`make plan`,
`make workstation`, `make check`). Make is another prerequisite; its recipes are
awkward for structured checks, safe conflict handling and portable quoting; and
targets conceal side effects, as the present `all` and `links` graph already
demonstrates. It would improve on today without being the right shape.

**A Homebrew-distributed bootstrap binary** (`brew install nilbot/tap/dotfiles`).
Note this is *not* the same as §2.1's build-from-checkout, which was adopted.
Distribution via Homebrew requires Homebrew *before* bootstrap can start, must
locate or embed the dotfiles it configures, creates release-version coordination
between binary and repository, and couples this spec to the unimplemented spec 5.
Building from the checkout has none of those properties. Publishing a release
binary may still be worthwhile later; it is the wrong *stage-zero* mechanism.

**A separate `bootstrap` subcommand inside the `agents` binary.** Rejected for
the reason spec 1 already names: it would turn a repo-context tool into an
unrelated workstation manager. Two Go modules, two binaries, one repo.

**Bash for the whole bootstrap.** This was the original decision and it was
reversed after one task of implementation. Reversed on evidence, not taste — see
[the six defects](#the-six-bash-defects-that-decided-1-2026-08-10). The
deciding one is that the §4 invariant, in shell, could only be enforced by
lexically scanning phase files for mutating command names, and that scan was
written wrong twice: once flagging the *correct* `do_run mkdir` form, once
missing a real `; do cp` violation. **A guarantee enforced by a heuristic its
own author cannot write correctly is not a guarantee.** In Go the same property
is an import-set assertion: exact, complete, and impossible to get subtly wrong.

The advantages shell genuinely had are real and were given up knowingly:
zero stage-zero dependencies, and a tool short enough to read before running it
on your own machine. §2.1's shim preserves the second for the only part that
still runs before anything is verified.

**Keeping `make dotfiles` as an alias.** Preserves an interface we no longer
want and leaves two apparent entry points — the overlap this spec exists to
remove.

**Per-file symlinks under `~/.config/fish`.** Narrows the surface without fixing
it: the file that must be tracked is the same file installers append to. See §7.

**Relocating fisher's state via fish variables** while keeping the directory
symlink. More moving parts than either alternative, and it depends on fisher
honouring those paths — unverified.

**A separate read-only `plan` implementation.** Two implementations of one
behaviour drift; §4.

**Extracting a shared Go test-helper package**, or a tracked `go.work`. The
former makes `agents` export a package solely for a consumer outside itself; the
latter alters module resolution during builds, so spec 5's release path would
have to remember `GOWORK=off` — a footgun handed to an unwritten spec in
exchange for one convenience command.

---

## Risks

| Risk | Mitigation |
|---|---|
| Linux support is written but never executed on Linux | Stated plainly, not claimed as working. Plan-mode and stub tests cover the logic; end-to-end verification is owed before the Linux path is described as supported. |
| The `gitconfig.shared` rename silently empties shared git config on existing machines | `migrate` repoints the include; `check` #5 detects a stale one. |
| The fish stub loses its `source` line, and the whole shared config goes dark | `check` #4. |
| Editing `~/.config/fish/config.fish` silently produces untracked changes | The stub's header says so, mirroring `gitconfig.local.template`. Reduced, not eliminated. |
| `migrate` moves untracked fisher state and could lose it | Each migration refuses unless preconditions hold; the move is within one filesystem and non-destructive to the source until it succeeds. |
| A reclaiming migration irreversibly deletes untracked data | Reclaiming migrations never run from a bare `migrate`; they must be named, and each refuses if anything on `PATH` still resolves inside the target. §8.1. |
| Bootstrap grows into an unmaintainable program | One package per concern; all machine access through `change.Interface`; an architecture test on the phase package's import set. |
| Go is a stage-zero dependency the old design did not have | The shim installs it via Homebrew when present, and otherwise refuses with the exact command. One manual step on a machine that has neither. §2.1. |
| The shim is shell, so it inherits the defects that motivated the switch | It is ~40 lines, does no reconciliation, and has no dry-run mode to keep honest. Its whole job is to reach Go. |
| Removals lose something later wanted | Per-group commits naming contents and rationale; `git show <sha>:<path>` recovers exactly. |

## Open questions

- **`snapshot.sh`'s replacement, if any.** The scope note asked whether it still
  does something wanted; the answer is that it does not work (BSD-only `date -j`,
  a 2021 `rclone` remote) and it is removed. Whether machine backup should exist
  at all is a separate question this spec does not answer.
- **The Linux distributions actually targeted.** Stage-zero commands are written
  for Debian/Ubuntu and Arch/Manjaro, matching what `super-install-dep.sh`
  covered. Neither is verified.
