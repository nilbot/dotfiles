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

A document that is still in force is kept true to the implementation. A live spec does
not get a mending document of its own: that doubles the store and leaves the wrong
sentence in front of the reader. The correction takes one of three forms, and one change
can call for two of them.

- **The document was wrong** — a count, a path that never existed, a rule attributed to
  the wrong spec. Correct it where it stands. An index entry does not stop a reader
  acting on a wrong instruction.
- **The document's meaning changed** — a decision was revisited. Record it as a dated
  amendment at the point of use, keeping the reasoning it replaced. Most of this store
  uses this form, and a document short enough to hold in mind needs no other.
- **The world moved under a correct claim** — a premise the design relied on stopped
  holding. The home for these is an **appendix** at the end of the document, pointed at
  from its head: it lists, in one screen, the parts that have moved, which a reader of a
  long document cannot hold in mind. Whether a given document earns one is a judgement
  rather than an obligation — this store has one appendix, and other long documents
  without one.

**Where two forms apply, the in-place text is the correction and the appendix entry is
the index to it.** A claim that is wrong and whose premise also moved is corrected where
it stands and listed in the appendix. Mark a section in place whenever leaving it
unmarked would mislead a reader acting on what it says — and that includes a list or a
table that has become incomplete, not only an instruction. Each appendix row says
whether its landing carries a marker, so the table answers that without the reader
opening the section.

An amendment goes with its subject when the section it annotates is deleted too — or
rewritten until the text it annotated is gone: then delete both and let git history hold
them, as the two-tier document did. An amendment whose decision was reversed stays where
it is, even when its section has gone, because the reversal is the record — the
decoupling note in `contributor-guardrails` annotates an argument that is still live. An
amendment naming no section annotates the document as a whole, so it goes only when the
document goes. One naming several is several annotations sharing a date: trim the part
whose section has gone, and delete the amendment when the last has gone.

## Specs

Numbers are stable catalog identifiers, not priority or order.

**The `agents` tool was reduced on 2026-09-21.** It has seven commands —
`init`, `wire`, `doctor`, `guard`, `githook`, `version`, `help` — and no layout
manifest, no migration, no trace record, no session record and no fleet registry.
Rows below say which sections that reduction deleted and which still stand.

**Where the specs stand.** 2, 5 and 6 are implemented and in force. 1, 3, 4 and 7
are deleted: none described something a reader cannot recover from git history,
and the rule above applies to a whole document the same way it applies to a
section.

A row whose spec name is **not a link** has no document in this store: it was
deleted, and the row stays so that the citations to its number elsewhere in
`docs/` resolve to something that says what happened to it.

| # | Spec | Status |
|---|---|---|
| 2 | [dotfiles hygiene](2026-08-07-spec-2-dotfiles-hygiene.md) | implemented |
| 3 | `agents distill` | **retired** — deleted 2026-10-12, never implemented |
| 4 | the wiring DSL | **deleted 2026-10-12** — designed 2026-08-22, never implemented, and its purpose went with the mechanism it configured: `agents wire` removes the hook entries an earlier version wrote and adds none, because nothing records harness lifecycle events any more |
| 5 | [the verification gate](2026-08-11-spec-5-verification-gate.md) | implemented and merged; **test matrix amended 2026-09-20** — `ubuntu-24.04-arm` runs `agents` (a shipped target no leg had run on), `bootstrap.d` keeps x86_64 Linux only, and a new `macos-dotfiles` job runs the real `./bootstrap plan` on macOS in 24s. **2026-09-21**: its historical baseline cites three artifacts the reduction deleted; the measurements are kept and marked at the point of use, because recomputing a baseline destroys it |
| 6 | [releases and distribution](2026-08-11-spec-6-releases-and-distribution.md) | implemented; **§5.1 amended 2026-09-20** — the release requires a green `master` run for the tagged commit instead of re-running the verification matrix, and four targets rather than three. Read Amendment 1 before touching `release.yml`: the guard is one API call and four of its details fail it silently. **§5.2 amended 2026-09-21** — this repository's copy of the Homebrew formula is deleted, and the tap is updated by editing the formula it already has rather than by pushing a rendered duplicate over it. Read §5.2 before touching the tap: `checksums.txt` lists the digests in a different order than the formula's four slots, and a paste in line order fails only in a user's `brew install`. |
| 7 | capture cheaply, review before tracking | **retired 2026-09-21, deleted 2026-10-12** — the capture instruction and the review queue were retired 2026-08-20, and the 2026-09-21 reduction deleted the machine-local trace store that §1–2 kept in force. Nothing in it describes a surviving mechanism, so it is a record now, not a design in force |
| — | [antigravity multi-harness onboarding](2026-08-28-antigravity-multi-harness-onboarding.md) | **implemented 2026-08-28** — adapter, dialect, dual-layer instruction topology. §Phase 5 is the 2026-08-28 rollout record and names two commands the reduction deleted; it is marked as a record, and its `init`-not-`update` conclusion still stands |
| — | [contributor guardrails and scaffold decoupling](2026-08-28-contributor-guardrails-and-scaffold-decoupling.md) | **implemented 2026-08-28** — conditional doctor, standalone support |
| — | [two-tier context and llm migration architecture](2026-08-29-two-tier-context-and-llm-migration-architecture.md) | **implemented 2026-08-31; the drift subsystem, `agents update` and the fleet-migration skill were deleted 2026-09-21, and the sections and amendments that specified them went with them.** The two-tier context, the router and symlink topology, asset embedding and the `scaffold:*` checks are the design in force. |
| — | layout manifest and store-root freedom | **retired 2026-09-21, deleted 2026-10-12** — the whole subject was deleted: no `agents.layout/v2` manifest, no role→path `stores` map, no version floor or deploy-before-flip gate, no migration. The tool writes one fixed set of stores under `docs/`. The reasoning a reader cannot recover from the code is in git history with it. |
| — | [`agents` and `bootstrap`: the boundary](2026-09-20-agents-and-bootstrap-boundary.md) | **review recorded 2026-09-20** — bootstrap reconciles the machine, `agents` reconciles a repository. Read §4 before adding a command that writes machine state: two owners already disagree about the same `$HOME` paths, and one of them is an incident. §5 declines `agents install-hooks`. §7's first item — the `~/.claude/skills` declaration that blocked `bootstrap plan workstation` — was **closed 2026-09-21** by deleting the row from `links.manifest`, which is what §6's rule 2 asks for. |
| — | [what a query is](2026-10-09-what-a-query-is.md) | **landed 2026-10-09** — `plan` and `check` promise to change nothing, and `check workstation` upgraded Homebrew on 2026-10-08: the call is `brew bundle check`, whose subcommand reads and reports and whose `brew` wrapper auto-updates before dispatching. Proposes removing the `packages` check rather than repairing it, since with the command gone it would report "every Brewfile entry is installed" having asked nothing, and dropping `Run` from the check machine interface so no future check can execute anything. CI does not change: the stage-zero job's liveness assertion cannot be replaced by a state check, and its exit code stays reported. Names the one declared read-subprocess, the fifteen enforcement sites the removal makes wrong, and the hazard the broken check was hiding. |
| — | [one record for binary identity](2026-10-07-one-record-for-binary-identity.md) | **landed 2026-10-09, released in v0.8.0** — there is one build of `agents`, the released one; the checkout build goes. The machine record, `chain.env`, replaces the compile-time stamp and `AGENTS_DOTFILES_ROOT` as the statement of which checkout supplies the personal stages: the chain states which binary git execs, its entries verify that before exec and fail closed for the guard, `doctor` reads the machine instead of holding a root, and `chain:checkout` verifies the record against the filesystem. §2's F2 and F4 are why the stamp and the variable cannot stay; §6's two stages are the order, with stage 1 changing only the reader. §3.1's entry script is the machine's commit path. |

**Spec 1 is the foundation** — terminology, the placement rule, the Go module and
installation boundaries. **The document is deleted from the tree (2026-10-12)**;
what follows is what it established that still stands, stated here so the
citations to it elsewhere resolve. What the 2026-08-19 redesign takes out of it is
`.agents/memory/` and the curated-knowledge tier; what 2026-09-21 takes out is the
record itself, §3.3–3.4. The tiers, §5's adapters and the exit-code vocabulary
stand.

**Spec 5 is merged.** Branch protection names the `gate` job and nothing else, so
adding a job is a workflow change rather than a settings change.

**Spec 7 is deleted**, not half in force. It left this store for the same reason its mechanism did: the mechanism it specified
was deleted, so keeping it here would have described a tool that does not exist.
Its §3a measurement, that an instruction alone causes drafting, was the ground the redesign stood on; the redesign and its consumer are both gone.

**Spec 2 is independent of the `agents` tool** and shares no code with it. It is
catalogued here because of one ordering constraint (spec 1 §8).

## Supporting material

- [`fixtures/`](fixtures/) — real hook payloads captured from live agent runs.
  Sanitized. The `agents/internal/harness` tests that once read them were deleted
  on 2026-09-21 (`f3b4a74`), and
  [Does `PreToolUse` fire inside a Claude Code subagent?](../qna/does-pretooluse-fire-inside-a-subagent.md)
  still cites two of the payloads as the evidence for its answer. The other sixteen
  payload files under `fixtures/` have no reader today.
- Implementation plans are in [`../plans/`](../plans/).
- Nothing is archived: a document whose subject is gone is deleted, and git
history holds it. `git log --diff-filter=D -- docs/` lists everything that went.
