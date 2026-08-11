---
name: harness-transcript-retention
description: Claude Code deletes subagent transcripts mid-session on a rule nothing can predict, so capture must be immediate and unconditional rather than scheduled
metadata:
  type: reference
sources:
  - kind: transcript
    machine: manboobs-26a6
    ref: 4d29608c-d467-497c-8286-40aa992e6d23   # session; resolve via the trace index
    note: "measured 2026-08-11 against fish 4.8.1 machine manboobs-26a6; numbers below are from that census"
---

Trace pointers go stale far faster than the 30-day default window assumes, and
the loss is **not age-ordered**. Measured on this machine, 2026-08-11:

- Main checkout: 27 pointers → 15 unique transcripts → **2 still on disk**.
- A live worktree session, still running: 243 pointers → 83 unique transcripts →
  58 reachable, **25 already deleted**.

The second number is the one that matters. Those 25 subagent transcripts were
recorded at `subagent-stop`, when the file demonstrably existed, and were gone
before the same session ended. This is not cleanup-at-expiry; **Claude Code
prunes subagent transcripts during the session.**

**The rule is not one we can predict, and it is worth resisting the urge to
guess at it.** An earlier draft of this entry said "retaining roughly the most
recent N". That is wrong. Plotting all 111 `subagent-stop` records of one
session oldest-first, `o` present and `X` gone:

```
XXXooXooXooXoXooooooooooooooooooooXooXoooXoooooXooXooXooooooooooooXXXooXoooooooXoooooooXXoooXXXXoXooooooooooooo
```

Casualties are scattered, several of the very oldest survive, and the newest ~15
are all present. Neither "keep the most recent N" nor age-ordered. What the
shape does establish is the only thing worth relying on: at and shortly after
the stop event the file is reliably there, and after that all bets are off.

Two distinct loss modes, which want different responses:

1. **Whole session files.** Six `session-start` pointers referenced session
   JSONLs that no longer exist though the project directory does. A
   `session-start` pointer is written before the session has produced anything
   durable, so a short or abandoned session leaves a pointer that was verified
   at write time and is dead within hours.
2. **Individual subagent transcripts.** Pruned mid-session as above. In one
   session, 53 of 106 files survived while six specific recorded ones did not.

**The trap.** In a second session the surviving subagent transcripts dated from
Aug 7-9 while the ones that had vanished were recorded Aug 10 — there, the
newest went first. Set beside the scattered timeline above, the two sessions
disagree about everything except the one thing that matters: **age does not
predict survival in either direction.** So any policy shaped as "cache things
once they get old" salvages the wrong set, and `-since 30d` silently spans a
window in which most of the content is already gone.

**How to apply:**

- The `subagent-stop` hook now caches the child transcript itself, which is the
  earliest moment a finished one exists. Manual `agents trace cache` remains the
  net for session transcripts and for anything recorded before that landed.
- Never reason about what is worth caching from a pointer's age or its `when`.
  Cache everything reachable; reachability is the only signal that means
  anything. A `--since` window is the wrong instrument here — it selects on
  exactly the axis the losses ignore.
- `pointer_verified: true` says the path existed **when recorded**. It is not a
  claim about now, and the gap between the two can be minutes.
- `agents trace show <agent-id or session-id>` reads a transcript back, from the
  harness's copy or from ours, and says on stderr which answered. That is what
  makes a `sources:` citation checkable after the harness has cleaned up.
- The cache lives in the git **common** directory, so every worktree shares one
  and it outlives any of them. It used to sit at `<root>/.agents/.trace-cache`,
  per worktree: one worktree here held 58 transcripts, 36 MB, that
  `git worktree remove` would have taken. Being inside the common directory is
  also what keeps transcript content unstageable — git does not track its own
  directory, so no ignore file has to be written or remembered.
- The trace **index** is tracked and appended to by every hook firing, so it
  dirties the working tree continuously and will block a fast-forward pull.
  Commit it with `agents save`; `merge=union` on `.agents/reports/traces/*.jsonl`
  then merges concurrent appends from several worktrees without conflict.
- Pruning is `agents trace cache prune --lane <name>`, dry-run unless `--yes`,
  and it removes copies only. Never infer that a lane is prunable from a branch
  or worktree being gone: a deleted branch is usually a merged one, and a
  throwaway worktree is often where the interesting work happened.

Related: [[undiscriminating-test-doubles]]
