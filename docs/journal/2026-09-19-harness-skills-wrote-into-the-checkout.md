# When the harness wrote into the dotfiles checkout

## What happened

`bootstrap.d/links.manifest` declared `~/.claude/skills` as a symlink into the
tracked checkout (`claude/skills`). A harness writing its own skill content into
`~/.claude/skills` therefore wrote **through the link and into the working
tree**. The operator found the checkout carrying 200+ pending changes, none of
them authored here.

Discovered and repaired by hand on **2026-09-19**: the symlink was replaced with a
real directory holding per-skill links into the checkout, leaving whatever else
had been written beside them. The repair was not written down at the time. It
surfaced again on 2026-09-20, from a different direction — `bootstrap plan
workstation` now refuses:

```text
bootstrap: config: refusing: /Users/nilbot/.claude/skills
  problem: exists and is not a symlink
  remedy:  move it aside deliberately, then retry
```

That refusal is correct and is the reason nothing was clobbered during the
repair. What it exposes is that the *declaration* is wrong, not the filesystem.

## Why it went that way

The rule already existed. [Spec 1 §8.4](../design/2026-08-07-agents-repo-context-design.md)
records the same defect twice, from 2026-08-07:

- `~/.gitconfig` was a symlink to `git/gitconfig.symlink`, so every
  `git config --global` wrote into tracked public content — which is how
  identity and 1Password's `gpg.format = ssh` came to be committed there.
- `~/.claude` was symlinked wholesale from the checkout, with the same result.

Both were fixed, and the lesson was written down as **link individual
subdirectories only**, because `~/.claude` and `~/.codex` are harness-owned. That
formulation is what failed here: `skills` *is* an individual subdirectory, so it
passed the test — while being exactly the kind of path the test existed to
exclude, because the harness writes into it. The rule needed to be "does anything
but this repository write here?", which is what the boundary review now proposes.

## What it cost

- The checkout was contaminated with 200+ foreign changes, and separating them
  from real work was manual.
- The machine can no longer be provisioned through the `workstation` profile:
  `plan` exits 2 at `config`, so nothing after that phase runs.
- A machine-local repair now diverges from the tracked manifest, so every reader
  of `links.manifest` sees a declaration the machine deliberately does not
  satisfy — the worst state to leave a declarative system in, and the reason this
  entry exists.

## What is still open

1. **The declaration.** Either `links.manifest` stops claiming
   `~/.claude/skills`, or the harness side stops writing there. Both cannot
   stand. This is a decision, recorded as item 1 in
   [the boundary review](../design/2026-09-20-agents-and-bootstrap-boundary.md).
2. **The writer is not identified with certainty.** The observed contents were a
   per-skill link into the checkout plus a directory named `synced/`
   (`~/.claude/skills/synced`, first seen 2026-09-19 08:56); nothing in this
   repository's Go code creates per-skill links under `$HOME`. Attribution is the
   operator's: a harness, or a tool acting for one. Guessing further would put a
   name in the record that the evidence does not support.
3. **Whether the same shape exists for the other harnesses.** `links.manifest`
   also declares `~/.gemini/skills` and `~/.codex/skills`; a harness that writes
   its own skills into either recreates this exactly.
