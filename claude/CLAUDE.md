# Global Claude Code Configuration

This file contains global settings and constraints that apply to all Claude Code sessions.

## Commit Message Constraints

**CRITICAL: These constraints MUST be enforced in ALL repositories and sessions:**

### Forbidden Content in Commit Messages
The following content is **PERMANENTLY BANNED** from all commit messages:

```
🤖 Generated with [Claude Code](https://claude.ai/code)
Co-Authored-By: Claude <noreply@anthropic.com>
```

## Implementation

**This protection system is integrated into the dotfiles.**

Run `./bootstrap apply workstation` from the dotfiles checkout. Its devtools
phase runs the installer's preflight, builds `~/bin/agents`, then runs the
installer. The installer checks for existing Git-hook and global-attributes
ownership before it builds or changes anything. It refuses a foreign global
`core.hooksPath` instead of replacing or implicitly chaining it.

After a successful install, Git invokes the Go-backed `agents` multicall binary
for `pre-commit`, `commit-msg`, `post-merge`, and `post-checkout` in new and
pre-existing repositories. Existing repository hooks and the executable
personal hooks in `git/hooks/` remain chained by the dispatcher. A repository
with its own local `core.hooksPath` intentionally overrides the global chain.

## Session Behavior Rules

### Before Any Git Commit
1. **ALWAYS** check commit message for forbidden content
2. **NEVER** use standard Claude Code commit templates with footers
3. **ALWAYS** use HEREDOC pattern for multi-line commits
4. **VERIFY** final commit message with `git log -1`

### Git Command Patterns
```bash
# ✅ CORRECT - Clean commit message
git commit -m "fix: resolve critical issue

Details about the fix here."

# ❌ INCORRECT - Contains forbidden footer
git commit -m "fix: resolve critical issue

🤖 Generated with [Claude Code](https://claude.ai/code)"
```

### Recommended Commit Template
Always use this pattern for multi-line commits:

```bash
git commit -m "$(cat <<'EOF'
Short summary of changes

Detailed explanation of what was changed and why.
Include any relevant context or technical details.
EOF
)"
```

## Verification

### Quick Status Check
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

### Expected Behavior
- ✅ Commits complete normally
- ✅ Claude footers are silently removed
- ✅ Commit messages remain clean in `git log`
- ✅ Existing repository and personal hooks run before the built-in stage
- ⚠️ A local `core.hooksPath` override bypasses the global chain by Git design

## Files Reference

- **`~/.claude/CLAUDE.md`** - This configuration file
- **`~/bin/agents`** - Go-backed multicall hook dispatcher
- **`~/dotfiles/git/install-hooks.sh`** - ownership-checking installer
- **`~/dotfiles/git/hooks/`** - optional executable personal hook stages

---

**🚨 CRITICAL**: This configuration is mandatory for ALL Claude Code usage.  
**🔄 AUTOMATIC AFTER INSTALL**: Protection applies through global Git hook dispatch.
**🔎 VERIFY LOCALLY**: Use the commands above after installation.
