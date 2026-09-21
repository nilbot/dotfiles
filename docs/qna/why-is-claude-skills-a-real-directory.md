# Why is `~/.claude/skills` a real directory when the manifest says symlink?

## Context

`bootstrap.d/links.manifest:20` declares:

```text
link    claude/skills                     .claude/skills                    *
```

On this machine `~/.claude/skills` is a real directory, so
`bootstrap plan workstation` refuses and stops the whole run:

```text
bootstrap: config: refusing: /Users/nilbot/.claude/skills
  problem: exists and is not a symlink
  remedy:  move it aside deliberately, then retry
```

## Answer

The symlink was **removed deliberately on 2026-09-19**, after a harness wrote its
own skill content through it and into the tracked checkout — 200+ foreign pending
changes, by the operator's account. See
[journal: when the harness wrote into the dotfiles checkout](../journal/2026-09-19-harness-skills-wrote-into-the-checkout.md).

This is the **third instance of one defect class**, and spec 1 §8.4 records the
first two from 2026-08-07: `~/.gitconfig` was a symlink to
`git/gitconfig.symlink`, so `git config --global` wrote into tracked content; and
`~/.claude` was symlinked wholesale from the checkout, with the same result. The
lesson written down then was "link individual subdirectories only", because
`~/.claude` and `~/.codex` are harness-owned.

**That formulation is what failed.** `skills` is an individual subdirectory, so
it passed the rule — while being exactly the kind of path the rule existed to
exclude, because the harness writes into it. The question that works is not "is
it a subdirectory?" but:

> **Does anything but this repository write here?**

`~/.claude/skills` answers yes. So does `~/.codex/skills` and
`~/.gemini/skills`, which the manifest declares the same way; any harness that
writes its own skills into one of them recreates this incident.

## What to do about the refusal

Do not "fix" it by restoring the symlink: that re-arms the incident. The
declaration has to change, and it is item 1 of the deferred list in
[the boundary review](../design/2026-09-20-agents-and-bootstrap-boundary.md).
Until it is decided, `bootstrap plan|apply workstation` is blocked on this
machine, and the machine's real directory — per-skill links into the checkout,
plus whatever the harness wrote — is the state to keep.

## Follow-up, 2026-09-21

**The declaration was changed, in the direction this entry argues.** The
`claude/skills` and `gemini/skills` rows were deleted from
`bootstrap.d/links.manifest` in the refactor that cut the `agents` tool to six
commands — the same change that deleted the layout, trace, save, ls, update and
hook subcommands, so the two per-harness skill trees had no product left to
carry. Measured after it:

```text
$ BOOTSTRAP_ROOT=~/dotfiles bootstrap plan dotfiles
== config
== verify
```

The `config` phase is empty, where it used to stop on
`bootstrap: config: refusing: /Users/nilbot/.claude/skills`. **The directory was
not changed.** `~/.claude/skills` is still a real directory holding per-skill
links into the checkout plus whatever the harness wrote, which is the state to
keep. What changed is that no row declares the path any more, so the refusal this
entry was written about no longer happens and `bootstrap plan|apply workstation`
is no longer blocked by it.

`~/.gemini/skills` is not on this machine at all, and the row that named it is
gone with it. `~/.codex/skills` is untouched: it was symlinked from the checkout
rather than from a per-harness skills tree, and nothing about it was refused.
