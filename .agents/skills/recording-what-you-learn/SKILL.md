---
name: recording-what-you-learn
description: Use when something worth keeping surfaces during work - the human says "save that", a run collapses, an approach is abandoned - or before asserting a claim about an area the repository may already have answered. Covers where knowledge goes, the shape it takes, and why it is documentation rather than a tool's private store.
---

# Recording what you learn

Knowledge about a codebase is **documentation**. It goes in the repository's
docs, in plain markdown, committed like anything else. There is no queue, no
promotion step and no separate store to sync.

Two stores, one retrieval axis each:

| | indexed by | answers |
|---|---|---|
| `docs/qna/` | topic | "how does X actually work / why did X happen" |
| `docs/journal/` | time | "what was I doing, and why did it go that way" |

A repository may name different directories in its own `AGENTS.md`. If it names
none and has a `docs/`, use these. If it has no `docs/` at all, ask before
creating one.

## When to write

**The human notices.** "Save that", "good to know", "worth remembering" — that is
the trigger, and it is the strongest one available because a human just told you
this mattered. Write it while the context is still in front of you. Do not batch
it, and do not ask whether it is worth it: they already answered that.

**The work hits something.** A bug understood, a training run collapsed, a build
broken in a way that took real effort to diagnose, an approach abandoned and why.
These earn a `docs/journal/` entry at the moment they resolve, not at session end.

Nothing fires because a session ended. A session ending is not evidence that
anything happened.

## When to read

**Before asserting a claim about an area, grep for it.** Not "consider checking" —
grep. The failure this prevents is specific and measured: an agent wrote that a
CI leg was not reproducible locally while the repository's own spec recorded the
correction, one `grep` away. Confident recall is indistinguishable from knowledge
from the inside, which is why the check has to be mechanical rather than a
judgement about whether you are sure.

```bash
grep -ril "<distinctive noun>" docs/qna/ docs/design/
```

Distinctive nouns are what work: an image name, a flag, a tool, an error string.
Common words return everything and tell you nothing.

## The shape

```markdown
# [The question you would ask when you hit this again]

## Context
[when and why it arose — with the concrete numbers]

## Answer
[the mechanism, with evidence]
```

**Question-first.** Name the file for the question, not the conclusion — a reader
arrives with a question. `opponent-pool-contamination.md`, not
`pooling-is-harmful.md`.

**Length is set by the finding, never by a rule.** Measured across two
repositories: entries written to a three-bullet bound clustered at 175-243 words;
entries sized by their content ranged 418-701. Uniform length is the tell of a
rule, not of a subject. Constrain shape; do not constrain length.

**Carry the numbers.** "Games got shorter" is not the entry. "Average game length
collapsed from 136.4 plies to 106.2, capture rate 29.9% to 22.9%" is. The numbers
are what make it checkable later, and what make it worth reading at all.

No frontmatter, no schema, no generated index. `ls` is the index and `grep` is
the query.

## Subagents will not do this

Measured on Claude Code: subagents inherit `AGENTS.md` but do not act on it — 0
of 31 observed subagents followed an inherited directive. If you are dispatching
subagents, **the recording is yours**, from what their reports tell you. A child
that discovers something will report it and then forget it; nothing downstream
picks it up.

This is a real gap, not a formality. Work done through subagents is exactly the
work most likely to produce findings and least likely to record them.

## Where this comes from

`docs/design/2026-08-19-knowledge-is-documentation.md` in the dotfiles
repository, with the evidence for each rule above. Read it before changing any
of this — several of these choices look arbitrary and are not.
