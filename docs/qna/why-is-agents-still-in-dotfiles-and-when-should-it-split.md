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

## Follow-up, 2026-09-21

**The tool is smaller than the one this entry argues about, and the argument is
unaffected.**

The opening description — "the operator's local harness wiring manager, trace
indexer, and repository context framework" — no longer fits. Measured:

```text
$ agents help --all
  agents doctor                                      report wiring, trust, and scaffold state
  agents guard --staged                              pre-commit checks (the only command that blocks)
  agents help [<command> [<subcommand>...]] [--all]  print the listing, or one command's page
  agents init [--local]                              create .agents/, the doc stores, and the wiring
  agents version                                     print binary version and build provenance
  agents wire                                        remove this tool's entries from harness configs
```

The trace index, the session record, the transcript cache, the layout manifest
and the fleet registry were deleted on 2026-09-21. Of the four
contributor-guardrail milestones named above, the release pipeline and the
verification gate template are still present as `.github/workflows/release.yml`
and `.github/workflows/verify.yml`.

Nothing about the split argument changes, and the reduced surface makes it
easier: fewer dotfiles-specific assumptions to extract, and the remaining
commands are the ones that were always the tool's own. The three reasons to
split, the `git subtree split` procedure and the tap update are all still the
plan.

What does need re-reading is the standalone claim. `DotfilesRoot()` still
resolves from the link-time stamp, then `AGENTS_DOTFILES_ROOT`, then empty — that
contract is unchanged — but several of the diagnostics a standalone binary used
to run no longer exist to run, and nothing the tool does now records machine-local
state for a mode difference to be about.
