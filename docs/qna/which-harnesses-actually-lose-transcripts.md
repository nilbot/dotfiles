# Which harnesses actually lose transcripts?

## Context

The transcript cache was built because
[Claude Code prunes subagent transcripts mid-session](why-are-subagent-transcripts-gone.md),
and it was wired identically on Claude Code and Codex from the start. Nobody
asked whether Codex had the problem.

Measured 2026-08-22 against this repository's own trace records — the machine-local
store under the git common directory, every record this tool has ever written:

```
records by harness:                claude-code 304    codex 14
verified pointers still on disk:   claude-code 165    codex 12
verified pointers now gone:        claude-code 110    codex  2
codex subagent-start/subagent-stop records:                  0
```

## Answer

**Only Claude Code has been shown to have it.**

Claude Code has lost **110 of 275** verified pointers — 40%. Codex has lost two,
and both belong to a single abandoned 2026-08-10 session: one `session-start` and
one `stop` naming the same rollout file. That is loss mode 1 from the linked
entry — a whole session file, written before the session produced anything
durable — not the mid-session subagent pruning that motivated the cache.

Codex has produced **zero** `subagent-start` or `subagent-stop` records in
production use. Spec 1 observed both events firing on Codex in a probe, so the
wiring is live and correct; the events have simply never fired here in real work.
There is no Codex subagent transcript to have been pruned, so there is no
evidence either way.

**What follows.** `pointer_verified: true` never claimed anything about now, and
this entry does not weaken it. What it corrects is narrower and more useful: a
harness's *needs* are as harness-specific as its capabilities. The cache is a
Claude Code remedy that was deployed fleet-wide because the model in force —
"the tool has needs, harnesses have capabilities" — had no way to express that a
need might not exist somewhere.

[Spec 4](../design/2026-08-07-spec-4-wiring-dsl.md) §3 makes that the second
axis: wire where the harness *exhibits the need* **and** *provides the
capability*. A missing capability is a gap to report; an undemonstrated need is
not a gap at all.

Two cautions on this measurement. Codex's n is 14 against Claude Code's 304 —
this is evidence of absence only in the weak sense, and one Codex-heavy week
could overturn it. And it says nothing about Antigravity, which has never been
wired: whether it destroys subagent trajectories is the open measurement spec 4
§9 names first.
