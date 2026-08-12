# Spec 7 — what happened while you were away

**Date:** 2026-08-12
**Branch:** `spec-7-capture-and-review`, 9 commits, **not pushed**
**Read this with:** [the spec](../specs/agents/2026-08-12-spec-7-capture-and-review.md),
[the plan](../plans/2026-08-12-spec-7-capture-and-review.md)

This is a review aid, not a design document. It records what was decided, what
was built, what was deliberately not built, where I departed from the plan, and
what needs your judgement.

---

## The short version

Your complaint — tracking traces in git is weird — was correct, and the defect
was one tier assignment in spec 1 §1: a *reference* to machine-bound material
was filed in the tracked tier. Measured on this repository, 48% of tracked
records pointed at transcripts already deleted on the machine that wrote them,
while the curated store they were meant to feed held **zero handoffs** after
twenty sessions.

The trace index is now machine-local. The working tree stays clean. And the
repository has its first handoff.

Two things changed direction mid-session, both because you pushed:

1. **You asked what `agents save` and `agents handoff` should look like.** That
   turned into: `save` is a smell in the normal path, because the churn it
   existed to manage was produced entirely by the file we were about to untrack.
   Promotion commits instead.
2. **You asked whether a properly worded instruction would just work.** It
   exposed that my spec justified a blocking `Stop` gate using a spec 1
   measurement about *subagents* — which do not read `CLAUDE.md` at all — while
   this repository's actual instruction told an agent *how* to write a handoff
   and never *whether* to. The gate was demoted to a contingency and the
   instruction shipped instead. **That is the most important change in the
   session** and it is the reason the built surface is small.

---

## What is built

| | |
|---|---|
| `repo.StoreDir` | `<git-common-dir>/agents/` — one store holding the index, the cache, and the queue |
| trace index | moved out of `.agents/reports/traces/` into `<store>/traces/` |
| `trace.PruneRetention` | age + size caps, enforced at `subagent-stop` where the cache grows |
| `agents trace migrate` | copies the tracked index into the store, unstages it, drops `merge=union` |
| `internal/queue` | untracked drafts, with `Validate` as the promotion contract |
| `agents handoff draft` | queues an unreviewed note; same stdin contract as `write` |
| `agents review` | list / `--show` / `--keep` / `--bin` / `--edit`; `--keep` promotes and commits in one act |
| `scaffold.CaptureInstruction` | the capture mechanism, in `CLAUDE.md` |
| doctor | `pointers:local-unreachable` deleted; `scaffold:capture-instruction`, `queue:pending`, `store:size` added |

## What is deliberately not built

**Spec 7 §3c — the blocking `Stop` gate.** Budget arithmetic, `(lane, session)`
watermarks, per-lane ceilings, the `redundant`/`keep` labelling, `review
--audit`. All specified in full, none implemented.

The spec orders capture cheapest-first: §3a an instruction, §3b a non-blocking
nudge, §3c the gate — each built only if the previous measurably fails. Building
3c now would repeat the exact error the session found: assuming a measurement
instead of taking one.

**§3b, the non-blocking nudge.** Depends on whether a `Stop` hook can add
context without blocking, which is unmeasured under both harnesses.

**Boundary notifications** (`post-checkout` / `post-merge` printing pending
counts). Named as a deliberate omission in the plan's self-review: a convenience
whose absence does not block the loop.

---

## Departures from the plan

Three, all recorded in the commits.

**1. Retention runs at `subagent-stop`, not `post-merge`.** The spec said
`post-merge`. Two reasons to change it: `internal/githook` is a low-level
dispatcher with no access to `repo`/`trace`, and — more substantively — the
`post-merge` rationale ("when a lane has landed, its material is least likely to
be wanted") belongs to `PruneLane`, which prunes *by lane*. Retention prunes by
age and size, so a merge is not a meaningful moment for it, and a repository
that never merges would never prune at all.

**2. `doctor.RunWithDeps` takes `storeDir` as a parameter, not through
`Dependencies`.** I first routed it through the dependency seam. A test fixture
using `doctor.Dependencies{}` then resolved the store to nothing and doctor
reported *"all trace index lines are readable"* and *"this harness has never
recorded here"* — a clean bill of health from a diagnostic that never found the
index. That is exactly the undiscriminating double
[`.agents/memory/undiscriminating-test-doubles.md`](../../../.agents/memory/undiscriminating-test-doubles.md)
warns about. An explicit parameter has no nil case to be wrong about.

**3. `merge=union` also had to retire at the global tier.** The plan only
covered the per-repo `.gitattributes`. `git/gitattributes` carried the same rule
and `doctor` asserted its presence. Both are updated; the file itself stays,
because `core.attributesFile` points at it.

---

## Verification, with the evidence

- **Both Go modules:** `go build`, `go vet`, `gofmt`, `go test ./...` all clean.
- **Migration on real data:** 67 records across three daily files, verified
  line-for-line present in the store *before* the working-tree copies were
  deleted.
- **The loop, end to end, with the installed binary:** drafted → confirmed the
  queue is invisible to `git status --untracked-files=all` → `--show` → `--keep`
  promoted, reindexed, and committed scoped to `.agents/`. Commit `8b77914`.
- **The tree stays clean:** fired a real `session-start` hook afterwards. The
  record landed in the store (29 → 30), `.agents/reports/traces/` was not
  recreated, `git status` stayed clean.
- **`agents doctor`** now warns on one thing only: `recording:codex`, a
  pre-existing machine-local condition unrelated to this work. The daily false
  `pointers:local-unreachable` warning is gone.

**Mutation testing, because the tests all passed first try and that is not
evidence.** I mutated the implementation to check each test could fail:

| mutation | caught? |
|---|---|
| hook-time cache prune disabled | yes |
| memory frontmatter validation removed | yes |
| promotion skips validation | yes |
| promotion commits everything, not just `.agents/` | yes |
| queue ID path-component checks removed | **no — test was undiscriminating** |

The last one passed because the traversal target did not exist, so `Get` failed
on a missing file rather than on a refused path. Rewritten to place a real
readable file at the far end of the traversal; it now catches the mutant.

---

## What needs your judgement

1. **Nothing is pushed.** Nine commits sit on `spec-7-capture-and-review`. The
   remote is public and you did not ask me to push.
2. **I installed the new binary** via `make agents`. This was not optional: the
   old binary at `~/bin/agents` still wrote to `.agents/reports/traces/`, so the
   next hook fire would have silently recreated the tracked directory and undone
   the migration. Flagging it because it changed your live environment.
3. **`handoff write --draft` now hard-fails** rather than being ignored. If any
   script or skill of yours passes it, it will exit `3`. Deliberate — silently
   ignoring it would write a tracked note claiming nobody checked it.
4. **The instruction's wording is a parameter, not a decision.** It names the
   moment, bounds the output to three bullets, and says drafting costs nothing.
   Whether that is *good enough* is the B′ measurement — a week of real use,
   counting sessions, drafts, and promotions against a baseline of zero. Revise
   and re-measure before escalating to §3b or §3c; a cheap trigger revised twice
   still costs less than the gate.
5. **Git history keeps the old trace records**, per your decision. They contain
   hostname, `$HOME` paths, session UUIDs, and lane names on a public remote.
   Low-sensitivity here; the note in the spec says a work repository with ticket
   ids as lane names should decide that on its own facts.

## Known limits, stated rather than buried

- A memory entry promoted on a feature branch is invisible to other lanes until
  merge and lost if the branch dies. Promotion warns and proceeds.
- Delete-and-re-clone loses the store, and any unpromoted draft with it.
- Cross-machine awareness is genuinely reduced: another machine can no longer
  learn that work happened here unless something was promoted. Accepted, and
  named as a loss in the spec rather than argued away.
- Codex reaches the instruction through `AGENTS.md` → `CLAUDE.md`, but whether
  Codex sessions act on it at the same rate as Claude Code sessions is a
  separate reading of the same week and should not be pooled.
