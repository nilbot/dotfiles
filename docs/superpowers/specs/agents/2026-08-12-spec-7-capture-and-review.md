# Spec 7 — capture cheaply, review before tracking

**Date:** 2026-08-12
**Status:** designed. Phases A and B′ implemented; §3c not built and deliberately
so.
**Depends on:** [spec 1](2026-08-07-agents-repo-context-design.md) for the tiers,
the record schema, the hook adapters, and the exit-code contract.
**Amends:** spec 1 §1, §2, §3.6, §3.7, §4. See
[Cross-spec impact](#cross-spec-impact). §6's exit-code rule is amended only if
§3c is built.
**Changes the premise of:** [spec 3](2026-08-07-spec-3-agents-distill.md), which
is scope-only and must not be designed against its current primary path.

---

## Origin

Spec 1 shipped and was used for five days. This spec is the review of what that
produced, from the observation that started it: *tracking traces in git is a weird
design, and the experience is weird.*

The complaint is correct, and the defect is one tier assignment made in spec 1 §1.
Everything below follows from repairing it.

**This document was restructured on 2026-08-12 after its own review.** The first
draft made capture a blocking `Stop` gate and justified it with a spec 1
measurement about *subagents* — a population that does not read `CLAUDE.md` at
all. The repository's own evidence is narrower and more interesting: the existing
instruction tells an agent *how* to write a handoff and never *that* it should, so
a properly worded instruction has never been tested here. Capture is now ordered
cheapest-first (§3), and the gate survives in full as the contingency §3c. The
history is kept because the error — assuming a measurement instead of taking it —
is the same one that produced the tracked trace store.

---

## Measured baseline (2026-08-12T17:15Z)

On this machine, this repository, five days after `agents init`.

**The tracked trace store**

- 63 records across 3 daily files, 24,409 bytes tracked.
- By event: 21 `session-start`, 26 `stop`, 16 `subagent-stop`.
- **30 of 63 records (48%) point at transcripts that no longer exist** — on the
  machine that wrote them. All 16 `subagent-stop` pointers are unreachable;
  13 of 21 `session-start`; 1 of 26 `stop`.
- 20 distinct sessions.

**Growth is decoupled from value.** Earlier the same day the store held 43
records from the same 20 sessions. The 20 records added between the two
measurements were produced by a single design conversation that wrote no code.

**The cache**

- `<git-common-dir>/agents/trace-cache`: 66 files, 51 MB, accumulated in
  roughly one day. No retention policy exists in any spec.

**What the tracked half has produced**

- 3 commits have ever touched `.agents/reports/traces/`.
- The working tree has been dirty with trace churn essentially continuously;
  it was dirty at the start of this design session and dirty at the end.
- 4 commits have ever touched `.agents/` at all. Three of them were traces.

**What the curated half has produced**

- 2 memory entries.
- **0 handoffs.** `reports/handoff/INDEX.md` is empty after 20 sessions and
  26 `stop` events.

**Health**

- `agents doctor` exits 1 on this healthy machine. One of its two warnings is
  `pointers:local-unreachable — 30 verified local pointer(s) are unreachable and
  uncached`, remediation *"run `agents trace cache` sooner; subagent transcripts
  are deleted mid-session."* That is advice to win a race that
  [`harness-transcript-retention`](../../../../.agents/memory/harness-transcript-retention.md)
  records as unwinnable.

**Exposure**

- `origin` is `github.com:nilbot/dotfiles`, which spec 1 §10 states is public,
  and the trace files are pushed. Each record publishes hostname, an absolute
  `$HOME` path, a session UUID, `cwd`, and `lane` — where `lane` is the branch
  name.

---

## Diagnosis

### The tier error

Spec 1 §1's three tiers are sound. The error is that **a reference to tier 3 was
filed in tier 2**.

Test a trace record against tier 2's own definition — holds "what this codebase
is," travels with the repo. The record travels. What it names cannot, ever, by
construction. On arrival it is a name for a file nobody can open.

§3.3 anticipated the objection and answered that provenance makes the pointer
meaningful elsewhere. That is true in a narrow sense — `machine` tells you whose
disk held it — and it is not the sense that matters. Knowing which machine holds
an unreadable file is a receipt, not a record.

### Why cross-machine recall was never an intermediate state

The stated goal is durable repo knowledge future agents can use. Cross-machine
recall was adopted as a step toward it. It is not a step; it is a fork with two
ends, and neither is an intermediate state:

- **Pointer travels, content does not** — the receiving agent learns a session
  existed and cannot read it. This is the measured condition of 48% of records
  on the *originating* machine.
- **Content travels** — it travels because it has been distilled into prose,
  which is the goal itself. The intermediate has collapsed into the destination.

**What is genuinely lost, stated as a loss rather than argued away.** A tracked
index does carry one thing a machine-local store does not: the bare knowledge that
*work happened elsewhere*. Under this spec, if a lane is worked for two days on
machine A and nothing is promoted, machine B cannot learn that anything occurred
at all. Today it would at least see records.

That is a real reduction for a multi-machine user and it is accepted, not denied.
The trade is: a signal that something exists, against 48% of records resolving to
files nobody can open, a permanently dirty working tree, and a curated store that
stayed empty for five days. The signal was never read, and the price of keeping it
was the thing that made the tool unpleasant to use. If cross-machine awareness
turns out to matter on its own, the honest way to provide it is a tracked artifact
built for that purpose — a promoted note saying work is in flight elsewhere — not
a byproduct of a pointer index.

Distillation can only run where the material is. Its *output* travels; its
*input* never needs to. Git is not involved at any point in that path.

### Three inversions

1. **The pointer is tracked; the payload is deferred.** What is worth committing
   is a conclusion. Conclusions are spec 3, unbuilt. The system commits the
   worthless half and defers the valuable one.
2. **Durability is assigned backwards.** The design assumed transcripts persist
   well enough for a pointer, making content capture optional and manual.
   The reverse is true: transcripts are ephemeral, the copy is durable. So the
   ephemeral reference is tracked and the irreplaceable content is machine-local
   with no retention policy.
3. **Cost accrues per session; value accrues per insight.** Every
   `session-start` writes a line whether or not the session did anything.

### The tell

Three mechanisms exist only to contain the damage of tracking a
machine-generated append-only log in the working tree: `merge=union` in
`.gitattributes`, guard's mixed-commit warning, and `agents save` as a scoped
commit command. Spec 1 §1 states the test they fail — *"anything that does not
clearly fit one tier is a design smell."*

### Why handoffs are empty, which is the more important failure

The trace store is noisy. The curated store is *empty*, and that is the failure
that matters, because the curated store is the entire point.

§4 named two ways a handoff comes into being: written deliberately via a skill,
or auto-drafted at `Stop`/`SessionEnd` and marked `draft`. **Neither was built.**
Nothing in `cmd_hook.go` or any harness adapter mentions handoff. Twenty-six
`Stop` events produced twenty-six trace lines and zero drafts.

Both surviving paths reduce to *a human or agent remembers, unprompted, to author
prose and pipe it to stdin.* Five-day score: zero.

**The queue was never the problem. Nothing filled it.**

### The instruction that was never actually given

It is tempting to read the zero as proof that instructing a model to record
knowledge does not work. The repository's own `CLAUDE.md` says:

> Write handoffs with `agents handoff write`, not by hand.

That sentence instructs **how** to write a handoff. It never says *that* one
should, or *when*. An agent following it perfectly writes zero handoffs, which is
exactly the observed outcome. This is not a failed instruction; it is an
instruction that was never given.

**A properly worded instruction has not been tested in this repository.** Any
design that skips past it to heavier machinery is assuming a measurement nobody
took.

### The constraint that shapes the repair, and its limits

Spec 1's Claude Code measurements record: *"Subagents inherit `CLAUDE.md` but do
not act on it — 0 of 31 observed subagents followed an inherited bootstrap
directive. This is why recording must be a hook and never an instruction."*

**That figure is about subagents, and it does not generalize to main sessions.**
The same measurement says subagents inherit `CLAUDE.md` without acting on it; main
sessions read and act on it, which is the mechanism this whole repository runs on
— including the `agents doctor` line in the same file, which is followed. Citing
0-of-31 as evidence that a main-session instruction will fail is evidence about
the wrong population.

What the measurement does establish is narrower and still important:

| | mechanism | reliability | value of output |
|---|---|---|---|
| pointers | hook | mechanical | ~nothing |
| meaning | instruction | **unmeasured at the main-session tier** | the entire point |

A hook is a subprocess; it cannot write prose. Only a model can, and directing a
model is instruction — so mechanical semantic capture is not available at any
price. What remains open is *how strong an instruction is needed*: an ambient line
in `CLAUDE.md`, a re-salienced nudge late in a session, or a blocking gate the
model must satisfy before the turn ends. Those differ by orders of magnitude in
cost, and the cheapest has never been tried.

That question is settled by measurement in §3, not by argument here.

---

## Design

### 1. The corrected rule

> **The tracked tier holds conclusions. Everything upstream of a conclusion is
> machine-local.**

| | before | after |
|---|---|---|
| trace index | tracked | machine-local store |
| cached transcripts | `.git/agents/trace-cache` | same store |
| drafts | did not exist | store, untracked queue |
| memory entries, handoffs | tracked | **unchanged** |

Spec 1 §1's operations table gains two entries and loses portability in two:

| Operation | Command | Behaviour |
|---|---|---|
| Check | `agents doctor` | local reachability, queue depth, store size, and that the capture instruction is present in `CLAUDE.md` |
| Materialize | `subagent-stop` hook, `agents trace cache` | unchanged, now bounded by retention |
| Read | `agents trace show <id>` | unchanged, local only |
| **Draft** | the `Stop` gate (§3) | model writes a candidate into the untracked queue |
| **Promote** | `agents review --keep <id>` (§4) | human selects; writes, reindexes, commits |
| Distil | `agents distill` (spec 3) | **demoted to fallback** — see cross-spec impact |

This puts the design in agreement with spec 3 rather than in conflict with it.
Spec 3 already requires that drafting "never writes unreviewed model output
straight into the tracked tree." Spec 3 assumed a queue and never said where it
lived. This supplies it.

### 2. One store

Index, cache, and queue become one machine-local store rather than two halves
under different rationales.

**Location: `<git-common-dir>/agents/`** — where the cache already is.

The competing option is XDG state under `machine.StateDir()`, keyed by a stable
repo identity, matching the fleet registry (§10). It was rejected here, and the
reasoning turns on a decision made in §3 of this spec rather than on §10's:

- **For `<git-common-dir>`:** lifecycle is free. Delete the repo, the store goes
  with it. XDG-keyed storage needs a stable repo identity — paths and remotes
  both move, so the key would have to be the root-commit hash — and strands
  orphaned directories of hundreds of megabytes for repos deleted months ago,
  requiring a GC command. That is one more chore, and chores are what this spec
  exists to remove.
- **Against, and unmitigated:** delete-and-re-clone loses the store. If
  unpromoted drafts are in the queue at that moment, that work is gone. The
  queue is short-lived by design and its depth is surfaced at boundaries and by
  `doctor`, which reduces the window without closing it.

The durability bar dropped because §3 moves drafting to the moment context is
live. The cache stops being upstream of anything and becomes forensic
convenience. Had drafting stayed downstream of the cache, XDG would be correct.

**Retention — new, and currently absent from every spec.** 51 MB/day unbounded
is the live defect. Capture is only possible at the instant the hook fires and
never afterwards, so caching stays; it must be bounded:

- age cap, default **14 days**
- size cap, default **1 GB**, evicting oldest first

  The two caps must be sized against each other or one is decoration. At the
  measured 51 MB/day, 14 days is ~714 MB; a 500 MB size cap would evict at ~10
  days and the age cap could never bind. 1 GB leaves age as the policy in an
  active repository and size as the backstop for one busier than this.
- pruned automatically at `post-merge`, when a lane has landed and its material
  is least likely to be wanted
- `agents trace cache prune --lane <n>` stays as the manual override

**The index keeps recording everything it records today, including
`session-start`.** Untracked it costs ~400 bytes a day, and the per-lane `stop`
count is what §3's budget arithmetic counts against. Its job changes from
portable provenance — which it could not do — to local forensics and the gate's
own bookkeeping, which it can.

### 3. Capture: the instruction first, the gate only if it fails

Capture has three possible triggers, differing by orders of magnitude in cost.
**They are tried in ascending order: each is put into real use, and only an
observed failure justifies building the next.** "Tried" is literal — the cheapest
is in use now and has produced no data yet.

| | trigger | cost to build | cost per session |
|---|---|---|---|
| **3a** | an instruction in `CLAUDE.md` | one sentence | none |
| **3b** | a non-blocking nudge re-salienced late in a session | a hook that injects context | negligible |
| **3c** | a blocking `Stop` gate | budget, watermarks, ceilings, a positive control | latency and context on every fire |

**None of the three has been measured, and 3a ships unmeasured.** The ordering is
not the result of a comparison and 3a is not the winner of one; it is a live
experiment. Two things put it first, and they are different claims:

1. **The case for 3c was never valid.** This spec's first draft went straight to
   the gate on the strength of a measurement about *subagents*, which do not act
   on `CLAUDE.md` at all — see
   [the constraint and its limits](#the-constraint-that-shapes-the-repair-and-its-limits).
   Withdrawing a bad argument for the gate does not establish that the gate is
   unnecessary. It leaves the question open, which is where it stands.
2. **Only the cheap trigger can be measured cheaply, and that is what makes the
   order non-arbitrary.** Shipping 3a *is* the experiment: a sentence costs
   nothing to try and produces the compliance data by existing. 3c cannot be
   tested that way, because building the budget, watermarks, ceilings and
   labelling *is* the expense that would need justifying. Constructing the
   subsystem to discover whether the subsystem was needed is circular.

The table above is a design-time **cost estimate**, not a measurement of
anything.

#### 3a. The instruction

`CLAUDE.md` gains one paragraph. It is a trigger, which is what §2 of spec 1 says
that file is for:

> When a stretch of work concludes — a bug understood, a decision made, an
> approach abandoned — record it before moving on: at most three bullets, covering
> what a future agent could not get from the code or the git log. Write it with
> `agents handoff draft --lane <lane> --session <id>`. Drafts are untracked until
> you review them, so drafting costs nothing and commits you to nothing.

Three properties are doing the work, and the existing sentence has none of them:

- **It names the moment.** "When a stretch of work concludes," not "when writing a
  handoff."
- **It bounds the output.** Three bullets. An unbounded ask reads as expensive and
  gets deferred.
- **It removes the perceived stake.** Untracked, reviewable, revocable. Nothing is
  being committed to the repository by drafting.

Everything downstream — the queue, `agents review`, promotion-commits — is
identical under all three triggers. Only the trigger changes.

#### 3b. The nudge, if 3a under-fires

An instruction read at session start competes with everything since. The longest
sessions have the most to record and the weakest instruction. A `Stop` hook that
*adds context without blocking* re-saliences the instruction late in a session at
no latency cost and with no decline gradient, because nothing is being demanded.

**Whether Claude Code or Codex can add context from `Stop` without blocking is
unmeasured.** It goes in the same probe as the blocking question below.

#### 3c. The gate, if 3a and 3b both fail

Everything from here to the end of §3 describes the blocking gate. **It is a
contingency, not the plan.** It is specified in full because a contingency nobody
designed is a contingency nobody can cost — but nothing in it is built until the
measurement below says the cheaper triggers were not enough.

**What decides, and it has not happened yet.** Two arms of scripted scenarios in
a throwaway repository — `agents/testdata/capture-experiment/`. One arm carries
the instruction, one has it stripped, and the scenarios are chosen so that two
have a real conclusion the diff cannot carry and two have none. `agents review
--stats` reports the draft rate over sessions that *did work*, taken from the
trace index, plus what was promoted and what was binned.

This replaces an earlier proposal to run 3a ambiently for a working week. That
measurement was paced by the calendar while the thing being measured is produced
by work — the same category error as recording per `session-start`, one document
later. Scripted scenarios answer in an afternoon and vary one thing at a time.

The comparison is not against perfection but against the baseline of zero.
Drafting on the two scenarios that have a conclusion and not on the two that do
not means 3c is over-engineering. Drafting on all four is a wording problem.
Drafting on none is the first real evidence for 3b, then 3c. A control arm that
drafts as often as the treatment arm means the paragraph is decoration.

**The drafts themselves outrank the rate.** Three bullets restating the diff are
a failure at any draft rate, and no number detects that.

Until that week has run, **nothing here is evidence about 3c either way.** The
gate is unbuilt because its justification was withdrawn, not because it was
tried and beaten.

This measurement is the compliance question the first draft of this spec deferred
until after the gate was built. Asked this way it costs a sentence, and it answers
the same thing: *does a model record knowledge when it is asked properly?*

**Order of operations on every `Stop`:**

1. Record the trace line. Always, silently, fail-open.
2. Budget spent by this session today, or the lane's ceiling reached? →
   **exit 0.** The common path, free.
3. `stop_hook_active` set? → **exit 0.** No loops.
4. Floor not met? → **exit 0.**
5. Otherwise **spend the budget — write the watermark before blocking** — and
   exit non-zero with a reason.

Spending before the outcome is deliberate. A decline must cost the same as a
draft, or the gate re-asks on the next turn and rebuilds the annoyance it exists
to avoid.

**The budget is elastic, indexed to work, not to the clock.**

A flat daily cap rations interruptions on a calendar while the thing being
rationed is produced by work. A 40-turn day and a 3-turn day would get the same
allowance — the same category error as recording per `session-start`.

**The budget is keyed on `(lane, session)`, with the ceiling on the lane.**

- **Ask #1:** floor met, no ask spent by **this session** today.
- **Ask #n+1:** **N `stop` records in this session since its last ask watermark**.
- **Ceiling K per lane per day**, across all sessions on it, so a pathological day
  cannot spiral no matter how many agents are running.

The key must not be the lane alone. Spec 1 §4 designs for concurrent agents on one
branch — that is why handoffs are keyed `(lane, session)` and why `--session` is
mandatory. A lane-keyed budget breaks the same case: agent A spends the lane's
allowance on its own work, agent B is never asked about entirely different work on
the same branch, and B's turns accelerate A's next ask. Watermarks are per session
for the same reason. The lane-level ceiling stays because the thing worth capping
globally is total interruptions, and that is a property of the lane.

Turns are the right unit. Commit counts over-trigger in a repository where the
agent commits several times an hour, and under-trigger for read-only work — and
read-only work is where the valuable sessions are: the two-hour debugging session
that touches nothing and concludes *why*. The count is free: the index already
records every `stop`, so turns-since-last-ask is a count over the store.

**The floor (step 4) stays deliberately low.** Its only job is to avoid asking on
a session that just started; the budget already bounds over-firing. Turn count or
a dispatched subagent is enough. The tempting proxies — files changed, commits
made — exclude exactly the sessions worth capturing.

**Starting constants, labelled as guesses: N = 10 `stop` records, K = 3 per lane
per day.** §4.1's lane-health thresholds have sat unimplemented at "tuned once
there is real data" because nothing collects the data. This gate collects its
own. Every ask records its outcome in the store — **the summary and its label**,
`keep` or `redundant`, and for kept drafts whether they were later promoted or
binned.

**Decline rate on its own is not a usable signal, and recording the reason is what
makes it one.** A high decline rate is equally consistent with three different
causes that call for opposite fixes:

| observation | could mean | so |
|---|---|---|
| high `redundant` rate | asking too often | raise N |
| high `redundant` rate | the model is dodging the work | the summaries say so on inspection; see below |
| high `redundant` rate | **the gate is working correctly** | change nothing |
| drafts written, rarely promoted | asking at the wrong moments | raise the floor; reconsider mid-work timing |
| lanes going a week with an empty queue | asking too rarely | lower N |

Stored summaries are what separate the first three: they are spot-checkable
against the session that produced them. A `redundant` label with no summary
recorded is indistinguishable from a dodge to end the turn faster, which is why
the summary is required rather than requested.

**The gate must not ask a yes/no question, because the two answers do not cost the
same.** Declining is one line and the turn ends; drafting is work. That is an
incentive gradient, and no wording sits on top of it — a request-with-opt-out
invites the cheap answer however it is phrased. Both edges are real (an agreeable
model over-drafting is the opposite failure) but only one of them is subsidised by
the mechanism.

So the summary is unconditional and the only judgment is a label:

> Before finishing: in at most three bullets, state what this session established
> that a future agent could not get from the code or the git log. Then label it —
> `keep` if it is worth carrying forward, `redundant` if the code and history
> already carry it. If `keep`, write it with
> `agents handoff draft --lane feat-x --session <id>`. The summary and label are
> recorded either way.

Declining now requires the same retrospective *thinking* as drafting: the session
must be summarized in order to argue that nothing came of it. Only the
writing-down differs, and §4 bounds that too — **the draft is those same three
bullets plus frontmatter, not an essay.** Attacking the gradient from both ends is
what closes it; expansion happens at review, where a human is present and the
material is already in front of them.

This also gives the compliance measurement something to read. A stored `redundant`
label now arrives with a summary attached, so a week of declines is falsifiable
evidence rather than a rate meaning three contradictory things.

`redundant` summaries are recorded in the store and **do not enter the queue** —
auditable without cluttering review. `agents review --audit` lists recent declines
with their summaries; where one looks wrong, the cached transcript is still there.
That is the first job in this design where the cache is load-bearing rather than
forensic insurance.

**This narrows the gradient; it does not remove it.** A model can still write
three lazy bullets and label them `redundant`. The difference is that it now has
to write something falsifiable in order to do so.

**Timing weakness, stated rather than engineered away.** The right moment to ask
is when work *ends*. `Stop` fires at the end of every turn and cannot know which
is last. `SessionEnd` fires once at the true end, with no model left to write
anything. So the gate necessarily asks mid-work and the draft is a snapshot
rather than a summing-up. Mitigations: drafts are revisable, the budget renews so
a long lane is asked again, and a mid-lane note is a truthful record of what was
learned by then. This is a cost of the approach.

**Note that 3a does not have this weakness**, which is a substantive argument for
trying it first rather than merely a cheaper one. An instruction fires when the
*model* judges that work concluded — the moment the gate structurally cannot
reach. The gate buys reliability and pays for it in timing; the instruction buys
timing and pays for it in reliability. Only measurement says which trade is worth
more here.

**The second cost is context, not latency.** A blocking `Stop` puts the
instruction *and the resulting draft* into the session's own transcript. At K = 3
that is three retrospectives injected into the working context of a session that
did not ask for them, priming subsequent turns. The draft belongs in the queue,
not in the conversation, and there is no mechanism that puts it in one without the
other. Keeping K small is the only lever.

**The harness probe, if the gate is ever reached.** Claude Code's `Stop` payload
carries `stop_hook_active`, and Codex's does too — a field that exists only to
bound a blocking stop hook. That is strong circumstantial evidence that both
harnesses honour a blocking `Stop`, and it is **not a measurement of the blocking
behaviour**. This repository's standard is positive controls (§"Measured facts").
Probe both, together with 3b's question of whether `Stop` can add context without
blocking — one probe answers both, and 3b needs the answer first. A harness that
cannot block gets an advisory gate that prints, and its queue stays empty rather
than pretending.

**Compliance is measured at 3a, not here.** The first draft of this spec deferred
"does the model write a useful draft, or game the gate to end the turn" until
after the gate was built — which put the expensive machinery upstream of the
question that decides whether the machinery is needed. Running 3a for a week
answers it for a sentence.

**Draft location:** `<store>/queue/<lane>/<session>-<n>.md`, untracked.

**Draft frontmatter is the promotion contract, not a suggestion.** A `kind:
handoff` draft carries `lane`, `session`, `when`. A `kind: memory` draft must
additionally carry everything a memory entry requires — `name` (the slug),
`description`, `metadata.type`, and any `sources:` — because `INDEX.md` is
generated from exactly those fields and `agents guard --staged` regenerates and
compares byte-for-byte. **Promotion validates and refuses an invalid draft; it
never synthesizes the missing fields.** Synthesis at promotion time would put a
guessed slug and description into the tracked tree at the one moment nobody is
reading carefully, and a malformed entry would surface as a blocked commit far
from its cause.

**Secrets.** The draft is unreviewed model output, which is precisely why it must
not land in the tracked tree — and does not. Because promotion is a commit,
`agents guard --staged` and gitleaks remain exactly the boundary they are today.
Nothing bypasses the existing gate.

### 4. Review: the queue is storage, the conversation is the interface

```
agents review [--lane x]      list pending drafts
agents review --show <id>     print one
agents review --keep <id>     promote: write + reindex + scoped commit
agents review --bin <id>      delete from the queue
agents review --edit <id>     $EDITOR, then keep
agents review --audit         recent `redundant` declines with their summaries
```

**Drafts are bounded.** A draft is the three bullets the gate asked for plus its
frontmatter — not an essay. This is half of what keeps declining from being the
cheap answer (§3); the other half is requiring the summary either way. A draft
that wants to be longer grows at review, where a human is present and the material
is in front of them.

**Promotion is one act.** `--keep` writes to `.agents/memory/<name>.md` or
`.agents/reports/handoff/<lane>/<date>-<session>.md` per the draft's `kind`,
regenerates the affected `INDEX.md`, and makes the `.agents/`-scoped commit —
inheriting the parts of `agents save` that are not merely `git commit`:
reindex-before-guard, path scoping, refusal mid-merge. There is no
"promoted but uncommitted" state in which work can be lost or swept into a code
commit.

**A promoted memory entry lands on whatever branch you are standing on, and that
is a problem the two kinds do not share.** A handoff is lane-scoped, so committing
it to the lane's own branch is exactly right. A memory entry is repo-wide
knowledge: promoted from a feature branch it is invisible to every other lane
until merge, and lost outright if the branch is abandoned. Promotion of a
`kind: memory` draft outside the default branch therefore **warns and proceeds** —
naming the branch and that the entry travels only if it merges. It does not
refuse: refusing would block the case where the knowledge came *from* that
branch's work and belongs in its merge. It does not silently retarget the default
branch either, which would commit to a branch the user is not on.

**There is no `--keep --all`, and the omission is load-bearing.** Pending drafts
are surfaced to agents at boundaries and at `SessionStart`. An agent able to bulk
promote closes the review loop with no human in it, which defeats the design.
Requiring an explicit id per draft leaves the agent one move — show the drafts and
ask — so selection happens in conversation, where the context is, while the queue
is what lets that conversation survive a three-day interruption.

**Notification, never blocking.** `post-checkout` and `post-merge` are already
installed, already firing, and today carry no built-in stage. They gain one: print
the lane's pending-draft count. Hook stdout reaches a human terminal and an
agent's tool result identically, with no interactivity, which is why the
notification lives here and the *selection* does not.

| hook | what happened | what it prints |
|---|---|---|
| `post-checkout` | left a lane, work unfinished | pending drafts for the lane being left |
| `post-merge` | a lane landed | pending drafts for the merged lane; prunes the cache |

**`agents handoff write` is unchanged** — same primitive, body on stdin,
`--session` required. It gains **`agents handoff draft`** as a sibling verb: same
stdin contract, writing to the queue instead of the tracked tree. That verb is
what the `Stop` gate invokes, and it is the caller §4 specified and never built.

**`handoff write --draft` retires with its meaning.** The flag currently writes
into the *tracked* tree with `status: draft`, which is the thing that must not
happen — unreviewed model output inside `.agents/`. Unreviewed now means
*unpromoted*, and unpromoted means *not in the tree*. The `status` frontmatter
field stays, because a promoted draft is still worth distinguishing from one
written deliberately by hand.

This is what makes model-authored content acceptable at all. Spec 1 rejected
redirecting harness auto-memory into `.agents/memory/` because it aimed
unreviewed model output at a tracked directory and made the secret guard
load-bearing rather than defence-in-depth. An untracked queue with explicit
promotion is that idea done safely: the tracked tree only ever receives what was
chosen.

**`agents save` retires to a documented escape hatch** — a hand-edited memory
entry, or batching several promotions into one commit. Not a reflex, not in the
normal path.

### 5. What retires

| | why |
|---|---|
| `.agents/reports/traces/` from tracking | §1 |
| `merge=union` on traces in `.gitattributes` | nothing tracked appends concurrently |
| doctor's `pointers:local-unreachable` | unreachable pointers are now normal, not a finding |
| `agents save` from the normal path | promotion commits |
| guard's mixed-commit warning as a daily event | stays; becomes rare again |

`pointers:local-unreachable` is deleted, not fixed. It reported a race the design
could not win, about data that no longer needs to survive. It is one of the two
warnings making `doctor` exit 1 on a healthy machine today.

**New checks:** queue depth per lane, store size against the caps, and **that
`CLAUDE.md` carries §3a's capture paragraph** — checked exactly like the existing
`scaffold:doctor-instruction`. Under B′ that paragraph *is* the capture mechanism,
so its silent absence would be the whole feature silently absent. If §3c is ever
built, gate liveness joins them.

### 6. Migration

1. Copy tracked trace content into the local store. Nothing is lost locally.
2. `git rm -r --cached .agents/reports/traces`, drop the `merge=union` line,
   remove the directory from the scaffold.
3. Prune the cache to the new caps on first run.
4. **Git history is left alone.** Decided 2026-08-12. Three commits contain
   records. Rewriting a public repository's history breaks every clone, and what
   is exposed here is low-sensitivity: lanes are `master` and
   `agents-design-spec`, and the GitHub handle already discloses the username.

**The exposure generalizes even though this instance is mild, and that is the
reason it is recorded.** `agents init` installs this behaviour in every
repository. In a work repository, `lane` is a ticket id and `cwd` names modules
and clients, published to whatever remote that repository has. Spec 1 §10 reasoned
about exactly this hazard — "a registry enumerating every repo path would publish
project names, client or employer names, and directory structure" — and reached
the opposite conclusion for a store carrying the same class of data. A repository
migrating with sensitive lane names should decide history rewriting on its own
facts.

### 7. Expected phasing

This is one spec because the pieces are interdependent — a queue with no gate
stays empty, a gate with no review command fills a queue nobody drains — but it is
not one landing. The plan is expected to split at the seam where new behaviour
begins:

| Phase | Contents | Ends somewhere working | Gate to the next phase |
|---|---|---|---|
| A | One store, retention, migration, untracking, doctor changes | Tracked tree clean, store bounded. No new behaviour. | — |
| B′ | §3a's instruction, the queue, `agents handoff draft`, `agents review`, the event log and `review --stats` | **The whole loop, closed, with a one-sentence trigger.** | The two-arm scenario run in `testdata/capture-experiment/` |
| B″ | §3b's non-blocking nudge | Same loop, re-salienced late in long sessions | Another week, if B′ under-fires |
| C | §3c's `Stop` gate: budget, watermarks, ceilings, the harness probe | Same loop, with capture forced | built only if B′ and B″ both fail |

**B′ closes the loop on its own.** That is the substantive change from this spec's
first draft, which put the gate in B and the review command in C — so nothing was
usable until both landed, and the expensive half came first. The queue, the draft
verb, and `agents review` are shared by all three triggers; only the trigger
differs. Ordering them cheapest-first costs one sentence to test and could retire
C entirely.

**Phase A is a net deletion, and it is the easy one.** It removes machinery and
produces no conclusions. If nothing after it lands, spec 7 leaves this repository
with *less* machinery and the same zero conclusions it has today — tidier, not
better. A is not a resting point, and B′ is cheap enough that there is no excuse
for stopping there.

**A and B′ should be planned together.** They are, jointly, smaller than the first
draft's Phase B alone.

---

## Cross-spec impact

Recorded so parallel work streams are not surprised. Each spec below gets an
amendment note pointing here, as spec 5 received on 2026-08-11.

### Spec 1 — amended, not rewritten

§1 gains the conclusions-versus-upstream rule and the Draft and Promote
operations; Materialize and Read become local-only. §3.6 and §3.7 lose
portability and `merge=union`. §4 gains an `agents handoff draft` verb targeting
the queue and retires `handoff write --draft`; the auto-draft caller it specified
arrives as §3a's instruction rather than as the `Stop` hook §4 imagined. §6 gains
`agents review` and `agents handoff draft`; `agents save` is demoted. §2's
scaffolded `CLAUDE.md` gains the capture paragraph.

**One direct contradiction, contingent on §3c and therefore not yet live.** §6
states: *"Recording hooks exit 0 on every path: a failed record must never disrupt
a dispatch. `agents guard` is the sole deliberate exception."*

Under §3a and §3b that sentence stays true — nothing new blocks. Under §3c the
gate would be a second exception, and a semantically different one:

| | blocks on | meaning |
|---|---|---|
| `agents guard` | failure | something is wrong; stop |
| the `Stop` gate | success | everything worked; a question is being asked |

If §3c is ever built, both halves must be stated: the recording path still fails
open — a failed trace write exits 0 — and only the gate blocks, only after
spending its budget. Until then spec 1 §6 needs no change on this point, which is
one more reason to try the cheap trigger first: it amends less.

### Spec 3 — premise inverts; do not design against the current text

`agents distill` was the path from raw material to knowledge. It becomes the
**fallback** for lanes never drafted, mining cached transcripts after the fact.

- Its open question 3 — *"is there a useful `--since <last-distill>` watermark,
  and where is it stored?"* — is answered: the store holds ask watermarks per
  `(lane, session)`.
- Its constraint that drafting must draft for review and never write unreviewed
  output into the tracked tree is now *implemented* by the untracked queue rather
  than left to the implementer.

Spec 3 gets smaller and easier. Whoever picks it up must read this first or they
will design the wrong primary path.

### Spec 4 — a capability requirement, contingent

**Only if §3c is ever built.** The wiring DSL would then have to express "this
hook may block," and "this hook adds context without blocking" for §3b — a
per-hook capability, not just a command string, with degradation on a harness that
fails the positive control.

Under §3a, wiring is unchanged: the instruction lives in `CLAUDE.md`, which is
scaffold, not wiring. Spec 4 should treat this as a requirement that may never
arrive rather than one to design around now.

### Spec 5 — unaffected in its claims, one sequencing collision

Both organizing claims still hold and get easier: with traces untracked, CI never
sees machine-local paths in the tracked tree.

The collision is §6's command registry and the help text derived from it.
`agents review` and `agents handoff draft` land in that registry. Either spec 5's
registry ships first, or these commands arrive without help text and spec 5
inherits them. This is a scheduling decision, not a conflict.

### Spec 6 — one addition to scope

The store now has a schema, machine-local, that a released binary must migrate
across versions. That is not currently in spec 6's scope.

### Spec 2 — unaffected

---

## Testing

Following §11's standards: golden files from real payloads, structural assertions
over string searches.

**Phases A and B′ — everything that gets built now:**

- **The queue is unreachable from git** — structural, not an ignore-rule check.
- **Promotion is atomic** — writes, reindexes, and commits with guard running; a
  failure at any step leaves the queue item intact.
- **Promotion refuses an invalid draft** — a `kind: memory` draft missing `name`,
  `description`, or `metadata.type` is rejected at promotion, and nothing reaches
  the tracked tree for the generated-index guard to block on later.
- **Promoting a memory entry off the default branch warns and proceeds** — it
  neither refuses nor retargets.
- **Promotion requires an explicit id.** The absence of bulk promotion is a design
  constraint, so it gets a test.
- **Retention** — age and size eviction; migration preserves content before
  untracking.
- **Recording still fails open** — a trace-write failure exits 0.
- **Redaction stays structural** — unchanged from §11, and now additionally
  relevant because drafts are model output.
- **The scaffolded `CLAUDE.md` carries §3a's paragraph** — `agents init` writes it
  and `doctor` reports its absence, the same way it already checks for the doctor
  instruction. An instruction that is the entire capture mechanism must not be
  silently droppable.

**Phase C only — not built until B′ and B″ have failed:**

- **Gate arithmetic** — budget spend-before-block, N-turns-since-watermark, the
  K-per-day ceiling, the `stop_hook_active` loop guard, the floor.
- **Concurrent sessions on one lane** — two sessions each get their own ask and
  their own watermark; neither consumes the other's; the lane ceiling still caps
  the pair. This is the defect a lane-keyed budget would have, so it gets a test
  rather than a comment.
- **A `redundant` label without a summary is rejected** — the gate's whole defence
  against a reflexive decline is that the summary is required, so the requirement
  is enforced rather than requested, and `--audit` surfaces what was stored.
- **Positive control per harness** that a blocking `Stop` is honoured, and whether
  `Stop` can add context without blocking (which 3b needs). A harness failing the
  first degrades to an advisory gate, and a test pins that degradation.

---

## Rejected alternatives

**Keeping traces tracked and fixing the cache.** The measured failure is not that
the cache misses transcripts; it is that a tracked pointer to a machine-local file
has no reader anywhere. A perfect cache would leave 48% of records resolving to a
file only one machine can open, and would not write a single conclusion.

**A work-order queue instead of a draft queue.** Items would be "lane x landed, 4
sessions, material cached here" — mechanical, cheap, and no model output until you
engage. Rejected because reviewing then means *doing the work*: a later agent must
re-read cached transcripts and reconstruct what mattered. That is a todo list, and
the handoff directory is already a todo list with zero items.

**An interactive picker in the hook.** The literal reading of a selection UI. At a
work boundary the actor is usually an agent running `git merge`; a TTY prompt
either hangs it or is auto-dismissed, and when it does reach a human it interrupts
mid-git-operation — the worst moment to ask what is worth remembering.

**`SessionEnd` instead of `Stop`.** Fires once, at the true end of work, which is
the right moment. No model remains to write anything.

**XDG state for the store.** See §2. Correct had drafting stayed downstream of the
cache; wrong once drafting moved to the moment context is live, because the
orphan-GC chore then outweighs a durability requirement that no longer exists.

**Building the `Stop` gate first.** This spec's own first draft. Rejected on
review: it justified skipping the cheap trigger with a measurement about
subagents, and it put the machinery upstream of the question that decides whether
the machinery is needed. The gate survives in full as §3c, as the contingency it
should always have been.

**Capturing mechanically at `Stop` and summarizing later at review time.** Removes
the incentive problem outright — no model judgment at capture, so nothing can be
dodged. Rejected because the reviewing agent then lacks the producing session's
context and has to reconstruct it from a transcript, which is the expensive, lossy
path that draft-at-Stop exists to avoid. It survives as `agents distill`
(spec 3), which is the right shape for material the gate never saw.

**A yes/no gate with carefully chosen wording.** Rejected in §3: the two answers
do not cost the same, and wording cannot flatten an incentive gradient built into
the mechanism.

**Rewriting git history to remove pushed trace records.** See §6.

---

## Risks

| Risk | Mitigation |
|---|---|
| **§3a's instruction is ignored and the queue stays empty** — the risk that decides everything downstream | This is measured, not mitigated. A week of B′ counts sessions, drafts and promotions against a baseline of zero. Failure promotes 3b, then 3c — which is why 3c is specified in full rather than hand-waved. |
| Building the gate first and discovering the instruction would have sufficed | The ordering in §7 exists for this. The first draft of this spec made exactly this error. |
| **Phase A lands, nothing after it does** — the repo ends with less machinery and the same zero conclusions | §7 names it; A is not a resting point, and B′ is one sentence plus the review path, so there is no cost excuse |
| An instruction decays in salience over a long session | §3b exists for this, and is cheaper than the gate. Unmeasured until B′ reports. |
| A session ends abruptly and the instruction never fires | Accepted under B′. It is one of the three things only the gate fixes, and it is what a failed B′ week would demonstrate. |
| A memory entry promoted on a feature branch is lost if the branch dies | Promotion warns and names the branch; it does not refuse or silently retarget |
| A `kind: memory` draft lacks the frontmatter a memory entry needs | Promotion validates and refuses rather than synthesizing a slug nobody reviewed |
| A memory entry promoted on a feature branch is lost if the branch dies | Promotion warns and names the branch; it does not refuse or silently retarget |
| A `kind: memory` draft lacks the frontmatter a memory entry needs | Promotion validates and refuses rather than synthesizing a slug nobody reviewed |
| Losing the bare signal that work happened on another machine | Accepted, and named as a loss in the diagnosis. If it matters on its own, it needs a purpose-built tracked artifact, not a pointer index |
| Queue becomes an inbox nobody empties — handoff's failure, repeated | Boundary hooks and `doctor` surface depth; items are pre-written, so review is seconds not work |
| Delete-and-re-clone loses unpromoted drafts | Queue is short-lived by design; depth surfaced at boundaries. Not eliminated. |
| An agent bulk-promotes its own drafts | No `--keep --all`; explicit id per draft; a test pins the constraint |
| Store grows unbounded, as the cache did | Age and size caps, pruned at `post-merge`; `doctor` reports size |

**Phase C only, and therefore currently hypothetical:**

| Risk | Mitigation |
|---|---|
| A harness does not honour a blocking `Stop` | Positive control before build; degrade to advisory and report it in `doctor` |
| Models decline reflexively to end the turn | The gate asks for an unconditional summary and a label, not a yes/no, so declining costs the same thinking as drafting; drafts are bounded so drafting is cheap too; `--audit` makes lazy labels findable. Narrowed, not closed. |
| The model drafts rather than declining | `redundant` is a first-class label; drafted-but-never-promoted rate is measured |
| Mid-work timing produces premature drafts | Drafts revisable; budget renews. §3a does not have this weakness at all. |
| Blocking injects drafts into the session's own context, priming later turns | Small K is the only lever; no mechanism separates the two. Not eliminated. |
| N and K are wrong | They are labelled guesses; the gate records outcomes so they are tuned from data rather than argued |

---

## Open questions

- **The wording of §3a's instruction.** The draft in §3a names the moment, bounds
  the output, and removes the perceived stake, which is three more properties than
  the sentence it replaces has. Whether it is *good enough* is the B′ measurement
  and nothing else. It is a parameter: revise and re-measure before escalating to
  3b or 3c, because a cheap trigger revised twice still costs less than the gate.
- **Whether a `Stop` hook can add context without blocking**, under either
  harness. 3b depends on it entirely and it is unmeasured. Probe it with the
  blocking question rather than separately.
- **Draft `kind` inference.** The model proposes `memory` or `handoff` in
  frontmatter. Whether that is reliable enough to skip a confirmation at promotion
  is unknown until there are drafts to look at.
- **The floor's exact definition (§3c).** Contingent on the gate being built at
  all. Turn count and dispatched-subagent are both defensible; neither has data.
- **Codex, under any trigger.** §3a reaches Codex through `AGENTS.md`, which is a
  symlink to `CLAUDE.md`, so the instruction is free there. Whether Codex sessions
  act on it at the same rate as Claude Code sessions is a separate reading of the
  same week, and the two should not be pooled.
- **Whether `session-start` records still earn their place** once the index is
  local and the budget counts `stop` records only. Cheap to keep; keeping them is
  not the same as needing them.
