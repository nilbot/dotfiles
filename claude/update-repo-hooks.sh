#!/bin/bash
# Update git hooks for existing repository using global template
# Usage: update-repo-hooks.sh [repo-path]

if [ "$1" ]; then
    REPO_PATH="$1"
else
    REPO_PATH="."
fi

if [ ! -d "$REPO_PATH/.git" ]; then
    echo "❌ Not a git repository: $REPO_PATH"
    exit 1
fi

echo "🔄 Updating git hooks for repository: $REPO_PATH"

cd "$REPO_PATH"

# Force copy hooks from global template (git init doesn't overwrite existing hooks)
if [ -f ~/.git-templates/hooks/commit-msg ]; then
    cp ~/.git-templates/hooks/commit-msg .git/hooks/commit-msg
    chmod +x .git/hooks/commit-msg
    echo "✅ Repository hooks updated successfully"
else
    echo "❌ Global template hook not found. Run 'make dotfiles' first."
    exit 1
fi

echo ""
echo "🧪 To test protection:"
echo "   ~/.claude/check-commits.sh $REPO_PATH"