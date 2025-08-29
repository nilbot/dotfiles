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

**This protection system is integrated into the dotfiles and works automatically.**

When you install dotfiles with `make dotfiles`, commit message protection is automatically configured system-wide. All new repositories and commits will have Claude Code footers automatically stripped.

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
# Verify protection is active
~/.claude/check-commits.sh

# Check recent commits for violations  
~/.claude/check-commits.sh . 10
```

### Expected Behavior
- ✅ Commits complete normally
- ✅ Claude footers are silently removed
- ✅ Commit messages remain clean in `git log`
- ✅ No performance issues or hanging

## Files Reference

- **`~/.claude/CLAUDE.md`** - This configuration file
- **`~/.claude/check-commits.sh`** - Violation detection script
- **`~/.claude/commit-msg`** - Standalone hook (if needed)

---

**🚨 CRITICAL**: This configuration is mandatory for ALL Claude Code usage.  
**🔄 AUTOMATIC**: Protection works transparently via dotfiles integration.  
**✅ VERIFIED**: System tested and optimized for performance.