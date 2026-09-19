# Why does `agents init` never update existing instruction files?

## Context

2026-08-28, evaluating how to make repositories initialized with `agents init` work
seamlessly for external collaborators who do not have the maintainer's `dotfiles`
clone or `agents` binary on their machine.

The question arose whether updating `scaffold.DefaultAgentsMD` in the `agents` Go source
would migrate existing repos across the fleet (such as `toolshed/cowork`), or if an
automatic template migration command should be built.

## Answer

`agents` deliberately never mutates, overwrites, or migrates `AGENTS.md` or `CLAUDE.md`
once created.

### 1. `writeIfAbsent` is a hard non-overwrite gate

In `agents/internal/scaffold/scaffold.go`, `scaffold.Create` checks existence before
writing:

```go
if err := writeIfAbsent(filepath.Join(root, "AGENTS.md"), DefaultAgentsMD); err != nil {
    return err
}
```

`writeIfAbsent` checks `os.Lstat(path)` and exits immediately if the file exists.
`agents init` is strictly idempotent: on an already-scaffolded repository, it touches
no tracked root files.

### 2. `wire` and `update` never write the root instruction files

Neither `agents wire` nor `agents update --all --apply` ever modifies `AGENTS.md`
or `CLAUDE.md`. `update` does read them: it inspects every registered repository
through the same drift classifier `agents drift` runs, which compares root
`AGENTS.md` against the canonical router for the resolved layout. Reading is how
it reports divergence; writing is not something it does to those files. Its own
writes are restricted to generated, machine-local, git-excluded files:
- `.claude/settings.json`
- `.codex/hooks.json`
- `.agents/hooks.json`
- `.claude/skills` and `.codex/skills` symlinks

The one tracked file `update` writes is the `agents`-owned
`migrating-fleet-context` skill under `.agents/skills/`, refreshed to the
canonical text for the repository's **resolved layout**: a v1 repository (no
`.agents/layout.json`) keeps the frozen v1 text, a v2 repository gets the
manifest-aware text. It is `agents`-owned, so that refresh is not a rewrite of
human-authored content — and it does not touch the root instructions.

### 3. Why automatic template migration is rejected

`AGENTS.md` is a tracked git file owned by the repository maintainer. Projects place
domain-specific engineering rules, test expectations, and safety constraints directly
in it.

An automatic migration mechanism faces three intractable states:
- Unmodified historical templates (safe to replace wholesale).
- Partial customizations (custom domain rules mixed with legacy scaffold blocks).
- Heavily customized or restructured instruction files.

Attempting AST or block-level heuristics to automatically rewrite `AGENTS.md` introduces
high complexity and risks clobbering human-authored engineering rules.

### How to update existing repos

Updating instruction files on existing repositories is a deliberate, manual maintainer
action committed to git:

1. Open the repository's `AGENTS.md`.
2. Update the `## Machine Wiring` section to make `agents doctor` conditional on the
   binary being present on PATH.
3. Commit the change directly to the repository.
