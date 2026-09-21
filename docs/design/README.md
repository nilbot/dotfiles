# Design documents

Documentation outlives the tools that produce it, so this tree is named for what
it holds rather than for the skill set used to write it.

| directory | holds |
|---|---|
| `docs/design/` | the design still in force — read these to understand the system |
| `docs/plans/` | step-by-step implementation plans |
| `docs/journal/` | dated record of what happened and retrospective execution notes |
| `docs/qna/` | answers indexed by the question you would ask on hitting it again |
| `docs/archive/` | executed plans, retired specs, past measurements pre-2026-08-20 — how it got here |

Nothing in `docs/archive/` is rewritten to stay true. A record edited to match
today is not a record, and the reasoning behind a retired decision is why nobody
rebuilds it.

## Specs

Numbers are stable catalog identifiers, not priority or order.

| # | Spec | Status |
|---|---|---|
| 1 | [repo-tracked agent context](2026-08-07-agents-repo-context-design.md) | implemented; its memory and `sources:` sections are superseded by the redesign below. **§8.5 added 2026-09-20** — a `brew upgrade` deletes the keg a pinned hook link names, git runs a dangling hook as if no hook existed, so the installer accepts a keg-resolving symlink and gained `--adopt-owned` |
| 2 | [dotfiles hygiene](2026-08-07-spec-2-dotfiles-hygiene.md) | implemented |
| 3 | [`agents distill`](../archive/specs/2026-08-07-spec-3-agents-distill.md) | **retired** — archived, never implemented |
| 4 | [the wiring DSL](2026-08-07-spec-4-wiring-dsl.md) | **designed 2026-08-22** — triggers fired; not implemented |
| 5 | [the verification gate](2026-08-11-spec-5-verification-gate.md) | implemented and merged; **test matrix amended 2026-09-20** — `ubuntu-24.04-arm` runs `agents` (a shipped target no leg had run on), `bootstrap.d` keeps x86_64 Linux only, and a new `macos-dotfiles` job runs the real `./bootstrap plan` on macOS in 24s |
| 6 | [releases and distribution](2026-08-11-spec-6-releases-and-distribution.md) | implemented; **§5.1 amended 2026-09-20** — the release requires a green `master` run for the tagged commit instead of re-running the verification matrix, and four targets rather than three. Read Amendment 1 before touching `release.yml`: the guard is one API call and four of its details fail it silently. |
| 7 | [capture cheaply, review before tracking](2026-08-12-spec-7-capture-and-review.md) | §1–2 in force; the capture half is retired |
| — | [knowledge is documentation](2026-08-19-knowledge-is-documentation.md) | **executed 2026-08-20** — the retired code and stores are deleted |
| — | [antigravity multi-harness onboarding](2026-08-28-antigravity-multi-harness-onboarding.md) | **implemented 2026-08-28** — adapter, dialect, dual-layer instruction topology |
| — | [contributor guardrails and scaffold decoupling](2026-08-28-contributor-guardrails-and-scaffold-decoupling.md) | **implemented 2026-08-28** — conditional doctor, standalone support |
| — | [two-tier context and llm migration architecture](2026-08-29-two-tier-context-and-llm-migration-architecture.md) | **implemented 2026-08-31; §7 amended 2026-09-01; store paths amended 2026-09-19** — two-tier context, 4-store layout, bundled skills, LLM migration. Read Amendment 1 before touching the migration skill: the original §7 contradicted §7.2 on archive immutability and left "3-way merge" undefined. Amendment 2 makes the four `docs/` paths the v1 default and `.agents/layout.json` the v2 authority. |
| — | [layout manifest and store-root freedom](2026-09-18-layout-manifest-and-store-root-design.md) | **implemented 2026-09-19 — v0.6.0 pending its release gate** — single-release `agents.layout/v2`, role→path stores, deploy-before-flip gate, `.context/` pilot for content vaults. Gates 1–2 are closed and the implementation plan's tasks are committed; gates 3–4 of §12 (the v0.6.0 release and the paperbubble `--apply`) are still open, so this row names no released version. |
| — | [`agents` and `bootstrap`: the boundary](2026-09-20-agents-and-bootstrap-boundary.md) | **review recorded 2026-09-20, no code changed** — bootstrap reconciles the machine, `agents` reconciles a repository. Read §4 before adding a command that writes machine state: two owners already disagree about the same `$HOME` paths, and one of them is an incident. §5 declines `agents install-hooks`; §7 is the deferred list, headed by the `~/.claude/skills` declaration that currently blocks `bootstrap plan workstation`. |


**Spec 1 is the foundation** — terminology, the placement rule, the pointer
format, the Go module and installation boundaries. What the 2026-08-19 redesign
takes out of it is `.agents/memory/` and the curated-knowledge tier; the tiers
themselves, the trace record schema and the exit-code vocabulary stand.

**Spec 5 is merged.** Branch protection names the `gate` job and nothing else, so
adding a job is a workflow change rather than a settings change.

**Spec 7 is half in force.** §1 and §2 — untracking the trace index, one
machine-local store under the git common directory, retention caps — are live and
were the right call. §3 and §4, the capture instruction and the review queue, are
what the redesign retires; read it before building anything against them.

**Spec 2 is independent of the `agents` tool** and shares no code with it. It is
catalogued here because of one ordering constraint (spec 1 §8).

## Supporting material

- [`fixtures/`](fixtures/) — real hook payloads captured from live agent runs,
  read directly by `agents/internal/harness` tests. Sanitized.
- [the capture experiment](../archive/analysis/2026-08-12-capture-instruction-experiment.md)
  — the two-arm protocol behind spec 7 §3a. Its result, that an instruction alone
  causes drafting, is load-bearing for the redesign.
- Implementation plans are in [`../plans/`](../plans/).
- Pre-2026-08-20 historical plans are in [`../archive/plans/`](../archive/plans/).
