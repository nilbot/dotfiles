# agents

Scaffolds and maintains the repository context an AI coding agent needs: the two-tier instruction files, the documentation stores, the bundled skill, and the harness wiring that makes them visible.

---

## Features

- **Repository Scaffold**: `agents init` creates `.agents/` and the four documentation stores under `docs/{design,plans,journal,qna}`, each with a README explaining what belongs in it, the root `AGENTS.md` router and its `CLAUDE.md` symlink, `.agents/AGENTS.md` for repository-specific rules, and the bundled `recording-what-you-learn` skill.
- **Harness Wiring**: Makes `.agents/skills/` visible where each harness looks for it — `.claude/skills` for Claude Code, and Antigravity's own config — without owning the harnesses' settings files.
- **Retired-Entry Cleanup**: `agents wire` removes from `.claude/settings.json`, `.codex/hooks.json` and `.agents/hooks.json` any hook entry this tool wrote in an earlier version, and leaves everything else in those files untouched. A config left holding nothing is removed rather than left behind as `{}`.
- **Repository Guardrails & Pre-Commit Secret Scanning**: `agents guard --staged` integrates `gitleaks` to catch secret leaks and staged-path hazards before commit. Invoked automatically from the pre-commit hook.
- **Commit Message Sanitization**: the installed `commit-msg` hook strips AI attribution footers and co-author tags so git histories read as the author's own work.
- **Self-Diagnostic Tooling**: `agents doctor` reports whether anything stale is left in the harness configs, what the harnesses trust, and the state of the files this tool owns. It observes; it never mutates.

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

Initialize any Git repository to create the agent context and wire it to the harnesses you use:

```bash
cd my-project
agents init
```

`init` takes one flag, `--local`, which keeps `.agents/` out of the repository. There is no layout to choose: the four stores are always `docs/{design,plans,journal,qna}`.

It exits 1 (advisory) even on success, because wiring is written but not yet live — each harness has a trust step no process can perform for you, and `init` prints the list.

Verify the repository is in the state it should be:

```bash
agents doctor
```

Re-run `agents wire` after upgrading from a version that installed hook entries; it removes them. That is the whole of its job.

## Operating Modes

`agents` operates in two modes:

### 1. Standalone Mode (Default)
When installed via Homebrew or downloaded from releases, `agents` operates as a standalone repository tool.
- Requires no external dotfiles clone.
- `agents doctor` reports:
  - `wiring:<harness>`: whether a harness config still holds an entry this tool wrote and no longer answers. Failures come with `agents wire` as the remedy; an absent config is OK, not a gap.
  - `scaffold:router`, `scaffold:symlink`, `scaffold:domain`: the root `AGENTS.md`, its `CLAUDE.md` symlink, and `.agents/AGENTS.md`. The symlink check is the one that catches a silent failure: extracted or synced without symlink support, `CLAUDE.md` becomes a regular file whose content is the text `AGENTS.md`, and a harness then reads that one line as the whole project context.
  - `trust:antigravity`: whether the Antigravity CLI config is readable and names this repository as trusted.
  - `scaffold:skill-recording`: the state of `.agents/skills/recording-what-you-learn/`. The skill is repository-customizable, so a local edit is reported as such without warning; only a missing one warns.
  - `gitleaks`, `root:exists`, `git-hooks:*`, `git-attributes`: scanner presence, the stamped checkout, and the git hook chain and attributes.
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
| `agents init` | create .agents/, the doc stores, and the wiring |
| `agents wire` | remove this tool's entries from harness configs |
| `agents doctor` | report wiring, trust, and scaffold state |
| `agents version` | print binary version and build provenance |
| `agents guard` | pre-commit checks (the only command that blocks) |
<!-- END GENERATED -->

### Examples

```bash
# Create the agent context in a repository
agents init

# Keep .agents/ out of the repository (refused inside a linked worktree)
agents init --local

# Remove hook entries an earlier version wrote
agents wire

# Report wiring, trust, and the state of the files this tool owns
agents doctor

# Pre-commit secret and staged-path scan (run automatically by the hook)
agents guard --staged
```

## Upgrading

Two things need attention when you upgrade this tool.

**Re-check the git hooks.** A package manager deletes the previous version's
directory, so hook links pinned to it dangle — and git runs a dangling hook as
if no hook existed, which turns the commit guard off with no error at all.
`agents doctor`'s `git-hooks:links` check catches it and prints the repair:

```bash
bash ~/dotfiles/git/install-hooks.sh install --adopt-owned \
  ~/dotfiles "$HOME" "$(command -v agents)"
```

Installing through `$(command -v agents)` rather than `$(realpath …)` avoids the
problem in the first place, because Homebrew repoints its stable path at the new
version. Details: [`git/README.md`](../git/README.md) and
[why a `brew upgrade` stops my commit guard](../docs/qna/why-does-a-brew-upgrade-stop-my-commit-guard.md).

**Run `agents wire` once, after upgrading past a version that installed hook
entries.** Earlier versions wrote an `agents hook …` entry into each harness
config to record session lifecycle events. This tool no longer records anything,
so those entries point at a subcommand that no longer exists — which the harness
would run at the start of every session and fail. `wire` removes exactly those
entries and leaves every other setting alone. `agents doctor` reports them as
`wiring:<harness>` failures with `agents wire` as the remedy.

---

## License

MIT
