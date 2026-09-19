# How does Two-Tier Agent Context prevent scaffold drift?

## Context

2026-08-29, designing fleet migration and scaffold evolution across repositories initialized with `agents init`.

When `agents` improves its root instructions (e.g. contributor-friendly doctor checks, new harness pointers, 4-store documentation layout), updating existing repositories previously required manual, repo-by-repo edits because automated AST/regex tools risk clobbering human-authored domain rules.

## Answer

Two-Tier Agent Context solves this by physically separating the machine-managed routing layer from the human-managed domain guidelines.

### 1. The Monolithic Failure Mode

In monolithic setups, `AGENTS.md` mixes:
- Machine wiring instructions (`agents doctor`, `.agents/hooks.json`).
- Documentation pointers (`docs/qna/`, `docs/plans/`, `docs/design/`).
- Repository-specific engineering standards (Go idioms, Python `uv`, test rules, safety gates).

Deterministic migration scripts cannot safely update such files:
- Blind overwrite clobbers custom domain rules.
- `writeIfAbsent` freezes files, leaving repositories on stale scaffold conventions indefinitely.
- Comment delimiters (`<!-- agents:start -->`) break easily when humans edit or reorder sections.

### 2. The Two-Tier Isolation Boundary

Two-Tier separates these concerns into two distinct files:
- **Tier 1 (Root Router — `AGENTS.md` & `CLAUDE.md`)**:
  Lightweight (~24 lines), strictly canonical across all repositories. Contains only the Bootstrap Protocol, documentation pointers, and conditional doctor directives. Managed by `agents` scaffold tooling.
- **Tier 2 (Domain Context — `.agents/AGENTS.md` & `docs/`)**:
  100% owned and maintained by the repository author. Contains all domain engineering rules, safety invariants, and tech stack requirements.

### 3. Solving the Indirection Hop via Bootstrap Protocol

Harnesses auto-load root `AGENTS.md` at session start, but do not automatically fetch secondary files. To ensure agents never skip `.agents/AGENTS.md`, root `AGENTS.md` places the Bootstrap Protocol directly in its preamble:

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
```

That block is the preamble of the v1 canonical router, verbatim — not the whole
of it. A repository with `.agents/layout.json` (schema `agents.layout/v2`) gets
the manifest-pointing router instead, which names the manifest rather than the
four `docs/` paths, because the v2 stores may live anywhere. Use
`agents layout show --router` for the complete bytes of whichever router the
resolved layout uses; it prints nothing and exits non-zero when a manifest is
present but did not resolve, so a restore never silently falls back to v1 bytes.
`agents layout path <role>` resolves one store without a second copy of the map
in prose.

A v1 repository becomes v2 through `agents layout migrate`, not by hand: the dry
run is the default and prints the plan, `--apply --backup-tag <name>` moves each
store with `git mv` and writes the manifest, and a run that stopped mid-way is
continued with `--resume --apply` — the journal is frozen at plan time and never
re-planned. The command deliberately stops at the mechanical half: it reports
the markdown links the move breaks, and the rewrite, the rule extraction, and
the router reconciliation stay with the `migrating-fleet-context` skill below,
under human approval.

### 4. LLM Sorter as Drift Reconciliation

When external contributors add domain rules directly to root `AGENTS.md`, the repository enters an unpartitioned drift state.

Rather than failing or clobbering, the LLM migration engine (the `migrating-fleet-context` skill, driven by the operator):
1. Detects drift against the canonical router for the resolved layout — `DefaultAgentsMD` on a v1 repository, the manifest-pointing router on v2.
2. Extracts human-authored rules and appends them to `.agents/AGENTS.md`.
3. Restores root `AGENTS.md` to canonical form, taking the bytes from `agents layout show --router` rather than embedding a second copy of the text.
4. Presents an interactive diff for explicit human approval.
