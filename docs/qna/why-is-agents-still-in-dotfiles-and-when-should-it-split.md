# Why does `agents` live inside `dotfiles` and when should it move to a dedicated repository?

## Context

`agents` originated inside `dotfiles` as the operator's local harness wiring manager, trace indexer, and repository context framework (Spec 1 & Spec 2).

As part of the contributor guardrail and standalone decoupling work (2026-08-28, Phases 1–3), `agents` has evolved into a self-contained, cross-platform CLI tool capable of:
- Operating without an operator's `dotfiles` checkout (Standalone Mode).
- Scaffolding contributor-friendly `AGENTS.md` and multi-harness configurations.
- Being distributed independently via binary releases and Homebrew tap (`nilbot/tap/agents`).

## Why it is still in `dotfiles` today

`agents` remains in `dotfiles` during the initial rollout to avoid mid-flight disruption while finishing the core contributor guardrail milestones (Scaffold Decoupling, Doctor Decoupling, Release Pipeline, and Verification Gate Template).

## The Path to a Dedicated Repository

Separating `agents` into its own repository (e.g., `github.com/nilbot/agents`) is the natural end-state:
1. **Clean Ownership**: Eliminates any lingering dotfiles-specific paths or assumptions from the repository structure.
2. **Independent Releases & Versioning**: Allows semantic version tagging (`v0.1.0`, etc.) independent of dotfiles commits.
3. **Contributor Ergonomics**: External contributors can clone, fork, submit PRs, and review CI checks without downloading personal dotfiles configurations.

When ready to split:
- Extract `agents/` using `git subtree split` (or `git-filter-repo`) to preserve complete commit history and blame.
- Establish `github.com/nilbot/agents` as the upstream source.
- Update `homebrew-tap` and release workflows to point to the dedicated repository.
