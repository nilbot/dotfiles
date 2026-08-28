# How do you sync Homebrew tap formulas from upstream CI without git cloning?

## The Problem

When a new release of a tool is published in its source repository (e.g. `nilbot/dotfiles`), the separate Homebrew tap repository (e.g. `nilbot/homebrew-tap`) must have its `Formula/<tool>.rb` updated with the new version and release archive SHA-256 checksums.

The traditional approach clones the tap repository inside CI (`git clone https://...`), sets up local git user identities, modifies the formula file, and pushes via git. This adds unnecessary disk overhead, network clone latency, and complex credential management.

## The Solution: Direct GitHub Contents REST API Commit

GitHub provides the **Repository Contents API** (`PUT /repos/<owner>/<repo>/contents/<path>`), which can update a single file and create a commit on the remote branch in a single HTTP request without any git operations.

Using the pre-installed `gh` CLI on GitHub Actions runners:

```bash
# 1. Base64-encode the rendered formula
CONTENT_B64=$(base64 -i Formula/agents.rb 2>/dev/null || base64 -w 0 Formula/agents.rb 2>/dev/null || base64 Formula/agents.rb)

# 2. Query existing file blob SHA (required by GitHub API for updates)
FILE_SHA=$(gh api repos/nilbot/homebrew-tap/contents/Formula/agents.rb --jq '.sha' 2>/dev/null || echo "")

# 3. Create or update file directly in 1 HTTPS call
gh api \
  --method PUT \
  -H "Accept: application/vnd.github+json" \
  repos/nilbot/homebrew-tap/contents/Formula/agents.rb \
  -f message="feat(agents): update formula to v${VERSION}" \
  -f content="${CONTENT_B64}" \
  ${FILE_SHA:+-f sha="${FILE_SHA}"}
```

## Security & Authentication

1. **Token Scope**: Requires a fine-grained Personal Access Token (PAT) with `Contents: Read and Write` permissions scoped exclusively to `nilbot/homebrew-tap`.
2. **Secret Storage**: Stored as encrypted repository secret `HOMEBREW_TAP_TOKEN` in the upstream source repository.
3. **Execution Time**: The entire synchronization completes in ~200ms without cloning, disk cleanup, or worktree overhead.
