# agents

A developer harness manager, repository context framework, and transcript recorder for AI coding agents (Claude Code, Codex, Antigravity, Cursor).

---

## Features

- **Multi-Harness Wiring**: Automatically configures and keeps in sync hook configurations for Claude Code (`.claude/settings.json`), Codex (`.codex/hooks.json`), and Antigravity (`.agents/hooks.json`).
- **Two-Tier Context & Drift Detection**: Enforces clean separation between canonical machine routing (`AGENTS.md`, `CLAUDE.md`) and repository domain guidelines (`.agents/AGENTS.md`). `agents drift` inspects context layout, canonical diffs, domain context, bundled skills, and misplaced documentation across repositories. Repository-specific skills under `.agents/skills/` are listed as `local_skills` and never classified as drift.
- **Fleet Maintenance & Skill Refresh**: `agents update` rewires machine hooks across registered repositories, refreshes the authoritative `migrating-fleet-context` skill, and emits advisory notices if any repository exhibits context drift.
- **Durable Transcript Caching**: Captures and preserves subagent conversation transcripts before harnesses delete them, storing them in `.agents/transcripts/` with retention and size bounding.
- **Repository Guardrails & Pre-Commit Secret Scanning**: Integrates `gitleaks` into `agents guard --staged` to catch secret leaks before commit.
- **Commit Message Sanitization**: Built-in Git `commit-msg` hook automatically strips AI attribution footers and co-author tags to keep git histories clean.
- **Self-Diagnostic Tooling**: `agents doctor` inspects harness wiring, trust permissions, transcript indices, repository hygiene, and 5 granular `scaffold:*` diagnostics without mutating your configuration.

---

## Installation

### Homebrew (macOS & Linux)

```bash
brew install nilbot/tap/agents
```

To upgrade:
```bash
brew update && brew upgrade nilbot/tap/agents
```

### Pre-built Binary (GitHub Releases)

Download pre-compiled binaries for Darwin (Apple Silicon / Intel) or Linux (x86_64 / ARM64) from [GitHub Releases](https://github.com/nilbot/dotfiles/releases).

Extract and place the binary on your `$PATH`:
```bash
tar -xzf agents_*_darwin_arm64.tar.gz
sudo mv agents /usr/local/bin/
```

### From Source (Go 1.26+)

```bash
go install github.com/nilbot/dotfiles/agents@latest
```

---

## Quickstart

Initialize any Git repository to track agent context, scaffold Two-Tier instructions, and wire harness triggers:

```bash
cd my-project
agents init
```

Run diagnostics to verify that harnesses, hooks, and scaffold integrity are intact:

```bash
agents doctor
```

Inspect repository or fleet-wide context layout and router drift:

```bash
# Check context layout and detect drift in current repository
agents drift

# Output full JSON drift report for AI agents and automation
agents drift --json

# Inspect a specific repository or check all registered fleet repositories
agents drift --repo /path/to/repo
agents drift --all
```

Update fleet wiring and refresh embedded migration skills:

```bash
# Dry run update across all registered repositories
agents update --all

# Apply harness rewiring and skill refresh across the fleet
agents update --all --apply
```

Inspect session transcripts and agent activity:

```bash
# List recorded sessions and subagent runs
agents trace ls

# View the full transcript of a specific turn or subagent
agents trace show <session-id>

# List recent recorded sessions and subagent runs with limit
agents trace ls --limit 10
```

---

## Operating Modes

`agents` operates in two modes:

### 1. Standalone Mode (Default)
When installed via Homebrew or downloaded from releases, `agents` operates as a standalone repository tool.
- Requires no external dotfiles clone.
- `agents doctor` checks repository-local harness wiring, repo `.gitattributes`, secret scanner presence, documentation freshness, and 5 granular `scaffold:*` diagnostics:
  - `scaffold:router`: Validates that root `AGENTS.md` matches the canonical router template without unpartitioned domain drift.
  - `scaffold:symlink`: Verifies that `CLAUDE.md` is a valid relative symlink to `AGENTS.md`.
  - `scaffold:domain`: Confirms presence of `.agents/AGENTS.md` for repository-specific domain rules.
  - `scaffold:skill-recording`: Checks status and customization state of `.agents/skills/recording-what-you-learn/`. This skill is repository-customizable, so local edits are reported without warning.
  - `scaffold:skill-migrating`: Checks that `.agents/skills/migrating-fleet-context/` matches the installed binary. This skill is `agents`-owned, so any divergence is staleness and warns; run `agents update --all --apply` to refresh it.
- Git hook dispatching executes repository-level hooks and built-in guards.

### 2. Dotfiles Operator Mode
For developers managing a centralized `dotfiles` checkout with machine-level Git hook chaining:
- **Build with Link Stamp**:
  ```bash
  go build -trimpath -ldflags "-X main.dotfilesRoot=$HOME/dotfiles" -o ~/bin/agents .
  ```
- **Or Set Environment Variable**:
  ```bash
  export AGENTS_DOTFILES_ROOT="$HOME/dotfiles"
  ```
- In Operator Mode, `agents` validates global `core.hooksPath` symlinks (`~/dotfiles/git/hooks.d/`) and chains personal hook scripts from `~/dotfiles/git/hooks/*`.

---

## CLI Reference

<!-- BEGIN GENERATED: agents help --render=markdown -->
| Command | What |
|---|---|
| `agents help` | print the listing, or one command's page |
| `agents init` | create .agents/, triggers, wiring, fleet entry |
| `agents wire` | regenerate harness configs (merges, never overwrites) |
| `agents doctor` | report wiring, trust evidence, reachability, and lane health |
| `agents drift` | inspect context layout and router drift |
| `agents save` | commit .agents/ paths and nothing else (escape hatch) |
| `agents trace` | query records; read one back; copy reachable ones |
| `agents trace ls` | query records |
| `agents trace show` | read one transcript back |
| `agents trace cache` | copy reachable transcripts into the store |
| `agents trace cache prune` | remove cached copies, never the records |
| `agents trace migrate` | move a tracked index into the machine-local store |
| `agents ls` | list the fleet on this machine |
| `agents update` | rewire every registered repo (dry run by default) |
| `agents version` | print binary version and build provenance |
| `agents guard` | pre-commit checks (the only command that blocks) |
| `agents hook` | harness hook entrypoint |
<!-- END GENERATED -->

---

## License

MIT
