# Design: Contributor Guardrails, Standalone Tooling, and Scaffold Decoupling

**Date:** 2026-08-28  
**Status:** Approved in Brainstorming  
**Applies to:** `agents` CLI, `scaffold` package, `doctor` diagnostic subsystem, `DefaultAgentsMD`, target repositories (e.g. `toolshed/cowork`)  
**Depends on:** [Spec 1](2026-08-07-agents-repo-context-design.md) (harness adapters, exit codes), [Spec 5](2026-08-11-spec-5-verification-gate.md) (verification gate), [Spec 6](2026-08-11-spec-6-releases-and-distribution.md) (releases and distribution), [Knowledge is Documentation](2026-08-19-knowledge-is-documentation.md) (2026-08-19)  
**Reads against:** [`docs/qna/why-does-agents-init-never-update-existing-instructions.md`](../qna/why-does-agents-init-never-update-existing-instructions.md)

---

## 1. Executive Summary & Problem Formulation

The `agents` CLI and context conventions were originally designed for a single operator managing a fleet of personal repositories from a centralized `dotfiles` checkout.

When repositories initialized with `agents init` are opened to external collaborators, two systemic friction points arise:
1. **Instruction Inflexibility & False Alarms**: The generated root `AGENTS.md` strictly directs AI harnesses that *"an empty or stale `.agents/` means the setup is broken rather than that there is nothing to say — report it rather than working around it"* and commands *"Run `agents doctor` early"*. External contributors and their LLM harnesses (Claude Code, Codex, Antigravity, Cursor) lack the operator's dotfiles and `agents` binary on `PATH`, causing the agent to report false setup failures.
2. **Coupled Diagnostics in `agents doctor`**: `agents doctor` assumes the host machine contains a stamped `dotfiles` checkout (`DotfilesRoot()`), checking global `core.hooksPath` pointing to `<dotfilesRoot>/git/hooks.d` and global attributes pointing to `<dotfilesRoot>/git/gitattributes`. In a standalone repository on a collaborator's machine, these checks fail even when the repository's local context and wiring are exact.

This specification formalizes the **Two-Tier Contributor Guardrail Architecture**:
- **Tier 1 (Zero-Install Contributor / External AI)**: Self-contained `AGENTS.md` and in-repo `.agents/skills/` instructing agents to consult `docs/qna/` and `docs/design/`, run repo verification commands, and gracefully skip `agents doctor` if the binary is absent. Authoritative PR verification runs in CI.
- **Tier 2 (Enhanced Local Tooling)**: Decoupled `agents` CLI functioning in standalone repository mode without requiring personal dotfiles paths, enabling collaborators to optionally install `agents` and obtain local pre-commit secret guards and transcript tracing.

---

## 2. The 3-Layer Guardrail Architecture

Guardrails for collaborative repositories enforce engineering rigor across three layers:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. Agent Guardrails (In-Repo Context & Skills)                              │
│    • AGENTS.md + docs/{design,qna,journal} + .agents/skills                 │
│    • Directs LLMs: consult docs/ before assumptions, cite evidence,         │
│      run test suite before completion, follow domain procedures             │
└──────────────────────────────────────┬──────────────────────────────────────┘
                                       │
┌──────────────────────────────────────▼──────────────────────────────────────┐
│ 2. Local Machine Guardrails (Pre-Commit Hooks & Harness Wiring)             │
│    • .git/hooks/pre-commit (gitleaks secret scan, linter)                   │
│    • Optional `agents` CLI: wires .claude, .codex, .agents, caches traces  │
└──────────────────────────────────────┬──────────────────────────────────────┘
                                       │
┌──────────────────────────────────────▼──────────────────────────────────────┐
│ 3. Authoritative Guardrail (CI / PR Verification Gate)                      │
│    • GitHub Actions verify.yml (Spec 5 philosophy)                          │
│    • Unbypassable backstop: catches unverified claims, secrets, test breaks │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Contributor-Friendly Scaffold (`DefaultAgentsMD`)

### 3.1 Eliminating False Alarms

The phrase:
> *"an empty or stale `.agents/` means the setup is broken rather than that there is nothing to say — report it rather than working around it."*

is removed from `DefaultAgentsMD`. In a clean clone, `.agents/` contains only `skills/` (and `.gitkeep`), which is valid and expected.

### 3.2 Conditional `DoctorInstruction`

The `DoctorInstruction` constant is updated from an unconditional command to a conditional diagnostic directive:

```go
const LegacyDoctorInstruction = "Run `agents doctor` early and report any warnings before relying on this context."

const DoctorInstruction = "If the `agents` CLI is installed, run `agents doctor` early and report any warnings before relying on this context. If `agents` is not installed on this machine, skip machine wiring checks and adhere directly to the repository instructions above."
```

### 3.3 The New `DefaultAgentsMD` Layout

```markdown
# Agent context

Durable context for this repo lives in `docs/`. Read it before assuming;
it is the record, and this file is only the pointer to it.

- `docs/qna/` — answers indexed by the question you would ask again
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

---

## 4. Backwards Compatibility & Diagnostic Support

### 4.1 Multi-Version Recognition in `doctor`

`doctor.checkScaffoldInstruction(repoRoot string)` must recognize **both** `scaffold.DoctorInstruction` and `scaffold.LegacyDoctorInstruction` (or any string containing `agents doctor`), ensuring existing repositories in the fleet (like `toolshed/cowork`) continue reporting `ok` on `scaffold:doctor-instruction`.

### 4.2 Non-Mutation Invariant

In accordance with [`docs/qna/why-does-agents-init-never-update-existing-instructions.md`](../qna/why-does-agents-init-never-update-existing-instructions.md), `agents init` and `agents wire` strictly maintain idempotency and never overwrite or mutate existing `AGENTS.md` or `CLAUDE.md` files.

---

## 5. Phased Delivery

1. **Phase 1 (Scaffold & Prompts)**: Update `DefaultAgentsMD`, `DoctorInstruction`, `doctor.checkScaffoldInstruction`, and corresponding unit tests in `agents/internal/scaffold` and `agents/internal/doctor`.
2. **Phase 2 (Doctor Decoupling)**: Separate repo-local checks from personal dotfiles checks in `agents doctor`.
3. **Phase 3 (Spec 6 Packaging)**: Standalone binary releases and Homebrew formula.
4. **Phase 4 (CI Gate Template)**: Reusable `.github/workflows/verify.yml` for managed repositories.
