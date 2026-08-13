# Git hook provisioning

How the global Git hook chain is installed and how to check it. Moved here from
`claude/CLAUDE.md`: that file is symlinked to `~/.claude/CLAUDE.md` and therefore
loads into every Claude Code session in every repository, where install-time
instructions cost context on every turn and are never acted on.

The rule that file still carries — no AI attribution in anything that lands in a
repository — stays there, because a session does act on it.

## Installing

Run `./bootstrap apply workstation` from the dotfiles checkout. Its devtools
phase runs the installer's preflight, builds `~/bin/agents`, then runs the
installer.

The installer checks for existing Git-hook and global-attributes ownership
before it builds or changes anything. It refuses a foreign global
`core.hooksPath` instead of replacing it or implicitly chaining it.

After a successful install, Git invokes the Go-backed `agents` multicall binary
for `pre-commit`, `commit-msg`, `post-merge`, and `post-checkout` in new and
pre-existing repositories. Existing repository hooks and the executable personal
hooks in `git/hooks/` remain chained by the dispatcher. A repository with its
own local `core.hooksPath` intentionally overrides the global chain.

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
rule is enforced by reading it, which is why it lives in `claude/CLAUDE.md`.

## Files

| Path | What it is |
|---|---|
| `claude/CLAUDE.md` | the tracked global instruction file, symlinked to `~/.claude/CLAUDE.md` |
| `bootstrap.d/links.manifest` | declares that symlink, and every other managed path |
| `~/bin/agents` | Go-backed multicall hook dispatcher |
| `git/install-hooks.sh` | ownership-checking installer |
| `git/hooks/` | optional executable personal hook stages |
