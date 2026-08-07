# `agents` — design specs

Everything under `docs/superpowers/specs/agents/` belongs to one body of work: the
`agents` tool and the tracked `.agents/` directory it maintains in each repo. Other
projects get sibling folders under `specs/`.

The specs, in dependency order:

| # | Spec | Status | Depends on |
|---|---|---|---|
| 1 | [`agents` — repo-tracked agent context](2026-08-07-agents-repo-context-design.md) | designed, not implemented | — |
| 2 | [dotfiles hygiene](2026-08-07-spec-2-dotfiles-hygiene.md) | scope only | spec 1 §8 must land first |
| 3 | [`agents distill`](2026-08-07-spec-3-agents-distill.md) | scope only | spec 1 (pointer format, `sources:` schema) |
| 4 | [the wiring DSL](2026-08-07-spec-4-wiring-dsl.md) | candidate, gated on triggers | spec 1 (adapters) |

**Start with spec 1.** It defines the terminology, the placement rule, and the
pointer format that specs 2–4 all assume. Specs 2–4 are scope documents, not
finished designs — each gets a full brainstorm → design → plan cycle when started.

## Implementation plans

| Spec | Plan | Status |
|---|---|---|
| 1 | [repo-tracked agent context](../../plans/2026-08-07-agents-repo-context.md) | written, not executed |

Spec 1's plan is phased: the record loop on Claude Code (Phase 1), Codex
(Phase 2), retrieval (Phase 3), memory and handoffs (Phase 4), guards and the
git-hook subsystem (Phase 5), fleet and doctor (Phase 6). Each phase ends
somewhere working.

## Supporting material

- [`fixtures/`](fixtures/) — real hook payloads captured from live agent runs,
  used as test fixtures by spec 1. Sanitized; see the fixtures README.
