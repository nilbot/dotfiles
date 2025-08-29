#!/bin/bash
# Setup Claude Code commit message protection system
# Run this script to install protection on a new machine or update existing protection

set -e

echo "🛡️  Setting up Claude Code commit message protection..."

# Create directories
mkdir -p ~/.git-templates/hooks
mkdir -p ~/.claude

# Check if files already exist
if [ -f ~/.claude/CLAUDE.md ]; then
    echo "✅ ~/.claude/CLAUDE.md already exists"
else
    echo "❌ ~/.claude/CLAUDE.md missing - please run this from Claude Code session"
    exit 1
fi

# Install git hook globally
echo "📝 Installing global commit-msg hook..."
cp ~/.git-templates/hooks/commit-msg ~/.git-templates/hooks/commit-msg.bak 2>/dev/null || true

cat > ~/.git-templates/hooks/commit-msg << 'EOF'
#!/bin/bash
# Global commit-msg hook to prevent Claude Code footer violations
# Automatically strips forbidden content from commit messages

# Path to the commit message file
COMMIT_MSG_FILE="$1"

# Strip Claude Code footers
sed -i '/🤖 Generated with \[Claude Code\]/d' "$COMMIT_MSG_FILE"
sed -i '/Co-Authored-By: Claude/d' "$COMMIT_MSG_FILE"

# Remove excessive blank lines (more than 2 consecutive)
sed -i '/^$/N;/^\n$/d' "$COMMIT_MSG_FILE"

# Remove trailing empty lines
sed -i -e :a -e '/^\s*$/N;$ba' -e '$d' "$COMMIT_MSG_FILE"

# Optional: Log cleaning action
# Uncomment next 3 lines if you want notifications
# if grep -q "Generated with.*Claude Code\|Co-Authored-By: Claude" "$COMMIT_MSG_FILE" 2>/dev/null; then
#     echo "🧹 Stripped Claude Code footers from commit message"
# fi
EOF

chmod +x ~/.git-templates/hooks/commit-msg

# Configure git to use templates globally
echo "⚙️  Configuring git to use global templates..."
git config --global init.templatedir ~/.git-templates

# Apply to existing repositories
echo "🔍 Searching for existing git repositories..."
REPOS_FOUND=0

# Search common development directories
for search_dir in ~/devel ~/dev ~/src ~/code ~/projects ~; do
    if [ -d "$search_dir" ]; then
        echo "   Searching in $search_dir..."
        while IFS= read -r -d '' git_dir; do
            repo_dir=$(dirname "$git_dir")
            hooks_dir="$git_dir/hooks"
            
            # Install hook in this repository
            cp ~/.git-templates/hooks/commit-msg "$hooks_dir/" 2>/dev/null || continue
            chmod +x "$hooks_dir/commit-msg" 2>/dev/null || continue
            
            echo "   ✅ Protected: $repo_dir"
            ((REPOS_FOUND++))
        done < <(find "$search_dir" -name ".git" -type d -print0 2>/dev/null)
    fi
done

echo "🎉 Setup complete!"
echo "   - Global template installed: ~/.git-templates/hooks/commit-msg"
echo "   - Git configured to use templates for new repos"
echo "   - Protected $REPOS_FOUND existing repositories"
echo ""
echo "🧪 To test protection:"
echo "   ~/.claude/check-commits.sh [repo-path]"
echo ""
echo "📖 Documentation available at:"
echo "   ~/.claude/CLAUDE.md"