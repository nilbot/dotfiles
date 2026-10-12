# Repository Architecture & Guidelines (dotfiles)

## 1. Documentation & Knowledge Architecture

Durable knowledge lives in `docs/` using the 4-store layout:
- `docs/design/` — Living architectural specifications. Read these to understand the active design and reasoning.
- `docs/plans/` — Step-by-step implementation plans, authored before code changes.
- `docs/journal/` — Dated records of what happened: run collapses, decisions made, retrospectives.
- `docs/qna/` — Topic-indexed findings answering "what question would you ask again". Written and committed directly.
- **Retired designs and executed plans are deleted, not archived.** A document whose
  subject is gone is removed from `docs/`, and git history is its archive:
  `git log --diff-filter=D -- docs/` lists them. This store is read by agents that
  act on what they find, so a retired design is an instruction with a warning label.

## 2. CLI Interface Documentation Invariant

> [!IMPORTANT]
> Whenever any `agents` CLI command, subcommand, flag, or signature is added, modified, or deprecated, all corresponding documentation MUST be updated in the same change set:
> 1. `agents/README.md` (Features, Quickstart, and CLI Reference table)
> 2. Root `README.md`
> 3. CLI built-in help text (`agents help`)
> 4. Applicable design docs and Q&A entries in `docs/`

## 3. Agent Harness & Workflow Guidelines

- **Branch Protection & Pull Request Policy**: Direct pushes to `master` are strictly prohibited by GitHub branch protection rulesets. All changes, features, bug fixes, and documentation updates MUST be authored on dedicated feature branches (e.g. `feat/`, `fix/`, `docs/`) and integrated via Pull Requests after passing CI gate verification (`gate`).
- **Planning Workflows**: Do NOT invoke Antigravity's native `<planning_mode>` or create brain artifacts with `RequestFeedback: true`. Write specs to `docs/design/` and plans to `docs/plans/` directly. A harness that provides `/brainstorming` and `writing-plans` may be used for them; those skills belong to the harness, and nothing in this repository defines them.
- **Evidence Before Assertions**: Always run test suites and verification commands (`agents doctor`, `go test ./...`) before claiming work is complete.
- **Recording Findings**: Follow `.agents/skills/recording-what-you-learn/`. Main agents record findings from subagent reports.

## 4. Engineering Standards

- **Go Programs (`agents`, `bootstrap.d`)**:
  - Use Go 1.26+ standard tooling (`go test -v ./...`, `go vet ./...`).
  - Strict non-mutation and idempotency invariants across all CLI operations.
- **Fish Shell & Scripts**:
  - Fish shell configurations in `fish/`.
  - Scripts must be idempotent, testable, and preserve existing machine state.
