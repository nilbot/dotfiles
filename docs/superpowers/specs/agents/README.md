# `agents` — design specs

Everything under `docs/superpowers/specs/agents/` belongs to one body of work: the
`agents` tool and the tracked `.agents/` directory it maintains in each repo. Other
projects get sibling folders under `specs/`.

Spec numbers are stable catalog identifiers, not priority or implementation
order. Follow the explicit dependencies and status instead; an independent later
number may be implemented before an earlier one without renumbering either.

| # | Spec | Status | Depends on |
|---|---|---|---|
| 1 | [`agents` — repo-tracked agent context](2026-08-07-agents-repo-context-design.md) | implemented | — |
| 2 | [dotfiles hygiene](2026-08-07-spec-2-dotfiles-hygiene.md) | implemented | spec 1 §8 (landed) |
| 3 | [`agents distill`](2026-08-07-spec-3-agents-distill.md) | scope only | spec 1 (pointer format, `sources:` schema) |
| 4 | [the wiring DSL](2026-08-07-spec-4-wiring-dsl.md) | candidate, gated on triggers | spec 1 (adapters) |
| 5 | [the verification gate](2026-08-11-spec-5-verification-gate.md) | designed | spec 1 (exit codes, security boundaries) |
| 6 | [releases and binary distribution](2026-08-11-spec-6-releases-and-distribution.md) | scope only | spec 1, spec 5 |
| 7 | [capture cheaply, review before tracking](2026-08-12-spec-7-capture-and-review.md) | phases A and B′ implemented; §3c deliberately not built | spec 1 (tiers, record schema, adapters, exit codes) |

**Spec 1 is the implemented foundation.** It defines the terminology, placement
rule, pointer format, Go module, and installation boundaries that the remaining
specs assume. Spec 2 is implemented; its three known gaps — chief among them
that no phase of it has ever run on Linux — are recorded in the spec itself, and
[spec 5 §7](2026-08-11-spec-5-verification-gate.md#7-linux) is what closes that
one. Specs 3, 4 and 6 are scope documents, not finished designs — each gets a
full brainstorm → design → plan cycle when started.

**Spec 5 was split on 2026-08-11.** Its 2026-08-10 scope note covered
verification, releases, publishing, and drift under one number; only verification
needed no open decisions, and no single plan could be written against the union.
Spec 5 kept verification and was designed; everything else moved to spec 6
intact. Spec 6 depends on spec 5 rather than the reverse: releases built on a
repository with no automated gate publish unverified binaries on a schedule.

**Spec 7 amends spec 1 and changes spec 3's premise.** It came out of using spec 1
for five days and finding that the tracked trace index is a reference to
machine-bound material filed in the tracked tier: 48% of its records were
unreachable on the machine that wrote them, while the curated store it was meant
to feed held zero handoffs. Spec 7 untracks the trace store, adds an untracked
draft queue, and makes promotion into `.agents/` a single reviewed act.

**Its capture triggers are ordered cheapest-first, and that ordering is the
point.** §3a is an instruction in `CLAUDE.md`; §3b a non-blocking nudge; §3c a
blocking `Stop` gate. §3a and the review path are implemented; **§3c is specified
in full and deliberately not built.**

**None of the three has been measured — §3a shipped unmeasured, as a live
experiment rather than the winner of a comparison.** The spec's own first draft
went straight to the gate, justified by a spec 1 measurement about subagents,
which do not read `CLAUDE.md` at all; withdrawing that leaves the question open
rather than settled. What puts the instruction first is that only the cheap
trigger can be measured cheaply — shipping a sentence *is* the experiment,
whereas building the gate to find out whether the gate was needed is circular. A
two-arm scenario run in [the capture experiment](../analysis/2026-08-12-capture-instruction-experiment.md) decides whether
§3b or §3c is ever justified — scripted, an afternoon, with a control arm.

Read spec 7 before designing spec 3 — `agents distill` is demoted from the primary
path to the fallback, and designing against spec 3's current text would build the
wrong thing. Spec 7 adds store migration to spec 6's scope and a *contingent*
capability requirement to spec 4 that arrives only if §3c is ever built; spec 1
§6's exit-code rule is likewise untouched until then. Its one collision with
spec 5 is the command registry: `agents review` and `agents handoff draft` land
in it.

Spec 2 is independent of the `agents` tool and shares no code with it; it lives
here because its numbering and its one ordering constraint (spec 1 §8) belong to
this catalog.

## Implementation plans

| Spec | Plan | Status |
|---|---|---|
| 1 | [repo-tracked agent context](../../plans/2026-08-07-agents-repo-context.md) | executed |
| 1, 2 | [checkout path and field defects](../../plans/2026-08-11-checkout-path-and-field-defects.md) | executed |
| 1 | [trace cache preservation](../../plans/2026-08-11-trace-cache-preservation.md) | executed |
| 2 | [dotfiles bootstrap](../../plans/2026-08-10-dotfiles-bootstrap.md) | executed |
| 7 | [capture and review, phases A and B′](../../plans/2026-08-12-spec-7-capture-and-review.md) | executed |

Spec 1's plan is phased: the record loop on Claude Code (Phase 1), Codex
(Phase 2), retrieval (Phase 3), memory and handoffs (Phase 4), guards and the
git-hook subsystem (Phase 5), fleet and doctor (Phase 6). Each phase ends
somewhere working.

Spec 2's plan is sixteen tasks: `change.Interface` and the shim first, then the
phases, then the removals, and the Makefile reduced to the `agents` target last.
Bootstrap landed before anything was removed, so provisioning never broke
mid-plan.

Spec 7's plan is six tasks across two phases. Phase A moved the trace index
into `<git-common-dir>/agents/`, bounded the transcript cache, and untracked
the index from this repository; Phase B′ added the capture instruction, the
untracked draft queue, `agents handoff draft`, and `agents review`. §3c's
blocking `Stop` gate is specified in the spec and deliberately not built --
the plan's first Global Constraint says so, because the cheap trigger has to
be measured before the expensive one is justified.

The two 2026-08-11 plans are corrective rather than new surface, and both came
out of running the thing on a real machine. The first stopped `agents` assuming
its checkout is `~/dotfiles` and fixed two defects a first provisioning run
exposed. The second closed four gaps in the transcript cache, the largest being
that it was write-only: nothing could read a cached transcript back, so a
`sources:` citation died the moment the harness cleaned up.

## Supporting material

- [`fixtures/`](fixtures/) — real hook payloads captured from live agent runs,
  used as test fixtures by spec 1. Sanitized; see the fixtures README.
- [the capture experiment](../../analysis/2026-08-12-capture-instruction-experiment.md)
  — the two-arm protocol that decides spec 7 §3a, with
  [`agents/experiment/capture-setup.sh`](../../../../agents/experiment/capture-setup.sh)
  as its harness. Not yet run.
