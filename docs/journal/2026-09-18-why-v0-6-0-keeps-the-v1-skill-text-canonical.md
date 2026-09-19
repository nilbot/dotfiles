# 2026-09-18 — Why v0.6.0 keeps the v1 skill text canonical

## Context

The second read of
[the layout design](../design/2026-09-18-layout-manifest-and-store-root-design.md)
took up the review queue's remaining four questions (Q4–Q7) and the
implementation plan written against them. The draft answer to Q7 was to accept
a fleet advisory: after v0.6.0, every v1 repository's
`recording-what-you-learn` copy would report `known_legacy`, so
`agents drift --all` would exit 1 until each repository migrated, and the
release notes would say so. Measuring the fleet instead of reasoning about it
changed three of the four answers.

## What the measurement showed

`agents drift --json` per repository with the installed v0.5.1 binary on
2026-09-18; the fleet is the six entries in
`~/.local/state/agents/registry.json`, all `local: false`:

| Repository | State | Consequence for the draft answer |
|---|---|---|
| paperbubble | v1, every field current | the pilot |
| cowork | v1, every field current | one of the two the advisory would newly cover |
| lewm-mlx | v1, every field current | the other one |
| dotfiles | v1, every field current | development repository; would have gone non-current too |
| autogo-mlx | router missing, recording skill **missing**, 1 of 4 stores, 2 misplaced docs | already non-current; a legacy digest for a copy that does not exist changes nothing |
| desktop_pet | router diverged, domain missing, recording skill **missing**, 0 of 4 stores | same |

Three consequences:

- `agents drift --all` **already exits 1** on v0.5.1. The advisory would not have
  been a new fleet state; the new part was exactly three repositories (cowork,
  lewm-mlx, dotfiles) moving from current to `known_legacy` for one field.
- Two of the five "deferred" repositories have no recording skill at all, so the
  design's sentence "the old skill is still correct for a v1 repository" was
  false for them. Their blocker is the 2026-08-29 two-tier migration, not this
  release, and "deferred" must not read as "one no-move migration away".
- Zero of six registered repositories used `init --local`. That is what turned
  Q6 from a policy question into a rule that costs nothing to write down:
  `--local` only ever meant "machine wiring is not committed".

## Why the advisory was withdrawn rather than accepted

`recording-what-you-learn` is **user-owned**: `agents update` never refreshes
it. The red would therefore have been permanent and unclearable by any
repository action, and the one command that clears an agents-owned asset,
`agents update --all --apply`, is forbidden during the pilot by the playbook
(§1, "No fleet-wide `agents update --all --apply` as part of the pilot"). An
expected-forever red also contradicts §2.1's promise that v1 behavior does not
change.

The resolution reuses the rule §8.1 already applies to the router: the resolved
layout selects the canonical bytes. v1 keeps the frozen v0.5.1 text — the same
bytes the plan already had to author as `LegacyRecordingSkillV051` for the
digest catalog — v2 gets the new role-based text, and each layout's catalog
contains the other layout's text. The same rule now covers the agents-owned
`migrating-fleet-context`, for the same reason: the only mechanical remedy is
the fleet update the pilot forbids. Design §0.8.

## Two defects the review found in the plan

- `TestResumeRefusesAmbiguousAndLostMoves` was an empty function body with a
  comment — the shape
  [can this check actually fail](../qna/can-this-check-actually-fail.md)
  records, in the one task (resume) where the design had already admitted the
  state machine was unwritten.
- The plan expected "No v1 repository changes behavior" while the playbook
  expected the opposite advisory. Both were written from the same design and
  nothing reconciled them.

## The four answers, in one line each

**None of these is an approval.** They are review resolutions, and the design
stays `Proposed — awaiting review` until a human approves it (design §12).

- **Q4**: the migration journal is a frozen plan with phases (`planned`,
  `moved`, `pruned`, `router`), a per-move content digest, idempotent
  `git add -A` index reconciliation, and `--abort --apply` for the
  nothing-moved case; `apply` and `resume` are one reconciliation function.
- **Q5**: archive immutability is content-blind; one planner blocker,
  `archive_not_in_source`, covers the nested-archive case V12 cannot see.
- **Q6**: trackedness is one question asked of git (`check-ignore --no-index`,
  fail closed); `layout.json` and `.gitignore` are not projections of each
  other; the pre-manifest check is a precondition as well as V16.
- **Q7**: the canonical skill text is selected by the resolved layout.
