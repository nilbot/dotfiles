# Design: Layout Manifest and Store-Root Freedom for Agent Context (`agents.layout/v2`)

**Date:** 2026-09-18
**Status:** **Proposed — awaiting review.** The §0 decisions are resolutions
recorded in review; none of them is an approval. No implementation, release, or
repository migration may start until this document, its implementation plan, and
its migration playbook are approved by a human.
**Applies to:** `agents` CLI (`layout`, `scaffold`, `drift`, `doctor`, `init`,
`update`), the root router (`AGENTS.md` / `CLAUDE.md`), `.agents/AGENTS.md`,
`.agents/skills/`, and the physical location of the four documentation stores.
**Depends on:** [Two-Tier Agent Context](2026-08-29-two-tier-context-and-llm-migration-architecture.md)
(including its 2026-09-01 Amendment 1), [Knowledge is Documentation](2026-08-19-knowledge-is-documentation.md),
[why the migration skill shipped hollow](../journal/2026-09-01-why-the-migration-skill-shipped-hollow.md),
[why `agents init` never updates existing instructions](../qna/why-does-agents-init-never-update-existing-instructions.md).
**Amends:** the assumption in the Two-Tier design §2, §3, §4, §5, §6, and §7
that the four stores are always `docs/{design,plans,journal,qna}`. Everything
else in that design stands.

---

## 0. Decisions (resolved in review, pending approval)

Every question below has a resolution recorded in review on 2026-09-18; **none
of them is an approval.** The plan and playbook are written against these
resolutions and change with them. §0.1 records how each question was resolved;
§0.2–§0.4 are the resolutions recorded in the first review, and §0.5–§0.8 the
four recorded in the second. If a resolution changes, the design, plan, and
playbook all change before implementation starts.

| # | Question | Recommendation | Why |
|---|---|---|---|
| 1 | Canonical `stores` form | **Role→path map is canonical and the only form in the manifest.** CLI template defaults may put all four roles under one parent, but they expand to the map; `--stores role=path` is the CLI override. There is no persisted `store_root`. | One shape to parse. A shorthand that survives on disk means two parsers and an ambiguity rule; expanding at creation time removes both. The map already carries every path, and template defaults cover the shared-parent case. |
| 2 | `content_root` for `content-vault` | **No — dropped.** `content-vault` already carries the semantic meaning: the repository's content is the vault and the stores are meta. The manifest describes physical paths, not a second content root. | The field had no consumer: the migration path never scans the whole content root, and `.context` is inside the repository root by construction. Keeping it would be schema weight without a behavior. |
| 3 | CLI template names and release number | CLI templates `code-repo` and `content-vault`; no template or `custom` means no defaults. The manifest has no `profile` field. Schema `agents.layout/v2`; **one release, v0.6.0, containing manifest parsing, the guard, write support, and migration.** | The guard and the `min_mut_ver_floor` version floor are code that ships once. The below-floor refusal is tested by injecting an older version into the new binary, not by releasing one. A single release plus a deploy-before-flip gate is sufficient for a fleet upgraded together, and it avoids maintaining two release paths for one feature. |
| 4 | Migration command and dry-run format | `agents layout migrate`, dry run by default and `--dry-run` accepted explicitly, `--apply` to execute, `--resume` to continue, `--json` for machines. Human output is a line-oriented plan (`move`, `keep`, `blocker`, `links`) ending in `N move, M keep, K blocked, L link(s)`. | Matches `agents update --all [--apply]`'s "dry run unless applied" convention, while keeping the invocation the plan already writes (`--dry-run`). The line-oriented form is reviewable in a terminal and diffable in a PR. |
| 5 | Archive handling during migration | **The archive is never moved and never rewritten.** The manifest records its existing path (`archive`), defaulting to `docs/archive` when that directory exists in a v1 repo. A `docs/` left holding only the archive is not a shell. | `.agents/AGENTS.md` declares `docs/archive/` strictly immutable, and the 2026-09-01 journal records what happens when a migration is ordered to move files out of it. Keeping the archive in place is the only policy that needs no exception. Moving the archive wholesale is a separate future operation, not part of v2. |
| 6 | `agents init --local` repositories | **v2 requires a tracked `.agents/`.** `--local` plus a v2 layout is refused; the manifest would otherwise be machine-local and a clone would silently fall back to v1. | `.agents/layout.json` is repository state shared by every clone. A manifest inside a git-excluded directory cannot be that. |
| 7 | The other repositories' bundled-skill copies in v0.6.0 (`recording-what-you-learn` is user-owned, `migrating-fleet-context` is agents-owned) | **Layout-selected canonical text (§0.8): a v1 repository's canonical text stays the frozen v0.5.1 text, and a v2 repository's canonical text is the new role-based or v2-aware text.** Nothing is refreshed or replaced in round 1, and no v1 repository becomes non-current. | Measured 2026-09-18 on v0.5.1: paperbubble is the pilot (v2 in round 1); cowork and lewm-mlx are ready later (v1, every field current); autogo-mlx and desktop_pet are not ready (their blocker is the 2026-08-29 two-tier migration, not this release); dotfiles is the development repository, v1, and stays `current` with no copy change. The previous draft's advisory was a permanent red on a **user-owned** file: `agents update` never refreshes it, so no repository action could ever clear it. Under layout selection the v1 text is canonical *for v1*, so v0.6.0 adds no new v1 advisory: `dotfiles`, `paperbubble`, `cowork`, and `lewm-mlx` stay `current`, and `autogo-mlx` and `desktop_pet` keep exiting 1 for their pre-existing two-tier reasons. §2.1's "v1 behavior unchanged" holds literally. |

The deferred repositories are **deferred, not permanently excluded**, and they
split into two groups that the release must not conflate. Measured on 2026-09-18
with the v0.5.1 binary:

| Repository | `agents` role | State at measurement (v0.5.1; v0.6.0 names) | Round-1 effect |
|---|---|---|---|
| paperbubble | pilot | v1, router/domain/skills/stores current | migrates to v2 |
| cowork | ready later | v1, router/domain/skills/stores current | none; v2 adoption later moves nothing |
| lewm-mlx | ready later | v1, router/domain/skills/stores current | none; v2 adoption later moves nothing |
| autogo-mlx | not ready | router missing, recording skill missing, 1 of 4 stores, 2 misplaced docs | none; blocked on the 2026-08-29 two-tier migration |
| desktop_pet | not ready | router diverged, domain missing, recording skill missing, 0 of 4 stores | none; blocked on the 2026-08-29 two-tier migration |
| dotfiles | development repository | v1, router/domain/skills/stores current | none; keeps the frozen v1 text |

A repository in the "ready later" group can adopt v2 with the same `docs/` paths
(the explicit `docs/<role>` map), so its layout migration moves nothing: the
change is the manifest, the v2 router, and the layout-selected skill text.
`autogo-mlx` and `desktop_pet` cannot do that — their blocker is the two-tier
migration, not this design — so "deferred" must not be read as "one no-move
migration away" for them. `agents drift --all` already exits 1 today on v0.5.1
because of those two; v0.6.0 adds no new v1 advisory, and the release notes say
which repositories are already non-current and why.

### 0.1 Review ledger

These questions came from the first read of this document on 2026-09-18. They
are recorded here rather than resolved one by one, because several of them can
invalidate the recommendations above. **All ten have a recorded resolution as of
2026-09-18, so the queue is empty** — but a recorded resolution is a proposal of
record, not an approval: §0 stays provisional for implementation purposes until
a human approves this document (§12). Q1, Q2, Q3, Q8, Q9, and Q10 were resolved
in the first pass — the ledger records each, and Q3, Q8, and Q9 have their own
sections in §0.2–§0.4; Q4–Q7 were resolved in the second pass and are recorded
in §0.5–§0.8. The measurements behind the second pass, and the two plan
defects it found, are recorded in the
[2026-09-18 journal](../journal/2026-09-18-why-v0-6-0-keeps-the-v1-skill-text-canonical.md).

| # | Anchored to | Question | Status |
|---|---|---|---|
| Q1 | §0 row 1, §4 | Can roles have different roots and different collaboration policies? Concrete example: `docs/{qna,journal}` for human collaboration and `docs/agents/{design,plans}` for almost-exclusively agent writes. | resolved 2026-09-18 — the `stores` map may point roles at different roots; the manifest is physical-only; collaboration policy belongs in `.agents/AGENTS.md` |
| Q2 | §0 row 2 | Is `content_root` needed at all? `content-vault` may already carry the semantic meaning, and `.context` is inside any content root by construction. | resolved 2026-09-18 — drop it; the domain doc carries the semantic meaning |
| Q3 | §0 row 3 | Do we need future profiles such as `code-repo-v2` for backwards compatibility, and what is the profile-evolution rule? | resolved 2026-09-18 — no persisted profile; `--template` is a CLI-only creation input; semantic conventions live in `.agents/AGENTS.md`; see §0.4 |
| Q4 | §9.4 | Is `--resume` really a linear list progression? The full state machine is not written down: partial directory moves, failure between `git mv` and the manifest update, and non-linear recovery all need explicit states and transitions. | resolved 2026-09-18 — it is not a linear progression; the journal becomes a frozen plan with an explicit phase, a per-move content identity, idempotent index reconciliation, and `--abort --apply`; see §0.5 |
| Q5 | §0 row 5, §9.5 | Does archive immutability depend on whether the archive holds meta or more explicit knowledge? What is the rule when the archive mixes both? | resolved 2026-09-18 — immutability is a property of the path and is content-blind; no classifier is added, and the planner gains one `archive_not_in_source` precondition; see §0.6 |
| Q6 | §0 row 6, §7.5 | For `--local` repositories: where are docs stored and tracked? How does `agents` determine trackedness? If docs are tracked while `.agents/` is not, how are skill writes to tracked docs governed? Is `.gitignore` a projection of `layout.json`, or is the manifest a projection of ignore state? | resolved 2026-09-18 — trackedness is one mechanical question asked of git; stores are always tracked, `--local` governs only `.agents/`, and neither file is a projection of the other; see §0.7 |
| Q7 | §0 row 7 | The other five repositories are deferred, not permanently excluded; a code repository's v2 migration can keep `docs/` and change only the manifest, router, and skills. Is the remaining advisory — `drift` exits 1 until each of them migrates — acceptable, or should the `recording-what-you-learn` asset change be deferred too? | resolved 2026-09-18 — neither: the canonical skill text is selected by the resolved layout, so v1 repositories keep the frozen v0.5.1 text and no advisory is created; see §0.8 |
| Q8 | §7.3, §8.3, `cmd_drift.go:isDriftClean` | Should `drift` accept a known-legacy user-owned skill? The old v0.5.1 names were `ok`/`clean_legacy`/`customized`; `isDriftClean` required the first for every embedded skill, while `doctor` already accepted the other two for the user-owned skill. | resolved 2026-09-18 — adopt §0.2: drift is strict currency, doctor is health, skill states are `current`/`known_legacy`/`diverged`/`missing` |
| Q9 | §8.1, §8.4 | Should the router states use the same currency vocabulary as the skill states? The old `clean_legacy` meant the same kind of thing in both state machines, so the word "clean" carried the same ambiguity. | resolved 2026-09-18 — full alignment: `current`/`known_legacy`/`diverged`/`missing`; see §0.3 |
| Q10 | §0 row 1, §4.2 (old V12) | Do we persist `store_root` at all? The `stores` map already carries every path; `store_root` adds a second manifest shape and a consistency rule. | resolved 2026-09-18 — remove `store_root` from the manifest and the CLI; template defaults plus `--stores role=path` cover every case |

### 0.2 Q8 model (resolved in review 2026-09-18)

Q8 is not really "should `drift` accept `clean_legacy`". It exposes that two
different questions share one word:

- **Currency**: does each embedded asset match the running binary's canonical
  asset for the resolved layout? That is `drift`'s question.
- **Health**: is the repository safe and acceptable to operate? That is
  `doctor`'s question.

The old `clean_legacy` said "clean" while `drift` treated it as not current;
that was the contradiction. The accepted model names the states for currency
and lets each consumer interpret them, instead of teaching the currency
predicate about ownership or layout.

`drift.skills` states:

| v0.5.1 name | v0.6.0 name | meaning |
|---|---|---|
| `ok` | `current` | matches the running binary's embedded asset |
| `clean_legacy` | `known_legacy` | matches a version in the legacy digest catalog |
| `customized` | `diverged` | matches neither; local edits or an unknown version |
| `missing` | `missing` | file absent |

Rules:

- `drift` is strict currency: exit 1 unless every embedded asset is `current`.
  The private `isDriftClean` predicate is renamed `isCurrent`. It does not
  consider ownership, layout, or health.
- `doctor` is health, and its current semantics stay: an agents-owned
  `known_legacy` is ok because the next `agents update` refreshes it;
  agents-owned `diverged` and `missing` warn. A user-owned `known_legacy` and
  `diverged` are ok because the repository owns that choice; user-owned
  `missing` warns. Any layout-specific note belongs in `doctor`, not in
  `drift`.
- The migration skill owns actions: agents-owned non-current → refresh (to the
  canonical text for the resolved layout, §0.8); user-owned `known_legacy` whose
  digest equals the **other** layout's canonical text → replace it with the
  canonical text for the resolved layout (the digest proves it is a shared copy,
  not a local edit; on v1 this is the exit from a state that would otherwise be
  a permanent red); user-owned `known_legacy` that is an older text of the same
  layout → leave on v1, replace on v2; `diverged` → three-way merge if a base is
  identifiable, otherwise stop and ask; `missing` → populate current.
- Consequence for v0.6.0 under §0.8: an unmodified v1 copy is `current`,
  because the frozen v0.5.1 text *is* the v1 canonical text. `known_legacy`
  remains for the older 2026-08-20 text and for the other layout's canonical
  text; `diverged` remains local edits or an unknown version. A repository whose
  two copies are present and unmodified reports both skills `current` and
  `doctor` reports ok, which is what makes §2.1's "v1 behavior unchanged"
  literal rather than approximate. This withdraws the advisory the first draft
  accepted here: it was permanent and unfixable because `agents update` never
  refreshes a user-owned skill. It does not make the fleet green — `autogo-mlx`
  and `desktop_pet` are already non-current for two-tier reasons (§0 row 7).

The state names are JSON-visible in `drift.skills`. The design is not
implemented yet, so renaming them now is free; keeping the old names would be
the same kind of debt as a schema field whose name no longer says what it
means.

These names apply to the embedded-asset states in `drift.skills`. The router
state machine in §8.1 adopts the same vocabulary (§0.3).

### 0.3 Q9 router rename (resolved in review 2026-09-18)

The two state machines share one vocabulary:

| v0.5.1 name | v0.6.0 name |
|---|---|
| `clean_current` | `current` |
| `clean_legacy` | `known_legacy` |
| `drifted` | `diverged` |
| `missing` | `missing` |

Impact:

- code: the `RouterState` constants, `drift` comparisons, `cmd_drift` output,
  `cmd_fleet`/`doctor` consumers, and their tests; the migration skill's
  required-substring test.
- prose: §8.1, §8.4, §10, §11; the migration skill's router-state table;
  `agents/README.md` and the harness skill if they name the states; the
  playbook's dry-run `router` line.
- history: the 2026-08-29 design and the 2026-09-01 journal keep the old names
  as records. The new design and living documents use the new names. The
  documentation checklist carries a pointer amendment into the old design.
- compatibility: v0.6.0 is the first release with the new names, so there is no
  repo migration for the vocabulary itself. A stale migration skill could read
  new JSON before it is refreshed; its Step 0 staleness check already blocks
  that path.
- cost: a lexical sweep, not a semantic change. `drifted` -> `diverged` is the
  largest part because "drifted" appears in the design's core narrative.

### 0.4 Q3 resolution (resolved in review 2026-09-18): no persisted profile

The manifest stores physical layout only. It has no `profile` field. The
semantic conventions — content vault versus code repository, per-role
collaboration policy, vocabulary — live in `.agents/AGENTS.md`, which the root
router already requires the agent to read.

Creation-time convenience is a CLI-only template:

- `agents init --template code-repo` and
  `agents layout migrate --template code-repo` expand to
  `docs/{design,plans,journal,qna}`.
- `--template content-vault` expands to
  `.context/{design,plans,journal,qna}`.
- No `--template`, or `--template custom`, means no defaults;
  `--stores role=path` must supply all four roles.
- `--stores` overrides individual roles on top of the template defaults; the
  dry run shows the expanded map.
- The template name is never written to the manifest. It has no wire identity,
  so it needs no registry, no reserved names, no open/closed enum, and no
  cross-version migration. Adding or removing a template is a CLI change; an
  unknown template is a clear CLI error, and existing repositories are
  unaffected.
- The template table lives in the CLI (`internal/layout/templates.go` or
  equivalent) and is covered by tests: every template's defaults pass
  V1–V16; the expanded map is written to the manifest; the `Manifest` type and
  its JSON have no `profile` field.

Why the persisted-profile model was rejected: `profile` is a non-physical
label with no concrete machine consumer in this design. The skills resolve
paths through the manifest; the router is profile-independent; `doctor` has no
profile-specific check. Its only consumer was creation-time defaults. Keeping
it would require a versioned registry, reserved-name rules, a warning channel,
raw-JSON preservation on rewrite, and a profile migration path — real
machinery for a label that only supplied defaults. If a concrete
machine-readable semantic label is needed later, it can be added with a new
schema version and a migration; the protobuf-style identity rules are recorded
in the [2026-09-18 journal](../journal/2026-09-18-why-layout-has-no-persisted-profile.md).

### 0.5 Q4 resolution (resolved in review 2026-09-18): the migration is a frozen plan

§9.4 as first drafted answered one question — did this move finish? — and called
everything else ambiguous. Three things were missing.

**Content identity.** `destination present` does not mean *our* move landed: a
hand-made directory or an interrupted `cp -r` looks the same. Each move
therefore records `files`, `bytes`, and `digest`. The digest is computed by the
CLI over the source tree — every entry, sorted, slash-separated relative paths,
path + size + contents — and deliberately not a git tree oid: content ignored
by `.gitignore` is invisible to git and is still moved by `git mv`.

**An enumerated state machine.** The journal gains a phase, and every phase
transition is a single idempotent action, so `apply` and `resume` are one
reconciliation function run from the recorded phase.

| Phase | Single idempotent action | Crash recovery |
|---|---|---|
| `planned` | write the journal with every move `pending`, before touching a store | nothing has moved; `--abort --apply` is available |
| *(per move)* | filesystem truth + digest, `git mv`, index reconciliation, journal rewrite | classified by the table below |
| `moved` | every move `done` | re-derived from the filesystem; the phase is a hint |
| `pruned` | remove emptied source directories; remove `docs/` only when empty | `ENOENT` and `ENOTEMPTY` are not failures |
| `router` | write `V2AgentsMD` when the old router was `current`/`known_legacy`; skip when it already matches | idempotent by comparison |
| *(absent)* | write the manifest `active` without the journal | the journal is the record until this write lands |

`migration` carries `from` (the v1 source layout, with explicit paths —
recorded, never re-derived, so resume cannot re-plan against a changed
repository), `started_at`, `backup_tag`, `created_manifest`, `phase`, and
`moves[]`. Per-move `state` is `pending`, `moving`, or `done`; `moving` is
written before `git mv` and `done` after, and the filesystem stays the truth.

**An exit from ambiguity.** The classification is filesystem-first:

| Source | Destination | Meaning | Action |
|---|---|---|---|
| present | absent | pending | `git mv`, then verify the digest at the destination |
| absent | present | done | digest equal → mark done; digest differs → refuse, naming both paths and the mismatch |
| present | present | ambiguous | refuse; when the digests are equal, say explicitly "a copy, not a move" |
| absent | absent | lost | refuse, naming the backup tag |

Every refusal prints a per-move remedy: which path, `git restore
--source=<tag>` versus removing the stray path, and `--resume --apply` as the
retry. There is no `--force`. A flag that moves trees on a guess is the
2026-09-01 class of accident with a nicer name.

`--abort --apply` is the only exit that is not a resume. It is permitted only
when nothing can have moved — `phase` is `planned`, every move is `pending`, and
`created_manifest` is true — and it deletes the manifest, leaving the backup tag
in place. Every other phase refuses and names the phase and the resume command.
Aborting is a mutation and passes the same version guard as every other write; a
binary below the floor refuses it, and the human then removes the manifest by
hand, which is safe precisely because the permitted state proves nothing moved.

**Index reconciliation** closes the crash window inside `git mv` itself, whose
working-tree rename and index update are two operations: after each move and for
each `done` classification the tool runs an idempotent `git add -A -- <from>
<to>`. Rename display and history are unaffected, because git detects renames at
diff and commit time, not at move time.

**Crash matrix.** The test enumerates boundaries rather than asserting coverage:
4 moves × 4 instants — before `git mv`; after `git mv` but before index
reconciliation; after index reconciliation but before the journal rewrite; after
the journal rewrite — plus the `moved`, `pruned`, and `router` boundaries, plus
one non-prefix case (a later move done while an earlier one is pending) to prove
no linear assumption is baked in. The four instants are the design's three,
with the middle one split so the index seam is its own row.

### 0.6 Q5 resolution (resolved in review 2026-09-18): the archive is a place, not a topic

Immutability does not depend on what the archive holds, and an archive that
mixes meta artifacts with content needs no rule of its own. Four reasons:

- the tool cannot classify content mechanically — §7.3 already concedes that a
  `*-plan.md` note in a vault is not an agents artifact;
- every content-dependent rule in this area has already produced a defect: the
  [2026-09-01 journal](../journal/2026-09-01-why-the-migration-skill-shipped-hollow.md)
  row 5, and `drift`'s walk of all of `docs/`;
- the repository's own contract is written about the **location**
  (`.agents/AGENTS.md`: `docs/archive/` is strictly immutable), not about
  subject matter;
- an archive that mixes both needs nothing beyond "do not touch it".

§3's vocabulary therefore states the rule in one sentence: *the archive is a
place, not a topic: everything under it is historical by definition, so nothing
under it is ever moved, rewritten, or reclassified.*

Live knowledge found in an archive is **promoted, never extracted**: a human
copies the still-relevant content into a live store as a new file, and the
archive copy stays as the record. That is a skill action, not a CLI action. The
design ships no archive-content report: it would need the classifier this
resolution rejects, and it has no consumer.

The one rule that is added is a **planner blocker, not a validation rule**:
`archive_not_in_source`. V12 validates the target layout only, so an archive
nested inside a move source — `docs/plans/archive/`, say — passes every V1–V16
check while `git mv docs/plans .context/plans` drags it along and leaves the
manifest naming a path that no longer holds it. Planning refuses, naming the
archive and the source store. A fixture with a mixed archive (a `*-plan.md` and
a `*-design.md` under `docs/archive/`) must plan successfully, report nothing in
`misplaced_docs`, and leave every archive blob byte-identical.

### 0.7 Q6 resolution (resolved in review 2026-09-18): trackedness is one question asked of git

Measured 2026-09-18: `--local`'s only mechanism is a `/.agents/` entry in
`.git/info/exclude` (`scaffold.go`); the stores, the root router and
`.gitattributes` are written and tracked exactly as without it. `--local` means
**"machine wiring is not committed"**, never "the layout is machine-local". Zero
of the six registered repositories set `local: true`.

Three states, three different correct answers:

| State | Question | Answer |
|---|---|---|
| **ignored** | a git ignore rule matches `.agents/layout.json` or `.agents/` | refuse every mutation — V16, plus a pre-flight refusal in `init` and `layout migrate --apply`, because the manifest may not exist yet; reads still report |
| **untracked, not ignored** | no rule matches, and the path is not in the index yet | allow: this is the normal window between `--apply` and the migration commit; `doctor` advises "not yet committed; a clone would fall back to v1" |
| **tracked** | in the index | normal |

The check is `git check-ignore -q --no-index -- <path>`: exit 0 is ignored,
exit 1 is not ignored, and anything else is an error that refuses mutation —
"could not determine" must never be read as "not ignored". `--no-index` is
required because git does not report a tracked path as ignored, and the question
V16 asks is whether the *rule* exists: that rule will hide the next new file in
every clone. When git cannot answer at all, mutation fails closed, exactly as it
does for an unstamped build (§5.3).

The question is asked about **both** the manifest path and `.agents/`, and the
union is the answer. Measured: `/.agents/**` and `.agents/*` match the manifest
path but not the directory, so a directory-only query answers "not ignored"
for a rule that is in force; `/.agents`, `.agents/`, and `.agents` match both
paths. The union is the rule because it can only add detections, never lose
one.

**Neither file is a projection of the other.** `layout.json` is repository state
for shared knowledge stores. `.git/info/exclude` is machine state for machine
wiring: never read as a layout input, never written from layout data. The
tracked `.gitignore` belongs to the repository's maintainers — the tool never
writes it, and the only coupling is the one-way check above. A declared store
that matches an ignore rule is a `doctor` advisory ("its content will not travel
with a clone"), not a validation failure.

`--local` plus v2 stays refused (Decision 6), for the same reason: the manifest
is repository state, and a `--local` clone's stores would resolve as v1 while
its tracked stores sat at the v2 paths. The per-machine-instructions hole — an
untracked skill writing into tracked stores — is a v1 hole that v2 neither
creates nor repairs; it is named in `doctor` and in the migration skill's
`--local` stop, and `agents update` behavior for `--local` repositories does not
change, because v1 behavior does not change and the registered population is
zero.

### 0.8 Q7 resolution (resolved in review 2026-09-18): the canonical skill text is selected by the layout

The row 7 advisory is withdrawn. It was permanent and unfixable: `drift` would
have reported a **user-owned** copy as `known_legacy` forever, and the one
command that refreshes an agents-owned asset (`agents update --all --apply`)
never touches a user-owned one.

The rule is the one §8.1 already applies to the router: **the resolved layout
selects the canonical bytes.**

| Resolved layout | `recording-what-you-learn` | `migrating-fleet-context` |
|---|---|---|
| v1 | frozen v0.5.1 text (the already-planned `LegacyRecordingSkillV051` bytes) | frozen v0.5.1 text (`LegacyMigratingSkillV051`) |
| v2 | the new role-based text | the new v2-aware text |

Both skills are selected this way, not only the user-owned one. The only
mechanical remedy for a non-current agents-owned copy is a fleet-wide `agents
update --all --apply`, and §12's playbook forbids exactly that during the pilot:
leaving the fleet non-current for the whole pilot window is a worse outcome than
freezing one more prose asset. For the agents-owned skill, `agents update`
writes the text for the resolved layout, which on an unmodified v1 repository is
a literal no-op: it compares the bytes first and does not rewrite, so there is no
mtime churn. The user-owned skill is written only when it is absent
(`agents init` on a fresh clone) or replaced by the migration skill.

Catalogs: the v2 catalog contains the v1 canonical text, so a v2 repository
still carrying it reports `known_legacy` and the migration replaces it; the v1
catalog contains the v2 canonical text, so a v1 repository carrying it reports
`known_legacy` rather than `diverged`, and the migration skill replaces it with
the v1 canonical text (§0.2) — a digest-proven shared copy, not a local edit, so
the state has an exit instead of being a permanent red. `isCurrent` stays
layout-blind — the resolver hands it the canonical digest, exactly as §8.1 hands
it the canonical router (§0.2 stands unchanged).

Freezing is mechanical: each v1 text is a real asset file pinned by a test
against the recorded v0.5.1 bytes, so nobody improves it by accident. The prose
gate that forbids a `docs/` path in the embedded skill is scoped to the **v2**
assets; the v1 assets get the opposite gate, because they must name the v1
paths. A positive control runs the v2 recording skill's fallback chain on a v1
fixture and asserts it resolves to `docs/qna` and `docs/journal` through
`agents layout path`.

Fleet consequence: **no new v1 advisory.** `dotfiles`, `paperbubble`, `cowork`,
and `lewm-mlx` stay `current`; `autogo-mlx` and `desktop_pet` continue to exit 1
for the pre-existing 2026-08-29 two-tier reasons this release neither creates
nor clears. The release notes name that baseline, the deploy-before-flip gate,
and the not-ready repositories (§0 row 7) — not an advisory of this release's
making.

---

## 1. Problem

The Two-Tier design separated machine routing from domain knowledge, but it
fixed the physical shape of the knowledge stores to one directory name. The
assumption is in six places:

| Surface | Current hardcoding |
|---|---|
| `scaffold.docsDirs` | `agents/internal/scaffold/scaffold.go:95` — `design`, `plans`, `journal`, `qna` under `docs/` |
| `scaffold.embeddedAssets` | `scaffold.go:102` — asset destinations are `docs/<role>/README.md` |
| `scaffold.DefaultAgentsMD` | `scaffold.go:34` — the root router names `docs/qna/`, `docs/plans/`, `docs/journal/`, `docs/design/` |
| `drift` stores and misplacement | `agents/internal/drift/drift.go:139` joins `docs/<role>`; `drift.go:146-171` walks `docs/` and compares against `docs/plans/` and `docs/design/` |
| `doctor` freshness | `agents/internal/doctor/doctor.go:948` reads `docs/qna` and reports `docs:qna` |
| Bundled skills | `recording-what-you-learn/SKILL.md:16-17,32,47` and `migrating-fleet-context/SKILL.md:149-152,241,259-261` name `docs/` paths directly |

That shape is correct for a code repository and wrong for a content vault.

### 1.1 The target repository

`/Users/nilbot/gist/paperbubble` is an Obsidian vault. Its knowledge-bearing
content is the vault: notes under `protein/`, `editorial/`, `daily/`,
`sidehustle/`, and so on. Its agent meta-knowledge — design, plans, journal,
Q&A about the vault and the editorial work — is a different thing and should
not be mixed into the vault's content tree.

Obsidian's **Excluded files** setting is not enough. It demotes files from
Search, Graph View, Unlinked Mentions, and Quick Switcher; it does not remove
them from the vault's file tree or prevent editing. A dot-directory is
completely invisible to Obsidian. The meta store therefore needs to be
something like `.context/`.

The current behavior is already visible in the pilot. Measured read-only on
2026-09-18:

```
paperbubble docs/            4 tracked files
  docs/design/README.md
  docs/plans/README.md
  docs/journal/README.md
  docs/qna/README.md
paperbubble docs/ content notes: 0
```

`agents init` created a four-store code-repository skeleton in a content vault.
That is the shell this design exists to prevent: four directories that exist
only because the tool could not express any other layout.

### 1.2 Why this must not be a flag day

The 2026-09-01 journal constrains the solution in two ways.

**Every changed asset is recognizable in both layouts.** Changing an embedded
asset without adding its old text to the legacy digest catalog made every
already-migrated repository report `diverged` and exit drift 1. A layout release
touches the router and both bundled skills. §0.8 resolves this by making the
canonical text a function of the resolved layout and keeping each layout's
catalog closed over the other's text, in the same release, gated by tests.

**A pre-manifest binary cannot be taught to refuse.** The old binary never
reads `.agents/layout.json`, so `min_mut_ver_floor` is invisible to it. Measured on a
disposable fixture on 2026-09-18 with the installed v0.5.1 binary:

- on a v1 repository, a second `agents init` left the working tree unchanged;
- on a v2 repository, `agents init` recreated
  `docs/{design,plans,journal,qna}/README.md`, and
  `agents update --all --apply` overwrote a v2-aware migration skill with the
  binary's old embedded text.

Those are examples of damage in the window after a repository flips and before
the binary is upgraded. They are not an argument for splitting the feature
across two releases. The hard constraint is ordering: **before any repository
flips to v2, every binary that can touch it must be manifest-aware, and the
binary actually resolved on `PATH` must be verified as such.** The sequence is
one release, deployed and verified everywhere, then the pilot migration. The
version matrix in §6 is therefore a **test matrix**, exercised by injecting an
older running version into the new binary; it is not a release matrix.

## 2. Goals and non-goals

### 2.1 Goals

- One physical-layout source per repository: `.agents/layout.json`.
- Logical roles (`design`, `plans`, `journal`, `qna`) separated from physical
  paths.
- A content vault can keep its meta stores in `.context/` with no `docs/`
  shell.
- No manifest means v1, and v1 filesystem and mutation behavior is unchanged.
  The v1 output changes are exactly three: the additive drift fields in §7.3,
  the doctor check rename in §7.4, and the currency-vocabulary rename of
  §0.2/§0.3 — `drift.skills` values change from
  `ok`/`clean_legacy`/`customized` to
  `current`/`known_legacy`/`diverged`, and the router states from
  `clean_current`/`clean_legacy`/`drifted` to
  `current`/`known_legacy`/`diverged`, without changing any exit code.
- Deploy-before-flip compatibility: one release, verified on every machine
  before any repository flips.
- Validation strict enough that a malformed manifest cannot redirect a write
  outside the repository, into `.agents/`, or onto another store.
- Migration that uses `git mv`, is resumable, never copies, never writes into
  or out of the archive, and leaves a rollback point.
- Skills and documentation that speak in roles, not paths, in every v2 asset.
  The v1 assets stay frozen with their v1 paths, because that is what v1 means
  (§0.8).
- Tests for the deterministic code and for the prose deliverables. The
  2026-09-01 journal's "migration skill shipped hollow" is the failure mode
  this requirement exists to prevent.

### 2.2 Non-goals

- Migrating any repository other than the paperbubble pilot in the first round.
- Supporting more or fewer than the four roles in v2, or arbitrary role names.
- Obsidian plugin or vault-configuration management.
- Rewriting links inside the vault by the deterministic CLI. The CLI reports
  link candidates; the `migrating-fleet-context` skill rewrites them.
- Moving or rewriting `docs/archive/` content. The archive stays where it is
  and is recorded in the manifest.
- Changing v1 repository behavior without an explicit migration.
- A single-repository `agents update`. The fleet-wide refresh semantics are
  outside this design; this design adds a gate in front of them.

## 3. Vocabulary

| Term | Meaning |
|---|---|
| **role** | One of `design`, `plans`, `journal`, `qna`. A logical store, independent of path. |
| **store** | The physical directory a role resolves to, repository-relative. |
| **manifest** | `.agents/layout.json`. Declares schema, the `min_mut_ver_floor` version floor, status, stores, archive, and the migration journal. |
| **template** | A CLI-only creation input (`--template code-repo\|content-vault`) that expands to a default store map. It is never persisted in the manifest. |
| **v1** | The legacy layout: no manifest, stores at `docs/{design,plans,journal,qna}`. |
| **v2** | The manifest layout defined here. |
| **inspection** | Parse, validate, and report the layout without writing it. |
| **mutation** | Create, change, or migrate the layout. Both are code paths in v0.6.0, not separate releases. |
| **archive** | An immutable history directory, conventionally `docs/archive/`. A place, not a topic: everything under it is historical by definition, so nothing under it is ever moved, rewritten, or reclassified. Recorded in the manifest; never a move source or destination, whatever it holds. |

## 4. The manifest

### 4.1 Location and encoding

The manifest is `.agents/layout.json`, UTF-8 JSON. It is configuration, not
knowledge: `.agents/` remains machine wiring plus repository-specific procedure,
and a layout manifest is machine wiring. It is tracked unless the repository
used `agents init --local`, which v2 refuses (Decision 6).

### 4.2 Fields

| Field | Type | Required | Meaning |
|---|---|---|---|
| `schema` | string | yes in v2 | Exactly `agents.layout/v2`. Any other value is an unknown schema and is unsupported for mutation. |
| `min_mut_ver_floor` | string | yes in v2 | Lowest binary version that may mutate this repository. v0.6.0 writes `0.6.0`. A binary below it may read and display, but every mutation is refused. |
| `layout_status` | string | yes in v2 | `active` or `migrating`. A `migrating` manifest permits only `agents layout migrate --resume --apply` or `--abort --apply`; every other write is refused and every read reports the state. |
| `stores` | object | yes in v2 | The role→path map. All four roles are required; each path is explicit. There is no `store_root` shorthand. |
| `archive` | string | optional | Repository-relative immutable history directory. Defaults to `docs/archive` when that directory exists in a v1 repository. Never a move source or destination. |
| `migration` | object | required while `layout_status` is `migrating` | The resumable journal of §0.5: `from` (the v1 source layout, explicit, never re-derived), `started_at` (RFC 3339), `backup_tag`, `created_manifest`, `phase` (`planned`, `moved`, `pruned`, `router`), and `moves` (`role`, `from`, `to`, `state` (`pending`, `moving`, `done`), `files`, `bytes`, `digest`). Removed when the layout becomes `active`. |

`stores` may point roles at different roots; all four paths are explicit.
Creation-time convenience comes from the CLI template's defaults or the CLI's
`--stores role=path`, but the manifest itself always contains the expanded
map. Collaboration policy — which stores humans edit and which agents mostly
write — is domain prose in `.agents/AGENTS.md`, not manifest data.

Unknown top-level fields are ignored by the parser and preserved only in the raw
file; the CLI never rewrites a manifest it did not create. Unknown fields are
not an extension mechanism for v2 — a new meaning requires a new schema.

`min_mut_ver_floor` is the minimum binary version allowed to mutate this
repository. It is a version floor, not a release, a binary kind, or a
read-only artifact.

### 4.3 Canonical example

The pilot target for paperbubble:

```json
{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
  "layout_status": "active",
  "stores": {
    "design": ".context/design",
    "plans": ".context/plans",
    "journal": ".context/journal",
    "qna": ".context/qna"
  }
}
```

`paperbubble` has no `docs/archive/`, so `archive` is absent.

The CLI template's creation defaults expand to this map:
`agents layout migrate --template content-vault` uses
`.context/{design,plans,journal,qna}`. `--stores role=path` overrides any
role; the CLI expands the overrides into the map before writing. The manifest
itself always contains the expanded map.

The scattered form is for a repository whose stores do not share a parent:

```json
{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
  "layout_status": "active",
  "stores": {
    "design": "architecture/design",
    "plans": "architecture/plans",
    "journal": "notes/log",
    "qna": "notes/qna"
  }
}
```

All four roles are required in v2. The architecture is four roles with one
retrieval axis each; only their paths vary. A repository that needs a different
set needs a future schema, not a partial manifest.

### 4.4 The v2 router

The root router for a v2 repository no longer names store paths. It points at
the manifest and at the CLI's resolved view:

```markdown
# Agent context

Durable context for this repo is described by `.agents/layout.json`
(`agents.layout/v2`). Read that manifest before assuming where anything lives;
it names the meta stores and their paths. This file is only the pointer.

- `.agents/layout.json` — status and one path per role
- `agents layout show` — the same layout, resolved and validated (when the CLI is installed)
- `agents layout path <role>` — one store, by role: design, plans, journal, qna

## Repository Architecture & Guidelines
- Domain engineering guidelines, commenting standards, and safety constraints
  are defined in `.agents/AGENTS.md`.
- Repo-specific procedures and skills are located in `.agents/skills/`.

## Machine Wiring
`.agents/` holds machine wiring and local skills. A hook cannot install itself
and a missing hook fails silently.
- If the `agents` CLI is installed, run `agents doctor` early and report any warnings before relying on this context.
- If `agents` is not installed on this machine, skip machine wiring checks and adhere directly to the repository instructions above.

Recording is covered by the global instruction and the `recording-what-you-learn`
skill; it is not repo-specific and is not restated here.
```

The v1 router (`scaffold.DefaultAgentsMD`) is unchanged and remains canonical
for v1 repositories. It is not rewritten in place; it becomes a legacy template
only for repositories that opt into v2.

## 5. Resolution, validation, and version support

### 5.1 Resolution

`internal/layout.Resolve(root)`:

1. If `.agents/layout.json` does not exist, return the implicit v1 layout:
   `schema=agents.layout/v1`, `layout_status=active`, stores
   `docs/{design,plans,journal,qna}`, and `archive="docs/archive"` when that
   directory exists.
2. If it exists, parse it. A JSON syntax error is an error. An unknown `schema`
   resolves successfully but is unsupported for mutation, so the CLI can still
   display what it found and refuse to touch it.
3. Read `stores` as the role→path map. The manifest must contain a JSON
   object; the CLI's `--stores` inputs are expanded before the manifest is
   written.
4. Validate. A manifest that fails validation resolves to an **invalid**
   layout: reads report the problem; every mutation is refused.
5. The caller compares `min_mut_ver_floor` with the running version through
   `Support`; resolution itself is version-independent so the CLI can display
   a layout it must not mutate.

Resolution never creates a directory and never writes.

### 5.2 Validation rules

Every rule below is machine-checkable and has a stable problem code so the
doctor, drift report, and CLI can name it.

| # | Code | Rule |
|---|---|---|
| V1 | `role_unknown` | A store role is not one of `design`, `plans`, `journal`, `qna`. |
| V2 | `role_missing` | A required role is absent. All four are required in v2. |
| V3 | `role_duplicate` | A role appears twice in the JSON object (detected at token level, not silently last-wins). |
| V4 | `role_duplicate_path` | Two roles resolve to the same path. |
| V5 | `path_empty` | A store path is empty. |
| V6 | `path_absolute` | A store path is absolute. |
| V7 | `path_escapes` | A store path contains `..` or resolves outside the repository root. |
| V8 | `path_symlink` | The store path, or any existing component of it, is a symlink. |
| V9 | `path_in_agents` | The store is `.agents/` or is under it. Configuration may live there; knowledge may not. |
| V10 | `path_overlap` | Two stores are equal, or one is a path-prefix of the other. |
| V11 | `path_case_collision` | Two store paths differ only by case. Enforced on every filesystem, not only on case-insensitive ones, so a repository does not become unportable. |
| V12 | `archive_overlap` | `archive` equals a store, contains a store, or is contained by a store. |
| V13 | `status_unknown` | `layout_status` is not `active` or `migrating`. |
| V14 | `migration_missing` | `layout_status` is `migrating` and the `migration` journal is absent or malformed. |
| V15 | `min_mut_ver_floor_invalid` | `min_mut_ver_floor` is not a parseable `MAJOR.MINOR.PATCH` version. |
| V16 | `local_agents` | A git ignore rule matches `.agents/layout.json` or `.agents/` (`git check-ignore -q --no-index`). A v2 manifest there is machine-local: a clone would resolve v1 while the stores sit at the v2 paths. When trackedness cannot be determined, mutation is refused (fail closed, §0.7). |

Case-insensitive collision detection is deliberately stronger than the
filesystem. A layout that works on macOS and fails after a clone to Linux is a
portability bug the tool can refuse cheaply.

V1–V16 validate a **resolved layout**. Three rules in this design are
deliberately not validation rules, because the layout they protect is valid:
`archive_not_in_source` and `docs_residue` are planner blockers (§0.6, §9.5),
and an ignored store, an uncommitted manifest, and an in-progress git operation
belong to `doctor` and the §9.1 preconditions (§0.7). A V-rule makes the whole
layout unsupported for mutation, which is too strong for a condition the human
can clear by committing or by moving a stray file.

### 5.3 Version comparison

`min_mut_ver_floor` is compared as semantic version `MAJOR.MINOR.PATCH`:

- A running version begins with an optional `v`; release tags are
  `vMAJOR.MINOR.PATCH`, so `v0.6.0` and `0.6.0` compare equal.
- A prerelease suffix sorts below its release (`0.6.0-rc.1 < 0.6.0`).
- `dev` or any unparseable running version is **unsupported for mutation**. A
  source build cannot prove which release it was built from, and the guard must
  fail closed: an older source build must not be mistaken for the released
  binary. Reads still work, so `layout show`, `layout validate`,
  `drift`, and `doctor` can display a v2 layout from a dev build. A v1
  repository is unaffected, because the implicit v1 layout is always mutable.
- A parseable running version below `min_mut_ver_floor` is unsupported for mutation:
  reads and reports still work, writes do not.

The guard is applied by a single helper used by every mutating command. No
command re-implements the comparison.

## 6. Compatibility model

### 6.1 One release, one version floor

There is one released version: **v0.6.0**. It contains:

- manifest parsing: `internal/layout` parser, resolver, validation, version
  comparison, the v2 router constant, drift layout fields, doctor layout
  checks, and read-only `agents layout show|validate|path`;
- the guard: `init` and fleet `update` resolve the manifest first and refuse or
  skip an unsupported, invalid, or `migrating` layout;
- mutation: layout-aware scaffold, `agents init` layout flags, atomic
  manifest writes;
- the migration: `agents layout migrate` with plan, apply, and resume;
- the skill changes: a role-based v2 `recording-what-you-learn` and a v2-aware
  `migrating-fleet-context`, with the v0.5.1 text of each frozen as the v1
  canonical text and each layout's catalog closed over the other's text (§0.8).

Inspection and mutation are code paths in one release, not two releases. The
guard exists so that a future manifest-aware binary older than a repository's
`min_mut_ver_floor` refuses mutation. The test matrix exercises it by building
version-stamped test binaries (for example `-X main.version=v0.5.99` and
`-X main.version=v0.6.0`) and running the same fixture against both. There is
no intermediate release; those binaries are test scaffolding only.

### 6.2 Binary version × repository layout

| Binary | v1 (no manifest) | v2 (`min_mut_ver_floor` 0.6.0) |
|---|---|---|
| **v0.5.1 and earlier** | Current behavior. Correct and unchanged. | **Unsafe.** The binary does not know the manifest exists: `init` creates `docs/{design,plans,journal,qna}` shells, `drift` reports the stores missing, and `update --all --apply` overwrites the repository's v2-aware migration skill with the pre-v2 asset. No technical guard is possible; the deploy-before-flip gate below is the guard. |
| **v0.5.99 (test-only older manifest-aware build)** | Correct v1 behavior. | Reads and validates; refuses mutation with `unsupported: below_floor`. This row exists only as a test control. |
| **v0.6.0** | Correct v1 behavior, including the frozen v1 skill texts, so no v1 repository's currency state changes (§0.8); the two already non-current repositories stay non-current for their pre-existing reasons. `drift` reports v1 and, when entities are stale, advises the migration skill. No automatic migration. | Full read/write support: `layout show\|validate\|path\|migrate`, layout-aware `init`, guarded fleet `update`, v2 canonical skills. |
| **v0.7.0+** | Correct v1 behavior. | Correct v2 behavior; future layouts set `min_mut_ver_floor` to the first binary that understands them. |

### 6.3 Deploy-before-flip gate

The repository cannot enforce this from inside a v2 manifest, because a binary
that does not read the manifest cannot be told to stop. The gate is operational
and ordered:

1. Implement and test v0.6.0, including the version-stamped guard matrix in
   §6.1.
2. Release v0.6.0.
3. Upgrade every machine that can run `agents init`, `agents update --all
   --apply`, or `agents save` against the fleet. Verify both `agents version`
   and that the `agents` resolved on `PATH` is that binary; the existing
   `binary` doctor check exists for exactly this.
4. Only after every machine passes, run the paperbubble migration playbook.
5. A machine still on v0.5.1 cannot be prevented from touching a v2
   repository; it is prevented from existing on a machine that touches the
   fleet. The release notes name this as a hard prerequisite, not a suggestion.

`v0.6.0` still reads each repository's manifest before wiring or refreshing in
fleet `update`, and skips unsupported, invalid, or `migrating` repositories
with a named reason. That is defense-in-depth for future schema versions and
for an older manifest-aware binary, not the primary gate.

### 6.4 Repair path when an old binary touched a v2 repository

Symptoms:

- `docs/design/README.md`, `docs/plans/README.md`, `docs/journal/README.md`, and
  `docs/qna/README.md` reappear as untracked or newly tracked files;
- `agents drift` reports `layout_version: v2` with `docs_stores` all false;
- `agents doctor` reports `layout:qna` empty;
- `.agents/skills/migrating-fleet-context/SKILL.md` no longer matches the
  installed binary.

Repair, in order:

1. Install v0.6.0 on the machine that will touch the repository.
2. `agents layout validate` to confirm the manifest itself was not damaged.
3. `git status --porcelain` and inspect the `docs/` entries. If `docs/` holds
   only the regenerated four-store shell, `git rm -r docs/` (or remove the
   untracked shell). If `docs/archive/` exists, remove only the four shell
   directories; the archive is never touched.
4. From a v0.6.0 binary, `agents update --all --apply` to restore the
   v2-aware migration skill.
5. `agents drift --json` (expect supported v2), `agents doctor`, and the
   repository's own test suite.
6. Commit the repair on a feature branch and open a PR. Do not amend the
   migration commit.

## 7. CLI surface

### 7.1 New command family

```
agents layout show [--json] [--router]
agents layout validate [--json]
agents layout path <role>
agents layout migrate [--template <p>] [--stores <role=path> ...]
                       [--archive <path>]
                       [--dry-run | --apply | --resume --apply | --abort --apply]
                       [--backup-tag <name>] [--json]
```

v0.6.0 ships all four subcommands and the layout flags on `agents init`.

`show` prints the resolved layout: schema, status, minimum mutating version
(`min_mut_ver_floor`), stores, archive, and one line per role. `--json` emits the normalized
`Layout` object (one role→path map, never the raw shorthand). `--router` prints
the exact canonical router for the resolved layout and nothing else, so the
migration skill can restore it without embedding a second copy.

`validate` runs V1–V16 and prints one line per problem with the manifest path
and the offending value. Exit 0 when the repository is a valid v1 or v2
layout; 1 when invalid or unsupported for mutation; 4 when not inside a
repository with `.agents/`.

`path <role>` prints the repository-relative store path and nothing else, so a
skill can use it in a command substitution. Unknown role and missing operand
are malformed (exit 3). An unsupported, invalid, or `migrating` layout prints
nothing and exits 4 or 1.

### 7.2 Migration command

Full semantics are in §9 and the migration playbook. Surface rules:

- Dry run is the default. `--dry-run` is an accepted explicit synonym.
- `--apply` is required to move anything.
- `--resume` requires `--apply`, a `migrating` manifest, and no other layout
  flags; the manifest supplies the target.
- `--abort` requires `--apply` and is permitted only when nothing can have
  moved: `phase: planned`, every move `pending`, and a manifest this migration
  created. It deletes the manifest and keeps the backup tag. Any other phase
  refuses, names the phase, and points at `--resume --apply` (§0.5).
- `--backup-tag <name>` is required with every `--apply` that can move something
  (that is, every `--apply` except `--resume` and `--abort`). The tool creates
  the annotated tag at HEAD before the first write, so the rollback point exists
  even if the operator forgot to create a branch.
- `--template` is optional. When present, its creation defaults supply the
  store map; `--stores role=path` overrides individual roles. When absent (or
  `custom`), `--stores` must supply all four roles.
- `--json` emits one object: `repo`, `dry_run`, `phase`, `from`, `to`, `router`,
  `archive`, `moves`, `link_candidates`, `blockers`, `counts`. `phase` is the
  migration phase the report describes: `planned` for a dry run or a fresh
  `--apply`, and the journal phase a `--resume` continued from.
- Exit codes: 0 applied and no link candidates, or `--abort` completed; 1 dry-run
  plan ready, applied with link candidates, blockers (including `docs_residue`),
  or a refused `--abort`; 3 malformed flags; 4 not a repository with `.agents/`;
  5 apply failed mid-way and the manifest remains `migrating` for resume.

### 7.3 Drift report changes

`DriftReport` keeps every existing field and adds:

| Field | v1 value | v2 value |
|---|---|---|
| `layout_version` | `v1` | `v2` or `unknown` |
| `min_mut_ver_floor` | `""` | manifest value |
| `layout_status` | `active` | `active` or `migrating` |
| `stores` | `docs/{design,plans,journal,qna}` | resolved role→path map |
| `unsupported` | `""` | `unknown_schema`, `below_floor`, `unreleased`, `invalid`, or `""` |
| `unsupported_detail` | `""` | human-readable reason |

`docs_stores` remains, populated with role presence for both versions, and is
deprecated. It is removed no earlier than two minor releases after v0.6.0 (not
before `v0.8.0`), and the release notes must name the removal.
`misplaced_docs` keeps its meaning. For v2 it walks the declared stores,
excluding the archive. It does not walk arbitrary vault content: a
`*-plan.md` note in a vault is not an agents plan artifact. v1 continues to
walk `docs/` exactly as today.

A report is current only when `unsupported` is empty, `layout_status` is
`active`, all four roles resolve to existing directories, the router matches
the canonical router for that layout, there are no misplaced live documents,
and every `drift.skills` value is `current`. Any `known_legacy`, `diverged`,
or `missing` skill makes the report non-current and exits 1. `doctor`, not
`drift`, decides whether a non-current state is healthy.

### 7.4 Doctor changes

| Check | Statuses |
|---|---|
| `layout:manifest` | `ok` for implicit v1 and active supported v2; `warn` when `migrating`, below `min_mut_ver_floor`, or unknown schema; `fail` for an invalid manifest |
| `layout:stores` | `ok` when all four roles resolve to directories; `warn` with the missing role and path otherwise |
| `layout:qna` | the freshness indicator, resolved through the `qna` role instead of hardcoded `docs/qna` |
| `layout:tracked` | `ok` when the manifest is tracked; `warn` when it is untracked but not ignored (not yet committed; a clone would fall back to v1), when a git ignore rule matches it or `.agents/` (mutation is refused, V16), or when a declared store is ignored (its content will not travel with a clone) |

`docs:qna` is removed; the rename is named in the v0.6.0 release notes.

The existing `scaffold:skill-recording` and `scaffold:skill-migrating` checks
remain. They use the §0.2 state names; their owner-based health semantics do
not change.

### 7.5 Init and update

`agents init` keeps its v1 behavior when no layout flag and no manifest is
present. In v0.6.0 it also accepts `--template`, `--stores`, and `--archive`;
any of them creates a v2 layout, with the template supplying creation defaults.
On a v2
repository with an active supported manifest, `init` is a no-op for an intact
layout and creates only missing *manifest-declared* stores. It never creates
`docs/{design,plans,journal,qna}` in a v2 repository.

If a manifest is absent but a v1 layout already exists (`AGENTS.md` or `docs/`
is present), layout flags are refused with `agents layout migrate` as the
remedy. `--local` with any layout flag is refused too (Decision 6, §0.7): a
manifest a clone cannot see would leave that clone resolving v1 while the
stores sat at the v2 paths, and the check runs before the manifest exists.
Adopting an existing repository is the migration command's job, because
it must move the stores and prove the router is boilerplate. Creating a v2
layout is a mutation and requires a running version that supports it; an
unstamped `dev` build refuses, per §5.3.

`agents update --all --apply` reads each repository's manifest before wiring or
refreshing. Unsupported or `migrating` repositories are skipped, named, and
left byte-for-byte unchanged. Supported repositories are wired and their
`migrating-fleet-context` skill refreshed to the canonical text for the resolved
layout, which is a byte-identical no-op on an unmodified v1 repository (§0.8).

`agents save` is unchanged. It still commits only `.agents/` paths. The
migration playbook explicitly forbids using it mid-migration, because the
manifest and the moved stores must land in one commit.

### 7.6 Documentation invariant

Adding commands and flags updates, in the same change set:

1. `agents/README.md` (features, quickstart, CLI reference);
2. root `README.md`, including the generated block
   (`agents help --render=markdown`);
3. CLI built-in help;
4. `claude/skills/agents-tool/SKILL.md` for every agent-facing command;
5. this design's catalog entry and any affected Q&A;
6. `.github/release-notes/<version>.md` when the change set adds a release gate
   or a fleet-wide expectation — the GitHub Release body is the authoritative
   carrier (`body_path`, not auto-generated notes), and the `agents/README.md`
   "Upgrading" section is the durable repo-side copy.

## 8. Router, digest catalog, and skills

### 8.1 Two canonical routers

`scaffold.DefaultAgentsMD` remains the v1 canonical router, byte-for-byte.
`scaffold.V2AgentsMD` is the manifest-pointing router in §4.4. `drift` selects
the canonical digest by the resolved layout:

- v1 → `DefaultAgentsMD`;
- v2 → `V2AgentsMD`.

The legacy router catalog keeps every historical v1 template and adds
`DefaultAgentsMD` itself. That makes a v2 repository still carrying the v1
router classify as `known_legacy` — known boilerplate, safe to replace — while
a v1 repository continues to classify as `current`. No v1 repository
changes state.

### 8.2 Skill digest catalog and layout-selected canonical text

v0.6.0 changes both embedded skills in the same release, and §0.8 makes the
canonical text a function of the resolved layout. Each skill therefore has two
canonical texts, and the catalogs are closed over both:

| Skill | v1 canonical | v2 canonical |
|---|---|---|
| `recording-what-you-learn` | the frozen v0.5.1 text | the role-based text of §8.3 |
| `migrating-fleet-context` | the frozen v0.5.1 text | the v2-aware text of §8.4 |

- The v2 catalog contains the v1 canonical text, so a v2 repository still
  carrying it reports `known_legacy` and the migration replaces it.
- The v1 catalog contains the v2 canonical text, so a v1 repository carrying it
  reports `known_legacy` rather than `diverged`.
- `recording-what-you-learn` keeps the existing 2026-08-20 entry in both
  catalogs; `migrating-fleet-context` has no earlier entry.

Without those entries an unmodified old copy would report `diverged` — the
2026-09-01 accident — and a repository carrying the other layout's text would
report `diverged` when it is in fact recognizable. The plan gates this with a
test that every canonical text of the other layout digests to a legacy value,
and with a digest pin on each frozen v1 asset so nobody edits it by accident.

### 8.3 `recording-what-you-learn`

The v2 canonical text is role-based. It resolves paths with `agents layout path
qna` and `agents layout path journal`, and falls back to reading
`.agents/layout.json` when the binary is absent. When neither exists — no
binary and no manifest — the repository is v1, and the v1 store names are the v1
canonical text's business: the v2 asset points at that text rather than naming
the paths itself. Prose gate: a test fails if `docs/` appears anywhere in the
**v2** asset, including in a fallback sentence. The gate is deliberately literal
rather than "no `docs/` in a v2-capable sentence", because a test cannot
classify sentences and the v1 text already owns the paths. The reverse gate
applies to the v1 asset, which must name them.

`agents init` writes the text for the resolved layout when the file is absent,
so a fresh v1 repository receives the v1 text. **The v1 texts cannot perform the
flip.** A v1 repository's `migrating-fleet-context` copy is the frozen v0.5.1
text: it predates `agents layout migrate` and cannot drive a v1→v2 migration.
The CLI and the playbook do that, and the v2 texts are installed *after* the
layout flips — by the playbook, which writes the v0.6.0 assets, or by the
migration skill's user-owned `known_legacy` replacement on an
already-v2 repository, after reviewing any local edits. `agents update` never
writes the user-owned skill, in either layout.

A positive control runs the resolved-path step against a v1 fixture and asserts
that `agents layout path qna` and `agents layout path journal` return `docs/qna`
and `docs/journal` through the implicit v1 layout: the v2 text's fallback then
has a resolved answer on both layouts even though it is canonical on one, and
the v2 asset never has to name a path to get it.

The old v0.5.1 behavior was stricter than `doctor`: `drift`'s `isDriftClean`
required every embedded skill to be `ok`, so a known legacy
`recording-what-you-learn` reported drift and exited 1, while `doctor`'s
`scaffold:skill-recording` already accepted `clean_legacy` and even
`customized`. v0.6.0 resolves this with the §0.2 model: `drift` is strict
currency through `isCurrent`, `doctor` reports owner-based health, and both use
the `current`/`known_legacy`/`diverged`/`missing` names.

### 8.4 `migrating-fleet-context`

The skill reads the manifest before acting:

| Layout state | Action |
|---|---|
| no manifest (v1) | current procedures, unchanged |
| `.agents/` is git-ignored (`--local`) | stop; v2 is unavailable while the manifest would be machine-local (§0.7) |
| v2, `active`, supported | no layout migration; only link rewrites and domain-prose reconciliation |
| v2, `migrating` | stop; tell the human to run `agents layout migrate --resume --apply` |
| v2, unsupported | stop; the binary is older than `min_mut_ver_floor` |
| unknown schema | stop; no guessing |

The text this skill is canonical for is selected by the resolved layout (§0.8):
a v1 repository keeps the frozen v0.5.1 text and stays `current`. That v1 text
does not know `agents layout migrate` and never performs the flip; the v2-aware
text is installed *after* the layout flips — by the migration playbook, or by
the migration skill's `known_legacy` replacement on an already-v2 repository.
For this agents-owned skill, `agents update` then keeps the installed text
current for the resolved layout.

It names `agents layout show`, `agents layout migrate --dry-run`, `--apply`,
`--resume`, and `agents layout path`; it distinguishes the four router states
as before; it treats the archive as immutable; and it produces the same
traceability table before the approval gate.

The skill keeps its existing pasted v1 router for the v1 path. That block is
still the correct canonical text for a repository with no manifest, and the
existing test binding it to `scaffold.DefaultAgentsMD` stays meaningful. The v2
path does not paste a router at all: it restores the exact bytes from
`agents layout show --router` after the tool has classified the old router as
`current` or `known_legacy`.

### 8.5 Domain context

`.agents/AGENTS.md` is user-owned and stays semantic: what the content is, what
each role is for, and which conventions apply. It does not name physical store
paths. The starter template is updated in v0.6.0; existing repositories keep their
own file and are corrected by the migration skill's prose reconciliation, not
by a deterministic overwrite.

Per-role collaboration policy belongs here: which stores humans edit, which
agents mostly write, and which are read-only for one side. The manifest records
where a role lives, not how the repository wants that role used.
The semantic profile also belongs here: whether the repository is a content
vault or a code repository, and any vocabulary that follows from that. The CLI
template name is not persisted and is not read back as a repo property.

## 9. Migration mechanics

### 9.1 Preconditions

`agents layout migrate` plans only when:

- the target is a git worktree with `.agents/`;
- the working tree is clean (`git status --porcelain` empty), and no git
  operation is in progress — merge, rebase, cherry-pick, revert, `am`, bisect
  (`repo.InProgress`). Each of those moves or discards HEAD on its own, and the
  migration's rollback point is a tag at HEAD;
- the current branch is not `master` or `main`;
- the root router is `current` or `known_legacy`, so replacing it with
  the v2 router is provably boilerplate-only. A `diverged` or `missing` router
  is a blocker: the `migrating-fleet-context` skill reconciles it first;
- every one of the four source stores exists as a real directory, not a
  symlink. A missing store is a blocker, with `agents init` as the remedy; the
  migration itself never creates a store, because "create the missing one" and
  "copy the existing one" are the same kind of operation and the second is
  forbidden;
- the archive, if one is declared or derived, is not equal to and not inside any
  move source (`archive_not_in_source`, §0.6). V12 only validates the target
  layout, so this is a planner blocker;
- `docs/` holds nothing but the four source stores and the archive. Any other
  entry is a `docs_residue` blocker, named one per line with its remedy,
  because the anti-shell promise of §9.5 cannot be kept while it remains;
- the running binary reports `Support(targetLayout)` true. A binary below
  `min_mut_ver_floor`, or an unstamped `dev` build, plans but refuses to apply;
- no target path exists, except the resumable "already moved" case in §9.4;
- the computed layout passes V1–V16;
- `.agents/layout.json` and `.agents/` are not matched by any git ignore rule
  (`git check-ignore -q --no-index`, §0.7). This check runs before the manifest
  exists, so it is a precondition as well as V16; when trackedness cannot be
  determined, planning refuses rather than guessing.

### 9.2 Plan and dry run

The planner reads the v1 layout, resolves the requested v2 layout, and produces
one action per store directory — the whole directory, including its `README.md`
and any nested content. It reports, but never performs, link candidates.

Human output:

```
layout migrate (dry run) — /Users/nilbot/gist/paperbubble
  from    agents.layout/v1  docs/{design,plans,journal,qna}
  to      agents.layout/v2  stores=.context/{design,plans,journal,qna}
  router  current -> canonical v2 (deterministic swap)
  archive none
  git     branch agents-editorial, tree clean

  move    docs/design   -> .context/design   (1 file)
  move    docs/plans    -> .context/plans    (1 file)
  move    docs/journal  -> .context/journal  (1 file)
  move    docs/qna      -> .context/qna      (1 file)
  keep    .agents/AGENTS.md  (user-owned; prose reviewed by the skill)
  remove  docs/ after the moves (no archive, no other tracked content)

  links   0 markdown links point into the moved stores
  result  4 move, 1 keep, 0 blocked, 0 link(s)

  apply   agents layout migrate --template content-vault \
            --apply --backup-tag pre-layout-v2-20260918
```

`--json` carries the same information as one object. `moves[].role` is present
so the skill can reason in roles.

### 9.3 Apply

Apply is the reconciliation function of §0.5 run from `phase: planned`. Every
step below is idempotent, so a crash is recovered by re-running it:

1. Create the required annotated `--backup-tag` at HEAD.
2. Walk each source store and record its `files`, `bytes`, and `digest`, then
   write `.agents/layout.json` with `layout_status: "migrating"`,
   `phase: "planned"`, `created_manifest: true`, and the full move list with
   every `state: "pending"` — before touching any store. The write is atomic
   (temp file + rename).
3. For each move, in journal order: re-verify the source against its recorded
   digest, write `state: "moving"`, `git mv` the source to the destination (a
   directory move takes its `README.md` and any nested or ignored content with
   it), reconcile the index with `git add -A -- <from> <to>`, write
   `state: "done"`, and rewrite the manifest atomically. The index
   reconciliation closes the window inside `git mv`, whose working-tree rename
   and index update are two operations; renames are still detected at diff and
   commit time, so history is unaffected.
4. When every move is `done`, record `phase: "moved"`.
5. Remove now-empty source directories with `os.Remove`, and `docs/` only when
   it is empty. Never remove a directory that still has entries. Never remove
   the archive. `ENOENT` and `ENOTEMPTY` are not failures; a `docs/` that still
   holds anything other than the archive is reported as a `docs_residue`
   blocker with its entries named. Record `phase: "pruned"`.
6. Replace a clean `AGENTS.md` with `V2AgentsMD`; skip when it already matches.
   Record `phase: "router"`.
7. Rewrite the manifest with `layout_status: "active"` and without the
   `migration` journal. The journal is the record until this write lands.
8. Report link candidates. The layout is complete; the links are not. If any
   candidate exists, exit advisory and name the migration skill as the next
   step.

No step copies a file. `git mv` is the only mechanism, so history and blob
identity are preserved and `git status` shows renames.

### 9.4 Resume and abort

`--resume --apply` reads a `migrating` manifest, enters the phase it records,
and continues the same reconciliation function. It never re-plans: the journal's
`from` is the frozen record of the source layout, and a repository that changed
underneath is a mismatch to report, not a plan to recompute.

For every move it compares the filesystem against the journal and the recorded
content identity:

| Source | Destination | Meaning | Action |
|---|---|---|---|
| present | absent | pending | `git mv` it, then verify the digest at the destination |
| absent | present | done | digest equal → mark done; digest differs → refuse, naming both paths and the mismatch |
| present | present | ambiguous | refuse; when the digests are equal, say explicitly "a copy, not a move" |
| absent | absent | lost | refuse, naming the backup tag |

The filesystem is the truth. The `state` field is a hint that makes the common
case fast and the crash window small. Resume never guesses and never copies, and
it does not assume the moves happened in journal order: a later move that landed
while an earlier one is pending is reconciled row by row, because the targets
are disjoint by V10.

Every refusal names the offending paths and prints the remedy for that row:
`git restore --source=<tag> -- <path>` when the destination must go, plain
removal when the stray path is untracked, and `--resume --apply` as the retry.
There is no `--force`; a flag that moves trees on a guess is the 2026-09-01
class of accident with a nicer name.

`--abort --apply` is the only exit that is not a resume, and it is available
only while nothing can have moved: `phase: "planned"`, every move `pending`, and
`created_manifest: true`. It deletes the manifest and leaves the backup tag as
the record. Any later phase refuses, names the phase, and points at
`--resume --apply`.

### 9.5 Archive, links, and `docs/` removal

- The archive path is recorded in the manifest and excluded from every walk,
  every move list, and every link rewrite.
- Migration never moves a file out of the archive and never writes into it. The
  rule is content-blind: an archive holding meta artifacts, vault content, or
  both is treated identically, because the contract is about the place and not
  about the subject (§0.6). Live knowledge found there is promoted into a live
  store by a human, never extracted by the tool.
- `docs_residue`: if `docs/` still holds anything after the moves and the
  pruning, other than the declared archive, apply reports it as a named blocker
  and exits advisory. The repository is not migrated cleanly while a `docs/`
  shell survives, and silent success there would falsify the anti-shell claim
  below.
- A v1 repository with no `docs/archive/` loses `docs/` entirely when the four
  stores move and the empty directories are removed. That is the intended
  anti-shell behavior.
- A v1 repository with `docs/archive/` keeps `docs/archive/` where it is. The
  manifest records `"archive": "docs/archive"`. `docs/` is not an empty shell;
  it holds the archive.
- Link candidates are reported by the CLI and rewritten by the
  `migrating-fleet-context` skill. The fixture test counts links before and
  after, asserts equal counts, and asserts every rewritten target resolves.

### 9.6 Rollback

The playbook creates a dedicated branch and the tool creates an annotated
backup tag. Rollback has three shapes:

| When | How |
|---|---|
| before commit, same branch | `git restore --source=<tag> --staged --worktree :/` for tracked paths and remove the new untracked store root, or switch back to the original branch |
| after commit, before merge | switch back to the original branch; the feature branch and tag remain as the record |
| after merge | revert the migration commit on a new branch; the backup tag still names the pre-migration tree |

`git reset --hard <tag>` is documented only as a last resort for a tree that
contains nothing but the migration, because it discards every uncommitted
change. The preferred path is branch isolation, which never needs it.

A `migrating` manifest with nothing moved is not a rollback case: `agents layout
migrate --abort --apply` removes it (§9.4), and the backup tag stays. A rollback
that leaves a `migrating` manifest behind is not a rollback.

## 10. Testing strategy

| Area | Tests | What fails without it |
|---|---|---|
| Resolution | implicit v1; v2 map; scattered map; template defaults | A layout shape with no resolver |
| Validation | V1–V16, including duplicate JSON keys, `..`, absolute paths, symlink components, `.agents/` stores, overlap, case-only collisions, and V16 across `/.agents/` in `info/exclude`, `/.agents` without a slash, `/.agents/**`, a tracked `.gitignore` entry, and a git error (which refuses mutation) | A manifest that redirects a write outside the repo, onto another store, or into a clone that cannot see it |
| Version | a simulated older manifest-aware version vs `min_mut_ver_floor: 0.6.0` refuses; `v0.6.0` allows; `dev` refuses mutation but reads; v1 positive control always allows | Silent mutation by an older manifest-aware binary |
| Router/digest | v1 stays `current`; v2 router accepted; v1 router in a v2 repo is `known_legacy`; each skill's canonical text is selected by the resolved layout; each frozen v1 asset is pinned by digest; each layout's catalog contains the other layout's canonical text | The 2026-09-01 fleet-wide `diverged` accident, a v1 router reported as drift, or a v1 repository turned non-current by a v2-only asset |
| Currency/health | `drift` is strict currency (`current` only) while `doctor` reports owner-based health for `known_legacy`/`diverged`/`missing`; the two results are allowed to differ | A tool “fixing” the divergence by teaching the currency predicate about ownership, or about which layout it is looking at, instead of handing it the canonical digest for that layout (§0.8) |
| Drift | v1 fields unchanged plus new fields; v2 stores/min_mut_ver_floor/status; `unsupported`; misplaced only inside declared stores; archive excluded | A report that cannot be acted on, or a v1 behavior change |
| Template | every CLI template expands to a valid map (V1–V16); `--stores` overrides per role; no template/custom requires all four; the manifest has no `profile` field | A template that writes an invalid layout, or a profile field that reappears in the manifest |
| Doctor | `layout:manifest` for v1/active/migrating/unsupported/invalid; `layout:stores`; `layout:qna` at `docs/qna` and `.context/qna`; `layout:tracked` for tracked, untracked-not-ignored, ignored manifest, and ignored store | A v2 repository with no health check, or a manifest that will not reach a clone |
| Init | v1 unchanged; v2 no-op on an intact repo; v2 creates only manifest-declared missing stores; `--local` + v2 refused | A docs shell reappearing |
| Fleet update | unsupported/migrating skipped before wiring and refresh; skill bytes unchanged; v1 positive control wired and refreshed; supported v2 wired and refreshed | A fleet command downgrading a v2 repository's skill |
| Migration plan | preconditions (clean tree, no git operation in progress, branch, router, stores, trackedness), blockers (`archive_not_in_source`, `docs_residue`), scattered target, archive recording, link candidates | A migration that plans an unsafe move, drags the archive along, or leaves a `docs/` shell |
| Migration apply/resume | `git mv`; no copies; N in / N out; blob identity; the enumerated crash matrix (4 moves × 4 instants, plus the `moved`, `pruned`, and `router` boundaries, plus one non-prefix case); a digest mismatch, both-present, neither-present, and the equal-digest copy case each refused with a named remedy; `--abort` only while nothing has moved | Data loss, an unresumable half-migration, or a tool that guesses |
| Prose | v2 router names no store path; the v2 recording asset names no `docs/`; the v1 assets must name the v1 paths; the v2 recording text's fallback resolves `docs/qna` on a v1 fixture; migration skill names the new commands, states, and the `--local` stop; README block current; agent-facing skill covers new commands | A "hollow" prose deliverable, the journal's recorded failure, or a v2 text that is wrong on v1 |
| Attributes | `git check-attr linguist-generated` keeps `.agents/layout.json` visible and stores ungenerated | A manifest hidden from PR review by `.agents/**` |

All Go test invocations in the plan use `-count=1`. The repository already
records why: tests that read tracked non-Go files do not invalidate a cached
pass, and a probe that never ran looks green.

## 11. Risks

| Risk | Mitigation |
|---|---|
| An old binary touches a v2 repo | Deploy-before-flip gate (§6.3); v0.6.0's mutation guard; §6.4 repair path; paperbubble is the only v2 repository in round 1 |
| A changed asset makes an unmodified copy report `diverged` | The canonical text is selected by the resolved layout and each layout's catalog contains the other layout's canonical text (§0.8); each frozen v1 asset is pinned by digest |
| `agents update --all --apply` refreshes a skill in an unsupported repo | The manifest gate runs before wiring and before `RefreshInfrastructuralSkills`; negative test asserts byte-identical skill |
| `--local` repo silently loses its manifest on clone | V16 asks git (`git check-ignore --no-index`) instead of reading one exclude file, the same check runs as a pre-flight before the manifest exists, and an undeterminable answer refuses mutation (§0.7); `init --local --template` refuses |
| The frozen v1 asset is edited by accident | Digest pin plus a test asserting the asset bytes equal the recorded v0.5.1 constant, and a prose gate that requires the v1 paths in the v1 asset (§0.8) |
| A migration reports success while a `docs/` shell survives | `docs_residue` is a planner blocker and an apply-time report; the fixture asserts `docs/` is gone |
| The deploy-before-flip prerequisite is invisible to the person upgrading | The GitHub Release body is the authoritative carrier: the release change set writes `.github/release-notes/<version>.md` and `release.yml` passes it as `body_path`, so a missing or empty notes file fails the release instead of shipping an empty body; `agents/README.md` gains an "Upgrading to v0.6.0" section as the durable repo-side copy. The notes name the gate and the not-ready repositories (§0 row 7) |
| Archive gets moved by a well-meaning migration | The archive is recorded and excluded from move lists and walks, `archive_not_in_source` blocks an archive nested in a move source, and a fixture asserts its blobs are unchanged |
| Vault content is scanned as if it were docs | v2 misplacement walks declared stores, not the whole vault |
| Resume guesses after a crash | A frozen plan, filesystem truth, and a per-move content identity; every mismatch refuses and names its remedy; no `--force` (§0.5) |
| The migration is split across commits | `agents save` is not used; the playbook stages the manifest, stores, router, and prose together |
| `docs_stores` consumers break | The field is retained for both layouts and removed no earlier than v0.8.0 |

## 12. Approval gates

No implementation starts until:

1. this design, its implementation plan, and its migration playbook are
   reviewed and **approved by a human**; the review queue Q1–Q10 has recorded
   resolutions (all ten, 2026-09-18) and this approval is what confirms §0;
2. the plan is approved for execution;
3. v0.6.0 is released, and every fleet machine is confirmed to resolve that
   binary on `PATH`;
4. the paperbubble dry run is presented and the human approves the `--apply`.

Until then, paperbubble stays frozen and no fleet repository is migrated.
