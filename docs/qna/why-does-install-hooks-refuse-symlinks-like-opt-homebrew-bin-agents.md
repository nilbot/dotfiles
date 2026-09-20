# Why does `install-hooks.sh` refuse symlinked binaries like `/opt/homebrew/bin/agents`?

## Context

Configuring personal dotfiles Git hooks against a Homebrew-installed binary:

```bash
bash ~/dotfiles/git/install-hooks.sh install ~/dotfiles "$HOME" "$(command -v agents)"
```

Until 2026-09-20 this halted and exited with an error:

```text
install-hooks: refusing: agents binary '/opt/homebrew/bin/agents' must be an executable regular file
```

## Why it happened

1. **Homebrew's symlink architecture.** Homebrew installs into isolated Cellar
   prefixes (`/opt/homebrew/Cellar/agents/<version>/bin/agents`) and symlinks
   those executables into `/opt/homebrew/bin/agents`.

2. **The non-symlink invariant.** [`git/install-hooks.sh`](../../git/install-hooks.sh)
   refused every symlinked binary input (`[ -L "$binary" ]`) to prevent **symlink
   chaining** — a link in `git/hooks.d/` pointing at a symlink in
   `/opt/homebrew/bin/` pointing into the Cellar — on the grounds that chained
   symlinks make `doctor` diagnostics fragile and subject to ambient `PATH`
   shadowing.

## What the rule is now

The invariant was stronger than anything `doctor` relied on: its check follows
the chain and compares file identity with the running binary, so the chain itself
was never the thing being verified. The cost of the stronger rule was paid on
every upgrade — see
[why a `brew upgrade` stops my commit guard](why-does-a-brew-upgrade-stop-my-commit-guard.md).

Since 2026-09-20 a symlink **is** accepted, when it resolves into a Homebrew keg
for agents (`*/Cellar/agents/*/bin/agents`). The path **recorded** is the one
passed in, not the keg it resolves to. A symlink to anywhere else is still
refused.

```bash
# In fish:
bash ~/dotfiles/git/install-hooks.sh install ~/dotfiles $HOME (command -v agents)

# In bash / zsh:
bash ~/dotfiles/git/install-hooks.sh install ~/dotfiles "$HOME" "$(command -v agents)"
```

Pass `$(command -v agents)` — Homebrew's stable `bin/agents` — and **not**
`$(realpath "$(which agents)")`. `realpath` resolves to
`Cellar/agents/<version>/bin/agents`, which the next `brew upgrade` deletes; the
installer now refuses that shape when a stable path is already installed, naming
the stable path to use instead.

The installer verifies the resolved file is an executable regular file in the
Cellar, links `~/dotfiles/git/hooks.d/*` at the path you passed, and `agents
doctor` confirms `git-hooks:links` with `ok`.
