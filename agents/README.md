# agents

A developer harness manager, repository context framework, and transcript recorder for AI coding agents (Claude Code, Codex, Antigravity, Cursor).

---

## Features

- **Multi-Harness Wiring**: Automatically configures and keeps in sync hook configurations for Claude Code (`.claude/settings.json`), Codex (`.codex/hooks.json`), and Antigravity (`.agents/hooks.json`).
- **Durable Transcript Caching**: Captures and preserves subagent conversation transcripts before harnesses delete them, storing them in `.agents/transcripts/` with retention and size bounding.
- **Repository Guardrails & Pre-Commit Secret Scanning**: Integrates `gitleaks` into `agents guard --staged` to catch secret leaks before commit.
- **Commit Message Sanitization**: Built-in Git `commit-msg` hook automatically strips AI attribution footers and co-author tags to keep git histories clean.
- **Self-Diagnostic Tooling**: `agents doctor` inspects harness wiring, trust permissions, transcript indices, and repository hygiene without mutating your configuration.

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

Initialize any Git repository to track agent context and wire harness triggers:

```bash
cd my-project
agents init
```

Run diagnostics to verify that harnesses and hooks are wired correctly:

```bash
agents doctor
```

Inspect session transcripts and agent activity:

```bash
# List recorded sessions and subagent runs
agents trace ls

# View the full transcript of a specific turn or subagent
agents trace show <session-id>

# View session statistics across lanes
agents trace stats
```

---

## Operating Modes

`agents` operates in two modes:

### 1. Standalone Mode (Default)
When installed via Homebrew or downloaded from releases, `agents` operates as a standalone repository tool.
- Requires no external dotfiles clone.
- `agents doctor` checks repository-local harness wiring, repo `.gitattributes`, secret scanner presence, and documentation freshness.
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

| Command | Description |
|---|---|
| `agents init` | Create `.agents/`, scaffold instructions, and wire harness triggers |
| `agents wire` | Regenerate and reconcile harness trigger configs across all installed agents |
| `agents doctor` | Run comprehensive diagnostic on wiring, trust states, and repository context |
| `agents guard` | Run pre-commit checks and secret scans on staged files |
| `agents trace` | Query, display, and manage cached conversation transcripts |
| `agents version` | Print version provenance and build metadata |
| `agents help` | View complete CLI documentation for any subcommand |

---

## License

MIT
