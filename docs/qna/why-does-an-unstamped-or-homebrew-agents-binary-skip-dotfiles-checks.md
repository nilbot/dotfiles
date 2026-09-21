# Why does an unstamped or Homebrew agents binary skip dotfiles checks?

## Context

2026-08-28, Spec 6 (Releases & Distribution) and Explicit Binary Identity & Standalone Resolution ([design doc](../design/2026-08-28-binary-identity-and-standalone-resolution.md)).

The `agents` CLI is distributed both as a local dotfiles tool (`make agents` / `./bootstrap apply workstation`) and as an independent standalone binary via Homebrew (`brew install nilbot/tap/agents`) and GitHub Releases.

Historically, `DotfilesRoot()` used a filesystem heuristic: if `$HOME/dotfiles` existed on disk, an unstamped binary inferred that it was running in Dotfiles Operator Mode. When running `agents doctor` inside a standalone repo (such as `cowork`) on a machine where `~/dotfiles` existed, the Homebrew binary attempted to validate machine-level hooks against `~/dotfiles/git/hooks.d` and failed because the hook symlinks pointed to `~/bin/agents` instead of the Homebrew Cellar binary.

To make binary mode resolution deterministic across all machines, `DotfilesRoot()` was transitioned to an explicit resolution contract:
1. Link-time stamp (`-X main.dotfilesRoot=<path>`)
2. Environment variable (`AGENTS_DOTFILES_ROOT=<path>`)
3. Standalone Mode default (`""`)

## Answer

### 1. Release and Homebrew binaries are unstamped by design

CI release builds and Homebrew formula installations produce generic binaries that belong to no specific dotfiles checkout (`main.dotfilesRoot == ""`). Without a compile-time stamp or an explicit `AGENTS_DOTFILES_ROOT` environment variable, `DotfilesRoot()` returns `""`, placing the binary in **Standalone Mode**.

### 2. Standalone Mode skips machine-level dotfiles checks

When `DotfilesRoot() == ""` (`deps.Root == ""`):
- `root:exists`: Skipped because the binary has no bound dotfiles checkout.
- `git-hooks:global`: Skipped because configuring global `core.hooksPath` is an operator dotfiles concern, not a repository concern.
- `git-hooks:links`: Skipped because verifying symlinks in `hooks.d` against the active binary only applies to dotfiles operator environments.
- `gitattributes:global`: Skipped (`~/.gitattributes` link check); instead, the repository-local `.gitattributes` is checked directly for `.agents/** linguist-generated=true`.
- **All repository diagnostics run in full**: PATH resolution, harness wiring (`.claude`, `.codex`, `.agents`), trust verification, transcript recording, secret scanning (`gitleaks`), trace indexing, pointer checks, and documentation freshness are strictly enforced.

### 3. Activating Dotfiles Operator Mode with a Homebrew binary

If an operator chooses to use the Homebrew binary (`/opt/homebrew/bin/agents`) to manage their personal machine and dotfiles hook chains, they can explicitly set `AGENTS_DOTFILES_ROOT`:

```bash
export AGENTS_DOTFILES_ROOT="$HOME/dotfiles"
```

With `AGENTS_DOTFILES_ROOT` exported in the environment:
- `DotfilesRoot()` resolves to the configured directory.
- `agents doctor` validates machine-level dotfiles health, global hooks in `$AGENTS_DOTFILES_ROOT/git/hooks.d`, and global `~/.gitattributes`.
- Git hook multi-call dispatches personal hook stages from `$AGENTS_DOTFILES_ROOT/git/hooks/*`.

## Follow-up, 2026-09-21

**The resolution contract is unchanged; one bullet in §2 is now wrong.**

The bullet listing what runs in full in Standalone Mode still names transcript
recording, trace indexing and pointer checks. All three were deleted on
2026-09-21, with the trace store, the session record and the fleet registry they
belonged to. The rest of that bullet — PATH resolution, harness wiring, trust
verification, secret scanning and documentation freshness — is still what runs.

The bullet listing what is skipped is still right, and was re-measured in
operator mode on this machine:

```text
$ agents doctor
ok    root:exists                  the stamped checkout exists
ok    git-hooks:global             global core.hooksPath is exact
ok    git-hooks:local              no repository-local core.hooksPath override
ok    git-hooks:effective          effective core.hooksPath is exact
ok    git-hooks:unmanaged          no unowned hook links dangle
ok    git-attributes               global and repository attributes are exact
```

The three-step order — link-time stamp, then `AGENTS_DOTFILES_ROOT`, then
Standalone Mode — is untouched, and so is the reason for it: a binary that
belongs to no checkout must not guess at one.

One consequence of the reduction does land here. With no session record and no
transcript cache, a Standalone Mode binary has no machine-local state to write at
all: it scaffolds on request, strips wiring on request, and otherwise reports.
The mode difference is now about which machine-level checks run, not about where
anything is stored.
