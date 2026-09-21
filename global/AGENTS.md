# Global instructions

Tracked at `global/AGENTS.md` in the dotfiles checkout and symlinked to
`~/.dsh/AGENTS.md`, `~/.claude/CLAUDE.md`, and `~/.codex/AGENTS.md`, so it loads
in **every session in every repository, under every harness**.

Everything here applies to every harness. A rule that only one harness needs
belongs in that harness's own file instead, not in this one — a shared file that
collects one harness's habits is a file the other harnesses read and pay for.
Keep it to rules a session acts on. Provisioning and verification live in
`git/README.md`.

## Language

Think and respond in the language of the ask. When an instruction's core idea or
main request arrives in language X, both the reasoning and the reply belong in
X. If you cannot think in X, fall back to English for both — and do not think in
one language and then translate the reply into another.

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

## Recording what you learn

Knowledge about a codebase is documentation: plain markdown in the repository,
committed like anything else. Two stores, unless a repository's own `AGENTS.md`
names different ones — `docs/qna/` indexed by topic, `docs/journal/` by date.

Write when **the human says so** — "save that", "good to know" — or when the
work **hits something**: a bug understood, a run collapsed, an approach
abandoned. Write it then, not at session end; a session ending is not evidence
that anything happened.

Read before you assert. Grepping `docs/qna/` for a distinctive noun costs a
second and catches the case where the repository already recorded the correction
you are about to contradict.

Subagents inherit this file and measurably do not act on it. If you dispatch
them, the recording is yours to do from their reports.

The `recording-what-you-learn` skill has the shape and the reasoning.

## Multi-line commit messages

Use a quoted heredoc, so backticks and `$` in the body reach Git unexpanded:

```bash
git commit -m "$(cat <<'EOF'
Short summary

Detail about what changed and why.
EOF
)"
```
