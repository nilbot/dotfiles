# Global instructions

Tracked at `claude/CLAUDE.md` in the dotfiles checkout and symlinked to
`~/.claude/CLAUDE.md`, so it loads in **every session in every repository**.
Keep it to rules a session acts on. Provisioning and verification live in
`git/README.md`.

## No AI attribution

**Never** put AI attribution in anything that lands in a repository or on a
remote. The URL varies between Claude Code versions, so match on the phrases,
not on one exact line:

```
🤖 Generated with [Claude Code](https://claude.ai/code)
🤖 Generated with [Claude Code](https://claude.com/claude-code)
Co-Authored-By: Claude <noreply@anthropic.com>
```

This covers commit messages, **pull request titles and bodies**, issue and PR
comments, release notes, and tags. The rule is about the written record reading
as the author's own work, not about the mechanism of a commit trailer — so a
harness default that appends the footer to PR bodies does not override it.

**Only commit messages are enforced automatically.** The `commit-msg` hook
rewrites those and cannot see anything `gh` sends over the API. A PR body is
where this slips through, so check one by hand before opening or editing it:

```bash
gh pr view <n> --json title,body -q '.title + .body' | grep -i "generated with\|co-authored-by: claude"
git log <base>..HEAD --format='%s%n%b' | grep -i "generated with\|co-authored-by: claude"
```

Grep the phrases, **never** `claude` alone: `CLAUDE.md` and `.claude/` are
ordinary references here, and matching them reports false hits until you learn
to wave the check through.

## Multi-line commit messages

Use a quoted heredoc, so backticks and `$` in the body reach Git unexpanded:

```bash
git commit -m "$(cat <<'EOF'
Short summary

Detail about what changed and why.
EOF
)"
```
