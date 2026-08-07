# Spec 2 — dotfiles hygiene

**Status:** scope only — not designed, not implemented
**Depends on:** [spec 1](2026-08-07-agents-repo-context-design.md) §8 must land first,
or the git-hook cleanup gets done twice.

This is the residue left over after spec 1: changes to the dotfiles repo that have
nothing to do with agents and were split out to keep spec 1 reviewable. No shared
code with spec 1; the only ordering constraint is §8.

## Scope

**Remove zsh.** `zsh/` is 1.6 MB across 335 files, mostly vendored oh-my-zsh custom
themes and plugins. The login shell has been fish for some time
(`dscl` reports `/opt/homebrew/bin/fish`). The `omz` Makefile target, the zsh
branches in `snapshot.sh` and `super-install-dep.sh`, and the zsh references in
`tools/*.sh` all go with it.

**Rationalize the link machinery.** `softlinks.sh` and the `Makefile` `dotfiles`
target overlap and have drifted. The two live landmines are already fixed
(2026-08-07, `37f00a0` — see spec 1 §8.4); what remains is consolidation of the two
overlapping mechanisms into one.

Also rename `git/gitconfig.symlink`. The name is now wrong: as of `37f00a0` it is
*included* by a machine-local `~/.gitconfig` rather than symlinked to it. Same for
`git/gitignore_global.symlink`, which is still genuinely symlinked — so the two
files now need different names for different reasons, and the `.symlink` suffix has
stopped carrying information.

**Remove the dead `claude/` scripts** left over once spec 1 §8 lands:
`claude/commit-msg` (GNU `sed -i`, broken on macOS, referenced by nothing),
`claude/check-commits.sh` (points at `~/.git-templates/`, a path that has never
existed), `claude/setup-protection.sh` (re-sets a value `gitconfig.symlink` already
sets), `claude/update-repo-hooks.sh` (obsoleted by `core.hooksPath`).

**Decide the fate of `git/hooks/go.pre-commit`.** It runs
`go build -n && go test && go fmt && go vet` on every commit in any repo with `.go`
files at the root. Under spec 1 §8's chain it keeps working unchanged; whether it
*should* run on every commit is a separate question worth asking once.

**Housekeeping noticed in passing:** `fish/config.fish.bak.1784491210` is an
untracked stray backup; `.DS_Store` files are committed in several directories.

## Explicitly out of scope

Anything touching `.agents/`, the `agents` binary, or harness wiring. That is spec 1.

## Open questions

- Is any zsh config still referenced by a remote machine or container image that
  clones this repo? Check before deleting rather than after.
- Does `snapshot.sh` still do something wanted? It is untouched since 2021.
