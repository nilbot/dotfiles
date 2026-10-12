# Hygiene audit for PR #52 — the work the green gate did not cover

The gate is green. That is not the same as mergeable, and this file is the list of
why. It exists so four parallel workstreams can be assigned without overlapping,
and so the reviewer can check the audit rather than reconstruct it.

**Status:** executed 2026-10-12 — D1 to D5 are closed, and D6 turned out not to be a
fault. Each debt carries a note saying what closed it; the measurements stay as
written, because they are what the audit found.

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

*Closed: `agents/README.md` names none of the deleted commands now, and
`agents/docs_test.go` guards it as a living document.*

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

*(Five of those nine were deleted on 2026-10-12, as records whose subject was gone:
`layout-manifest-and-store-root-design`, `agents-repo-context-design`,
`spec-7-capture-and-review`, `knowledge-is-documentation` and
`binary-identity-and-standalone-resolution`. The four that remain are guarded by
`TestLivingDocumentsNameNoDeletedCommand`: a deleted command named in `docs/design/`
has to say it is gone — on the line itself, or anywhere in a document carrying a
`## Follow-up, ` section, which exempts the whole file.)*

Not all of these are faults, and saying which is which is the work:

- A **dated record** may name anything true of its date. `docs/journal/` is a
  record, and `docs/plans/` records what was planned. Rewriting either destroys
  evidence. The repo's own `docs/archive/README.md` said the same of the archive;
  both were deleted on 2026-10-12, and `git show` recovers the README.
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

*Closed: the index records the layout manifest as retired and deleted, and the
boundary review's blocker as closed.*

**D4. The machine-global instruction file does not exist in the form the
decision requires.** Measured: `~/.claude/CLAUDE.md` is a symlink to the tracked
`claude/CLAUDE.md`; `~/.dsh/AGENTS.md` does not exist, and DSH's
`dsh-agent-instructions` reads `$DSH_HOME/AGENTS.md` as the one user-global
instruction file; `~/.codex/AGENTS.md` is a hand-made real file holding a
language rule that is in no dotfiles file and would not survive this machine.
The owner's decision: universal rules in one shared file, harness-specific rules
in that harness's own file, one file per harness at its native path.

*Closed: `global/AGENTS.md` is tracked, and `bootstrap.d/links.manifest:22-24` declares
the three harness paths at it.*

**D5. Q&A entries that contradict the code.** `docs/qna/` is a living store —
`AGENTS.md` tells an agent to read it *before asserting* — so an entry whose
answer is now false is a trap. Specifically
`why-is-claude-skills-a-real-directory.md` (answers with the blocker this PR
removes) and `why-does-agents-init-never-update-existing-instructions.md` (names
`agents update` and `drift`).

*Closed: both carry a dated follow-up that retires the answer's mechanism.*

**D6. Not a fault, recorded so nobody "fixes" them.** Seven relative links resolved
to nothing across all of `docs/` when this was written: three are `%s`/`...`/`target`
placeholders inside plan bodies, two are `../plans/a-plan.md` template paths, one
pointed at an experiment script removed by an earlier spec, and one was inside an
archived analysis. The last of those went with `docs/archive/` on 2026-10-12. Plan
placeholders are not defects, and neither were immutable archives.

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
