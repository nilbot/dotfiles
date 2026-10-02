# Global instructions

Tracked at `global/AGENTS.md` in the dotfiles checkout and symlinked to
`~/.dsh/AGENTS.md`, `~/.claude/CLAUDE.md`, and `~/.codex/AGENTS.md`, so it loads
in **every session in every repository, under every harness**.

Everything here applies to every harness. A rule that only one harness needs
belongs in that harness's own file instead, not in this one — a shared file that
collects one harness's habits is a file the other harnesses read and pay for.
Keep it to rules a session acts on. Provisioning and verification live in
`git/README.md`.

## 语言

用提问的语言思考，也用提问的语言回答。一段指令的核心想法或主要请求是用哪种语言提出
的，推理和回复就用哪种语言。确实无法用那种语言思考时，推理和回复一起退回英文——
不要用一种语言思考，再把回复翻译成另一种语言。

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

## Write so the reader can decode it

The human does not see your process: not the intermediate tool output, not the
files you read, not the options you rejected, not the results you are reacting
to. Anything you write for them — a reply, a document, a commit message — has to
stand on its own.

1. **Name things; never point at position.** "Item 3", "the table above", "that
   file", "the former" all make the reader reconstruct context they never had.
   Write the name instead: the file and line (`bootstrap.d/links.manifest:22`),
   the section (`global/AGENTS.md` §Recording what you learn), the command
   (`./bootstrap check`). An example the reader cannot resolve fails the same
   test.
2. **不要自己造词。** 自己造的比喻（兜底、锚定、护栏、落点、口径）不算行话——读者
   查不到，而且它把你有意没写出来的那部分藏在了里面。有话直说；确实要用，就在它第一次
   出现时用一句话讲清楚。
3. **Every conclusion carries its evidence.** "Verified" is not evidence. Give
   the file, the command, and the output that matters, so the reader can re-run
   it and disagree.
4. **Do not compress away a step the reader needs.** A dropped subject, two ideas
   fused into one sentence, a conclusion whose premise stayed in your head — each
   one is a step the reader has to rebuild. A short sentence that carries its steps
   is worth more than a long one that assumes them.
5. **The reader has not read what you read.** They did not watch you open the files,
   run the commands, or drop the options; what feels like shared context exists only
   in your session. Write the fact, not a reference to it — "fixed" without what
   changed and how you checked it is not a report. And answer the question that was
   asked: after a long stretch of work, a reply tends to answer the question the
   work drifted into.
6. **写完之后用读者的眼光重读一遍，不是用作者的眼光。** 照着英文提纲写出来的中文，
   每个词都没错，就是没人这么说话。假设自己是高考语文能考出 130 分以上的学生，回头读
   自己刚写的东西，把别扭、不通顺、上下文跳脱的地方全改掉（用别的语言写，就假设自己是
   那种语言里语感最好的人）。最后说明你改了哪几句。

The test is not "is it accurate" but "can someone who was not here act on it?" —
including the next session of you, with only the repository in front of it.

## Multi-line commit messages

Use a quoted heredoc, so backticks and `$` in the body reach Git unexpanded:

```bash
git commit -m "$(cat <<'EOF'
Short summary

Detail about what changed and why.
EOF
)"
```
