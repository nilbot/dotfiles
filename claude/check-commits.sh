#!/bin/bash
# Check for Claude Code footer violations in git repositories
# Usage: check-commits.sh [repo-path] [number-of-commits]

REPO_PATH="${1:-.}"
NUM_COMMITS="${2:-20}"

cd "$REPO_PATH" || exit 1

echo "🔍 Checking last $NUM_COMMITS commits for Claude Code footer violations in $(pwd)..."

# Check recent commits for violations
VIOLATIONS=$(git log --oneline -"$NUM_COMMITS" --grep="Generated with.*Claude Code" --grep="Co-Authored-By: Claude")

if [ -n "$VIOLATIONS" ]; then
    echo "❌ VIOLATIONS FOUND:"
    echo "$VIOLATIONS"
    echo ""
    echo "💡 To fix these violations:"
    echo "   git filter-branch -f --msg-filter 'sed \"/🤖 Generated with.*Claude Code/d; /Co-Authored-By: Claude/d\"' HEAD~$NUM_COMMITS..HEAD"
    echo ""
    echo "⚠️  Note: This will rewrite commit history. Use with caution on shared branches."
    exit 1
else
    echo "✅ No violations found in last $NUM_COMMITS commits"
    
    # Check if protection hook is installed
    if [ -f ".git/hooks/commit-msg" ]; then
        echo "✅ Commit message protection hook is installed"
    else
        echo "⚠️  Protection hook not found. Installing..."
        cp ~/.git-templates/hooks/commit-msg .git/hooks/ 2>/dev/null || echo "❌ Failed to install hook"
    fi
    
    exit 0
fi