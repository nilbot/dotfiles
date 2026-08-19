# Knowledge is documentation, not a subsystem

**Date:** 2026-08-19
**Status:** design — approved to execute
**Retires:** spec 7's capture half (queue, review, promotion); spec 3 in full
**Leaves intact:** spec 1's placement rule, tiers and record schema; specs 2, 5, 6

This document is deliberately short. A redesign whose central finding is that
20,000 lines of specification produced four documents should not open with
another eight hundred.

## What forced it

Two repositories, both this operator's, measured 2026-08-18.

| | `dotfiles` | `autogo-mlx` |
|---|---|---|
| specification | 20,197 lines of specs, plans and analysis | one ~60-line `AGENTS.md` |
| mechanism | queue, review, promotion, indexes, guard | none |
| trigger | session end, unconditional | human says "save it"; a run collapses |
| review | after writing — 33% promoted, 5-day lag | before writing |
| corpus | 2 memory entries, 2 handoffs | 9 Q&As, a lessons index, 6 session logs, 3 reports |
| subject | the toolchain itself | the actual work |

The fleet holds two real repositories. In the one that does not build the tool,
`.agents/` has **zero** memory entries and zero handoffs after a week. Every
artifact the system has ever produced is about the system.

Spec 7's own controlled experiment already carried the answer and stopped one
step short: treatment drafted 5 of 7 sessions and all 5 were promoted, control
drafted 0 of 3. It concluded "the instruction causes the drafting." The
unasked question is what the rest of the apparatus was for.

**The error was treating a content problem as a mechanism problem.** Capture was
never the bottleneck. `autogo-mlx` captured abundantly with no tool, no queue and
no gate, because the work produced findings worth keeping. `dotfiles` captured
findings about `dotfiles` because that is what its work produced. The content
decided; the mechanism only looked responsible.

## The inversion

> `agents` stops managing knowledge and goes back to managing the machine.
> Knowledge becomes documentation, governed by instruction.

The structural reason it works: **approval moves upstream of writing.** Spec 7 is
*model writes → queue → human reviews → promote*, so it needs an untracked queue,
a promotion verb, index regeneration, a scoped commit and a review-lag metric —
machinery for adjudicating content nobody asked for. The replacement is *human
recognises → model writes → done*. Spec 1's founding constraint, that unreviewed
model output must never enter the tracked tree, stops applying: the output is not
unreviewed. A human asked for it, by name, about a specific thing they just read.

Knowledge about a codebase **is documentation**. Every repository already has a
place for it. `.agents/memory/` was a second per-repo knowledge hierarchy built
beside the one that already existed, and then starved.

## Two triggers

| | fires when |
|---|---|
| **write** | the human notices — "save that", "good to know" — or an event in the work: a bug understood, a run collapsed, an approach abandoned, a priority pivot |
| **read** | before asserting a claim about an area, grep the Q&A for it |

Nothing fires at session end. No gate, no queue, no promotion.

The read trigger is not decoration. Twice in the session that produced this
document, an agent asserted a thing was true when the repository already recorded
the correction — that the Arch CI leg was not locally reproducible, and that a
gitleaks allowlist was missing. Both were one `grep` away. The moment that needs
a reader is mid-work, when a claim is about to be made, which is why this is an
instruction and not a `session-start` hook. Spec 7 rejected a session-start
banner in its own words: it could not fire without becoming wallpaper.

## Two stores, one retrieval axis each

```
docs/qna/       indexed by TOPIC — the question you would ask on hitting it again
docs/journal/   indexed by TIME  — dated record of what happened
```

Both flat. Hierarchy earns its place when a level has enough entries to need
subdividing *and* names something the filename cannot; date-directories fail
both. `docs/journal/2026-08-19-spec-5-gate.md` sorts chronologically for free and
greps without depth. Year subdirectories are a mechanical change if it ever
passes a few hundred files.

Q&A form, taken from `autogo-mlx` unchanged:

```markdown
# [The question you would ask when you hit this again]

## Context
[when and why it arose — with the concrete numbers]

## Answer
[the mechanism, with evidence]
```

**Question-first, not claim-first.** `.agents/memory/` indexed entries by what
they concluded; a reader arrives with a question, not a conclusion. No
frontmatter, no schema, no generated index — `ls` is the index at this scale and
`grep` is the query.

`docs/journal/` is not named `docs/trace/`: `agents trace` survives this redesign
as the transcript cache, which is tier 3 and machine-bound, and one word must not
span two tiers.

## Delivery

| tier | carries | lives in |
|---|---|---|
| Global | *when* to record, *what shape* | `claude/CLAUDE.md` + a fleet skill, symlinked from dotfiles |
| Per-repo | *where*, and domain conventions | that repository's own `CLAUDE.md` |
| Machine | raw material, never authoritative | harness dirs, pointed at |

The global tier is already delivered by symlinks this repository maintains, so
adopting a repository needs no scaffolding written into it and no `doctor` check
enforcing that scaffolding's presence.

## What retires, and what stays

**Retires:** `.agents/memory/`; `agents handoff write|draft`; `agents review` and
the untracked queue; promotion; `--stats`; the `scaffold:capture-instruction`
check; both `INDEX.md` generators and the guard rule blocking on their drift.
Spec 3 dies with the queue that demoted it to a fallback. Most of this is
deleting code that exists, which is cheap and reversible.

**Stays, each on its own merit:**

- **spec 5's verification gate** — merged, independent, found five real defects.
- **`agents trace` and the `subagent-stop` cache** — the one thing an instruction
  physically cannot do. Transcripts are deleted mid-session; capture is
  now-or-never, and the loss is not age-ordered.
- **`agents doctor`** — narrowed to mechanical facts about the machine: wiring,
  hooks, gitleaks, fleet.
- **`agents init` / `wire`** — hook wiring only.
- **`.agents/skills/`** — the only part of `.agents/` that `autogo-mlx` used
  organically.
- **spec 1's placement rule and the exit-code vocabulary** — good invariants.

## Deliberately not decided

- **Repository migration is not specified.** Two repositories is not a population;
  automating a two-instance operation is negative value, and specifying a
  migration format for an adoption that has not happened is the same error this
  document exists to correct. `autogo-mlx` is migrated by hand as the second data
  point, against a checklist in the fleet skill. If a third and fourth make the
  steps stable and boring, that repetition is the spec, written from evidence.
- **There is no read-side metric.** The write side has a cheap leading indicator —
  `doctor` reporting how long since the Q&A directory last changed. Nothing
  equivalent measures retrieval, and inventing a proxy would repeat the mistake of
  measuring what is easy instead of what matters.

## How we will know this is wrong

State it now, while it is cheap to be honest.

- If `docs/qna/` in an actively worked repository is still empty after four weeks,
  instruction-only has failed too, and the problem is neither capture nor review
  but that the work is not producing findings worth keeping.
- If entries accumulate and are never read — no grep ever hits, no claim is ever
  corrected by one — then the store has writers and no readers, and the correct
  response is to stop writing, not to build retrieval.
- If the two triggers produce material indistinguishable from what the session-end
  instruction produced, then the trigger was never the variable and this document
  is wrong about its central claim.
