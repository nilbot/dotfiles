# Why does `install-hooks.sh` refuse symlinked binaries like `/opt/homebrew/bin/agents`?

## Context

When configuring personal dotfiles Git hooks against a Homebrew-installed binary using:

```bash
bash ~/dotfiles/git/install-hooks.sh install ~/dotfiles $HOME (which agents)
```

`install-hooks.sh` halts and exits with an error:

```text
install-hooks: refusing: agents binary '/opt/homebrew/bin/agents' must be an executable regular file
```

## Why it happens

1. **Homebrew's Symlink Architecture**:
   Homebrew installs software into isolated Cellar prefixes (`/opt/homebrew/Cellar/agents/<version>/bin/agents`) and symlinks those executables into `/opt/homebrew/bin/agents`.

2. **The Non-Symlink Invariant in `install-hooks.sh`**:
   [`git/install-hooks.sh`](../../git/install-hooks.sh) enforces strict safety validation before configuring `core.hooksPath`:
   ```bash
   validate_binary() {
       if [ ! -f "$binary" ] || [ ! -x "$binary" ] || [ -L "$binary" ]; then
           refuse "agents binary '$binary' must be an executable regular file"
       fi
   }
   ```
   It deliberately refuses symlinked binary inputs (`[ -L "$binary" ]`) to prevent **symlink chaining** (a symlink in `git/hooks.d/` pointing to a symlink in `/opt/homebrew/bin/` pointing to the Cellar).
   Chained symlinks make `doctor` diagnostics fragile and subject to ambient `PATH` shadowing.

## How to resolve

Use `realpath` to resolve the canonical regular file in Homebrew's Cellar before invoking the installer:

```bash
# In fish:
bash ~/dotfiles/git/install-hooks.sh install ~/dotfiles $HOME (realpath (which agents))

# In bash / zsh:
bash ~/dotfiles/git/install-hooks.sh install ~/dotfiles "$HOME" "$(realpath "$(which agents)")"
```

The installer verifies the underlying regular executable in the Cellar, links `~/dotfiles/git/hooks.d/*` directly to it, and `agents doctor` confirms `git-hooks:links` with `ok`.
