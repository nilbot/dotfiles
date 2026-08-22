# Spec 4 — the wiring DSL

**Date:** 2026-08-07 (deferred) / **2026-08-22 (designed — triggers fired)**
**Status:** designed, not implemented.
**Depends on:** [spec 1](2026-08-07-agents-repo-context-design.md) §5 (harness
adapters), §6 (exit codes), §3.2 (the redaction guarantee).
**Reads against:** [knowledge is documentation](2026-08-19-knowledge-is-documentation.md)
— which decides that most of what we deliver is not wiring at all.

> **History.** This document was "deferred, not rejected" from 2026-08-07 to
> 2026-08-22, gated on three named triggers. Two have fired. The 2026-08-12
> contingent requirement from spec 7 lapsed with spec 7's capture half; it is
> preserved in the archive, not restated here. The original text's core
> argument — that a DSL forces the *semantic event* to be named separately from
> each vendor's encoding of it — survives intact and is now the design.

---

## 1. Why now

Two of the three build triggers are true.

**Trigger 1 — a third target landed.** The original wording was "a new harness,
or `agy` gaining workspace-local hook support." Both, at once.
[Antigravity is not out of scope](../qna/is-antigravity-really-out-of-scope.md)
records the re-test: workspace-local `<workspace>/.agents/hooks.json` has loaded
since `agy` 1.1.1, and the Antigravity **app** ships the same hook machinery as
the CLI (measured 2026-08-22 — see
[the app shares the harness](../qna/does-the-antigravity-app-share-the-cli-harness.md)).

**Trigger 3 — capability requirements need to be declarative.** The original
example was, verbatim: *"this hook requires the subagent-transcript-pointer
capability; skip it on harnesses that lack it."* That is now the central
requirement, and §3 shows why it is not sufficient.

Trigger 2 — per-repo wiring divergence — remains false. Every repo still wants
identical wiring. **The DSL varies by harness, not by repository**, and nothing
below introduces a per-repo knob.

One engineering fact the original could not know: the "two targets whose schemas
are ~90% identical" premise is dead. Claude Code and Codex both nest under a
`hooks` object. Antigravity keys on the **hook name at top level**, with no
wrapper — a config written in the Claude dialect loads nothing, which cost a
probe. Three targets, two structurally different renderers, and a third vendor is
one release away from a fourth.

## 2. What it is

A declarative source of truth for hook wiring: one tracked file describing which
**semantic moments** this tool wants to act on and **what each needs to work**,
compiled per harness into `.claude/settings.json`, `<repo>/.codex/hooks.json`,
`<workspace>/.agents/hooks.json`, and whatever comes next.

It covers **wiring only**. Skills, subagent definitions and rules are
markdown-with-frontmatter, already near-common across all three vendors, and get
*placed* — never generated. See §7.

## 3. The correction that reshapes the design

The 2026-08-07 model was one-sided: *the tool has needs; harnesses have
capabilities; emit where capability ⊇ need.* Measured 2026-08-22 against this
repository's own trace records, that is wrong.

```
records by harness:                claude-code 304    codex 14
verified pointers still on disk:   claude-code 165    codex 12
verified pointers now gone:        claude-code 110    codex  2
codex subagent-start/subagent-stop records:                  0
```

Claude Code has lost **110 of 275** verified pointers — 40% — including
[25 subagent transcripts deleted inside the session that produced them](../qna/why-are-subagent-transcripts-gone.md).
Codex's two losses are both from one abandoned 2026-08-10 session: loss mode 1
(a whole session file), not mid-session pruning. Codex has produced **no**
subagent records at all in production use, though spec 1 observed both events
firing in a probe — so the wiring is live and the need has never been
demonstrated.

**The transcript cache exists because Claude Code destroys raw material
mid-session. That is a measured property of one harness, not of harnesses.** We
wired `subagent-stop` on Codex for a problem Codex has not been shown to have,
and the one-sided model is what let us do it without noticing.

So the model has two axes, not one:

> Emit a hook where the harness **exhibits the need** *and* **provides the
> capability**. Absence of either is a reason not to wire, and the two absences
> mean different things.

| | provides | does not provide |
|---|---|---|
| **exhibits the need** | wire it | **a real gap** — degrade, and say so |
| **need not shown** | wire nothing; record the hypothesis | not a gap; silence |

The bottom-left cell is where Codex's `subagent-stop` sits today. The top-right
is where Antigravity's missing `SessionStart` sits — *if* the need is ever shown
there.

This axis is not decoration. It is the difference between a tool that ports its
author's harness's problems to every other vendor, and one that asks each vendor
what it actually breaks.

## 4. The vocabulary

Three tables. The point of the DSL is that these are **data you can diff**,
rather than conditionals rederived per adapter.

### 4a. Semantic moments

Named for what happens, never for a vendor's spelling.

| moment | meaning |
|---|---|
| `session.begin` | a top-level conversation starts |
| `turn.before-model` | the model is about to run, within a turn |
| `turn.after-model` | the model has run, within a turn |
| `turn.end` | the agent is about to stop |
| `subagent.begin` | a child agent starts |
| `subagent.end` | a child agent finishes |
| `tool.before` / `tool.after` | around one tool call |

### 4b. Capabilities

What a harness can supply *at* a moment. `Capabilities` in
`agents/internal/harness` is this table with exactly one entry today
(`Description`); the DSL is what makes it a vocabulary.

| capability | means |
|---|---|
| `payload.child-transcript` | the payload names the child's transcript file |
| `payload.description` | a human label for a subagent is available |
| `payload.turn-id` | a per-turn identifier exists |
| `result.inject-context` | the hook's stdout can add context to the model |
| `result.block` | the hook can refuse, or force continuation |

### 4c. Harness matrix

Measured, with the unmeasured marked. **A blank is not a zero** — it is a
measurement nobody has taken, and the DSL must not encode it as a decision.

| moment | Claude Code | Codex | Antigravity |
|---|---|---|---|
| `session.begin` | `SessionStart` | `SessionStart` | **absent** |
| `turn.before-model` | — | — | `PreInvocation` + `result.inject-context` |
| `turn.after-model` | — | — | `PostInvocation` + `result.block` |
| `turn.end` | `Stop` | `Stop` | `Stop` (1.1.10+) |
| `subagent.begin` | `SubagentStart` | `SubagentStart` | *unmeasured* — `PostToolUse` on `invoke_subagent`? |
| `subagent.end` | `SubagentStop` + `payload.child-transcript` | `SubagentStop` + `payload.child-transcript` | *unmeasured* |
| `tool.before` / `tool.after` | `PreToolUse`/`PostToolUse` | same | same (+ `result.block` on `PreToolUse`) |

The asymmetry runs both ways, which is the finding. Antigravity has no
`session.begin` and its subagent events are unmeasured — but it is the only
harness in the fleet offering `turn.before-model` with `result.inject-context`.
That is a per-turn injection point with a concrete subject that can stay silent
on no match: precisely the mechanism the redesign wanted for the read trigger and
recorded as not having.

## 5. Schema

TOML — precedent in Codex's own `config.toml`, and Go parses it for free. **Not a
hand-written grammar.** If it needs its own lexer, the design went wrong.

```toml
# .agents/wiring.toml — tracked, hand-edited, the only source of hook truth.

[intent.cache-subagent-transcript]
moment   = "subagent.end"
run      = "hook subagent-stop"
requires = ["payload.child-transcript"]
# Wire only where the harness is known to destroy transcripts mid-session.
needed-by = ["claude-code"]
why = """
Claude Code prunes subagent transcripts during the producing session; capture is
now-or-never. Measured: docs/qna/why-are-subagent-transcripts-gone.md.
Codex is deliberately absent — 0 subagent records, no pruning observed.
"""

[intent.record-session-pointer]
moment   = "session.begin"
run      = "hook session-start"
requires = []
needed-by = ["claude-code", "codex"]
why = "A pointer to the session transcript, written before it can be lost."

[intent.record-turn-end]
moment = "turn.end"
run    = "hook stop"
requires = []
needed-by = ["claude-code", "codex", "antigravity"]

[target.claude-code]
path     = ".claude/settings.json"
dialect  = "nested-hooks"      # {"hooks": {"<Vendor>": [...]}}
tracked  = false
ownership = "merge"            # holds unrelated settings; never clobber

[target.codex]
path     = ".codex/hooks.json"
dialect  = "nested-hooks"
tracked  = false
ownership = "own"

[target.antigravity]
path     = ".agents/hooks.json"
dialect  = "flat-named"        # {"<Vendor>": [...]} — no wrapper
tracked  = true                # .agents/ is tracked context; see §7
ownership = "merge"
```

`needed-by` is an allowlist, not a denylist, and it is the axis §3 adds. Omitting
a harness is a claim that the need has not been demonstrated there — which is
falsifiable, and which `why` must state.

## 6. Compilation and degradation

For each target, for each intent:

1. Skip unless the target is in `needed-by`. Silent: not a gap.
2. Look up `moment` in the harness matrix. If absent → **degrade**, and emit an
   advisory (exit 1) naming the intent, the harness, and the missing moment.
3. Check `requires` against the harness's capabilities. Missing → same.
4. Render `run` into the target's dialect at the vendor spelling.

**Degradation must be loud and must not block.** A missing hook produces no error
on its own — that is the whole reason `doctor` exists — so an intent that cannot
be wired has to be visible somewhere a human looks. Exit 1 (advisory), never 2
(block): an unwireable intent on one harness is not a broken repository.

`agents doctor` gains one check: **every intent in `needed-by` is either wired or
reported as degraded.** Nothing else in the tool learns the vocabulary — the DSL
is read by the compiler and by `doctor`, and by nothing else.

## 7. What it must not do

- **Must not swallow content.** Skills, rules, and subagent definitions are
  placed, not generated. All three vendors read markdown-with-frontmatter from a
  directory; Antigravity's customization root *is* `.agents/`. That convergence
  is free and needs no DSL.
- **Must not introduce per-repo knobs.** Trigger 2 has not fired.
- **Must not encode unmeasured behaviour as a decision.** A blank cell in §4c is
  an open measurement. The compiler must distinguish "this harness lacks the
  moment" from "nobody has checked," and report the second differently.
- **Must not defeat a trust gate.** Unchanged from spec 1: no harness lets a
  freshly wired repo's hooks fire unattended, and that is by design.
- **Must not grow a `[repo.*]` section.** If one is ever proposed, re-read
  trigger 2 first.

## 8. The reason to build it that is not cost-justified

Preserved from the original, because it still will not show up in any
cost/benefit table, and because it is now half-earned.

A DSL forces the semantic event to be named separately from each vendor's
encoding of it. "A subagent finished" is not `SubagentStop`, and it is not
`PostToolUse` matching `invoke_subagent` — those are two spellings of one idea.
Writing the abstraction down turns the differences between vendors into **data
you can compare**, rather than implementation detail rederived each time.

What has changed since 2026-08-07 is that the comparison has started paying. §3
is a finding about *our own wiring* that only became visible once needs and
capabilities were tabulated separately. §4c is a finding about the vendors: the
one harness we excluded for fifteen releases is the only one offering the
mechanism the redesign said it lacked.

Read alongside
[the empty knowledge stores](../qna/do-vendor-knowledge-subsystems-get-used.md),
the shape of the landscape is not what "bridge the divide between vendors"
assumes. All three vendors converged on the same *placement* primitives, so most
compatibility is already free. They diverge on hooks — the surface with the least
demonstrated value in this repository's record. The divide worth bridging is not
vendor-to-vendor. It is **mechanism versus instruction**, and this document is
the small, honest mechanism half.

## 9. Open, and deliberately not decided

- **Does Antigravity destroy subagent trajectories?** The decisive measurement
  for whether `cache-subagent-transcript` ever gains `antigravity`. Its tooling
  says *"logs and artifacts are preserved"* when a subagent tree is killed, while
  a `Failed to prune trajectory` path also exists. Until measured, the cell stays
  blank.
- **Do Antigravity hooks fire inside its subagents?** Unmeasured, and it decides
  whether `turn.before-model` can carry the read trigger where instructions
  demonstrably fail (0 of 31).
- **How does the Antigravity app grant workspace trust?** `~/.gemini/antigravity/`
  has no `settings.json` at all, so the CLI's `trustedWorkspaces` mechanism is
  not the app's. Trust is the gate the CLI probe had to clear.
- **Should Codex's `subagent-stop` wiring be removed?** §3 says the need is
  undemonstrated. It is also nearly free and already trusted. Left in place
  pending either a Codex pruning observation or a decision to enforce
  `needed-by` retroactively.
- **Whether `turn.before-model` gets an intent at all.** Nothing here proposes
  one. The redesign's read trigger is delivered by instruction today; mechanising
  it is a separate design with its own falsifier, and this document only records
  that the capability now exists.
