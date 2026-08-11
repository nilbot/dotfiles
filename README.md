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

**Linux is untested.** No phase of this has ever run on Linux. The code is
written and unit-tested for Debian/Ubuntu and Arch/Manjaro; nothing more than
that is claimed. Two further gaps — `plan` refusing on a machine that lacks
Homebrew or fish, and no managed nerd font on Linux — are recorded with the rest
in [spec 2](docs/superpowers/specs/agents/2026-08-07-spec-2-dotfiles-hygiene.md#known-gaps-2026-08-11).

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

## Layout

| Path | What |
|---|---|
| `bootstrap`, `bootstrap.d/` | the provisioner: shim, phases, `links.manifest`, `Brewfile` |
| `agents/` | the `agents` binary — repo-tracked agent context (spec 1) |
| `fish/`, `tmux/`, `claude/`, `gemini/`, `macOS/`, `starship.toml` | tracked configuration, reconciled by `bootstrap.d/links.manifest` |
| `git/` | partly the manifest's (`gitignore_global`, the local template) and partly `install-hooks.sh`'s: `~/.gitattributes` and `core.hooksPath` are the installer's, not the manifest's |
| `docs/superpowers/` | the specs and plans that carry the reasoning |

Start with [the spec index](docs/superpowers/specs/agents/README.md).
