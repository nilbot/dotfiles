# Design documents

Documentation outlives the tools that produce it, so this tree is named for what
it holds rather than for the skill set used to write it.

| directory | holds |
|---|---|
| `docs/design/` | the design still in force — read these to understand the system |
| `docs/plans/` | step-by-step implementation plans |
| `docs/journal/` | dated record of what happened and retrospective execution notes |
| `docs/qna/` | answers indexed by the question you would ask on hitting it again |
| `docs/archive/` | executed plans, retired specs, retired designs, past measurements — how it got here |

Nothing in `docs/archive/` is rewritten to stay true. A record edited to match
today is not a record, and the reasoning behind a retired decision is why nobody
rebuilds it.

## Specs

Numbers are stable catalog identifiers, not priority or order.

**The tool described here was reduced on 2026-09-21.** It has six commands —
`init`, `wire`, `doctor`, `guard`, `version`, `help` — and no layout manifest, no
migration, no trace record, no session record and no fleet registry. Rows below
say which spec sections that deleted and which still stand. Two documents left
this store for `../archive/design/` because their whole subject was deleted; a
document stays here only while it describes something in force.

| # | Spec | Status |
|---|---|---|
| 1 | [repo-tracked agent context](2026-08-07-agents-repo-context-design.md) | implemented; its memory and `sources:` sections are superseded by the redesign below. **§8.5 added 2026-09-20** — a `brew upgrade` deletes the keg a pinned hook link names, git runs a dangling hook as if no hook existed, so the installer accepts a keg-resolving symlink and gained `--adopt-owned`. **§3, §4 and §10 deleted 2026-09-21** — the trace record and its JSONL schema, lane-scoped handoffs, and the fleet registry all went with the reduction, along with the commands in §1's operations table and §6's binary surface. §1's placement rule and its three tiers, §5's harness adapters, §6's exit-code vocabulary, §7's guard layering, §8's Go-backed git hooks and §9's bootstrap boundary stand. |
| 2 | [dotfiles hygiene](2026-08-07-spec-2-dotfiles-hygiene.md) | implemented |
| 3 | [`agents distill`](../archive/specs/2026-08-07-spec-3-agents-distill.md) | **retired** — archived, never implemented |
| 4 | [the wiring DSL](2026-08-07-spec-4-wiring-dsl.md) | **designed 2026-08-22** — triggers fired; not implemented |
| 5 | [the verification gate](2026-08-11-spec-5-verification-gate.md) | implemented and merged; **test matrix amended 2026-09-20** — `ubuntu-24.04-arm` runs `agents` (a shipped target no leg had run on), `bootstrap.d` keeps x86_64 Linux only, and a new `macos-dotfiles` job runs the real `./bootstrap plan` on macOS in 24s. **2026-09-21**: its historical baseline cites three artifacts the reduction deleted; the measurements are kept and marked at the point of use, because recomputing a baseline destroys it |
| 6 | [releases and distribution](2026-08-11-spec-6-releases-and-distribution.md) | implemented; **§5.1 amended 2026-09-20** — the release requires a green `master` run for the tagged commit instead of re-running the verification matrix, and four targets rather than three. Read Amendment 1 before touching `release.yml`: the guard is one API call and four of its details fail it silently. **§5.2 amended 2026-09-21** — this repository's copy of the Homebrew formula is deleted, and the tap is updated by editing the formula it already has rather than by pushing a rendered duplicate over it. Read §5.2 before touching the tap: `checksums.txt` lists the digests in a different order than the formula's four slots, and a paste in line order fails only in a user's `brew install`. |
| 7 | [capture cheaply, review before tracking](../archive/design/2026-08-12-spec-7-capture-and-review.md) | **archived 2026-09-21** — the capture instruction and the review queue were retired 2026-08-20, and the 2026-09-21 reduction deleted the machine-local trace store that §1–2 kept in force. Nothing in it describes a surviving mechanism, so it is a record now, not a design in force |
| — | [knowledge is documentation](2026-08-19-knowledge-is-documentation.md) | **executed 2026-08-20** — the retired code and stores are deleted; **amended 2026-09-21** — the trace cache and `subagent-stop` hook this document kept on their own merit were then deleted too. That is a reversal of its judgement and the body records it as one, because the argument is worth more than the outcome |
| — | [antigravity multi-harness onboarding](2026-08-28-antigravity-multi-harness-onboarding.md) | **implemented 2026-08-28** — adapter, dialect, dual-layer instruction topology. §Phase 5 is the 2026-08-28 rollout record and names two commands the reduction deleted; it is marked as a record, and its `init`-not-`update` conclusion still stands |
| — | [contributor guardrails and scaffold decoupling](2026-08-28-contributor-guardrails-and-scaffold-decoupling.md) | **implemented 2026-08-28** — conditional doctor, standalone support |
| — | [binary identity and standalone resolution](2026-08-28-binary-identity-and-standalone-resolution.md) | **implemented 2026-08-28** — the explicit `DotfilesRoot()` contract, in force. **Amended 2026-09-21** — §3.2's `agents hook` entry point is deleted (dispatch is now keyed on the invoked hook name) and §3.1's doctor check list was rewritten to the checks that exist |
| — | [two-tier context and llm migration architecture](2026-08-29-two-tier-context-and-llm-migration-architecture.md) | **implemented 2026-08-31; §7 amended 2026-09-01; store paths amended 2026-09-19; §4, §5, §7 and §10's phases 2–4 deleted 2026-09-21** — two-tier context, the router and symlink topology, asset embedding and the `scaffold:*` checks are the design in force and the reason this document stays. Amendment 3 records what the reduction deleted: the drift subsystem and its digest catalog, `agents update`, and the `migrating-fleet-context` migration skill. Amendments 1 and 2 are retained as history — Amendment 1's corrections describe a skill that no longer ships, and Amendment 2's `.agents/layout.json` authority was itself deleted. |
| — | [layout manifest and store-root freedom](../archive/design/2026-09-18-layout-manifest-and-store-root-design.md) | **archived 2026-09-21** — the whole subject was deleted: no `agents.layout/v2` manifest, no role→path `stores` map, no version floor or deploy-before-flip gate, no migration. The tool writes one fixed set of stores under `docs/`. Kept because it is the record of a reviewed, approved decision that was then reversed, which is the reasoning nobody should have to reconstruct. |
| — | [`agents` and `bootstrap`: the boundary](2026-09-20-agents-and-bootstrap-boundary.md) | **review recorded 2026-09-20** — bootstrap reconciles the machine, `agents` reconciles a repository. Read §4 before adding a command that writes machine state: two owners already disagree about the same `$HOME` paths, and one of them is an incident. §5 declines `agents install-hooks`. §7's first item — the `~/.claude/skills` declaration that blocked `bootstrap plan workstation` — was **closed 2026-09-21** by deleting the row from `links.manifest`, which is what §6's rule 2 asks for. |


**Spec 1 is the foundation** — terminology, the placement rule, the Go module and
installation boundaries. What the 2026-08-19 redesign takes out of it is
`.agents/memory/` and the curated-knowledge tier; what 2026-09-21 takes out is the
record itself, §3.3–3.4. The tiers, §5's adapters and the exit-code vocabulary
stand.

**Spec 5 is merged.** Branch protection names the `gate` job and nothing else, so
adding a job is a workflow change rather than a settings change.

**Spec 7 is archived**, not half in force. It was the second document to leave
this store, and the reason is the same as the first's: the mechanism it specified
was deleted, so keeping it here would have described a tool that does not exist.
Its §3a measurement still stands and is cited by the archived experiment.

**Spec 2 is independent of the `agents` tool** and shares no code with it. It is
catalogued here because of one ordering constraint (spec 1 §8).

## Supporting material

- [`fixtures/`](fixtures/) — real hook payloads captured from live agent runs,
  read directly by `agents/internal/harness` tests. Sanitized.
- [the capture experiment](../archive/analysis/2026-08-12-capture-instruction-experiment.md)
  — the two-arm protocol behind spec 7 §3a. Its result, that an instruction alone
  causes drafting, is what the 2026-08-19 redesign was built on and what the
  2026-09-21 reduction deleted the consumer of. The measurement stands; only its
  reader is gone.
- Implementation plans are in [`../plans/`](../plans/).
- Pre-2026-08-20 historical plans are in [`../archive/plans/`](../archive/plans/).
- Designs whose subject was deleted are in [`../archive/design/`](../archive/design/).
