# Design: Two-Tier Agent Context and Bundled Skills

**Date:** 2026-08-29 (updated 2026-08-31; the amendments of 2026-09-01 and 2026-09-21 were deleted with the
sections they corrected)  
**Status:** implemented. The two-tier context, the bundled-skill embedding and the
multi-harness protocol are in force. The subsystems retired on 2026-09-21 -- the
drift classifier, the command that refreshed skills, and the fleet-migration
skill -- are deleted from this document with the sections that specified them;
`agents help --all` is the command surface.  
**Applies to:** `agents` CLI (`init`, `wire`, `doctor`), `AGENTS.md` / `CLAUDE.md`, `.agents/AGENTS.md`, `.agents/skills/`  
**Depends on:** Spec 1 (harness adapters, exit codes), Knowledge is Documentation — deleted 2026-10-12 — [Contributor Guardrails](2026-08-28-contributor-guardrails-and-scaffold-decoupling.md) (2026-08-28), and Binary Identity & Standalone Resolution (deleted 2026-10-12; superseded by [one record for binary identity](2026-10-07-one-record-for-binary-identity.md))  
**Reads against:** [`docs/qna/why-does-agents-init-never-update-existing-instructions.md`](../qna/why-does-agents-init-never-update-existing-instructions.md), [`docs/qna/how-does-two-tier-agent-context-prevent-scaffold-drift.md`](../qna/how-does-two-tier-agent-context-prevent-scaffold-drift.md)

---

## 1. Executive Summary & Problem Formulation

### 1.1 Why the root router and the domain context are separate

Two tiers exist because one file was worse. A repository's infrastructural directives
and its own domain rules shared a single root file, and every programme that tried to
keep such a file current was defeated by the same problems.
Earlier iterations of repository scaffolding treated `AGENTS.md` / `CLAUDE.md` as an immutable file (`writeIfAbsent`). As the `agents` ecosystem evolved (adding multi-harness support, contributor-friendly doctor checks, 4-store documentation layouts, and retiring outdated capture systems), updating existing repositories across the fleet encountered a fundamental limitation of deterministic programming:
- **Monolithic Mixing**: Repositories mixed infrastructural harness directives with custom, human-authored domain rules, test protocols, and language conventions in a single root file.
- **Deterministic Brittleness**: Blindly overwriting `AGENTS.md` clobbers human-authored domain rules. Conversely, refraining from touching the file freezes repositories on obsolete scaffold conventions. Regex/AST parsers cannot reliably disentangle domain rules from reworded or reordered scaffold text.
- **Skill Drift & Obsolescence**: Bundled agent skills (`recording-what-you-learn`) also evolve over time. If skills are frozen after initial scaffold, repositories miss upstream improvements; if overwritten blindly, local customizations are destroyed.
- **Documentation Directory Drift**: Conflicting defaults across agent tools sent plans to different directories for the same purpose. The four-store layout under `docs/` is the answer to that.

### 1.2 The Solution: Two-Tier Context
This specification establishes a robust **Two-Tier Agent Context Architecture**:
1. **Clean Separation of Concerns (Two-Tier Isolation)**:
   - **Tier 1 (Root Router)**: Standardized, lightweight router (~24 lines) at `AGENTS.md` (with `CLAUDE.md -> AGENTS.md` symlink) that directs agents to durable docs and domain rules, and conditionally invokes `agents doctor`.
   - **Tier 2 (Domain Context)**: Dedicated repository-specific guidelines stored in `.agents/AGENTS.md` and durable knowledge in `docs/` (`design/`, `plans/`, `journal/`, `qna/`).
2. **Deterministic CLI Engine of Truth**:
   - `agents/internal/scaffold/`: Embeds canonical starter assets via Go `embed.FS` and scaffolds full Two-Tier + 4-store skeleton on `agents init`.
   - `agents doctor`: Granular diagnostic checks (`scaffold:router`, `scaffold:symlink`, `scaffold:domain`, `scaffold:skill-recording`).

---

## 2. The Two-Tier Context Architecture

```
Repository Root
├── AGENTS.md                              [Tier 1: Canonical Root Router]
├── CLAUDE.md -> AGENTS.md                 [Harness Compatibility Symlink]
│
├── docs/                                  [Durable Knowledge Tier]
│   ├── design/README.md                   (Living architecture & specs)
│   ├── plans/README.md                    (Step-by-step implementation plans)
│   ├── journal/README.md                  (Dated records of what happened & retros)
│   ├── qna/README.md                      (Topic-indexed findings & learnings)
│   └── archive/                           (Immutable historical records pre-2026-08-20)
│
└── .agents/
    ├── AGENTS.md                          [Tier 2: Domain Context Tier]
    │   └── (Engineering guidelines, style, safety rules, stack conventions)
    ├── skills/                            (Repo-specific executable agent skills)
    │   └── recording-what-you-learn/      (Bundled canonical recording skill)
    └── hooks.json                         (Machine-local wiring)
```

### 2.1 Tier 1: Canonical Root Router (`AGENTS.md`)
The root `AGENTS.md` is strictly identical across repositories in the fleet. It solves the **indirection hop** by presenting an un-ignorable **Bootstrap Protocol** in its preamble:

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

### 2.2 Tier 2: Domain Context (`.agents/AGENTS.md`)
Owned entirely by the repository maintainer. Contains:
- Tech stack requirements (e.g. Python `uv`, Go idioms, TypeScript rules).
- Safety constraints & test mandates (e.g. mandatory TDD, pre-commit checks).
- Architecture invariants and documentation guidelines.
- Harness execution constraints (e.g. shadowing platform native planning in favor of `/brainstorming` and `writing-plans`).

---

## 3. Bundled Skills Delivery & Asset Embedding (`embed.FS`)

To support standalone operation on contributor machines without requiring a local `dotfiles` checkout, `agents` embeds canonical starter templates directly in the Go binary using `//go:embed assets/*`:

```
agents/internal/scaffold/assets/
├── skills/
│   └── recording-what-you-learn/
│       └── SKILL.md                       (Durable knowledge recording skill)
├── dotagents/
│   └── AGENTS.md                          (Starter Tier 2 domain context template)
└── docs/
    ├── design/README.md                   (Living architecture index)
    ├── plans/README.md                    (Step-by-step plans index)
    ├── journal/README.md                  (Chronological logs index)
    └── qna/README.md                      (Topic-indexed Q&A index)
```

### 3.1 Idempotent Scaffolding (`scaffold.Create`)
When `agents init <path>` is executed:
1. **Directory Tree**: Creates `.agents/skills/`, `.agents/`, and the 4-store `docs/` directories (`docs/design/`, `docs/plans/`, `docs/journal/`, `docs/qna/`).
2. **Non-Destructive File Population (`writeIfAbsent`)**:
   - Populates `.agents/skills/recording-what-you-learn/SKILL.md` from embedded assets.
   - Populates `.agents/AGENTS.md` with the starter domain template.
   - Populates `docs/{design,plans,journal,qna}/README.md` index files (ensuring git tracks directories without needing `.gitkeep`).
   - Populates root `AGENTS.md` (`DefaultAgentsMD`) and symlinks `CLAUDE.md -> AGENTS.md`.
   - Appends `.agents/** linguist-generated=true` to `.gitattributes`.
   - Appends local generated paths to `.git/info/exclude`.

---

## 4. Enhanced `agents doctor` Diagnostics

Replaces the legacy generic `scaffold:doctor-instruction` with granular checks. Four checks, and they are what `agents doctor` reports today.

| Check Name | Status `ok` | Status `warn` / `fail` / `info` | Remedy Message |
| :--- | :--- | :--- | :--- |
| `scaffold:router` | Present | `info` / `warn` (missing) | Run `agents init` to write the router |
| `scaffold:symlink` | `CLAUDE.md` is relative symlink to `AGENTS.md` | `warn` (missing) / `fail` (a regular file, a wrong target, or a dangling link) | Run `agents init` or recreate symlink |
| `scaffold:domain` | `.agents/AGENTS.md` exists | `warn` (missing) | Run `agents init` or create `.agents/AGENTS.md` |
| `scaffold:skill-recording` | `.agents/skills/recording-what-you-learn/` exists | `warn` (missing) | Run `agents init` to populate bundled skill |

`doctor` reports whether the router is *present*, which is all it can check
without a canonical text to compare against.

---

## 5. Multi-Harness Integration & Platform Workflow Shadowing

### 5.1 Symlink Protocol
- **Canonical Root Entry**: `AGENTS.md` is the regular file at repository root.
- **Claude Code Compatibility**: `CLAUDE.md` is a relative symlink pointing to `AGENTS.md` (`ln -s AGENTS.md CLAUDE.md`).
- **Antigravity Compatibility**: Antigravity natively discovers root `AGENTS.md`, `.agents/AGENTS.md`, and `.agents/skills/`.
- **Codex / Cursor Compatibility**: Configured via machine-local harness wiring generated by `agents wire`.

### 5.2 Shadowing Native Platform Planning Workflows
To prevent AI harnesses (such as Antigravity) from intercepting workflows with modal platform prompts or transient brain artifacts:
- All architectural specs are written directly to `docs/design/YYYY-MM-DD-<topic>.md`.
- All implementation plans are written directly to `docs/plans/YYYY-MM-DD-<topic>-plan.md`.
- Transient brain artifacts are avoided or authored without `RequestFeedback: true`, maintaining conversational flow and human approval in chat via `/brainstorming` and `writing-plans`.

---

## 6. Documentation & CLI Maintenance Invariants

> [!IMPORTANT]
> **CLI Interface Documentation Invariant**  
> Whenever any `agents` CLI command, subcommand, flag, or invocation interface is added, modified, or deprecated, all corresponding documentation MUST be updated in the same change set:
> 1. `agents/README.md` (Features, Quickstart, and CLI Reference table)
> 2. Root `README.md`
> 3. CLI built-in help text (`agents help`)
> 4. Applicable design docs and Q&A entries in `docs/`

---

