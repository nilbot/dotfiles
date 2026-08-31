---
name: migrating-fleet-context
description: Use when migrating a repository to the Two-Tier Agent Context architecture or resolving context drift flagged by `agents doctor` or `agents drift`. Covers git branch isolation, semantic un-nesting of domain rules into `.agents/AGENTS.md`, 3-way skill merging, and docs relocation.
---

# Migrating Fleet Context

Migrates a repository to the Two-Tier Agent Context architecture.

## Workflow

1. **Pre-flight Check**: Ensure repository working tree is clean (`git status --porcelain`).
2. **Branch Creation**: Create a dedicated migration branch (`git checkout -b feat/two-tier-context-migration`).
3. **Inspect Drift**: Run `agents drift --json` to inspect current router, domain, skill, and docs states.
4. **Semantic Un-nesting**:
   - Extract repository-specific domain rules from root `AGENTS.md` into `.agents/AGENTS.md`.
   - Restore root `AGENTS.md` to canonical router (`DefaultAgentsMD`).
   - 3-way merge customized skills (e.g. `recording-what-you-learn`).
   - Move any misplaced plans into `docs/plans/`.
5. **Verification**: Run `agents doctor` and test suites (`go test ./...`).
6. **Review**: Present diff summary for human approval before commit and PR.
