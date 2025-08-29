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

### Implementation Methods

#### Method 1: Git Hook (Recommended)
Create a commit-msg hook that automatically strips forbidden content:

```bash
# Create the hook
cat > ~/.git-templates/hooks/commit-msg << 'EOF'
#!/bin/bash
# Strip Claude Code footers from commit messages

# Remove Claude footers
sed -i '/🤖 Generated with \[Claude Code\]/d' "$1"
sed -i '/Co-Authored-By: Claude/d' "$1"

# Remove trailing empty lines
sed -i -e :a -e '/^\s*$/N;$ba' -e '$d' "$1"
EOF

# Make executable
chmod +x ~/.git-templates/hooks/commit-msg

# Configure git to use template
git config --global init.templatedir ~/.git-templates
```

#### Method 2: Global Git Alias
Create a git alias that automatically cleans commit messages:

```bash
# Add to ~/.gitconfig
git config --global alias.commit-clean '!f() { 
    git commit "$@" && 
    git log -1 --format="%B" | 
    sed "/🤖 Generated with \[Claude Code\]/d; /Co-Authored-By: Claude/d" | 
    git commit --amend -F -; 
}; f'
```

#### Method 3: Pre-commit Hook Integration
If using pre-commit, add this to `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: local
    hooks:
      - id: strip-claude-footers
        name: Strip Claude Code footers
        entry: bash -c 'sed -i "/🤖 Generated with \[Claude Code\]/d; /Co-Authored-By: Claude/d" "$@"'
        language: system
        stages: [commit-msg]
```

#### Method 4: Claude Code Setting Override
For Claude Code sessions, always use this pattern in commit commands:

```bash
# Good pattern - use HEREDOC without footers
git commit -m "$(cat <<'EOF'
Your commit message here

Technical details...
EOF
)"
```

### Automatic Detection Script
Create a script to detect violations in existing repos:

```bash
#!/bin/bash
# ~/.claude/check-commits.sh
# Usage: check-commits.sh [repo-path]

REPO_PATH="${1:-.}"
cd "$REPO_PATH"

echo "🔍 Checking for Claude Code footer violations..."

# Check recent commits for violations
VIOLATIONS=$(git log --oneline -20 --grep="Generated with.*Claude Code" --grep="Co-Authored-By: Claude")

if [ -n "$VIOLATIONS" ]; then
    echo "❌ VIOLATIONS FOUND:"
    echo "$VIOLATIONS"
    echo ""
    echo "💡 To fix: git filter-branch --msg-filter 'sed \"/🤖 Generated with.*Claude Code/d; /Co-Authored-By: Claude/d\"' HEAD~20..HEAD"
    exit 1
else
    echo "✅ No violations found in recent commits"
    exit 0
fi
```

### Repository-Specific Integration
Add this to any project's CLAUDE.md:

```markdown
## Commit Message Constraints (ENFORCED)

**NEVER include these footers in commit messages:**
- `🤖 Generated with [Claude Code](https://claude.ai/code)`
- `Co-Authored-By: Claude <noreply@anthropic.com>`

This constraint is enforced by:
1. Global git hooks (see ~/.claude/CLAUDE.md)
2. Pre-commit validation
3. Manual review process
```

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

## Setup Instructions

### One-time Global Setup
```bash
# 1. Create git template directory
mkdir -p ~/.git-templates/hooks

# 2. Install commit-msg hook
cp ~/.claude/commit-msg ~/.git-templates/hooks/
chmod +x ~/.git-templates/hooks/commit-msg

# 3. Configure git to use templates
git config --global init.templatedir ~/.git-templates

# 4. Apply to existing repos
find ~/devel -name ".git" -type d -exec cp ~/.git-templates/hooks/commit-msg {}/hooks/ \;
```

### Per-Repository Verification
```bash
# Check if protection is active
ls -la .git/hooks/commit-msg

# Test protection (should strip footers)
echo "Test commit

🤖 Generated with [Claude Code](https://claude.ai/code)" | .git/hooks/commit-msg /dev/stdin
```

## Troubleshooting

### If Violation Occurs
1. **Immediate fix**: `git commit --amend` to clean message
2. **Bulk fix**: Use `git filter-branch` for multiple commits  
3. **Prevention**: Verify hooks are installed and active

### Hook Not Working
```bash
# Reinstall hook
cp ~/.claude/commit-msg ~/.git-templates/hooks/
chmod +x ~/.git-templates/hooks/commit-msg

# Copy to current repo
cp ~/.git-templates/hooks/commit-msg .git/hooks/
```

---

**🚨 CRITICAL**: This configuration is mandatory for ALL Claude Code usage.
**🔄 SYNC**: Keep this file updated across all development machines.
**✅ VERIFY**: Test protection after any git configuration changes.