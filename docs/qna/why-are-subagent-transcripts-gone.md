# Why has a transcript the trace index points at already been deleted?

## Context

Measured on this machine 2026-08-11, against Claude Code:

- Main checkout: 27 pointers, 15 unique transcripts, **2 still on disk**.
- A live worktree session, still running: 243 pointers, 83 unique transcripts,
  58 reachable, **25 already deleted**.

The second number is the one that matters. Those 25 subagent transcripts were
recorded at `subagent-stop`, when the file demonstrably existed, and were gone
before the same session ended. This is not cleanup at expiry: **Claude Code
prunes subagent transcripts during the session that produced them.**

## Answer

**The loss is not age-ordered, in either direction.** An earlier version of this
finding guessed "it retains roughly the most recent N". That is wrong. Plotting
all 111 `subagent-stop` records of one session oldest-first, `o` present and `X`
gone:

```
XXXooXooXooXoXooooooooooooooooooooXooXoooXoooooXooXooXooooooooooooXXXooXoooooooXoooooooXXoooXXXXoXooooooooooooo
```

Casualties are scattered, several of the very oldest survive, and the newest ~15
are all present. In a second session the opposite held — the survivors dated from
Aug 7-9 while the vanished ones were recorded Aug 10. The two sessions disagree
about everything except the one thing worth relying on: **age does not predict
survival.**

Two distinct loss modes:

1. **Whole session files.** Six `session-start` pointers referenced session
   JSONLs that no longer existed though the project directory did. That pointer
   is written before the session has produced anything durable, so a short or
   abandoned session leaves one that was verified at write time and is dead
   within hours.
2. **Individual subagent transcripts**, pruned mid-session as above.

**What follows from it.** Capture has to be immediate and unconditional, which is
why the `subagent-stop` hook caches the child transcript as it records the
pointer — the earliest moment a finished one exists. Any policy shaped as "cache
things once they get old" salvages the wrong set, and a `--since 30d` window
silently spans a period in which most of the content is already gone.

`pointer_verified: true` says the path existed **when recorded**. It is not a
claim about now, and the gap between the two can be minutes.

This is the reason the transcript cache survived the 2026-08-19 redesign that
retired the rest of the capture apparatus: an instruction cannot copy a file that
the harness deletes mid-session.
