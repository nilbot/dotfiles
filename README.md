# dotfiles

Configuration for this machine, and the provisioner that applies it.

## Provisioning

```sh
./bootstrap plan  workstation    # what it would do, changing nothing
./bootstrap apply workstation    # do it
./bootstrap check                # is this machine still converged
./bootstrap migrate              # what needs migrating, and how
```

`plan`, `apply` and `check` take a profile. `workstation` is the whole machine.
`dotfiles` is preflight, config and verify only — no sudo, no network, no
package manager, no login-shell change — which is what makes it safe in a
container or on a machine whose packages are managed elsewhere.

Exit codes: `0` ok, `1` advisory, `2` block, `3` malformed input, `4` not
applicable. `./bootstrap --help` prints the full surface.

`./bootstrap` is a small shell shim that builds `bootstrap.d/` and hands over to
it. It installs nothing: on a machine without Go it refuses and names the exact
command to run first.

**Linux is partly proven, and the boundary matters.** Every pull request and
every push to `master` runs two container jobs:

- **the `dotfiles` profile, gated in full** on `debian:stable-slim` — `plan`,
  `apply` and `check` all exit 0, and `check` runs *after* `apply` so the claim
  is convergence rather than "the applier reported success".
- **the `workstation` profile, gated as far as stage zero** on
  `debian:stable-slim` and `archlinux:base`, asserting the run reaches stage
  zero and that its prerequisites exist afterwards.

Beyond stage zero, `apply workstation` has been observed running every phase to
completion and exiting 0 on both images (2026-08-17), but that is **not gated,
and its exit code is not a convergence claim** — the verify phase reports and
returns nil by design, so `apply` exits 0 while verify reports 3 failures on
Debian. Those three, plus the two standing gaps — `plan` exits 2 for
`workstation` on a machine lacking Homebrew or fish, and Linux has no managed
nerd font — are recorded with the rest in
[spec 2](docs/design/2026-08-07-spec-2-dotfiles-hygiene.md#known-gaps-2026-08-11),
which is the authority. This paragraph is a summary of it and will go stale
first.

## The Makefile

`make agents` builds the `agents` binary to `~/bin/agents`. It is the only
target left, and it is a developer convenience for inner-loop work on `agents/`
— `./bootstrap apply workstation` builds the same binary in its devtools phase.
Provisioning belongs to `./bootstrap`; `make dotfiles` is retired, not aliased.

**Run it from the main checkout, not a linked worktree.** The binary is stamped
with the checkout it was built from, and this target writes the single global
`~/bin/agents` — so from a worktree it publishes a binary stamped to a temporary
path. Delete that worktree and the stamp names nothing: `doctor` still passes,
because it compares paths that agree with each other, while the git hook chain
finds no extras directory and silently runs none of your personal hooks, at exit
0. Rebuild from the main checkout to repair.

## The `agents` tool

`agents` manages the machine: harness wiring, the git hook chain, the
pre-commit guard, and the machine-local cache of agent transcripts. It does
**not** manage knowledge — that half was retired on 2026-08-20, and what a
repository knows now lives in its own `docs/`, written by instruction rather
than by a command. See
[knowledge is documentation](docs/design/2026-08-19-knowledge-is-documentation.md).

Repositories follow a **Two-Tier Agent Context** structure:
- **Tier 1 (Root Router)**: Standardized, lightweight `AGENTS.md` (with `CLAUDE.md -> AGENTS.md` symlink) serving as the entrypoint bootstrap router pointing to durable docs and domain rules.
- **Tier 2 (Domain Context & Durable Knowledge)**: Repository-specific domain guidelines in `.agents/AGENTS.md`, executable skills in `.agents/skills/`, and durable project knowledge organized in a **4-store layout** under `docs/` (`design/`, `plans/`, `journal/`, `qna/`).

`agents help <command>` explains any command in full; **when** to reach for one
is in the skill under `claude/skills/agents-tool/`, and when to write something
down is in `.agents/skills/recording-what-you-learn/`.

The prose here is hand-written because knowing *when* to reach for a command is
judgment. Only the table between the markers is derived, and it comes from the
same declarations `agents help` reads, so it cannot describe a command set the
binary does not have. If it drifts, regenerate it rather than editing it:

```bash
agents help --render=markdown
```

<!-- BEGIN GENERATED: agents help --render=markdown -->
| Command | What |
|---|---|
| `agents help` | print the listing, or one command's page |
| `agents init` | create .agents/, triggers, wiring, fleet entry |
| `agents wire` | regenerate harness configs (merges, never overwrites) |
| `agents doctor` | report wiring, trust evidence, reachability, and lane health |
| `agents drift` | inspect context layout and router drift |
| `agents save` | commit .agents/ paths and nothing else (escape hatch) |
| `agents trace` | query records; read one back; copy reachable ones |
| `agents trace ls` | query records |
| `agents trace show` | read one transcript back |
| `agents trace cache` | copy reachable transcripts into the store |
| `agents trace cache prune` | remove cached copies, never the records |
| `agents trace migrate` | move a tracked index into the machine-local store |
| `agents ls` | list the fleet on this machine |
| `agents update` | rewire every registered repo (dry run by default) |
| `agents version` | print binary version and build provenance |
| `agents guard` | pre-commit checks (the only command that blocks) |
| `agents hook` | harness hook entrypoint |
<!-- END GENERATED -->

## Development & Contributing

- **Branch Protection & Pull Requests**: Direct pushes to `master` are prohibited by GitHub Rulesets. All contributions, features, and documentation fixes must be developed on dedicated branches and merged via Pull Requests.
- **Verification Gate**: Pull Requests must pass the required CI verification workflow (`gate` job) before merging.
- **Documentation Invariant**: Whenever an `agents` CLI command, subcommand, or flag is added, changed, or retired, `agents/README.md`, `README.md`, help texts (`agents help`), and `docs/` must be updated in the same change set.

## Layout

| Path | What |
|---|---|
| `bootstrap`, `bootstrap.d/` | the provisioner: shim, phases, `links.manifest`, `Brewfile` |
| `agents/` | the `agents` binary — repo-tracked agent context, harness wiring, drift detection, and trace cache |
| `AGENTS.md`, `CLAUDE.md` | Tier 1 canonical root router and harness compatibility symlink |
| `.agents/` | Tier 2 repository engineering guidelines (`.agents/AGENTS.md`), bundled skills (`.agents/skills/`), and harness hooks |
| `fish/`, `tmux/`, `claude/`, `gemini/`, `macOS/`, `starship.toml` | tracked configuration, reconciled by `bootstrap.d/links.manifest` |
| `git/` | partly the manifest's (`gitignore_global`, the local template) and partly `install-hooks.sh`'s: `~/.gitattributes` and `core.hooksPath` are the installer's, not the manifest's |
| `docs/` | 4-store layout: `design/` living specs, `plans/` implementation plans, `journal/` dated records, `qna/` answers by question, `archive/` immutable pre-2026-08-20 history |

Start with [the spec index](docs/design/README.md).
