---
name: migrating-fleet-context
description: Safely migrates repository agent context from legacy or drifted structures to the Two-Tier Agent Context architecture (Tier 1 router + Tier 2 domain guidelines + 4-store docs layout) using LLM semantic merge on a dedicated feature branch.
---

# Migrating Fleet Context

Migrates repository agent context from legacy single-file or drifted structures to the **Two-Tier Agent Context Architecture** with **4-store documentation layout**.

## Why this skill exists

Earlier iterations of repository scaffolding treated `AGENTS.md` (or `CLAUDE.md`) as a single monolithic context file. Over time, repositories accumulated human-authored domain rules, test protocols, and language conventions mixed directly with machine wiring and router instructions.

Deterministic tools (`agents init`, `agents update`) cannot safely disentangle reordered, edited, or appended domain rules from router boilerplate without risking silent deletion of repository guidelines. Conversely, leaving legacy files unmaintained freezes repositories on obsolete scaffold conventions.

This skill provides an authoritative, LLM-in-the-loop migration protocol to safely partition context into two isolated tiers:
1. **Tier 1 (Root Router)**: Standardized root `AGENTS.md` (and relative symlink `CLAUDE.md -> AGENTS.md`) acting solely as a durable docs and machine wiring pointer.
2. **Tier 2 (Domain Context)**: Dedicated repository-specific engineering guidelines, test mandates, architectural invariants, and safety constraints in `.agents/AGENTS.md`.
3. **4-Store Documentation**: Durable repository knowledge in `docs/` partitioned across `design/`, `plans/`, `journal/`, and `qna/`.

---

## When to Run

Run this skill when:
- `agents doctor` reports warnings or info for any `scaffold:*` check (`scaffold:router`, `scaffold:symlink`, `scaffold:domain`, `scaffold:skill-recording`, `scaffold:skill-migrating`).
- `agents drift` or `agents drift --json` detects `router_state: clean_legacy`, `router_state: drifted`, `domain_state: missing`, or `misplaced_docs`.
- Migrating a repository from legacy monolithic `AGENTS.md` or `CLAUDE.md` to Two-Tier context.
- Realigning misplaced plan or design documents across `docs/`.

---

## Invariants & Safety Constraints

1. **Zero Rule Dropping**: Every domain guideline, architectural rule, test mandate, commenting standard, and safety constraint in the existing files MUST be preserved in `.agents/AGENTS.md`. Never discard domain context during migration.
2. **Feature Branch Isolation**: Never perform migrations directly on `master`, `main`, or protected branches. Always execute on a dedicated feature branch (`feat/migrate-agent-context` or `feat/two-tier-context-migration`).
3. **Deterministic Preflight Cleanliness**: Require a clean working tree (`git status --porcelain`) before modifying any files.
4. **Canonical Router Exactness**: Root `AGENTS.md` must match the canonical Tier 1 router template verbatim.
5. **Relative Symlink**: `CLAUDE.md` must be a relative symlink pointing to `AGENTS.md` (`CLAUDE.md -> AGENTS.md`).
6. **Archive Immutability**: Active work and modern plans belong in `docs/plans/`. Never write new plans or active context to `docs/archive/`.

---

## Phased Migration Workflow

Follow all five phases sequentially. Do not skip phases or verification gates.

```
Phase 1: Deterministic Preflight & Assessment
  │  (agents drift --json, git status --porcelain)
  ▼
Phase 2: Feature Branch Isolation
  │  (git checkout -b feat/migrate-agent-context)
  ▼
Phase 3: Semantic Un-Nesting & Partitioning
  │  (extract domain rules -> .agents/AGENTS.md,
  │   restore root AGENTS.md router, relink CLAUDE.md)
  ▼
Phase 4: Docs Store Realignment & Skill Refresh
  │  (relocate misplaced plans -> docs/plans/,
  │   ensure 4 docs stores & bundled skills)
  ▼
Phase 5: Automated Verification & Diagnostic Gate
     (agents drift, agents doctor, repo test suite, commit)
```

---

### Phase 1: Deterministic Preflight & Assessment

1. **Verify working tree cleanliness**:
   ```bash
   git status --porcelain
   ```
   If untracked or uncommitted changes exist, stop immediately and report to the user or commit/stash before proceeding.

2. **Inspect repository drift state**:
   ```bash
   agents drift --json
   ```
   Evaluate the JSON output fields:
   - `router_state`: `clean_current`, `clean_legacy`, `drifted`, or `missing`.
   - `symlink_state`: `ok`, `not_symlink`, `broken`, or `missing`.
   - `domain_state`: `ok` or `missing`.
   - `skills`: status of bundled skills (`recording-what-you-learn`, `migrating-fleet-context`).
   - `docs_stores`: presence of `design`, `plans`, `journal`, `qna`.
   - `misplaced_docs`: list of files needing relocation (e.g. `*-plan.md` in `docs/journal/`).
   - `diff`: unified diff highlighting custom additions in root `AGENTS.md`.

---

### Phase 2: Feature Branch Isolation

1. Check current branch:
   ```bash
   git branch --show-current
   ```
2. If on `master`, `main`, or any protected branch, create and switch to a dedicated migration branch:
   ```bash
   git checkout -b feat/migrate-agent-context
   ```

---

### Phase 3: Semantic Un-Nesting & Partitioning

1. **Inspect and Read Root Context**:
   Read root `AGENTS.md` (and `CLAUDE.md` if it is a regular file rather than a symlink).

2. **Disentangle Domain Knowledge from Router Boilerplate**:
   Identify and separate:
   - **Router Boilerplate (to be replaced by canonical router)**:
     - Old pointer tables to `docs/` or `.agents/memory/`.
     - Outdated `agents doctor` single-line instructions.
     - Retired commands (such as legacy handoff, review, index, or memory tools).
   - **Repository Domain Rules (to be preserved in `.agents/AGENTS.md`)**:
     - Tech stack conventions (Go idioms, Python `uv`, Node/TypeScript rules, Fish scripts, etc.).
     - Safety constraints and testing mandates (TDD rules, pre-commit checks, required flags).
     - Architecture invariants and subsystem documentation.
     - Platform workflow guidelines (shadowing platform native planning, PR policies).

3. **Author or Merge `.agents/AGENTS.md`**:
   - If `.agents/AGENTS.md` does not exist: create it with the extracted domain rules organized under standard sections:
     ```markdown
     # Repository Guidelines & Domain Context

     ## 1. Tech Stack & Standards
     - ...

     ## 2. Safety & Verification Mandates
     - ...

     ## 3. Workflow & Harness Guidelines
     - ...

     ## 4. Architecture & Engineering Standards
     - ...
     ```
   - If `.agents/AGENTS.md` already exists: perform a semantic 3-way merge. Retain existing content and append any new extracted guidelines into appropriate sections without duplication.

4. **Replace Root `AGENTS.md` with Canonical Tier 1 Router**:
   Overwrite root `AGENTS.md` with the exact canonical template:

   ```markdown
   # Agent context

   Durable context for this repo lives in `docs/`. Read it before assuming;
   it is the record, and this file is only the pointer to it.

   - `docs/qna/` — answers indexed by the question you would ask again
   - `docs/plans/` — implementation plans
   - `docs/journal/` — dated record of what happened
   - `docs/design/` — the design still in force

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

5. **Ensure Relative `CLAUDE.md` Symlink**:
   Ensure `CLAUDE.md` is a relative symlink pointing to `AGENTS.md`:
   ```bash
   rm -f CLAUDE.md
   ln -s AGENTS.md CLAUDE.md
   ```

---

### Phase 4: Docs Store Realignment & Skill Refresh

1. **Relocate Misplaced Documentation**:
   Review `misplaced_docs` flagged during Phase 1:
   - Move misplaced plan files (`*-plan.md` in `docs/journal/` or legacy archive paths) into `docs/plans/`:
     ```bash
     git mv docs/journal/<file>-plan.md docs/plans/
     ```
   - Move design specifications out of `docs/journal/` into `docs/design/`.
   - Fix any broken internal relative markdown links within relocated files.

2. **Ensure 4 Documentation Stores Exist**:
   Ensure directories and starter `README.md` files exist for all 4 stores:
   - `docs/design/README.md`
   - `docs/plans/README.md`
   - `docs/journal/README.md`
   - `docs/qna/README.md`
   If any are missing, run `agents init` or scaffold them non-destructively.

3. **Verify Bundled Skills**:
   - Ensure `.agents/skills/recording-what-you-learn/SKILL.md` is present. If customized, perform 3-way merge; if missing, populate from canonical template.
   - Ensure `.agents/skills/migrating-fleet-context/SKILL.md` is present and matches the binary's embedded asset.

---

### Phase 5: Automated Verification & Diagnostic Gate

1. **Verify Context Cleanliness with `agents drift`**:
   ```bash
   agents drift
   ```
   Must return exit code `0` (`Router: clean_current`, `Symlink: ok`, `Domain: ok`).

2. **Verify Granular Diagnostics with `agents doctor`**:
   ```bash
   agents doctor
   ```
   Verify that all 5 `scaffold:*` checks pass with status `ok`:
   - `scaffold:router` (OK)
   - `scaffold:symlink` (OK)
   - `scaffold:domain` (OK)
   - `scaffold:skill-recording` (OK)
   - `scaffold:skill-migrating` (OK)

3. **Run Repository Test Suite**:
   Run the repository's native test commands if applicable (e.g. `go test ./...`, `npm test`, `pytest`). Ensure full completion with exit code 0.

4. **Commit Changes**:
   Stage and commit all migration changes:
   ```bash
   git add AGENTS.md CLAUDE.md .agents/ docs/
   git commit -m "refactor(context): migrate to two-tier agent context and 4-store layout"
   ```

---

## Where this comes from

- `docs/design/2026-08-29-two-tier-context-and-llm-migration-architecture.md` (Architecture specification)
- `docs/plans/2026-08-31-two-tier-context-and-llm-migration-plan.md` (Implementation plan)
- `docs/qna/why-does-agents-init-never-update-existing-instructions.md` (Scaffold immutability rationale)
- `docs/qna/how-does-two-tier-agent-context-prevent-scaffold-drift.md` (Two-Tier isolation rationale)
