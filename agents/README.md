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

The binary is self-contained. Two builders in this repository run the build for
you: `make agents` from the repository root, and the devtools phase of
`./bootstrap apply workstation`. Both also pass a link-time
`-X main.dotfilesRoot=<checkout>` stamp. Nothing in this version reads it — the
checkout a hook needs now arrives from the chain record — and the flag and its
two producers are removed by a later change.

### Released Binaries

```bash
brew install nilbot/tap/agents
```

This installs whichever release `nilbot/homebrew-tap` currently points at — the
formula lives in that repository and is maintained there, not here.

Releases are cut from tags by
[`.github/workflows/release.yml`](../.github/workflows/release.yml):
`script/package-release.sh` builds darwin/{arm64,amd64} and linux/{arm64,amd64}
archives plus `checksums.txt`, and the workflow asserts the packaged binary
reports the tag's version and commit before publishing them.

**Releasing updates the tap from the release itself.** `release.yml` runs
`script/sync-homebrew-formula.sh` after it publishes the archives, then runs it
again with `--check` to assert the tap moved. The script reads the formula the
tap already has through the Contents API and rewrites only its four `url` and
four `sha256` lines, so `nilbot/homebrew-tap` stays the single source of truth
and this repository keeps no copy of the formula. The step needs the
`HOMEBREW_TAP_TOKEN` secret; without it the job fails *after* the release is
published, and the tap keeps pointing at the previous version until the sync is
run.

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

## Git hooks

The binary has no modes. There is one way it runs, and it is the same whether
the binary was built from a checkout or installed from a release: the machine
either has a hook chain, or it does not.

A chain is the directory Git's `core.hooksPath` names. `git/install-hooks.sh`
writes it, and it holds two kinds of thing:

- `chain.env`, the machine's record: `format=1`, the `binary` Git should run,
  and the `checkout` whose `git/hooks/` supplies the personal stages — `-` when
  there is none.
- Four executable entries named `pre-commit`, `commit-msg`, `post-merge` and
  `post-checkout`. Each parses the record and ends by executing
  `agents githook <name> --checkout <checkout>` with Git's own arguments
  untouched.

`agents githook` runs one hook in three parts, in order: the repository's own
hook of that name, then the executable personal stages named
`<anything>.<hook>` under `<checkout>/git/hooks/`, then the built-in stage. It
reads no record and derives nothing — the checkout is handed to it, and `-`
means there is none, never that one should be guessed at.

A machine still wired the old way, with four symlinks naming the binary, keeps
working for one release through a compatibility shim. The shim answers all four
hook names with Git's arguments, and says on stderr that the personal stages
are unavailable on that route, because a symlink names a program and no
checkout. Re-running the installer converts it.

`agents doctor` reports the repository-level checks only for now. Reading the
chain is its own change; until that lands, nothing reports the state of the
chain except the entries themselves.

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

`agents help` prints the listing a person reads, which leaves out `guard` and
`githook` — the commands git invokes, not a person. `agents help --all` includes
them.

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
directory, so a chain entry naming it dangles — and git runs a dangling hook as
if no hook existed, which turns the commit guard off with no error at all. The
chain record names the binary to run, so re-run the installer after an upgrade:

```bash
bash ~/dotfiles/git/install-hooks.sh install --adopt-owned \
  ~/dotfiles "$HOME" "$(command -v agents)"
```

Installing through `$(command -v agents)` rather than `$(realpath …)` avoids the
problem in the first place, because Homebrew repoints its stable path — the
`/opt/homebrew/bin/agents` symlink into the current keg — at the new version.
Details: [`git/README.md`](../git/README.md) and
[why a `brew upgrade` stops my commit guard](../docs/qna/why-does-a-brew-upgrade-stop-my-commit-guard.md).

**Converting an install that still uses symlinks.** `--adopt-owned` replaces
the four symlinks with four generated entries. Without it the installer refuses
a symlink it wrote itself, which is the state a machine wired by an earlier
version is in. Until it is converted, every hook reports on stderr that the
personal stages are unavailable on the symlink route.

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
