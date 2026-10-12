# Spec 2 — dotfiles hygiene: a phased, cross-platform bootstrap

**Date:** 2026-08-07 (scope) / 2026-08-10 (design; implementation language changed from shell to Go the same day)
**Status:** implemented. The plan it was executed under,
`2026-08-10-dotfiles-bootstrap`, is **not in this store**: pre-2026-08-20 plans
lived in `docs/archive/plans/`, deleted on 2026-10-12, and the branch it names
(`claude/spec-2-dotfiles-hygiene-5f92d4`) is in git history rather than here.
**Read [the appendix](#appendix-what-has-changed-since-this-was-written) before acting
on any section.** It is the index of what has moved under this document since
2026-08-07, and where a section and the appendix disagree, the appendix is the state of
the world.

**Read [Known gaps](#known-gaps-2026-08-11) before running this on a machine
that is not this one.** The first of the three was that no phase of this design
had ever run on Linux. Since 2026-08-17 the **`dotfiles` profile** does, on
every pull request, in a Debian container — see Known gap 1 for what that
covers and the larger part it does not.
**Implementation language:** Go, with a shell shim for stage zero (§2.1).
**Depends on:** spec 1 §8, which has landed.

---

## What this spec settles

**"Bootstrap" must mean reaching a usable workstation, not creating symlinks.**
There is one entry point. An alias for the interface this replaced would leave
two apparent ones, and that overlap is what this spec exists to remove.

The framing came from a larger error in the scope note it replaced: the note read
as a link-management task, and reaching a usable workstation is the task.

## 1. The rule this spec is built on

Spec 1 §8.4 — a design since deleted — fixed `~/.gitconfig` and cautioned that `~/.claude` and `~/.codex`
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
`softlinks.sh` used `pwd` and silently linked the wrong tree when run from
anywhere else.

Two routes to a wrong root are silent, and the shim closes both — the first by
clearing `CDPATH`, the second by refusing:

- **A poisoned `CDPATH`.** `cd -- "$(dirname "$0")"` without `CDPATH=` in front of
  it searches `CDPATH` for a relative `$0` and echoes the directory it resolved
  to, so the root is somewhere else and every path derived from it is wrong with
  no error. `TestShimIgnoresAPoisonedCDPATH` reproduces it.
- **Another tree's `bootstrap`.** Reached through a symlink or a copy on `PATH`,
  the shim resolves *that* tree's root, builds it and runs it, and exits 0 with no
  symptom at all. Same device and inode is the only test that settles it, so the
  shim compares `$BOOTSTRAP_ROOT/bootstrap` against `$0` and refuses when they
  differ.

For a new machine, `./bootstrap apply workstation` performs the complete setup.
For an existing machine the same command converges whatever is missing.

### 2.1 The shim, and the one manual step

`./bootstrap` is a short shell shim. It is the only shell in the design, and
it has exactly one job: get to Go, then hand over.

1. `go` on `PATH` → use it.
2. Otherwise **refuse**, naming the exact command for the detected platform.

**It installs nothing.** An earlier draft ran `brew install go` for you. That
branch was removed rather than repaired, because it produced the design's worst
measured defect: `set -e` does not apply inside `$( )`, so a failed or
off-`PATH` install was swallowed, leaving an empty path that reached the build
as `"" build …` — exit 127, or worse, a silent `exec` of a stale cached binary.
Deleting the
branch removes the failure mode; patching it would have left the shim carrying
exactly the bash semantics that disqualified shell everywhere else here.

It then builds `bootstrap.d/` to a binary under
`${XDG_CACHE_HOME:-~/.cache}/dotfiles-bootstrap/<key>/`, where **`<key>` is
derived from the repository root** — without that, a main checkout and a git
worktree share one binary and whichever built first wins, running old code
against a new tree. It rebuilds when any `.go` file, `go.mod`, or a source
directory is newer than the binary (directories, so a deletion counts), then
`exec`s it with the original arguments. Every failure the shim can *report* exits
`2`; a failed `exec` is the one it cannot intercept, and the shell's own code
reaches the caller — measured as 1 on bash 3.2.57 and 126/127 on 5.3.15.

**Building from the checkout, not installing a release.** The binary always
matches the tree you cloned: no release-version coordination, no drift between
an installed tool and the configuration it applies, and no dependency on
[spec 6](2026-08-11-spec-6-releases-and-distribution.md), which was still unwritten when this was designed.

**The honest cost:** a machine without Go needs one manual command before
`./bootstrap` runs. That is deliberate, and it is the same call spec 1 §9 made
about Codex hook trust: the design's job is to make a required manual step *one
obvious step* rather than a silent failure.

### 2.2 The seam that would remove Go as a dependency

Recorded now because it is the actual answer to §2.1's manual step, and because
a future reader will otherwise re-litigate the auto-install branch.

*(This seam was scoped under spec 5 until 2026-08-11, when spec 5 was narrowed
to the verification gate and distribution became spec 6.)*

**If [spec 6](2026-08-11-spec-6-releases-and-distribution.md) publishes verified
release binaries, the shim stops needing Go at all.** That is not true of spec 6 as
written: its pipeline builds and publishes `agents`, and this shim runs a
`bootstrap` binary, so the seam waits on a `bootstrap` release that nothing builds
today. Its structure becomes:

1. A released binary matching this checkout is cached and verified → `exec` it.
2. Otherwise, if a release exists for this platform → download, verify checksum,
   cache, `exec`.
3. Otherwise `go` is on `PATH` → build from source as today.
4. Otherwise refuse.

Steps 3 and 4 are exactly today's shim, so this is an extension, not a rewrite,
and **spec 2 still does not depend on spec 6** — build-from-source remains the
floor. The design constraint that matters is the one already in place: the shim
must never `exec` a binary it cannot attribute to the current checkout. Today
that is the cache key; under spec 6 it becomes the checksum plus a version the
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
| 40 | devtools | `uv`, the released `agents` from the tap, global Git hooks | ✓ | — |
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
alternative — per-distro native package lists, which `super-install-dep.sh` did —
needs two or three manifests kept in sync across distros whose package
names disagree (`fd` vs `fd-find`, `bat` vs `batcat`), and several tools in use
here are not consistently packaged natively at all.

The PATH assumption already exists: `fish/mypre.fish` adds
`/home/linuxbrew/.linuxbrew/bin` on Linux and `/opt/homebrew/bin` on macOS.

The `Brewfile` carries platform sections, because casks are macOS-only. It
replaced the three `brew/*.list` files — deleted 2026-08-11 when both platforms
were unified on Homebrew (`30369c3`) — and those lists were **audited, not
mechanically translated**: of their thirty entries, `micromamba` was dropped and
`youtube-dl` replaced by `yt-dlp`, so the `Brewfile` is not their concatenation.

### 3.2 Fish

Confirm fish was installed by phase 10; add its resolved path to `/etc/shells`
if absent; change the login shell only when it is not already fish; install
fisher plugins explicitly rather than relying on a shell-start side effect.

This phase first ran for real on 2026-08-17 — see [Measured facts](#measured-facts-2026-08-10).

### 3.3 Devtools

`uv`, the released `agents`, and global Git hooks. The Git-hook step
**delegates to `git/install-hooks.sh`**, which already performs this carefully
and has tests. It is not reimplemented.

*Corrected 2026-10-09:* this phase used to build `agents` into `~/bin/agents`.
The personal build was removed — the `Makefile` and its builder are gone, and
`bootstrap.d/internal/phase/devtools.go` resolves the released binary through
Homebrew instead — so there is no `~/bin/agents` to install or to name.

> **Amended 2026-10-09 — the middle item is retiring.** `devtools` stops building
> `agents`: the personal build goes in favour of the released binary
> (`brew install nilbot/tap/agents`), decided 2026-10-09 and landing with the
> change that consumes the tap. The phase keeps `uv` and the Git-hook step; what
> it loses is the second `agents` it compiled into `~/bin/agents` — the artifact
> this document's own §9 records as the one no phase reconciled and no check
> noticed. The Git-hook step's delegation, and the reason it is delegated rather
> than reimplemented, are untouched. See
> the 2026-10-09 decision: nothing builds `agents` from a checkout.

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

	// mutations the migrations need (§8.1). They are in the interface the
	// phases use, and the phase package still cannot reach the machine any
	// other way.
	Copy(source, target string) error
	Rename(source, target string) error
	RemoveAll(path string) error
	WriteFile(path string, data []byte) error
}
```

**The phase package imports nothing capable of I/O.** Not `os`, not `os/exec`.
It receives a `change.Interface` and can reach the machine no other way, so "a
phase cannot mutate outside dry-run control" is a property of the **import
graph** — exact, complete, and machine-checked by one test that asserts the
package's import set.

This is the same move spec 1 §3.2 makes for redaction, and the reason it had to
be this rather than a lexical scan is that the shell version's grep-based
equivalent was written wrong twice — once flagging the *correct* form, once
missing a real violation.

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

This is a change in kind. The Makefile, deleted 2026-10-09, did `rm -rf $HOME/.vim`,
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
# the shape of the table; bootstrap.d/links.manifest is the list
# kind  source                            target                            platform
link    starship.toml                     .config/starship.toml             *
link    tmux/tmux.conf                    .tmux.conf                        *
link    git/gitignore_global              .gitignore                        *
link    macOS/ghostty                     .config/ghostty                   darwin
link    macOS/alacritty/alacritty.toml    .config/alacritty/alacritty.toml  darwin
dir     -                                 .config/fish                      *
seed    fish/config.fish.template         .config/fish/config.fish          *
seed    git/gitconfig.local.template      .gitconfig                        *
```

The machine's Git identity and signing live in `~/etc/extras.secret/gitconfig`,
which `~/.gitconfig` reaches through an include inside the shared config. It is
machine-local and deliberately untracked: `git/gitconfig.shared:245` says so at
the point where the temptation to put identity there would arise. Nothing in this
repository writes it and nothing checks it, so an edit that drops the include
loses identity and signing with no error — the same silent-loss shape as section 8's shared include, with credentials
rather than settings.

`check` verifies each target is the **right kind**. A `link` target that has
become a real file is a finding. A `seed` target that has become a symlink into
the repo is a finding — that second one is the `~/.gitconfig` regression spec 1
fixed and the fish regression §7 fixes.

**Harness instruction files are ordinary rows.** `links.manifest` points three
harness paths at one tracked file. Skill directories are `agents wire`'s job, not
this table's: a harness writes into its own skills path, and §1's rule says a path
another program writes must never be a link into this repository.

This is the bootstrap's job rather than `agents`'. The reason once given was
ordering: **`agents` was a Go binary built by phase 40, so it could not be a
dependency of phase 20.** That reason is gone — it arrives with the packages phase's
`Brewfile` row, well before phase 20 — and what remains is the division of labour:
this table links, and generation is `agents`'. The trigger for revisiting, stated the
way the deleted spec 4 states its triggers:

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
Makefile target did this until it was deleted; `install-hooks.sh` validates the file but never
creates it), then *validated* by `install-hooks.sh` in phase 40.

`check` asserts no path is claimed twice. Two owners is how `softlinks.sh` and
the Makefile drifted apart in the first place.

## 7. Fish: zero symlinks under `~/.config/fish`

`~/.config/fish` was a symlink to `dotfiles/fish/`, so fisher wrote `functions/`,
`completions/`, `conf.d/`, `fish_plugins` and `fish_variables` into the tracked
repo, held back only by `.gitignore`. It is a real directory today:
`bootstrap.d/links.manifest:28` declares it `dir`, and rows `:29` and `:34` seed
the two files fisher writes, `config.fish` and `fish_plugins`.

Per-file symlinking does **not** fix this, and that is the important part.
Third-party installers write to `config.fish` by appending managed blocks, so
the one file that must be tracked is also the one file that gets written to. The
evidence is in git history: tracked `fish/config.fish` carried a
`# >>> grok installer >>>` block, and tracked `fish/mypost.fish` carried a block
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

1. **Five `.gitignore` lines were deleted** — `fish/fish_variables`,
   `fish/fish_plugins`, `fish/functions/`, `fish/completions/`, `fish/conf.d/`.
   They existed only to hold back writes that no longer arrive. `.gitignore` has
   stopped being load-bearing.
2. **Two tracked functions were built around the old layout and were rewritten,
   not merely relinked.** `fish_reset_all` did
   `rm -rf $HOME/dotfiles/fish/{functions,completions,conf.d}/` and now targets
   `$__fish_config_dir`; `install_fisher` read `$HOME/dotfiles/fish/fishfile`
   and now resolves it from `(status dirname)`.
3. **`check` gained a real check.** If anything strips the `source` line from the
   stub, the entire shared configuration goes dark with no error at all. That is
   a silent total failure and it is cheap to detect.

The `# >>> grok installer >>>` block was lifted out of tracked `config.fish` into
the local stub. The mamba block in `mypost.fish` went entirely (§9).

## 8. The `.symlink` rename, and the migration hazard inside it

The suffix has stopped carrying information, and for two different reasons:

- `git/gitconfig.symlink` → **`git/gitconfig.shared`**. Since `37f00a0` it is
  *included* by a machine-local `~/.gitconfig`, not symlinked to it.
- `git/gitignore_global.symlink` → **`git/gitignore_global`**. Still genuinely
  symlinked, but the manifest's `kind` column now carries what the suffix used
  to.

**The first rename is a live hazard.** Every existing machine's `~/.gitconfig`
contains `path = ~/dotfiles/git/gitconfig.symlink`. It is a `seed` row, so
bootstrap never overwrites it — the include would silently point at a missing
file and *every shared git setting would disappear with no error*.

Hence `migrate`, and hence a `check` for it.

### 8.1 `./bootstrap migrate`

Declared, idempotent, one-time operations. Each refuses unless its preconditions
hold. Keeping them out of `apply` preserves §5 intact: `apply` never clobbers,
and the code that knows about the past is quarantined where it can be pruned
once no machine needs it.

Migrations come in two kinds, and the kind determines whether a bare
`./bootstrap migrate` runs it:

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

**Reclaiming migrations** run only when named — `./bootstrap migrate <name>`:

3. ~~**mambaforge** — remove `~/sdk/mambaforge` (3.5 GB).~~ **Deleted
   2026-10-09. Nothing declares this kind now**, so a bare `./bootstrap migrate`
   has no reclamation to list and `./bootstrap migrate mambaforge` answers
   `unknown migration "mambaforge"; known migrations are fish, gitconfig,
   gitignore` at exit 3. The reasoning is kept because it is the record of what
   the kind was for and what deleting it costs:

   > Precondition: refuse if anything on `PATH` resolves inside it. Measured on
   > this machine: nothing does, and its four environments are named by Python
   > version alone (`3_9`–`3_12`), which is the job `uv` now performs.

   It went with the package manager it served. `~/sdk/mambaforge` was 3.5 GB of
   untracked data surviving from May 2024; a migration whose subject has left
   the machine is machinery, not safety, and the guard it carried — six
   `LookPath` probes over a hand-written symlink resolver — was more code than
   the removal it protected.

A bare `./bootstrap migrate` **lists** the reclaiming migrations it is eligible
to run, with the exact command for each, and performs none of them. So nothing
has to be remembered or rediscovered later — the tool says what is reclaimable
and how — while a routine invocation can never destroy untracked data. This is
the same shape as the rest of the design: declare the kind, and behaviour
follows from the kind.

**The listing and the kind are kept with no instance to exercise them.** That is
deliberate, and the alternatives are both worse: deleting the kind takes the
rule with it and re-deriving it later is how a safety property ships untested,
and keeping a migration with no subject means carrying a destructive code path
for a machine state that no longer occurs — the thing this document's §9 calls
rot. What the deletion does change is testability: `Names(due, Reclaiming)` is
empty on every machine, so `phase.Preflight`'s reconciling-only filter can no
longer be exercised through the phase, and the rule is now proved only against
migrations a test constructs.

## 9. What is removed

Each group got **its own commit naming what was removed and why**. Git history
is the archive — `git show <sha>:bin/rgr.bin` recovers any of this byte-for-byte
forever — so the only real obligation is findability, and that is a
commit-message obligation, not a working-tree one. Keeping dead files in the
tree as "reference" is the mechanism by which a dotfiles repo rots: every future
reader must re-derive which files are live.

| Group | Removed |
|---|---|
| zsh | `zsh/` (344 tracked files), the `omz` target, zsh from `super-install-dep.sh` |
| tools | `tools/` |
| conda/mamba | `miniforge/`, `micromamba` from the package list, the mamba block in `fish/mypost.fish`. The `conda`/`mamba` aliases in `alias.fish` are `type -q`-guarded and go quietly with it. `~/sdk/mambaforge` itself *was* to be reclaimed by §8.1's third migration; that migration was deleted on 2026-10-09, so the installation is reclaimed by hand or not at all |
| stale scripts | `snapshot.sh`, `recover.sh`, `mountcrypt.sh`, `mountsshfs.sh`, `post-install.sh` |
| superseded installers | `super-install-dep.sh`, `user-install-dep.sh`, `softlinks.sh` — content carried into phases 10 and 20 |
| editors | the `editors`, `tmux` and `extra` targets, and `spacemacs/` |
| fonts | `install-font-linux.sh` |
| bins | `bin/` (all four) and the `bins` target |
| go hook | `git/hooks/go.pre-commit` |
| unlinked | `gnupg/`, `macOS/iterm2/` |
| Makefile | everything except the `agents` target — ~~and that survives~~ **superseded 2026-10-09: the `agents` target is retired with the personal build, so the whole file goes** |

`macOS/filebrowser/` stays: a self-contained opt-in launchd setup with its own
script, claimed by no phase.

Two side effects the removal commits state: `~/.spacemacs` becomes a dangling
symlink on this machine, and `~/.tmux/plugins/tpm` and `~/.vim` remain on disk.
Dropping a target stops *managing* a thing; it does not delete it.

### 9.1 Why `go.pre-commit` goes

It runs `go build -n && go test && go fmt && go vet` on every commit in any repo
with `.go` files at the root. Three defects, any one of which is disqualifying:

- Every command is `>/dev/null 2>&1`, so a failure yields a generic message and
  **no diagnostic**.
- `go fmt` **rewrites files mid-commit without staging them**, so a formatting
  fix silently fails to be committed.
- `go test` on a large module makes every commit slow.

This is CI's job, and CI is [spec 5](2026-08-11-spec-5-verification-gate.md).

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
7. `agents` on `PATH`. *(workstation only)* — a `LookPath` and nothing more;
   `agents doctor`'s result was never folded in.
8. `Brewfile` packages present. *(workstation only)* — **removed 2026-10-09.** The
   check ran `brew bundle check`, and the `brew` wrapper auto-updates Homebrew before
   it dispatches, so the query verb mutated the machine it was reporting on. With the
   command gone it would have reported "every Brewfile entry is installed" having
   asked nothing, so [the query design](2026-10-09-what-a-query-is.md) removes the
   check rather than repairing it. Seven checks remain.

`check` takes the same profile argument as `apply` and defaults to
`workstation`. Checks 6–7 concern state the `dotfiles` profile deliberately does
not manage, so under that profile they report **not applicable** rather than a
finding — otherwise every container run would report two false failures.

Output is a concise healthy / advisory / failure summary.

Exit codes are the shared vocabulary a design since deleted fixed, with the same meanings
(`agents/internal/exitcode` and its test pin the values now): `0` ok, `1` advisory, `2` block, `3` malformed
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
  internal/check/               the seven checks
  internal/migrate/             reconciling migrations (the reclaiming kind is kept; its one instance was deleted)
```

The directory keeps its `.d` suffix: the module *path* is what Go cares about,
and renaming buys nothing. Verified rather than assumed — a Go module in a
dot-suffixed directory builds and tests correctly, and the module path need not
match the directory name.

**No `go.work`, and no shared helper package with `agents/`.** Two independent
`go test ./...` invocations mean a bootstrap failure can never block an `agents`
binary release, which matters because spec 6 builds release CI around that
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
- `apply dotfiles`, then `check dotfiles` (`TestCheckIsHealthyAfterApply`): the
  machine stays converged once the applier has exited. Deliberately not a second
  `apply`, which would hide the defect by fixing what it measured. Per-operation
  idempotence is pinned at the unit level, and it is the property the Makefile
  never had.
- Phase 40 invokes `install-hooks.sh` with the correct arguments.

**Architecture test:** `internal/phase`'s import set contains no I/O package.
This is the §4 invariant, checked exactly.

**Honest limits.** Phases 10, 30 and 40 need network, `sudo`, and a package
manager; their logic is covered by fakes. On Linux `linux-stage-zero` executes them,
and the job would still pass if one failed; on macOS they are not executed at all.
**No Linux machine was available to verify against**, so
Linux support first shipped written and unit-tested but never executed there. Since
2026-08-17 it is: CI runs the `dotfiles` profile's `plan`, `apply` and `check` in a
Debian container on every pull request, and `linux-stage-zero` runs
`apply workstation` on Debian and Arch. What remains unasserted is recorded in
[Known gaps](#known-gaps-2026-08-11) — and, for the phases after stage zero, in
[Risks](#risks) as well — not claimed as working.
The shim itself is shell and has a small integration test, but its Go-absent branch
cannot be tested on a machine that has Go.

## 12. Commit order

Bootstrap landed **before** anything was removed, so the repo was never in a
state where a machine could not be provisioned. This is the order the plan
executed in:

1. `change.Interface` with both implementations, `manifest`, the shim, and the
   dispatcher. Nothing removed; both paths work.
2. Fish inversion (§7) and the `migrate` verb (§8.1).
3. Phase 20 reaches parity with the Makefile's link targets → those targets go.
4. Phase 10 and the `Brewfile` → `super-install-dep.sh` and
   `user-install-dep.sh` go.
5. The removal commits (§9), one per group.
6. ~~Makefile reduced to the `agents` target.~~ *Corrected 2026-10-09:* the
   `Makefile` was removed entirely with the personal build, not reduced. That is
   this list's own logic carried one step further — the target was the last
   thing no phase owned. The release path is `script/package-release.sh` and the
   Homebrew tap. Nothing is built from a checkout; the tap's binary is installed.
7. This document's status, and the README table row, updated.

---

## Measured facts (2026-08-10)

Observed on this machine, not read from documentation.

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
  resolves inside it. *(This is what made it reclaimable, and §8.1's third
  migration was written to do it. **That migration was deleted on 2026-10-09**,
  so nothing in this repository removes the directory now: `./bootstrap migrate
  mambaforge` answers `unknown migration` at exit 3, and the 3.5 GB is reclaimed
  by hand or not at all. The measurement above is kept because it is the reason
  the migration could be written, and the reason its deletion is safe.)*
- `gnupg/`, `gemini/skills/` and `macOS/iterm2/` are tracked and referenced by
  no target. *(All three are gone now. `gnupg/` and `macOS/iterm2/` went in
  `ac5286a` on 2026-08-11; `gemini/skills/` was still linked by a manifest row
  then, and went on 2026-10-01 once that row had been deleted.)*
- `bin/rgr.bin` and `bin/git-chdate.bin` have never worked (§9.2).

### Go module layout

A Go module in a dot-suffixed sibling directory (`bootstrap.d/`) builds and
tests correctly, and its module path need not match the directory name.

### An ancestor that is a symlink is refused (2026-08-10)

Recorded because it is a deliberate behaviour choice, not a consequence.

`bootstrap` refuses to create a directory when any ancestor is a symlink, even
one pointing at a perfectly good directory that `MkdirAll` would have followed.
A machine with `~/.config` symlinked elsewhere is therefore refused rather than
silently provisioned through the link.

This follows this document's own §1 rule — a well-known path's kind is declared and
checked, never discovered — and `dirVerdict` already refuses a
symlink at a `dir` target, and this design's whole premise is that a path's
*kind* is declared and checked rather than discovered. Distinguishing a
symlink-to-directory from a dangling one needs a following `Stat` on
`change.Interface`, which is not worth adding for a case with no live instance
on any machine here — and the refusal names its own remediation, so the cost of
being wrong is one clear message rather than a silent surprise.

---

## Known gaps (2026-08-11)

Measured at the end of implementation. These are facts about what was built, not
caveats: each is stated here so that nobody has to rediscover it on a machine
where it matters.

**1. Linux is partly tested — the `dotfiles` profile and stage zero.** Narrowed
2026-08-17 by spec 5's `linux-dotfiles` job, which runs `plan`, `apply` and
`check` for that profile in a `debian:stable-slim` container on every pull
request. All three exit 0, and `check` after `apply` asserts the machine stays
converged rather than merely that the applier reported success.

What that covers: preflight, the config phase's ten manifest rows on Linux
paths, and the verify phase. What is still not covered:

- any distribution other than Debian and Arch, the two the stage-zero job runs
- the no-tty `sudo` failure: `Applier.run` leaves `cmd.Stdin` nil, so stage
  zero's `Sudo` calls need cached credentials, and the job's passwordless
  `sudo` hides that
- an assertion on the phases after stage zero, which the job executes on both
  images and reports `apply`'s exit code for rather than gating on

The `dotfiles` profile is precisely the one designed to need none of the
machine-specific paths the `workstation` profile touches. That is what makes it safe
to run in CI, and it is also why it cannot stand in for the `workstation` profile.

**Stage zero itself is now covered too, and no further.** Spec 5's
`linux-stage-zero` job runs `apply workstation` on `debian:stable-slim` and
`archlinux:base` — the only route to `stageZero`, since `phase.For` excludes
`packages` from the `dotfiles` profile and there is no per-phase flag. It
gates that the run reaches stage zero, that stage zero's four tools are present
afterwards, and that the tap's `agents` reached the machine — the last added
2026-10-09 with the `Brewfile` row that ships it. It deliberately does **not** gate
the phases after stage zero, because those phases are executed but not asserted — the
bullet above records the missing assertion — and gating them would make every pull
request wait on the whole Linux port. The job reports `apply`'s exit
code rather than swallowing it, so a later-phase failure is visible in the log
without blocking a merge.

Three defects this found, all fixed, none reproducible on macOS:

- `pacman -S` ran with no prior database sync, so Arch stage zero had never
  worked at all — a fresh Arch system ships an empty sync database.
- the fish phase resolved `fish` by bare name, twice. `brew bundle` installs it
  into the Homebrew prefix, a shellenv line in a profile cannot change the PATH
  of the running process, and the phase then told the operator to run the
  command that was already running. macOS never showed it because
  `/opt/homebrew/bin` is on PATH there long before bootstrap starts.
- the `devtools` phase invoked `brew` by bare name for the `uv` install, which
  is the same defect a third time and in a package where `resolveBrew` already
  existed. Found 2026-08-17 only after the fish fix let the run get one phase
  further.

**Updated 2026-08-17 after the devtools fix.** `apply workstation` now runs
every phase to completion on both images and exits 0. That exit code is not a
convergence claim: `Verify` reports and returns nil by design, so `apply` exits
0 while its own verify phase reports failures. On Debian it reports `5 ok, 0
warn, 3 fail`, and those three were what remained open on Linux:

- **`packages` — closed 2026-10-09 by removal rather than repair.** "Homebrew is
  not installed" was the resolve-vs-PATH defect a fourth time, this one in
  `internal/check`: the check asked PATH for `brew` while Homebrew sat at
  `/home/linuxbrew/.linuxbrew`. [The query design](2026-10-09-what-a-query-is.md)
  removed the check, so the gap goes with it. Two of the three stand.
- **`login-shell`** — `chsh` runs and succeeds, but a running process's shell
  cannot change underneath it, so the check can only ever report the shell the
  container started with. Asserting the real effect needs a fresh login.
- **`agents`** — the `devtools` phase built it to `~/bin`, which is not on the
  container's PATH. The personal build was removed on 2026-10-09, and
  `.github/workflows/verify.yml` now probes for the tap's binary with the Homebrew
  prefix on `PATH` — the re-check the removal called for.

`brew bundle` is exercised in full on Debian: `complete! 30 Brewfile dependencies
now installed` was the whole file when that was measured — 30 `brew` and 2 `cask`
lines, and casks are macOS-only. It holds 31 `brew` lines today, the tap's `agents`
row having arrived on 2026-10-09, so the quoted output is a dated measurement rather
than the current count. (This sentence previously said 36 formulae; 36 was never a
count of anything in it.) On Arch it has failed once inside
`brew bundle` on a corrupted partial download in Homebrew's own cache — not
this repository's code, and absorbed by the narrowing rather than fixed.

**2. `plan` exits 2 on a machine that lacks Homebrew or fish**, rather than
previewing. Measured 2026-08-17: this does **not** affect the `dotfiles`
profile, whose `plan` exits 0 in a container with neither installed — it
reports the missing links as the state it intends to change. The gap stands for
`workstation`, which is still unexercised on Linux. `Planner` records a command without performing it, and nothing in
`Machine` reports which happened, so a read that depends on a prior `Run` cannot
tell "it ran and produced nothing" from "it was only recorded". Closing this
needs a design decision about how `Planner` represents a command's effects.
Accepted deliberately: the alternative is a per-operation mode signal every
phase could branch on.

**3. Linux has no managed nerd font.** `font-symbols-only-nerd-font` is a cask,
casks do not exist on Linux, and Homebrew publishes no formula for it. Task 14
removed the vendored `install-font-linux.sh` that used to cover this.

### Found in the field, 2026-08-11, on the first real provisioning run

Both were hit on the repository owner's machine immediately after merge. Neither
was visible to the test suite, because every test drives the fake `Machine` and
`install_fisher` is never really executed.

**4. (closed 2026-08-11 by `92fd687`.)** The fish phase was not idempotent, and its
failure destroyed the record that would fix it. `install_fisher` ran `fisher install`
unconditionally on every apply. Where fisher's own record —
`$__fish_config_dir/fish_plugins` — did not list plugins whose files were already on
disk, fisher refused each with "please remove or move conflicting files first" and
the phase exited 1, taking devtools and verify with it. It then made the next run
fail identically: with nothing successfully installed, `fisher.fish` reached
`command rm -f $fish_plugins` and **deleted the record**, so the machine could not
recover by retrying. The phase now installs only onto a machine with no plugin files
and otherwise reports "leaving them alone", and `links.manifest:34` seeds the record
from `fish/fishfile`.

**5. (closed 2026-08-11 by `c9a9921`.)** The login-shell check trusted `$SHELL`.
`check`'s `login-shell` and the fish phase's `chsh` decision both read
`Context.Shell`. That is inherited and need not describe the account: run from a
process started under another shell, `check` reported "the login shell is /bin/zsh,
not fish" on a machine whose passwd entry says `/opt/homebrew/bin/fish`, and the
phase planned a `sudo chsh` that was not needed. `main.go` now asks the passwd
database first — `dscl` on macOS, `getent` elsewhere — and falls back to `$SHELL`
only when neither answers.

---

## Risks

| Risk | Mitigation |
|---|---|
| Linux support was written before any Linux machine was available to execute it | Retired for the `dotfiles` profile and for stage zero on 2026-08-17: `linux-dotfiles` runs that profile's `plan`, `apply` and `check` in a Debian container on every pull request, and `linux-stage-zero` runs `apply workstation` on Debian and Arch. The `workstation` profile's phases after stage zero remain unasserted — the job executes them and reports `apply`'s exit code — and no distribution other than Debian and Arch is exercised: see [Known gaps](#known-gaps-2026-08-11) 1. |
| `plan` exits 2 on a machine that lacks Homebrew or fish, instead of previewing | Accepted deliberately, not mitigated. [Known gaps](#known-gaps-2026-08-11) 2 records why, and what closing it would cost. |
| Linux has no managed nerd font | Not mitigated. The cask is macOS-only and the vendored Linux installer was removed. [Known gaps](#known-gaps-2026-08-11) 3. |
| The `gitconfig.shared` rename silently empties shared git config on existing machines | `migrate` repoints the include; `check` #5 detects a stale one. |
| The fish stub loses its `source` line, and the whole shared config goes dark | `check` #4. |
| Editing `~/.config/fish/config.fish` silently produces untracked changes | The stub's header says so, mirroring `gitconfig.local.template`. Reduced, not eliminated. |
| `migrate` moves untracked fisher state and could lose it | Each migration refuses unless preconditions hold; the move is within one filesystem and non-destructive to the source until it succeeds. |
| A reclaiming migration irreversibly deletes untracked data | Reclaiming migrations never run from a bare `migrate`; they must be named, and each refuses if anything on `PATH` still resolves inside the target. §8.1. |
| Bootstrap grows into an unmaintainable program | One package per concern; all machine access through `change.Interface`; an architecture test on the phase package's import set. |
| Go is a stage-zero dependency the old design did not have | The shim installs nothing. It refuses with the exact one-liner for the platform — `brew install go`, `apt-get`/`pacman`, or the go.dev download. An earlier version did install via Homebrew, until `set -e` not applying inside `$( )` let a failed install reach the build as an empty command. One manual step on a machine without Go. §2.2's seam would remove it and is unrealised: spec 6 publishes `agents`, while the shim runs a `bootstrap` binary, which has no release path. §2.1, §2.2. |
| The shim is shell, so it inherits the defects that motivated the switch | It does no reconciliation, and has no dry-run mode to keep honest. Its whole job is to reach Go. |
| Removals lose something later wanted | Per-group commits naming contents and rationale; `git show <sha>:<path>` recovers exactly. |

## Open questions

- **Whether machine backup should exist at all.** `snapshot.sh` did not work and
  is removed; nothing replaced it, and no decision has been taken about whether
  it should be.

## Appendix: what has changed since this was written

This document is dated 2026-08-07. Its sections describe the design as it was made,
and they are the authority on the design; this appendix is the authority on what has
moved under it. Each row names what moved, what carries it, where it lands, and whether
that landing carries a dated marker of its own — so a reader can see, without opening
the section, whether the text there has been corrected.

The table lists the changes that landed on a section of *this* document. Sections also
carry dated markers it does not index: §2.2's re-scoping away from spec 5, §3.2's first
real run on 2026-08-17, §5's note on what the deleted Makefile did, and the block of
Measured facts — and so does the head, which records that the `dotfiles` profile has run
on Linux since 2026-08-17. A reader who needs certainty about one section should read it.

| when | what moved | carried by | lands on |
|---|---|---|---|
| 2026-08-11 | the three `brew/*.list` files were deleted; `bootstrap.d/Brewfile` replaced them, audited rather than translated | `30369c3` | §3.1 — marked |
| 2026-08-11 | the fish phase stopped running `fisher install` unconditionally, and the manifest began seeding fisher's record | `92fd687` | Known gap 4 — marked closed |
| 2026-08-11 | the login shell is read from the passwd database, with `$SHELL` only as a fallback | `c9a9921` | Known gap 5 — marked closed |
| 2026-08-17 | `linux-dotfiles` and `linux-stage-zero` landed. Linux executes now; the phases after stage zero are still unasserted, and what the jobs do not gate is the coverage that remains | `d64c9bb`, `4415212` | the honest limits in §11, Known gap 1, and Risks — all three marked |
| 2026-08-28 | spec 6's release pipeline was built and publishes `agents`. It builds no `bootstrap` binary, and the shim runs one, so §2.2's seam for removing the Go dependency has no release to wait on | `ddeebe4`, which builds it in `script/package-release.sh:34-42`; [spec 6](2026-08-11-spec-6-releases-and-distribution.md) | §2.2 and Risks — both corrected in place, no dated marker |
| 2026-09-21 | the tool was cut to six commands, and `~/.claude/skills` stopped being a manifest row. Harness skill symlinking is `agents wire`'s job | `f3b4a74` | §6 — corrected in place, no dated marker; the command cut lands on no section here |
| 2026-10-09 | `agents` is installed from the Homebrew tap as a `Brewfile` row instead of being built from a checkout | `1d6e323` | §3.3 and §12 — marked; §6 — corrected in place, no dated marker |
| 2026-10-09 | the `Makefile`, and with it the last way to build `agents` from a checkout, was deleted | `db03eb3` | §9, §12 — marked |
| 2026-10-09 | the `packages` check was removed rather than repaired | `856cedf` | §10, which keeps seven checks, and Known gap 1 — both marked |
| 2026-10-09 | the reclaiming migration was deleted, so the kind is kept with no instance and `~/sdk/mambaforge` is reclaimed by hand or not at all | `4c5fcff` | §8.1, §9, Measured facts — all marked |
| 2026-10-09 | `agents githook` replaced the `argv[0]` multicall, and the tool has seven commands | `35d9322` | no section of this document; the count is in `docs/design/README.md` and the qna store |
| 2026-10-12 | `docs/archive/` was deleted, taking the plan this spec was executed under | `f083dac` | the status note at the top — marked |

One consequence is worth stating outright, because it is the change most likely to be
missed: **`agents` is no longer built from this checkout.** It arrives as a package, in
the phase that installs packages, so the ordering argument in §6 — that `agents` was
built by phase 40 and therefore could not be a dependency of phase 20 — no longer holds.
What keeps the harness rows in `links.manifest` is the division of labour: that table
links, and `agents` generates.
