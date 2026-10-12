# Design documents

Documentation outlives the tools that produce it, so this tree is named for what
it holds rather than for the skill set used to write it.

| directory | holds |
|---|---|
| `docs/design/` | the design still in force — read these to understand the system |
| `docs/plans/` | step-by-step implementation plans |
| `docs/journal/` | dated record of what happened and retrospective execution notes |
| `docs/qna/` | answers indexed by the question you would ask on hitting it again |

A document whose subject is gone is deleted rather than kept as a record. This
store is read by agents that act on what they find, so a retired design is an
instruction with a warning label; git history holds every one of them, and
`git log --diff-filter=D -- docs/` lists what went.

## Specs

Numbers are stable catalog identifiers, not priority or order.

**The tool described here was reduced on 2026-09-21.** It has seven commands —
`init`, `wire`, `doctor`, `guard`, `githook`, `version`, `help` — and no layout
manifest, no migration, no trace record, no session record and no fleet registry.
Rows below say which spec sections that deleted and which still stand.

**A document stays here only while it describes something in force.** When its
whole subject is gone the document is deleted and git history is its archive:
this store is read by agents that act on what they find, and a retired design is
an instruction with a warning label rather than an inert record. Specs 1 and 4
are deleted on those grounds; specs 3 and 7 are in `../archive/`, which is where
the reasoning for a reversal nobody should rebuild is still worth keeping.

| # | Spec | Status |
|---|---|---|
| 2 | [dotfiles hygiene](2026-08-07-spec-2-dotfiles-hygiene.md) | implemented |
| 3 | `agents distill` | **retired** — deleted 2026-10-12, never implemented |
| 4 | the wiring DSL | **deleted 2026-10-12** — designed 2026-08-22, never implemented, and its purpose went with the mechanism it configured: `agents wire` removes the hook entries an earlier version wrote and adds none, because nothing records harness lifecycle events any more |
| 5 | [the verification gate](2026-08-11-spec-5-verification-gate.md) | implemented and merged; **test matrix amended 2026-09-20** — `ubuntu-24.04-arm` runs `agents` (a shipped target no leg had run on), `bootstrap.d` keeps x86_64 Linux only, and a new `macos-dotfiles` job runs the real `./bootstrap plan` on macOS in 24s. **2026-09-21**: its historical baseline cites three artifacts the reduction deleted; the measurements are kept and marked at the point of use, because recomputing a baseline destroys it |
| 6 | [releases and distribution](2026-08-11-spec-6-releases-and-distribution.md) | implemented; **§5.1 amended 2026-09-20** — the release requires a green `master` run for the tagged commit instead of re-running the verification matrix, and four targets rather than three. Read Amendment 1 before touching `release.yml`: the guard is one API call and four of its details fail it silently. **§5.2 amended 2026-09-21** — this repository's copy of the Homebrew formula is deleted, and the tap is updated by editing the formula it already has rather than by pushing a rendered duplicate over it. Read §5.2 before touching the tap: `checksums.txt` lists the digests in a different order than the formula's four slots, and a paste in line order fails only in a user's `brew install`. |
| 7 | capture cheaply, review before tracking | **archived 2026-09-21** — the capture instruction and the review queue were retired 2026-08-20, and the 2026-09-21 reduction deleted the machine-local trace store that §1–2 kept in force. Nothing in it describes a surviving mechanism, so it is a record now, not a design in force |
| — | [antigravity multi-harness onboarding](2026-08-28-antigravity-multi-harness-onboarding.md) | **implemented 2026-08-28** — adapter, dialect, dual-layer instruction topology. §Phase 5 is the 2026-08-28 rollout record and names two commands the reduction deleted; it is marked as a record, and its `init`-not-`update` conclusion still stands |
| — | [contributor guardrails and scaffold decoupling](2026-08-28-contributor-guardrails-and-scaffold-decoupling.md) | **implemented 2026-08-28** — conditional doctor, standalone support |
| — | [two-tier context and llm migration architecture](2026-08-29-two-tier-context-and-llm-migration-architecture.md) | **implemented 2026-08-31; §7 amended 2026-09-01; store paths amended 2026-09-19; the drift subsystem, `agents update` and the fleet-migration skill deleted 2026-09-21, with the sections that specified them** — two-tier context, the router and symlink topology, asset embedding and the `scaffold:*` checks are the design in force and the reason this document stays. Amendment 3 records what the reduction deleted: the drift subsystem and its digest catalog, `agents update`, and the `migrating-fleet-context` migration skill. Amendments 1 and 2 are retained as history — Amendment 1's corrections describe a skill that no longer ships, and Amendment 2's `.agents/layout.json` authority was itself deleted. |
| — | layout manifest and store-root freedom | **archived 2026-09-21** — the whole subject was deleted: no `agents.layout/v2` manifest, no role→path `stores` map, no version floor or deploy-before-flip gate, no migration. The tool writes one fixed set of stores under `docs/`. Kept because it is the record of a reviewed, approved decision that was then reversed, which is the reasoning nobody should have to reconstruct. |
| — | [`agents` and `bootstrap`: the boundary](2026-09-20-agents-and-bootstrap-boundary.md) | **review recorded 2026-09-20** — bootstrap reconciles the machine, `agents` reconciles a repository. Read §4 before adding a command that writes machine state: two owners already disagree about the same `$HOME` paths, and one of them is an incident. §5 declines `agents install-hooks`. §7's first item — the `~/.claude/skills` declaration that blocked `bootstrap plan workstation` — was **closed 2026-09-21** by deleting the row from `links.manifest`, which is what §6's rule 2 asks for. |
| — | [what a query is](2026-10-09-what-a-query-is.md) | **proposed 2026-10-09, reviewed** — `plan` and `check` promise to change nothing, and `check workstation` upgraded Homebrew on 2026-10-08: the call is `brew bundle check`, whose subcommand reads and reports and whose `brew` wrapper auto-updates before dispatching. Proposes removing the `packages` check rather than repairing it, since with the command gone it would report "every Brewfile entry is installed" having asked nothing, and dropping `Run` from the check machine interface so no future check can execute anything. CI does not change: the stage-zero job's liveness assertion cannot be replaced by a state check, and its exit code stays reported. Names the one declared read-subprocess, the fifteen enforcement sites the removal makes wrong, and the hazard the broken check was hiding. |

| — | [one record for binary identity](2026-10-07-one-record-for-binary-identity.md) | **proposed 2026-10-07, premise rewritten 2026-10-09** — there is one build of `agents`, the released one; the checkout build goes. The machine record, `chain.env`, replaces the compile-time stamp and `AGENTS_DOTFILES_ROOT` as the statement of which checkout supplies the personal stages: the chain states which binary git execs, its entries verify that before exec and fail closed for the guard, `doctor` reads the machine instead of holding a root, and `chain:checkout` verifies the record against the filesystem. §2's F2 and F4 are why the stamp and the variable cannot stay; §6's two stages are the order, with stage 1 changing only the reader. §3.1's entry script is the machine's commit path. |

**Spec 1 is the foundation** — terminology, the placement rule, the Go module and
installation boundaries. **The document is deleted from the tree (2026-10-12)**;
what follows is what it established that still stands, stated here so the
citations to it elsewhere resolve. What the 2026-08-19 redesign takes out of it is
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
- the capture experiment
  — the two-arm protocol behind spec 7 §3a. Its result, that an instruction alone
  causes drafting, is what the 2026-08-19 redesign was built on and what the
  2026-09-21 reduction deleted the consumer of. The measurement stands; only its
  reader is gone.
- Implementation plans are in [`../plans/`](../plans/).
- Pre-2026-08-20 historical plans are in `../archive/plans/`.
- Nothing is archived: a document whose subject is gone is deleted, and git
history holds it. `git log --diff-filter=D -- docs/` lists everything that went.
