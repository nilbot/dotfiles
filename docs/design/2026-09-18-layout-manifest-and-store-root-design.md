# Design: Layout Manifest and Store-Root Freedom for Agent Context (`agents.layout/v2`)

**Date:** 2026-09-18
**Status:** **Proposed — awaiting review.** No implementation, release, or
repository migration may start until this document, its implementation plan,
and its migration playbook are approved.
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

## 0. Decisions needing confirmation

This design recommends an answer to each open question. The plan and playbook
are written against the recommendations below. If a recommendation changes,
the design, plan, and playbook all change before implementation starts.

| # | Question | Recommendation | Why |
|---|---|---|---|
| 1 | Canonical `stores` form | **Role→path map is canonical.** `store_root` + a names array is an accepted input shorthand; the CLI always expands it. When both are present, the map wins and `store_root` must be a common prefix of every store. | One shape to parse. A shorthand that survives on disk means two parsers and an ambiguity rule; an input shorthand has neither. `store_root` is still persisted when known, because it is the human-readable summary and the natural scan boundary for misplaced-document detection. |
| 2 | `content_root` for `content-vault` | **Yes, with default `.`.** Emitted explicitly by `content-vault`; optional for the other profiles. It is semantic (what is content, what is meta), not a directory the tool creates. | It gives the recording and migration skills a machine-readable statement that the vault is the content and the stores are meta. Without it, "content-vault" is a label with no referent. |
| 3 | Profile names and release number | Profiles `code-repo`, `content-vault`, `custom`; schema `agents.layout/v2`; **one release, v0.6.0, containing manifest parsing, the guard, write support, and migration.** | The guard and the `min_mut_ver_floor` version floor are code that ships once. The below-floor refusal is tested by injecting an older version into the new binary, not by releasing one. A single release plus a deploy-before-flip gate is sufficient for a fleet upgraded together, and it avoids maintaining two release paths for one feature. |
| 4 | Migration command and dry-run format | `agents layout migrate`, dry run by default and `--dry-run` accepted explicitly, `--apply` to execute, `--resume` to continue, `--json` for machines. Human output is a line-oriented plan (`move`, `keep`, `blocker`, `links`) ending in `N move, M keep, K blocked, L link(s)`. | Matches `agents update --all [--apply]`'s "dry run unless applied" convention, while keeping the invocation the plan already writes (`--dry-run`). The line-oriented form is reviewable in a terminal and diffable in a PR. |
| 5 | Archive handling during migration | **The archive is never moved and never rewritten.** The manifest records its existing path (`archive`), defaulting to `docs/archive` when that directory exists in a v1 repo. A `docs/` left holding only the archive is not a shell. | `.agents/AGENTS.md` declares `docs/archive/` strictly immutable, and the 2026-09-01 journal records what happens when a migration is ordered to move files out of it. Keeping the archive in place is the only policy that needs no exception. Moving the archive wholesale is a separate future operation, not part of v2. |
| 6 | `agents init --local` repositories | **v2 requires a tracked `.agents/`.** `--local` plus a v2 profile is refused; the manifest would otherwise be machine-local and a clone would silently fall back to v1. | `.agents/layout.json` is repository state shared by every clone. A manifest inside a git-excluded directory cannot be that. |
| 7 | The other five repositories' `recording-what-you-learn` copies in v0.6.0 | **Do not touch those repositories.** Add the old asset text to the legacy digest catalog so they report `clean_legacy`, never `customized`. `agents drift --all` still exits 1 until each repository migrates; that is accepted and named in the release notes. | The old skill is still correct for a v1 repository; the only cost is an advisory. Without the legacy digest, the same state would be indistinguishable from a local customization — the 2026-09-01 accident. This row does not migrate any repository in the first round. |

### 0.1 Review queue (open)

These questions came from the first read of this document on 2026-09-18. They
are recorded here rather than resolved one by one, because several of them can
invalidate the recommendations above. Until they are answered or explicitly
dropped, §0 is provisional.

| # | Anchored to | Question | Status |
|---|---|---|---|
| Q1 | §0 row 1, §4 | Is one `store_root` and one meta root the right model, or can roles have different roots and different collaboration policies? Concrete example: `docs/{qna,journal}` for human collaboration and `docs/agents/{design,plans}` for almost-exclusively agent writes. | open |
| Q2 | §0 row 2 | Is `content_root` needed at all? `content-vault` may already carry the semantic meaning, and `.context` is inside any content root by construction. | open |
| Q3 | §0 row 3 | Do we need future profiles such as `code-repo-v2` for backwards compatibility, and what is the profile-evolution rule? | open |
| Q4 | §9.4 | Is `--resume` really a linear list progression? The full state machine is not written down: partial directory moves, failure between `git mv` and the manifest update, and non-linear recovery all need explicit states and transitions. | open |
| Q5 | §0 row 5, §9.5 | Does archive immutability depend on whether the archive holds meta or more explicit knowledge? What is the rule when the archive mixes both? | open |
| Q6 | §0 row 6, §7.5 | For `--local` repositories: where are docs stored and tracked? How does `agents` determine trackedness? If docs are tracked while `.agents/` is not, how are skill writes to tracked docs governed? Is `.gitignore` a projection of `layout.json`, or is the manifest a projection of ignore state? | open |
| Q7 | §0 row 7 | Why is fleet-wide `recording-what-you-learn` staleness after v0.6.0 a problem at all? The old skill is still correct for a v1 repository, and the legacy digest already prevents `customized`. What decision does this row actually ask for? | open — row 7 reframed; confirm whether the advisory is acceptable |

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

**Every changed asset carries a legacy digest.** Changing an embedded asset
without adding its old text to the legacy digest catalog made every
already-migrated repository report `customized` and exit drift 1. A layout
release touches the router and both bundled skills. Every asset change must
carry a legacy digest, in the same release, gated by a test.

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
  The only v1 output changes are the additive drift fields in §7.3 and the
  doctor check rename in §7.4.
- Deploy-before-flip compatibility: one release, verified on every machine
  before any repository flips.
- Validation strict enough that a malformed manifest cannot redirect a write
  outside the repository, into `.agents/`, or onto another store.
- Migration that uses `git mv`, is resumable, never copies, never writes into
  or out of the archive, and leaves a rollback point.
- Skills and documentation that speak in roles, not paths.
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
| **store_root** | Optional common parent of the four stores, used as an input shorthand and persisted as a summary. |
| **content_root** | The repository-relative root of the repository's own content. Defaults to `.`. Semantic, not created. |
| **manifest** | `.agents/layout.json`. Declares schema, the `min_mut_ver_floor` version floor, profile, status, and role→path mapping. |
| **v1** | The legacy layout: no manifest, stores at `docs/{design,plans,journal,qna}`. |
| **v2** | The manifest layout defined here. |
| **inspection** | Parse, validate, and report the layout without writing it. |
| **mutation** | Create, change, or migrate the layout. Both are code paths in v0.6.0, not separate releases. |
| **archive** | An immutable history directory, conventionally `docs/archive/`. Recorded in the manifest; never moved or rewritten by migration. |

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
| `profile` | string | yes in v2 | `code-repo`, `content-vault`, or `custom`. Selects defaults and the vocabulary the skills use; it never changes validation. |
| `layout_status` | string | yes in v2 | `active` or `migrating`. A `migrating` manifest permits only `agents layout migrate --resume --apply`; every other write is refused and every read reports the state. |
| `stores` | object or array | yes in v2 | The role→path map (canonical), or an array of role names to be joined onto `store_root` (input shorthand). |
| `store_root` | string | required with the array form; optional with the map form | Repository-relative common parent of the stores. With a map, it must be a lexical prefix of every store path; it is a summary, never an override. |
| `content_root` | string | optional, default `.` | Repository-relative root of the repository's own content. Emitted explicitly by `content-vault`. |
| `archive` | string | optional | Repository-relative immutable history directory. Defaults to `docs/archive` when that directory exists in a v1 repository. Never a move source or destination. |
| `migration` | object | required while `layout_status` is `migrating` | The resumable journal: `from`, `started_at` (RFC 3339), and `moves` (`from`, `to`, `role`, `state`). Removed when the layout becomes `active`. |

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
  "profile": "content-vault",
  "layout_status": "active",
  "content_root": ".",
  "store_root": ".context",
  "stores": {
    "design": ".context/design",
    "plans": ".context/plans",
    "journal": ".context/journal",
    "qna": ".context/qna"
  }
}
```

`paperbubble` has no `docs/archive/`, so `archive` is absent.

The input shorthand that produces the same map:

```json
{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
  "profile": "content-vault",
  "layout_status": "active",
  "content_root": ".",
  "store_root": ".context",
  "stores": ["design", "plans", "journal", "qna"]
}
```

The CLI always emits the map form. A hand-written shorthand is accepted; a
hand-written map with a contradicting `store_root` is rejected.

The scattered form is for a repository whose stores do not share a parent:

```json
{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
  "profile": "custom",
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

- `.agents/layout.json` — profile, status, and one path per role
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
   `docs/{design,plans,journal,qna}`, `content_root="."`, and
   `archive="docs/archive"` when that directory exists.
2. If it exists, parse it. A JSON syntax error is an error. An unknown `schema`
   resolves successfully but is unsupported for mutation, so the CLI can still
   display what it found and refuse to touch it.
3. Normalize `stores`:
   - object form → use the role→path map directly;
   - array form → join each name onto `store_root`.
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
| V3 | `role_duplicate` | A role appears twice in the array form, or a role appears twice in the JSON object (detected at token level, not silently last-wins). |
| V4 | `role_duplicate_path` | Two roles resolve to the same path. |
| V5 | `path_empty` | A store path is empty. |
| V6 | `path_absolute` | A store path is absolute. |
| V7 | `path_escapes` | A store path contains `..` or resolves outside the repository root. |
| V8 | `path_symlink` | The store path, or any existing component of it, is a symlink. |
| V9 | `path_in_agents` | The store is `.agents/` or is under it. Configuration may live there; knowledge may not. |
| V10 | `path_overlap` | Two stores are equal, or one is a path-prefix of the other. |
| V11 | `path_case_collision` | Two store paths differ only by case. Enforced on every filesystem, not only on case-insensitive ones, so a repository does not become unportable. |
| V12 | `store_root_mismatch` | `store_root` is present with the map form and is not a lexical prefix of every store path. |
| V13 | `archive_overlap` | `archive` equals a store, contains a store, or is contained by a store. |
| V14 | `profile_unknown` | `profile` is not `code-repo`, `content-vault`, or `custom`. |
| V15 | `status_unknown` | `layout_status` is not `active` or `migrating`. |
| V16 | `migration_missing` | `layout_status` is `migrating` and the `migration` journal is absent or malformed. |
| V17 | `min_mut_ver_floor_invalid` | `min_mut_ver_floor` is not a parseable `MAJOR.MINOR.PATCH` version. |
| V18 | `local_agents` | A v2 manifest is present in a repository whose `.agents/` is git-excluded or otherwise untracked. |
| V19 | `content_root_invalid` | `content_root` is absolute, contains `..`, escapes the repository, or is a symlink. It may contain stores; it is a scope, not a store. |

Case-insensitive collision detection is deliberately stronger than the
filesystem. A layout that works on macOS and fails after a clone to Linux is a
portability bug the tool can refuse cheaply.

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
- the skill changes: role-based `recording-what-you-learn` and v2-aware
  `migrating-fleet-context`, each with a legacy digest for its previous text.

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
| **v0.6.0** | Correct v1 behavior. `drift` reports v1 and, when entities are stale, advises the migration skill. No automatic migration. | Full read/write support: `layout show\|validate\|path\|migrate`, layout-aware `init`, guarded fleet `update`, v2-aware skills. |
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
agents layout migrate --profile <p> [--store-root <path> | --stores <role=path> ...]
                       [--content-root <path>] [--archive <path>]
                       [--dry-run | --apply | --resume --apply]
                       [--backup-tag <name>] [--json]
```

v0.6.0 ships all four subcommands and the layout flags on `agents init`.

`show` prints the resolved layout: schema, profile, status, minimum mutating
version (`min_mut_ver_floor`), content root, archive, and one line per role. `--json` emits the normalized
`Layout` object (one role→path map, never the raw shorthand). `--router` prints
the exact canonical router for the resolved layout and nothing else, so the
migration skill can restore it without embedding a second copy.

`validate` runs V1–V19 and prints one line per problem with the manifest path
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
- `--backup-tag <name>` is required with every non-resume `--apply`. The tool
  creates the annotated tag at HEAD before the first write, so the rollback
  point exists even if the operator forgot to create a branch.
- `--profile` and one of `--store-root`/`--stores` are required on the first
  `--apply` and on `--dry-run` for a v1 repository.
- `--json` emits one object: `repo`, `dry_run`, `from`, `to`, `router`,
  `archive`, `moves`, `link_candidates`, `blockers`, `counts`.
- Exit codes: 0 applied and no link candidates; 1 dry-run plan ready, or
  applied with link candidates, or blockers; 3 malformed flags; 4 not a
  repository with `.agents/`; 5 apply failed mid-way and the manifest remains
  `migrating` for resume.

### 7.3 Drift report changes

`DriftReport` keeps every existing field and adds:

| Field | v1 value | v2 value |
|---|---|---|
| `layout_version` | `v1` | `v2` or `unknown` |
| `profile` | `""` | manifest value |
| `min_mut_ver_floor` | `""` | manifest value |
| `layout_status` | `active` | `active` or `migrating` |
| `stores` | `docs/{design,plans,journal,qna}` | resolved role→path map |
| `unsupported` | `""` | `unknown_schema`, `below_floor`, `unreleased`, `invalid`, or `""` |
| `unsupported_detail` | `""` | human-readable reason |

`docs_stores` remains, populated with role presence for both versions, and is
deprecated. It is removed no earlier than two minor releases after v0.6.0 (not
before `v0.8.0`), and the release notes must name the removal.
`misplaced_docs` keeps its meaning. For v2 it walks the declared stores and, when
`store_root` is present, that root, excluding the archive. It does not walk
arbitrary vault content: a `*-plan.md` note in a vault is not an agents plan
artifact. v1 continues to walk `docs/` exactly as today.

A report is clean only when `unsupported` is empty, `layout_status` is
`active`, all four roles resolve to existing directories, the router matches
the canonical router for that layout, and there are no misplaced live
documents.

### 7.4 Doctor changes

| Check | Statuses |
|---|---|
| `layout:manifest` | `ok` for implicit v1 and active supported v2; `warn` when `migrating`, below `min_mut_ver_floor`, or unknown schema; `fail` for an invalid manifest |
| `layout:stores` | `ok` when all four roles resolve to directories; `warn` with the missing role and path otherwise |
| `layout:qna` | the freshness indicator, resolved through the `qna` role instead of hardcoded `docs/qna` |

`docs:qna` is removed; the rename is named in the v0.6.0 release notes.

### 7.5 Init and update

`agents init` keeps its v1 behavior when no layout flag and no manifest is
present. In v0.6.0 it also accepts `--profile`, `--store-root`, `--content-root`,
`--stores`, and `--archive`; any of them creates a v2 layout. On a v2
repository with an active supported manifest, `init` is a no-op for an intact
layout and creates only missing *manifest-declared* stores. It never creates
`docs/{design,plans,journal,qna}` in a v2 repository.

If a manifest is absent but a v1 layout already exists (`AGENTS.md` or `docs/`
is present), layout flags are refused with `agents layout migrate` as the
remedy. Adopting an existing repository is the migration command's job, because
it must move the stores and prove the router is boilerplate. Creating a v2
layout is a mutation and requires a running version that supports it; an
unstamped `dev` build refuses, per §5.3.

`agents update --all --apply` reads each repository's manifest before wiring or
refreshing. Unsupported or `migrating` repositories are skipped, named, and
left byte-for-byte unchanged. Supported repositories are wired and their
`migrating-fleet-context` skill refreshed.

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
5. this design's catalog entry and any affected Q&A.

## 8. Router, digest catalog, and skills

### 8.1 Two canonical routers

`scaffold.DefaultAgentsMD` remains the v1 canonical router, byte-for-byte.
`scaffold.V2AgentsMD` is the manifest-pointing router in §4.4. `drift` selects
the canonical digest by the resolved layout:

- v1 → `DefaultAgentsMD`;
- v2 → `V2AgentsMD`.

The legacy router catalog keeps every historical v1 template and adds
`DefaultAgentsMD` itself. That makes a v2 repository still carrying the v1
router classify as `clean_legacy` — known boilerplate, safe to replace — while
a v1 repository continues to classify as `clean_current`. No v1 repository
changes state.

### 8.2 Skill digest catalog

v0.6.0 changes both embedded skills in the same release, so both old versions
enter the legacy digest catalog:

- `recording-what-you-learn`: keep the existing 2026-08-20 legacy entry and add
  the v0.5.1 text as a second legacy entry;
- `migrating-fleet-context`: add the v0.5.1 text as its first legacy entry.

Without those entries, every repository reports `customized` and drift exits 1
on a state that is actually `clean_legacy`. The journal records that exact
accident. The plan gates this with a test that the old asset bytes digest to a
legacy value.

### 8.3 `recording-what-you-learn`

The skill becomes role-based. It resolves paths with `agents layout path qna`
and `agents layout path journal`, falling back to reading
`.agents/layout.json` when the binary is absent, and to the v1 defaults
(`docs/qna`, `docs/journal`) only when neither exists and the repository still
has a `docs/` directory. It contains no hardcoded `docs/` path in a v2-capable
sentence. Prose gate: a test fails if `docs/` appears in the embedded skill.

The skill is user-owned and is never refreshed by `agents update`. A
repository receives the new version through `agents init` on a fresh clone or
through the migration skill's `clean_legacy` replacement, after reviewing any
local edits.

### 8.4 `migrating-fleet-context`

The skill reads the manifest before acting:

| Layout state | Action |
|---|---|
| no manifest (v1) | current procedures, unchanged |
| v2, `active`, supported | no layout migration; only link rewrites and domain-prose reconciliation |
| v2, `migrating` | stop; tell the human to run `agents layout migrate --resume --apply` |
| v2, unsupported | stop; the binary is older than `min_mut_ver_floor` |
| unknown schema | stop; no guessing |

It names `agents layout show`, `agents layout migrate --dry-run`, `--apply`,
`--resume`, and `agents layout path`; it distinguishes the four router states
as before; it treats the archive as immutable; and it produces the same
traceability table before the approval gate.

The skill keeps its existing pasted v1 router for the v1 path. That block is
still the correct canonical text for a repository with no manifest, and the
existing test binding it to `scaffold.DefaultAgentsMD` stays meaningful. The v2
path does not paste a router at all: it restores the exact bytes from
`agents layout show --router` after the tool has classified the old router as
`clean_current` or `clean_legacy`.

### 8.5 Domain context

`.agents/AGENTS.md` is user-owned and stays semantic: what the content is, what
each role is for, and which conventions apply. It does not name physical store
paths. The starter template is updated in v0.6.0; existing repositories keep their
own file and are corrected by the migration skill's prose reconciliation, not
by a deterministic overwrite.

## 9. Migration mechanics

### 9.1 Preconditions

`agents layout migrate` plans only when:

- the target is a git worktree with `.agents/`;
- the working tree is clean (`git status --porcelain` empty);
- the current branch is not `master` or `main`;
- the root router is `clean_current` or `clean_legacy`, so replacing it with
  the v2 router is provably boilerplate-only. A `drifted` or `missing` router
  is a blocker: the `migrating-fleet-context` skill reconciles it first;
- every one of the four source stores exists as a real directory, not a
  symlink. A missing store is a blocker, with `agents init` as the remedy; the
  migration itself never creates a store, because "create the missing one" and
  "copy the existing one" are the same kind of operation and the second is
  forbidden;
- the running binary reports `Support(targetLayout)` true. A binary below
  `min_mut_ver_floor`, or an unstamped `dev` build, plans but refuses to apply;
- no target path exists, except the resumable "already moved" case in §9.4;
- the computed layout passes V1–V19;
- `.agents/` is tracked (Decision 6).

### 9.2 Plan and dry run

The planner reads the v1 layout, resolves the requested v2 layout, and produces
one action per store directory — the whole directory, including its `README.md`
and any nested content. It reports, but never performs, link candidates.

Human output:

```
layout migrate (dry run) — /Users/nilbot/gist/paperbubble
  from    agents.layout/v1  docs/{design,plans,journal,qna}
  to      agents.layout/v2  profile=content-vault  store_root=.context
  router  clean_current -> canonical v2 (deterministic swap)
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

  apply   agents layout migrate --profile content-vault --store-root .context \
            --apply --backup-tag pre-layout-v2-20260918
```

`--json` carries the same information as one object. `moves[].role` is present
so the skill can reason in roles.

### 9.3 Apply

1. Create the required annotated `--backup-tag` at HEAD.
2. Write `.agents/layout.json` with `layout_status: "migrating"` and the full
   move list, `state: "pending"`, before touching any store.
3. For each move, `git mv` the source to the destination. If the source is a
   directory, `git mv` moves the directory and its README together. After each
   successful move, rewrite the manifest with that move `state: "done"`.
4. Remove now-empty source directories with `os.Remove`. Never remove a
   directory that still has entries. Never remove the archive.
5. Replace a clean `AGENTS.md` with `V2AgentsMD`.
6. Rewrite the manifest with `layout_status: "active"` and without the
   `migration` journal.
7. Report link candidates. The layout is complete; the links are not. If any
   candidate exists, exit advisory and name the migration skill as the next
   step.

No step copies a file. `git mv` is the only mechanism, so history and blob
identity are preserved and `git status` shows renames.

### 9.4 Resume

`--resume --apply` reads a `migrating` manifest. For every move it compares the
filesystem against the journal:

| Source | Destination | Meaning | Action |
|---|---|---|---|
| present | absent | pending | move it |
| absent | present | done | mark done, continue |
| present | present | ambiguous | refuse; name both paths |
| absent | absent | lost or already reconciled | refuse; name both paths |

The filesystem is the truth. The `state` field is a hint that makes the common
case fast and the crash window small. Resume never guesses and never copies.

### 9.5 Archive, links, and `docs/` removal

- The archive path is recorded in the manifest and excluded from every walk,
  every move list, and every link rewrite.
- Migration never moves a file out of the archive and never writes into it.
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

## 10. Testing strategy

| Area | Tests | What fails without it |
|---|---|---|
| Resolution | implicit v1; v2 map; v2 shorthand; scattered map; `store_root` agreement | A layout shape with no resolver |
| Validation | V1–V19, including duplicate JSON keys, `..`, absolute paths, symlink components, `.agents/` stores, overlap, case-only collisions | A manifest that redirects a write outside the repo or onto another store |
| Version | a simulated older manifest-aware version vs `min_mut_ver_floor: 0.6.0` refuses; `v0.6.0` allows; `dev` refuses mutation but reads; v1 positive control always allows | Silent mutation by an older manifest-aware binary |
| Router/digest | v1 stays `clean_current`; v2 router accepted; v1 router in a v2 repo is `clean_legacy`; every changed asset has a legacy digest | The 2026-09-01 fleet-wide `customized` accident, or a v1 router reported as drift |
| Drift | v1 fields unchanged plus new fields; v2 stores/profile/min_mut_ver_floor/status; `unsupported`; misplaced only inside declared stores; archive excluded | A report that cannot be acted on, or a v1 behavior change |
| Doctor | `layout:manifest` for v1/active/migrating/unsupported/invalid; `layout:stores`; `layout:qna` at `docs/qna` and `.context/qna` | A v2 repository with no health check |
| Init | v1 unchanged; v2 no-op on an intact repo; v2 creates only manifest-declared missing stores; `--local` + v2 refused | A docs shell reappearing |
| Fleet update | unsupported/migrating skipped before wiring and refresh; skill bytes unchanged; v1 positive control wired and refreshed; supported v2 wired and refreshed | A fleet command downgrading a v2 repository's skill |
| Migration plan | preconditions, blockers, scattered target, archive recording, link candidates | A migration that plans an unsafe move |
| Migration apply/resume | `git mv`; no copies; N in / N out; blob identity; crash fixture at every move boundary; both/neither ambiguity refused | Data loss or an unresumable half-migration |
| Prose | v2 router names no store path; recording skill names no `docs/`; migration skill names the new commands and states; README block current; agent-facing skill covers new commands | A "hollow" prose deliverable, the journal's recorded failure |
| Attributes | `git check-attr linguist-generated` keeps `.agents/layout.json` visible and stores ungenerated | A manifest hidden from PR review by `.agents/**` |

All Go test invocations in the plan use `-count=1`. The repository already
records why: tests that read tracked non-Go files do not invalidate a cached
pass, and a probe that never ran looks green.

## 11. Risks

| Risk | Mitigation |
|---|---|
| An old binary touches a v2 repo | Deploy-before-flip gate (§6.3); v0.6.0's mutation guard; §6.4 repair path; paperbubble is the only v2 repository in round 1 |
| A changed asset makes the fleet report `customized` | v0.6.0 adds legacy digests for both changed skills in the same release, gated by tests |
| `agents update --all --apply` refreshes a skill in an unsupported repo | The manifest gate runs before wiring and before `RefreshInfrastructuralSkills`; negative test asserts byte-identical skill |
| `--local` repo silently loses its manifest on clone | V18 rejects v2 in a repo whose `.agents/` is excluded; `init --local --profile` refuses |
| Archive gets moved by a well-meaning migration | The archive is recorded, excluded from move lists and walks, and a fixture asserts its blobs are unchanged |
| Vault content is scanned as if it were docs | v2 misplacement walks declared stores and `store_root`, not the whole vault; `content_root` is declared but not scanned for artifacts |
| Resume guesses after a crash | Filesystem truth table; both/neither present refuses and names both paths |
| The migration is split across commits | `agents save` is not used; the playbook stages the manifest, stores, router, and prose together |
| `docs_stores` consumers break | The field is retained for both layouts and removed no earlier than v0.8.0 |

## 12. Approval gates

No implementation starts until:

1. this design, its implementation plan, and its migration playbook are
   reviewed and the seven decisions in §0 are confirmed or changed;
2. the plan is approved for execution;
3. v0.6.0 is released, and every fleet machine is confirmed to resolve that
   binary on `PATH`;
4. the paperbubble dry run is presented and the human approves the `--apply`.

Until then, paperbubble stays frozen and no fleet repository is migrated.
