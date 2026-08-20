# What shapes does repository knowledge actually take?

## Context

`autogo-mlx` is the only repository here that grew a knowledge system without a
tool imposing one — a 60-line `AGENTS.md`, no queue, no schema. Measured
2026-08-20, it had evolved **five** distinct shapes:

| shape | example | indexed by |
|---|---|---|
| topic answers | `docs/qna/` — 9 files, 418-701 words | the question |
| dated work record | `docs/sessions/20260527…` — 6 directories | time |
| findings reports | `docs/rl_findings/`, `rl_evolution_report.md` | experiment |
| cumulative synthesis | `docs/lessons_learned.md` — 68 lines, attempts 1-7 | narrative |
| proposals and overview | `sibling_ensembling_proposal.md`, `system_overview.md` | subject |

The redesign that replaced this repository's capture apparatus specifies two
stores plus `design/`. It covers the first, second and fifth. It has no home for
the third or fourth.

## Answer

**The gap that matters is the fourth: cumulative synthesis.**
`lessons_learned.md` is one file, chronological, with findings nested under the
attempt that produced them. It is not a table of contents over the Q&A entries —
it is a re-reading of them that says which mattered and how they connect.

That distinction is the point. This repository generated `INDEX.md` files
mechanically and is now deleting them, on the grounds that `ls` is the index at
this scale. That reasoning is right for a **table of contents** and says nothing
about **synthesis**, which no generator can produce and which the one working
example here found worth hand-writing.

So: do not read "no generated index" as "no synthesis layer". If a Q&A directory
grows past the point where reading all of it is cheap, the answer is a
hand-written cumulative document, not a regenerated list of titles.

**Do not flatten a repository's existing taxonomy to match ours.** The global
instruction defaults to `docs/qna/` and `docs/journal/` and defers to whatever a
repository's own `CLAUDE.md` names, deliberately. `autogo-mlx` keeps `sessions/`
rather than being renamed to `journal/`; churning a working system for
cross-repository consistency would be spending its practice to buy our tidiness.

Related: `docs/design/2026-08-19-knowledge-is-documentation.md`, which specifies
the two stores and does not claim they are exhaustive.
