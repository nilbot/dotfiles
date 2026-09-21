# Hygiene audit for PR #52 — the work the green gate did not cover

The gate is green. That is not the same as mergeable, and this file is the list of
why. It exists so four parallel workstreams can be assigned without overlapping,
and so the reviewer can check the audit rather than reconstruct it.

## State at the time of writing

Branch `simplify/agents-single-home`, PR #52, 12/12 checks SUCCESS. The change
deleted commands (`agents drift`, `agents layout*`, `agents trace*`, `agents
save`, `agents ls`, `agents update`, `agents hook`), deleted packages
(`internal/{layout,registry,trace,record,machine,pointer,lane,drift}`), removed
two skills, and stopped declaring two manifest rows.

## Measured debt

**D1. `agents/README.md` describes a tool that no longer exists.** It documents
`agents drift`, the layout manifest and its migration, `agents trace` and
transcript caching, and `agents update` for fleet skill refresh. It also still
advertises installation from a release pipeline. Nothing tests it.

**D2. Design docs in `docs/design/` name deleted commands.** Counts by document:
`2026-09-18-layout-manifest-and-store-root-design.md` 47 references,
`2026-08-29-two-tier-context-and-llm-migration-architecture.md` 20,
`2026-08-07-agents-repo-context-design.md` 16,
`2026-08-12-spec-7-capture-and-review.md` 11,
`2026-08-11-spec-5-verification-gate.md` 6,
`2026-08-28-antigravity-multi-harness-onboarding.md` 4,
`2026-09-20-agents-and-bootstrap-boundary.md` 2,
`2026-08-19-knowledge-is-documentation.md` 2,
`2026-08-28-binary-identity-and-standalone-resolution.md` 1.

Not all of these are faults, and saying which is which is the work:

- A **dated record** may name anything true of its date. `docs/journal/` is a
  record, and `docs/plans/` records what was planned. Rewriting either destroys
  evidence. The repo's own `docs/archive/README.md` says the archive is
  immutable.
- The repository's `AGENTS.md` says `docs/design/` is *"the design still in
  force"*. A document under that heading that describes a deleted command is a
  contradiction a reader cannot resolve, and that is a fault.
- So each design document needs one of two outcomes, chosen deliberately and
  recorded: **updated** to describe the tool that exists, or **moved out of the
  design store** because it no longer describes anything in force. A document
  that is mostly about the deleted layout schema is a record, not a design.

**D3. The design store's index is stale.** `docs/design/README.md` still lists
the layout manifest as "implemented 2026-09-19 — v0.6.0 pending its release
gate" and the boundary review as "`bootstrap plan workstation` is blocked".

**D4. The machine-global instruction file does not exist in the form the
decision requires.** Measured: `~/.claude/CLAUDE.md` is a symlink to the tracked
`claude/CLAUDE.md`; `~/.dsh/AGENTS.md` does not exist, and DSH's
`dsh-agent-instructions` reads `$DSH_HOME/AGENTS.md` as the one user-global
instruction file; `~/.codex/AGENTS.md` is a hand-made real file holding a
language rule that is in no dotfiles file and would not survive this machine.
The owner's decision: universal rules in one shared file, harness-specific rules
in that harness's own file, one file per harness at its native path.

**D5. Q&A entries that contradict the code.** `docs/qna/` is a living store —
`AGENTS.md` tells an agent to read it *before asserting* — so an entry whose
answer is now false is a trap. Specifically
`why-is-claude-skills-a-real-directory.md` (answers with the blocker this PR
removes) and `why-does-agents-init-never-update-existing-instructions.md` (names
`agents update` and `drift`).

**D6. Not a fault, recorded so nobody "fixes" them.** Seven relative links
resolve to nothing across all of `docs/`: three are `%s`/`...`/`target`
placeholders inside plan bodies, two are `../plans/a-plan.md` template paths, one
points at an experiment script removed by an earlier spec, and one is inside an
archived analysis. Plan placeholders and immutable archives are not defects.

## Workstreams

Each owns a disjoint file set. No workstream commits; the parent verifies and
commits. Every workstream must leave `go test ./...` green in both modules and
`gofmt -l` empty, and must not touch `agents/internal/`, `agents/cmd_*.go`, or
`bootstrap.d/` except where its brief says so.

- **H1** — `agents/README.md`, root `README.md` (D1).
- **H2** — `docs/design/**` and `docs/design/README.md` (D2, D3).
- **H3** — `docs/qna/**` (D5).
- **H4** — the machine-global instruction file (D4): the in-repo move, the
  manifest rows, and the docs that name the paths.

## The rule all four share

A claim about the current state of the tool must be checkable by a command. If a
sentence could be verified by running something, the workstream runs it and puts
the command in its report. The three reviewers who audited the code found that
every one of their findings came from a claim nobody had run; there is no reason
to expect prose to fail differently.
