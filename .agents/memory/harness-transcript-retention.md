---
name: harness-transcript-retention
description: Claude Code deletes subagent transcripts while the session is still running, so trace pointers must be cached promptly and never on an age heuristic
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
prunes subagent transcripts during the session**, apparently retaining roughly
the most recent N.

Two distinct loss modes, which want different responses:

1. **Whole session files.** Six `session-start` pointers referenced session
   JSONLs that no longer exist though the project directory does. A
   `session-start` pointer is written before the session has produced anything
   durable, so a short or abandoned session leaves a pointer that was verified
   at write time and is dead within hours.
2. **Individual subagent transcripts.** Pruned mid-session as above. In one
   session, 53 of 106 files survived while six specific recorded ones did not.

**The trap.** Surviving subagent transcripts in that session dated from Aug 7-9;
the ones that had vanished were recorded Aug 10. The newest went first. So any
policy shaped as "cache things once they get old" salvages exactly the wrong
set, and `-since 30d` silently covers a window in which most of the content is
already gone.

**How to apply:**

- Run `agents trace cache` **early and often**, not at the end of a session and
  not before archiving. By the end of one session ~30% was already unrecoverable.
  The natural moment is right after the trace is recorded.
- Never reason about what is worth caching from a pointer's age or its `when`.
  Cache everything reachable; reachability is the only signal that means anything.
- `pointer_verified: true` says the path existed **when recorded**. It is not a
  claim about now, and the gap between the two can be minutes.
- Traces recorded inside a git worktree land in that worktree's own untracked
  `.agents/reports/traces/*.jsonl`. They do not reach the main checkout and are
  destroyed with the worktree. Cache and commit before removing one — a worktree
  here held 243 lines against the main checkout's 27.
- `.agents/reports/traces/*.jsonl` is `merge=union`, so concurrent appends from
  several worktrees merge without conflict. The cache itself
  (`.agents/.trace-cache/`) is git-ignored via `.git/info/exclude` and holds the
  bytes, so caching does not bloat the repository.

Related: [[undiscriminating-test-doubles]]
