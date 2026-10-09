# Git hook provisioning

How the global Git hook chain is installed and how to check it. Moved here from
the global instruction file, now `global/AGENTS.md`: that file is symlinked to
`~/.dsh/AGENTS.md`, `~/.claude/CLAUDE.md`, and `~/.codex/AGENTS.md`, so it loads
into every session in every repository, where install-time instructions cost
context on every turn and are never acted on.

The rule that file still carries — no AI attribution in anything that lands in a
repository — stays there, because a session does act on it.

## Installing

### Option 1: Via Bootstrap
Run `./bootstrap apply workstation` from the dotfiles checkout. Its devtools
phase resolves the released `agents`, runs the installer's preflight, then runs
the installer — both with `--adopt-owned`, so the phase converts a chain an
earlier binary installed instead of refusing it.

### Option 2: Pointing to an Existing Binary (e.g. Homebrew)
If `agents` is installed via Homebrew (`brew install nilbot/tap/agents`), pass the
path that Homebrew keeps stable and repoints on every upgrade:

```bash
bash ~/dotfiles/git/install-hooks.sh install ~/dotfiles "$HOME" "$(command -v agents)"
```

Do **not** resolve it first with `realpath`. That yields
`Cellar/agents/<version>/bin/agents`, and Homebrew deletes that directory when it
upgrades — which leaves the hooks dangling, which git runs as if no hook existed.

The installer checks for existing Git-hook and global-attributes ownership
before it writes anything. It refuses a foreign global `core.hooksPath` instead
of replacing it or implicitly chaining it. It accepts a symlinked binary when it
resolves into a Homebrew keg for agents, and records the path it was given, so
passing `$(command -v agents)` records the stable path.

After a successful install, `core.hooksPath` names a chain directory holding the
machine's record (`chain.env`) and four executable entries for `pre-commit`,
`commit-msg`, `post-merge` and `post-checkout`. Each entry parses the record and
runs `agents githook <name> --checkout <checkout>`, which runs the repository's
own hook, then the executable personal hooks in `git/hooks/`, then the built-in
stage. A repository with its own local `core.hooksPath` intentionally overrides
the global chain.

**The chain is machine-owned, and that is the point of where it lives.** It sits
under the home directory rather than inside a checkout, because a checkout is a
directory a person moves and deletes: while the chain lived at
`<checkout>/git/hooks.d`, deleting the checkout took the commit guard with it and
Git reported nothing. An install that finds the chain in the checkout — the shape
stage 1 wrote — retires those entries and repoints `core.hooksPath` at the
machine-owned directory.

**This repository no longer produces the binary the chain names** — decided
2026-10-09 and removed with the change that consumes the tap. The chain names the
released `agents`, installed with `brew install nilbot/tap/agents`. See
[the personal build removal analysis](../docs/design/2026-10-09-the-personal-build-removal-analysis.md).

## After upgrading `agents`

A package upgrade deletes the previous version's directory. If the chain names
it, it dangles — and **git runs a dangling hook as if no hook existed**, so the
commit guard stops running with no error. Re-run the installer; when a name holds
an entry this installer wrote, it refuses and prints the exact command that
repairs it, which is this one:

```bash
bash ~/dotfiles/git/install-hooks.sh install --adopt-owned \
  ~/dotfiles "$HOME" "$(command -v agents)"
```

`--adopt-owned` replaces a hook name this installer wrote for an earlier binary —
a symlink it installed then, or an entry naming another checkout or written by an
older form of the script. A foreign file, or a link to another program, is still
refused with or without the flag. Installing through `$(command -v agents)` in
the first place means this step never comes up: Homebrew repoints its stable path
for you.

## Checking an install

```bash
# The global value should name the machine-owned chain directory.
git config --global --show-origin --get-all core.hooksPath

# The record should name the binary to run and the checkout it belongs to.
cat "$HOME/.config/agents/hooks.d/chain.env"

# Each entry should be an executable regular file ending in the command git
# will run for that name.
for hook in pre-commit commit-msg post-merge post-checkout; do
  test -x "$HOME/.config/agents/hooks.d/$hook" && tail -n 1 "$HOME/.config/agents/hooks.d/$hook"
done

# The global attributes link should resolve to the tracked attributes file.
readlink "$HOME/.gitattributes"
```

`agents doctor`, run inside any repository, reads that chain where
`core.hooksPath` points it and reports it by name: `chain:hooks-path`,
`chain:entries`, `chain:record`, `chain:running` (the recorded binary against
the one running), `chain:checkout` (the personal stages, and how many were
found), `chain:unmanaged`, `chain:local`, `chain:legacy`, and
`attributes:global`.

### Expected behaviour

- Commits complete normally
- Claude attribution footers are stripped from commit messages
- Commit messages remain clean in `git log`
- Existing repository and personal hooks run before the built-in stage
- A local `core.hooksPath` override bypasses the global chain, by Git's design

**The hooks cannot see pull requests.** `gh` talks to the GitHub API, not to
Git, so nothing here protects a PR title or body. That half of the attribution
rule is enforced by reading it, which is why it lives in `global/AGENTS.md`.

## Files

| Path | What it is |
|---|---|
| `global/AGENTS.md` | the tracked global instruction file, symlinked to `~/.dsh/AGENTS.md`, `~/.claude/CLAUDE.md`, and `~/.codex/AGENTS.md` |
| `bootstrap.d/links.manifest` | declares that symlink, and every other managed path |
| `~/.config/agents/hooks.d/` | the chain `core.hooksPath` names: `chain.env`, and the four generated entries git runs. Machine-owned, so a moved or deleted checkout cannot switch the guard off |
| `git/install-hooks.sh` | ownership-checking installer |
| `git/hooks/` | optional executable personal hook stages |
| `git/hooks.d/` | where stage 1 kept the chain. The installer reads it to retire what it wrote there, then removes the directory |
