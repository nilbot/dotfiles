# Measuring the capture instruction

**Date:** 2026-08-12, redesigned 2026-08-13 after the first run
**Status:** v2, not yet run. [v1 results](#v1-results-2026-08-12) below.
**Measures:** [spec 7](../specs/agents/2026-08-12-spec-7-capture-and-review.md) §3a
**Harness:** [`agents/experiment/capture-setup.sh`](../../../agents/experiment/capture-setup.sh)
**Reported by:** `agents review --stats [--lane <scenario>]`

The question: **does an agent record a durable conclusion when it is asked
properly?** The baseline is zero — twenty sessions under the previous
instruction produced no handoffs.

---

## What the instruction actually asks

Everything below is designed against this one sentence from `CLAUDE.md`, because
v1 was designed against my intuition instead and got a scenario wrong:

> …what a future agent could not get from the code or the git log.

That is a conjunction of two independent facts, and a note is warranted only in
one of the four cells:

| | a conclusion exists | the code or diff already carries it | note warranted |
|---|---|---|---|
| **A** | yes | no | **yes** |
| **B** | yes | yes | no |
| **C** | no | — | no |

**B is the case that tests discrimination**, and v1 had exactly one of them —
misclassified as an A. v2 has three A's and two B's, because a scenario set made
of easy negatives measures politeness rather than judgement.

## Setup

```bash
agents/experiment/capture-setup.sh /tmp/cap-treatment
agents/experiment/capture-setup.sh /tmp/cap-control --no-instruction
```

Run from the dotfiles checkout root; each takes a second and needs `agents` on
`PATH`. The two repositories are identical except for the paragraph under test.

Every scenario touches **its own file** so changes never commingle, and runs on
**its own branch** so the lane names the scenario and the result slices
mechanically.

## Running

For each scenario: a **fresh Claude Code session**, `cd` into the arm's
directory, create the branch, paste the prompt verbatim.

Fresh sessions matter — the instruction is read at session start, so reusing a
session measures its memory instead. Do not tell the agent what is being
measured.

**Commit after any scenario that changed code, before starting the next.** The
criterion includes "or the git log", so leaving work uncommitted deletes half of
what the agent is asked to weigh against. v1 missed this and ran with a git log
containing only `initial import`.

Run **all seven** in the treatment arm. In the control arm run **only the three
A scenarios** — the risk a control guards against is "the model drafts anyway",
which is only visible where drafting is warranted.

---

### s1-retry — B: the fix explains itself

```bash
git checkout -b s1-retry
```

> `src/retry.py` retries but the backoff never actually delays between attempts.
> Find out why and fix it.

The `time.sleep` sits outside the loop. Moving it in *is* the explanation, fully
legible in the diff. **Expect no draft.** A draft here is a false positive.

### s2-config — A: read-only, no diff to carry it

```bash
git checkout -b s2-config
```

> Something is wrong with how timeouts are configured in `src/config.py`. Work
> out what and explain it. Do not change any code.

`REQUEST_TIMEOUT_MS` holds a seconds value, and nothing in the repo consumes it,
so no test can reach it. Zero diff, so the conclusion has no other carrier.
**Expect a draft.**

### s3-cache — A: the conclusion is an interaction, not a line

```bash
git checkout -b s3-cache
```

> `src/cache.py` is supposed to bound memory two ways: a TTL and a maximum entry
> count. Under steady traffic it still grows. Explain why. Do not change any
> code.

`get()` refreshes the timestamp on read, so a frequently-read key never expires
and the LRU-ish eviction never reaches it. The finding spans two methods and
survives no single line. **Expect a draft.**

### s4-auth — A: the reason lives in another file

```bash
git checkout -b s4-auth
```

> `src/auth.py` refreshes the token when a call fails, but in production the
> refresh never happens. Work out why. Do not change any code.

`docs/vendor.md` records that this vendor returns **403** for expired
credentials, not 401, so the `== 401` branch can never fire. The conclusion is a
cross-file fact about an external system. **Expect a draft.**

### s5-pool — B: an off-by-one whose fix explains itself

```bash
git checkout -b s5-pool
```

> `src/pool.py` is configured with `max_size=10` but callers report exhaustion
> at nine connections. Fix it.

`< self._max_size - 1`. The corrected comparison carries the whole story.
**Expect no draft.**

### s6-parser — C: mechanical, no conclusion

```bash
git checkout -b s6-parser
```

> Add type hints to the functions in `src/parser.py`.

**Expect no draft.**

### s7-question — C: no work at all

```bash
git checkout -b s7-question
```

> What does `src/parser.py` do?

**Expect no draft.**

---

## Reading the result

```bash
cd /tmp/cap-treatment && agents review --stats
cd /tmp/cap-control   && agents review --stats

# per scenario
agents review --stats --lane s3-cache
```

Score it as a confusion matrix over the seven treatment scenarios:

| | drafted | did not draft |
|---|---|---|
| **A** (s2, s3, s4) | true positive | **miss** |
| **B, C** (s1, s5, s6, s7) | **false positive** | true negative |

| observation | reading |
|---|---|
| 3 A's drafted, 4 B/C's silent, control silent | the instruction works and discriminates. §3c is not justified. |
| A's drafted **and** B's drafted | fires indiscriminately — revise the wording, do not build the gate |
| some A's missed | partial. Look at which: a missed s4 (cross-file) means something different from a missed s2 |
| nothing drafted anywhere | first real evidence for §3b, then §3c. Revise the wording and re-run first. |
| control drafts on A's too | the paragraph is decoration; the model was doing it anyway |
| A's drafted but the drafts read as filler | wording problem, not a trigger problem |

**Then read the drafts.** Three bullets restating the diff are a failure at any
rate, and no number detects that.

```bash
agents review --show <id>
agents review --keep <id>   # or --bin
```

**Promote or bin every draft.** The promotion rate is the half of the
measurement that asks whether the material is worth having, and until something
is decided `--stats` correctly refuses to conclude anything.

## What this does not measure

- **Long sessions.** Every scenario is short, so instruction decay over a long
  context is untested. That is precisely what §3b exists for and this cannot
  settle it.
- **Codex.** `AGENTS.md` symlinks to `CLAUDE.md`, so the instruction reaches it
  for free, but these arms are Claude Code. Run separately; do not pool.
- **Variance.** One run per arm. If the result is close to a boundary, run it
  again before concluding.
- **Whether you still want the note a month later.** Promotion is measured
  minutes after drafting, by the person who just watched the work.

## Honest limit on who is grading

I wrote the instruction, the scenarios, and the expected outcomes. A flattering
result is weak evidence. The parts not mine to influence are the drafts
themselves and the control arm — and v1 already demonstrated the rubric can be
wrong, since the agent applied my own criterion more accurately than I did.

---

## v1 results (2026-08-12)

Four scenarios per arm, one run each.

| | treatment | control |
|---|---|---|
| sessions that did work | 4 | 4 |
| …drafted something | **1** | **0** |
| draft rate | 25% | 0% |

**The control arm was the finding.** Zero drafts without the paragraph, one with
it: the instruction caused the only draft in the experiment, which a single arm
could never have shown. No false positives.

The draft came from the read-only config scenario and found two things not
planted in the fixture — that the header comment's "30-microsecond" arithmetic
contradicted the `_MS` suffix, and that a corrected 30 000 ms deadline would be
shorter than `CONNECT_TIMEOUT + READ_TIMEOUT` and preempt it. It declined to fix
anything until the SDK's real unit was confirmed.

**Two defects in v1, both of which v2 fixes:**

1. The retry scenario was labelled "expect a draft" on the reasoning that a
   one-line fix leaves its rationale invisible. Wrong — a `sleep` moving inside
   a loop *is* the rationale. The agent applied the instruction's criterion more
   accurately than the scenario author did. It is now a B case.
2. Nothing was committed, so the git-log half of the criterion was vacuous
   throughout, and two scenarios' changes commingled in one file.

v1 therefore had one usable positive case. v2 has three, plus two hard negatives
and per-scenario slicing.
