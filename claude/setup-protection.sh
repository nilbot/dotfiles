#!/bin/bash
# Setup Claude Code commit message protection system
# Run this script to install protection on a new machine or update existing protection

set -e

# Function to handle errors without exiting
handle_error() {
    echo "⚠️  Warning: $1"
    return 0
}

echo "🛡️  Setting up Claude Code commit message protection..."

# Create directories
mkdir -p ~/.git-templates/hooks
mkdir -p ~/.claude

# Check if files already exist
if [ -f ~/.claude/CLAUDE.md ]; then
    echo "✅ ~/.claude/CLAUDE.md already exists"
else
    echo "📋 Creating ~/.claude/CLAUDE.md from dotfiles..."
    # CLAUDE.md should already be linked via the dotfiles Makefile
    if [ ! -f ~/.claude/CLAUDE.md ]; then
        echo "❌ ~/.claude/CLAUDE.md still missing after dotfiles setup"
        exit 1
    fi
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

echo "🎉 Setup complete!"
echo "   - Global template installed: ~/.git-templates/hooks/commit-msg"  
echo "   - Git configured to use templates for new repos"
echo ""
echo "📋 For existing repositories, run:"
echo "   git init  # This will apply the global template to current repo"
echo ""  
echo "🧪 To test protection:"
echo "   ~/.claude/check-commits.sh [repo-path]"
echo ""
echo "📖 Documentation available at:"
echo "   ~/.claude/CLAUDE.md"