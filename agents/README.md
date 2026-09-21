# agents

Scaffolds and maintains the repository context an AI coding agent needs: the two-tier instruction files, the documentation stores, the bundled skill, and the harness wiring that makes them visible.

---

## Features

- **Repository Scaffold**: `agents init` creates `.agents/` and the four documentation stores under `docs/{design,plans,journal,qna}`, each with a README explaining what belongs in it, the root `AGENTS.md` router and its `CLAUDE.md` symlink, `.agents/AGENTS.md` for repository-specific rules, the bundled `recording-what-you-learn` skill, and the `.agents/** linguist-generated=true` rule in the repository's `.gitattributes`.
- **Harness Wiring**: Makes `.agents/skills/` visible where each harness looks for it — a relative `.claude/skills` symlink for Claude Code and a relative `.codex/skills` symlink for Codex, both pointing at `.agents/skills`. Antigravity reads `.agents/` in place and needs no symlink. The tool writes no hook entries of its own: nothing records harness lifecycle events any more.
- **Retired-Entry Cleanup**: `agents wire` removes from `.claude/settings.json`, `.codex/hooks.json` and `.agents/hooks.json` the hook entries this tool wrote in an earlier version, and preserves every other setting in those files. A config left holding nothing is removed rather than left behind as `{}`.
- **Repository Guardrails & Pre-Commit Secret Scanning**: `agents guard --staged` scans the staged `.agents/` blobs with `gitleaks` to catch secret leaks, blocks a staged `.agents/` path carrying a control character, and warns when one commit mixes agent context with code. Invoked automatically from the pre-commit hook.
- **Commit Message Sanitization**: the installed `commit-msg` hook strips the Claude attribution footer and the `Co-Authored-By: Claude` trailer from the trailing trailer block, so git histories read as the author's own work. A human co-author trailer or another tool's footer is left as written.
- **Self-Diagnostic Tooling**: `agents doctor` reports whether anything stale is left in the harness configs, what the harnesses trust, and the state of the files this tool owns. It observes; it never mutates.

---

## Installation

### Build from Source (Go 1.26+)

```bash
cd agents
go build -o ~/bin/agents .
```

The binary is self-contained. Two builders in this repository run the same
command with the operator-mode stamp added: `make agents` from the repository
root, and the devtools phase of `./bootstrap apply workstation`.

### Released Binaries

```bash
brew install nilbot/tap/agents
```

This installs the newest tagged release from the `nilbot/homebrew-tap` tap.
Releases are cut from tags by
[`.github/workflows/release.yml`](../.github/workflows/release.yml):
`script/package-release.sh` builds darwin/{arm64,amd64} and linux/{arm64,amd64}
archives plus `checksums.txt`, the workflow asserts the packaged binary reports
the tag's version and commit, and `script/sync-homebrew-formula.sh` pushes the
regenerated `Formula/agents.rb` to the tap.

A release carries the tree at its tag, which is not necessarily the tree you are
reading. The module has no `agents/vX.Y.Z` tags, so
`go install github.com/nilbot/dotfiles/agents@latest` resolves to a
pseudo-version of the default branch rather than a release. `agents version`
prints what a binary actually is.

---

## Quickstart

Initialize any Git repository to create the agent context and wire it to the harnesses you use:

```bash
cd my-project
agents init
```

`init` takes one flag, `--local`, which keeps `.agents/` out of the repository.
It is refused inside a linked worktree, where `info/exclude` is shared with the
main checkout.

It exits 1 (advisory) even on success, because wiring is written but not yet live — each harness has a trust step no process can perform for you, and `init` prints the list.

Verify the repository is in the state it should be:

```bash
agents doctor
```

Re-run `agents wire` after upgrading from a version that installed hook entries;
it removes them.

## Operating Modes

`agents` operates in two modes, chosen by whether the binary knows a dotfiles
checkout. The stamp beats the environment variable, so a stamped binary cannot
be redirected by `AGENTS_DOTFILES_ROOT`.

### 1. Standalone Mode (Default)

A binary built without the stamp and without `AGENTS_DOTFILES_ROOT` operates as
a standalone repository tool.
- Requires no external dotfiles clone.
- `agents doctor` reports:
  - `binary`: whether the `agents` on `PATH` is the running executable.
  - `wiring:<harness>`: whether a harness config still holds an entry this tool wrote and no longer answers. Failures come with `agents wire` as the remedy; an absent config is OK, not a gap.
  - `trust:antigravity`: whether the Antigravity CLI config is readable and names this repository as trusted.
  - `gitleaks`: scanner presence.
  - `scaffold:router`, `scaffold:symlink`, `scaffold:domain`: the root `AGENTS.md`, its `CLAUDE.md` symlink, and `.agents/AGENTS.md`. The symlink check is the one that catches a silent failure: extracted or synced without symlink support, `CLAUDE.md` becomes a regular file whose content is the text `AGENTS.md`, and a harness then reads that one line as the whole project context.
  - `scaffold:skill-recording`: the state of `.agents/skills/recording-what-you-learn/`. The skill is repository-customizable, so a local edit is reported as such without warning; only a missing one warns.
  - `git-hooks:local`, `git-hooks:legacy`, `git-attributes`: a repository-local `core.hooksPath` override, an exact retired dispatcher left in the repository's hooks directory, and the repository `.gitattributes` rule.
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
- Operator Mode adds `root:exists` and the `git-hooks:global`, `git-hooks:effective`, `git-hooks:links` and `git-hooks:unmanaged` checks, which hold the global `core.hooksPath` and the four installed hook links in `~/dotfiles/git/hooks.d/` to what this tool expects.
- The dispatcher runs the repository's own hook, then the executable personal hooks named `<anything>.<hook>` in `~/dotfiles/git/hooks/`; on `pre-commit` the built-in guard runs last.

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

`agents help` prints the listing a person reads, which leaves out `guard` — the
one command only the hook invokes. `agents help --all` includes it.

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
if no hook existed, which turns the commit guard off with no error at all. In
Operator Mode, `agents doctor`'s `git-hooks:links` check catches it and prints
the repair, and `git-hooks:unmanaged` warns about dangling links this tool does
not own:

```bash
bash ~/dotfiles/git/install-hooks.sh install --adopt-owned \
  ~/dotfiles "$HOME" "$(command -v agents)"
```

Installing through `$(command -v agents)` rather than `$(realpath …)` avoids the
problem in the first place, because Homebrew repoints its stable path — the
`/opt/homebrew/bin/agents` symlink into the current keg — at the new version.
Details: [`git/README.md`](../git/README.md) and
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
