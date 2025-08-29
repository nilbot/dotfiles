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

## Implementation - Dotfiles Integration

**This protection system is now fully integrated into the dotfiles repository.**

### Automated Setup via Dotfiles
The protection is automatically installed when you set up dotfiles:

```bash
# Standard dotfiles installation includes Claude protection
make dotfiles

# This automatically:
# 1. Symlinks ~/.claude to dotfiles/claude/
# 2. Configures git to use dotfiles/git/templates/
# 3. Installs commit-msg hooks in all new repositories
# 4. Applies protection to the current repository
```

### How It Works
1. **Template System**: `dotfiles/git/templates/hooks/commit-msg` runs modular hooks
2. **Modular Hooks**: `dotfiles/git/hooks/claude.commit-msg` strips Claude footers
3. **Automatic Application**: All new `git init` and `git clone` get protection
4. **Symlinked Access**: `~/.claude/` points to `dotfiles/claude/` for easy access

## Manual Installation Methods (Alternative)

### Method 1: Standalone Git Hook
If not using dotfiles integration:

```bash
# Create hook directory
mkdir -p ~/.git-templates/hooks

# Install protection hook
cp ~/.claude/commit-msg ~/.git-templates/hooks/
chmod +x ~/.git-templates/hooks/commit-msg

# Configure git globally
git config --global init.templatedir ~/.git-templates
```

### Method 2: Per-Repository Hook
For individual repositories:

```bash
# Copy hook to specific repo
cp ~/.claude/commit-msg .git/hooks/
chmod +x .git/hooks/commit-msg
```

## Verification and Testing

### Check Protection Status
```bash
# Verify dotfiles integration
ls -la ~/.claude  # Should show symlink to dotfiles/claude/

# Check git configuration
git config --global --get init.templatedir  # Should show dotfiles/git/templates

# Verify hook in current repo
ls -la .git/hooks/commit-msg
```

### Test Protection
```bash
# Check for violations in any repository
~/.claude/check-commits.sh [repo-path] [num-commits]

# Test hook functionality (creates no actual commit)
echo "Test

🤖 Generated with [Claude Code](https://claude.ai/code)" | .git/hooks/commit-msg /dev/stdin
```

## Files and Scripts

### Core Files (in dotfiles/claude/)
- **`CLAUDE.md`** - This configuration file
- **`commit-msg`** - Standalone commit message hook
- **`check-commits.sh`** - Violation detection script  
- **`setup-protection.sh`** - Manual setup helper (deprecated - use dotfiles)

### Dotfiles Integration Files
- **`git/templates/hooks/commit-msg`** - Template hook dispatcher
- **`git/hooks/claude.commit-msg`** - Modular Claude protection hook
- **`Makefile`** - Updated to include ~/.claude symlink management

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

## Troubleshooting

### If Protection Isn't Working
```bash
# 1. Verify dotfiles setup
make dotfiles

# 2. Check current repo has hook
ls -la .git/hooks/commit-msg

# 3. Manually install if needed
cp ~/dotfiles/git/templates/hooks/commit-msg .git/hooks/
chmod +x .git/hooks/commit-msg

# 4. Test hook manually
echo "test" > /tmp/msg && .git/hooks/commit-msg /tmp/msg
```

### If Violations Occurred
```bash
# Immediate fix for last commit
git commit --amend -m "Clean message without footers"

# Bulk fix for multiple commits (DANGEROUS - rewrites history)
git filter-branch -f --msg-filter 'grep -v "🤖 Generated with.*Claude Code" | grep -v "Co-Authored-By: Claude"' HEAD~N..HEAD
```

### Hook Performance Issues
If commits hang:
1. Check hook syntax: `bash -n .git/hooks/commit-msg`
2. Test manually: `.git/hooks/commit-msg /tmp/test_msg`
3. Reinstall from templates: `cp ~/dotfiles/git/templates/hooks/commit-msg .git/hooks/`

## Migration Notes

### Upgrading from Standalone System
If you previously used `~/.git-templates/hooks/commit-msg`:

```bash
# 1. Install dotfiles integration
make dotfiles

# 2. Update git configuration (done automatically by dotfiles)
git config --global init.templatedir ~/dotfiles/git/templates

# 3. Update existing repositories
find ~/devel -name ".git" -type d -exec cp ~/dotfiles/git/templates/hooks/commit-msg {}/hooks/ \;
```

---

**🚨 CRITICAL**: This configuration is mandatory for ALL Claude Code usage.  
**🔄 DOTFILES**: Protection is now managed through dotfiles repository.  
**✅ VERIFY**: Test protection after any system changes or updates.