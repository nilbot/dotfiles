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
| 2 | [dotfiles hygiene](2026-08-07-spec-2-dotfiles-hygiene.md) | designed | spec 1 §8 (landed) |
| 3 | [`agents distill`](2026-08-07-spec-3-agents-distill.md) | scope only | spec 1 (pointer format, `sources:` schema) |
| 4 | [the wiring DSL](2026-08-07-spec-4-wiring-dsl.md) | candidate, gated on triggers | spec 1 (adapters) |
| 5 | [CI, releases, and binary distribution](2026-08-10-spec-5-ci-release-distribution.md) | scope only | spec 1 (module and installation boundaries) |

**Spec 1 is the implemented foundation.** It defines the terminology, placement
rule, pointer format, Go module, and installation boundaries that the remaining
specs assume. Spec 2 is designed and awaiting an implementation plan. Specs 3–5
are scope documents, not finished designs — each gets a full brainstorm → design
→ plan cycle when started.

Spec 2 is independent of the `agents` tool and shares no code with it; it lives
here because its numbering and its one ordering constraint (spec 1 §8) belong to
this catalog.

## Implementation plans

| Spec | Plan | Status |
|---|---|---|
| 1 | [repo-tracked agent context](../../plans/2026-08-07-agents-repo-context.md) | executed |

Spec 1's plan is phased: the record loop on Claude Code (Phase 1), Codex
(Phase 2), retrieval (Phase 3), memory and handoffs (Phase 4), guards and the
git-hook subsystem (Phase 5), fleet and doctor (Phase 6). Each phase ends
somewhere working.

## Supporting material

- [`fixtures/`](fixtures/) — real hook payloads captured from live agent runs,
  used as test fixtures by spec 1. Sanitized; see the fixtures README.
