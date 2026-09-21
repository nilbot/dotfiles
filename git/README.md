# Git hook provisioning

How the global Git hook chain is installed and how to check it. Moved here from
the global instruction file, now `global/AGENTS.md`: that file is symlinked to
`~/.dsh/AGENTS.md`, `~/.claude/CLAUDE.md`, and `~/.codex/AGENTS.md`, so it loads
into every session in every repository, where install-time instructions cost
context on every turn and are never acted on.

The rule that file still carries — no AI attribution in anything that lands in a
repository — stays there, because a session does act on it.

## Installing

### Option 1: Via Bootstrap (Build from Source)
Run `./bootstrap apply workstation` from the dotfiles checkout. Its devtools
phase runs the installer's preflight, builds `~/bin/agents`, then runs the
installer.

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
before it builds or changes anything. It refuses a foreign global
`core.hooksPath` instead of replacing it or implicitly chaining it. It accepts a
symlinked binary when it resolves into a Homebrew keg for agents, and records the
path it was given, so passing `$(command -v agents)` records the stable path.

After a successful install, Git invokes the Go-backed `agents` multicall binary
for `pre-commit`, `commit-msg`, `post-merge`, and `post-checkout` in new and
pre-existing repositories. Existing repository hooks and the executable personal
hooks in `git/hooks/` remain chained by the dispatcher. A repository with its
own local `core.hooksPath` intentionally overrides the global chain.

## After upgrading `agents`

A package upgrade deletes the previous version's directory. If the hooks name it,
they dangle — and **git runs a dangling hook as if no hook existed**, so the
commit guard stops running with no error. Run `agents doctor` and check
`git-hooks:links`; when the link is one this installer wrote, it prints the exact
command that repairs it, which is this one:

```bash
bash ~/dotfiles/git/install-hooks.sh install --adopt-owned \
  ~/dotfiles "$HOME" "$(command -v agents)"
```

`--adopt-owned` repoints links this installer wrote for an earlier binary. A
foreign file, or a link to another program, is still refused with or without the
flag. Installing through `$(command -v agents)` in the first place means this
step never comes up: Homebrew repoints its stable path for you.

## Checking an install

```bash
# The global value should be this checkout's git/hooks.d directory.
git config --global --show-origin --get-all core.hooksPath

# Each installed hook should resolve to the freshly built ~/bin/agents.
for hook in pre-commit commit-msg post-merge post-checkout; do
  readlink "$HOME/dotfiles/git/hooks.d/$hook"
done

# The global attributes link should resolve to the tracked attributes file.
readlink "$HOME/.gitattributes"
```

`agents doctor`, run inside any repository, checks all of the above and more.

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
| `~/bin/agents` | Go-backed multicall hook dispatcher |
| `git/install-hooks.sh` | ownership-checking installer |
| `git/hooks/` | optional executable personal hook stages |
