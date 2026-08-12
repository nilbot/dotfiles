# Measuring the capture instruction

The question: **does an agent record a durable conclusion when it is asked
properly?** The baseline is zero — twenty sessions under the previous
instruction produced no handoffs.

This takes an afternoon, not a week. It replaces the "run it for a working week
and see" measurement, which was paced by the calendar rather than by work — the
same defect the trace store had.

## Setup

Two arms, because a draft rate on its own proves nothing: an agent might record
conclusions with no instruction at all, and then the paragraph is decoration.

```bash
cd agents/testdata/capture-experiment
./setup.sh /tmp/cap-treatment
./setup.sh /tmp/cap-control --no-instruction
```

Both repositories are identical except for the paragraph under test. Each has
`agents init` run and the hooks wired, so `stop` records accumulate and the
denominator is real.

## Running

Start a **fresh Claude Code session per scenario**, `cd` into the arm's
directory first, and paste the prompt verbatim. Fresh sessions matter: the
instruction is read at session start, and reusing one session measures its
memory rather than the instruction.

Run every scenario in both arms. Do not tell the agent what is being measured.

### Scenario 1 — a conclusion the diff cannot carry

> `src/retry.py` retries but the backoff never actually delays between
> attempts. Find out why and fix it.

The `time.sleep` is outside the loop. The fix is one line, so the reasoning is
invisible in the diff — this is the case where a note earns its place.
**Expect a draft.**

### Scenario 2 — a unit mismatch found by reading

> Something is wrong with how timeouts are configured in `src/config.py`.
> Work out what and explain it. Do not change any code.

Read-only, no diff at all, and a real conclusion: `REQUEST_TIMEOUT_MS` is in
milliseconds while its neighbours are seconds. This is the session type the
whole design is for — valuable, and invisible to git.
**Expect a draft.**

### Scenario 3 — nothing worth recording

> Add a docstring to `fetch_with_retry` describing its parameters.

Mechanical, fully described by the diff, no conclusion.
**Expect no draft.** A draft here is a false positive and means the instruction
fires indiscriminately, which is a wording problem.

### Scenario 4 — a question, not a task

> What does `src/config.py` do?

No work, no conclusion.
**Expect no draft.**

## Reading the result

```bash
cd /tmp/cap-treatment && agents review --stats
cd /tmp/cap-control   && agents review --stats
```

Then review what was drafted, which is the part no number can answer:

```bash
agents review              # list
agents review --show <id>  # read one
```

| observation | reading |
|---|---|
| treatment drafts on 1 & 2, not on 3 & 4 | the instruction works and discriminates. §3c is not justified. |
| treatment drafts on all four | it fires indiscriminately — revise the wording, do not build the gate |
| treatment drafts on none | the first real evidence for §3b, then §3c. Revise the wording and re-run first. |
| control drafts as often as treatment | the paragraph is decoration; the model was doing it anyway |
| drafts appear but read as filler | wording problem, not a trigger problem |

**The drafts themselves matter more than the rate.** Three bullets that restate
the diff are a failure even at a 100% draft rate. Read them.

## What this does not measure

- **Long sessions.** Every scenario here is short, so instruction decay over a
  long context is untested. That is exactly what §3b exists for and this cannot
  settle it.
- **Codex.** `AGENTS.md` symlinks to `CLAUDE.md`, so the instruction reaches it
  for free, but the arms above are Claude Code. Run them under Codex separately
  and do not pool the results.
- **Whether you keep the drafts a month later.** Promotion rate here is measured
  minutes after drafting, by the person who just watched the work.

## Honest limit on who is grading

I wrote both the instruction and these scenarios, and the expectations above are
mine. If the results come back flattering, that is weak evidence. The
independent parts are the drafts themselves and the control arm — neither is
mine to influence.
