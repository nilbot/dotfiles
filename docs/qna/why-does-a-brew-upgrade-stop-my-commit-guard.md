# Why does a `brew upgrade agents` stop my commit guard?

## Context

Measured on 2026-09-20, upgrading this machine from agents v0.5.1 to v0.6.0:

```text
$ brew upgrade agents
==> Cleanup
Removing: /opt/homebrew/Cellar/agents/0.5.1... (5 files, 3.5MB)
```

All four links in `~/dotfiles/git/hooks.d/` pointed at
`/opt/homebrew/Cellar/agents/0.5.1/bin/agents` — the version-specific keg that
cleanup had just deleted. `agents doctor` reported:

```text
FAIL  git-hooks:links              pre-commit does not resolve to the current binary
```

## Answer

**Git runs a dangling hook as if no hook existed.** Measured in a disposable
repository whose `core.hooksPath` held a `pre-commit` pointing at the deleted keg:

```text
$ git commit --allow-empty -m test
[main (root-commit) f19410f] test
```

Exit 0, no warning, commit created. The guard that scans staged changes
(`agents guard --staged`) had silently stopped running. Nothing else fails: the
commit succeeds, the message is written, and only `agents doctor` will ever say
otherwise — if it is run, in a repository, before the next commit that matters.

Two causes, and the second is what made the failure silent:

1. **The link was pinned to a version.** Homebrew installs into
   `Cellar/agents/<version>/` and deletes the old keg on upgrade. `bin/agents`
   and `opt/agents/bin/agents` are repointed at the new keg; a link to the keg
   itself is not.
2. **The installer refused to repair it.** `git/install-hooks.sh` required every
   existing link to name the binary it was handed, so that it could never
   overwrite a hook it did not write. Its own link from the previous install is
   not that, so the repair was refused:

```text
install-hooks: refusing: owned pre-commit hook ... points to
'/opt/homebrew/Cellar/agents/0.5.1/bin/agents', not
'/opt/homebrew/Cellar/agents/0.6.0/bin/agents'; move it aside deliberately, then retry
```

Doctor's remedy at the time — "run the reviewed global hook installer" — named no
path, no arguments and no flag, so it pointed straight at that refusal.

### What to do

Install and repair through the **stable** path, never the keg:

```bash
bash ~/dotfiles/git/install-hooks.sh install \
  ~/dotfiles "$HOME" "$(command -v agents)"
```

`$(command -v agents)` is Homebrew's `bin/agents`, which is repointed on every
upgrade, so the hooks keep resolving to the current binary with nothing to re-run.

When the links are already stale — the shape above — add `--adopt-owned`:

```bash
bash ~/dotfiles/git/install-hooks.sh install --adopt-owned \
  ~/dotfiles "$HOME" "$(command -v agents)"
```

`--adopt-owned` repoints a link only when it is provably one this installer could
have written: a keg path for agents, current or already deleted. A foreign file,
or a link to another program, is refused with or without the flag. It is accepted
on either side of the mode, because doctor prints `install --adopt-owned`.

### Changed 2026-09-20

- The installer accepts a symlinked binary when it resolves into a Homebrew keg
  for agents, and records the path it was given — so it records `bin/agents`, not
  the keg it resolves to.
- `--adopt-owned` exists, and the ordinary refusal now names it.
- Passing the keg path while the stable path is already installed is refused with
  the stable path named, instead of offering a flag that would re-pin the hooks.
- `agents doctor` prints the exact command that repairs the check, with
  `--adopt-owned` when the failing link is ours, and gained `git-hooks:unmanaged`,
  a warning for dangling links under names this repository does not manage. Two
  such links (`pre-commit-user`, `post-checkout-user`) outlived this upgrade
  unremarked until the check existed.
