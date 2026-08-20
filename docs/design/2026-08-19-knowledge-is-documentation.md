# Knowledge is documentation, not a subsystem

**Date:** 2026-08-19
**Status:** design — approved; execution not started
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

The read trigger is not decoration. In the session that produced this document an
agent asserted that the Arch CI leg was not locally reproducible, while spec 5
recorded the opposite correction — and recorded that believing it was what left
an earlier diagnosis unchecked. It was one `grep` away. (A second error the same
session, about a gitleaks allowlist, was cwd drift rather than a retrieval
failure, and is not evidence for this.)

The moment that needs a reader is mid-work, when a claim is about to be made.
That rules out a `session-start` hook, which fires when relevance is still
unknown — spec 7 rejected a session-start banner in its own words, as something
that could not fire without becoming wallpaper. It does not rule out a hook at
the moment text is written, which has a concrete subject and can stay silent on
no match. See **The subagent hole** below, which decides whether such a hook
could work at all.

## The subagent hole

Spec 1 measured this on Claude Code 2.1.224 and it constrains both triggers:

> `SessionStart` does **not** fire for subagents … Subagents inherit `CLAUDE.md`
> but do not act on it — 0 of 31 observed subagents followed an inherited
> bootstrap directive.

Read precisely, because the direction matters. Subagents **see** the instruction
and ignore it; `SubagentStart` and `SubagentStop` were both observed **firing**.
For subagents it is the instruction that fails, not the hook — the reverse of
this document's general argument, and the reason spec 1 concluded that recording
must be a hook.

Two consequences, both recorded here rather than discovered later.

**Trigger 2 does not fire inside subagents.** The human-recognition trigger is
safe: a human addresses the controller, which acts. But the event trigger — a bug
understood, a run collapsed, an approach abandoned — describes things that happen
*inside a child* during subagent-driven work, which is how this repository
executes plans. A child will not act on the inherited instruction. Capture there
degrades to the controller noticing from the child's report. Weaker, not absent,
and the fleet skill should say so rather than imply coverage the measurement
denies.

**Whether a write-time read hook can cover subagents is unmeasured.** The events
that experiment dumped were `SessionStart`, `SubagentStart`, `SubagentStop` and
`Stop`. `PreToolUse` was not among them, so whether it fires inside a child is
unknown — and that single fact decides whether the read trigger can be
mechanised where instructions demonstrably fail. Measure it with the same
throwaway-repo dump script spec 1 used, before building anything.

**One caution on spec 1's own generalisation.** "Recording must be a hook and
never an instruction" is broader than 0-of-31 on a single directive type
supports. `autogo-mlx` is a live counter-example of an inherited instruction that
*is* acted on — by main agents. Narrowed to what was measured: *subagents* do not
act on inherited directives.

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

**Constrain shape, not length.** Measured 2026-08-20 across both repositories:
the four session-end drafts span 175-243 words (spread 68) while `autogo-mlx`'s
nine Q&A entries span 418-701 (spread 283). The drafts are uniform because a rule
set their size — spec 7 bounds a draft to three bullets — and the Q&A entries
vary because the finding set theirs. Spec 7's answer to that bound was that a
draft "grows at review"; the two promoted handoffs are 46 and 257 words, so it
did not. Note what this does *not* show: the two `.agents/memory/` entries are 570
and 746 words, larger than the Q&A average, so the store never produced poor
artifacts. It produced two, by hand — only one draft of seven was ever promoted.
The mechanism did not make the good entries; deliberate writing did.

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

**Not emptiness.** An empty `docs/qna/` refutes nothing. The write trigger is
conditioned on a human recognising something, so the absence of recognition
produces zero entries *correctly* — a true negative, not a failure. Two cases
make this concrete: work run autonomously, where the human has no context from
which to recognise anything, and work that genuinely surfaced nothing
interesting. Neither refutes anything, and the first is already covered by the
second trigger, which fires on events in the work rather than on a human being
present. A test that blames the mechanism for a state the mechanism reported
accurately is not a test.

**Upstream approval approves the topic, not the text.** This is the real cost of
moving the gate before the writing, and it is the thing most likely to be wrong
here. A human who says "save that" has vouched for the *subject*; they may never
read the artifact. Downstream review, whatever else was wrong with it, read the
words. Falsifier: sample the entries after four weeks and check them. If they are
wrong, misleading, or leak something, then review was doing work that recognition
does not do. Partial mitigation already exists and needs no new machinery —
writing an entry produces a commit, so entries surface in diffs, and the commit
review is a gate this repository already runs.

**The read instruction may simply not fire.** The bar is not "entries are never
read"; the fleet skill names the store, so agents will visit it. The measured
problem is narrower and worse. This repository's `CLAUDE.md` already says "Read
it before assuming; it is the record", and in the session that produced this
document an agent asserted a claim about Arch reproducibility that spec 5 had
already corrected. The instruction was present and did not fire. If corrections
keep being missed while sitting in `docs/qna/`, the read trigger has failed —
which, together with subagents, is where a mechanism earns its place in a design
that otherwise prefers instructions.

**The subagent hole widens.** If trigger 2 proves unrecoverable inside
subagent-driven work — findings made in a child and never surfaced by the
controller — then instruction-based capture does not cover the way this
repository actually executes plans, and the hole is structural rather than
incidental.
