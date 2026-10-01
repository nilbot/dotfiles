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
2. **Do not invent shorthand.** A private metaphor (兜底, 锚定, 护栏, 落点, 口径)
   is not jargon — the reader cannot look it up, and it silently encodes the part
   you chose not to spell out. Say the plain thing, or define it in one sentence
   the first time it appears.
3. **Every conclusion carries its evidence.** "Verified" is not evidence. Give
   the file, the command, and the output that matters, so the reader can re-run
   it and disagree.
4. **Err long.** Compressed clauses, dropped subjects, two ideas fused into one
   sentence — each bets that the reader already knows what you left out.
5. **Assume they have read nothing else.** "Fixed now" tells them neither what
   changed nor how you checked it.
6. **Read the draft back with a checklist, sentence by sentence — and report what
   you checked.** The five rules above cover references and definitions; none of
   them looks at wording. Drafting from an English-shaped plan produces literal
   renderings in which every word is correct and none of it is how anyone speaks.

   "Re-read it for translationese" is not a step you can honestly tell yourself you
   did — that exact phrasing stood here, and the very next document shipped
   「A 和 B 都需要你各启动一次游戏」. Ask these instead, one sentence at a time:

   - **Pronouns** — does 这 / 那 / 它 / 其 / 该 / 此 point at something named in the
     previous sentence? If not, write the name.
   - **Quantity words** — 各 / 均 / 分别 / 共同 need a plural subject.
     「A 和 B 都需要你各启动一次游戏」falls apart because 你 is singular; write
     「A 要做一次，B 也要做一次；每次都需要你启动一遍游戏」.
   - **Noun piles** — 引用版本, 版本标记状态, 语料规模. Expand to a clause: the version
     of the assembly being *referenced*; the mark recording *which version this
     document was checked against*.
   - **Squeezed verbs** — 编过, 跑过, 挂掉, 降级通过. Write the verb out: 编译通过,
     运行过, 崩溃, 把程序集版本降到 6.0 后编译通过.
   - **Say it aloud** — if the sentence is not something a person would say to
     another person, rewrite it.
   - **Register** — 收盘 for "end of session" borrows the stock market; plain is
     这次会话结束时.

   **Report the outcome of that pass in the delivery** — name the sentences you
   rewrote. A check whose result is invisible is a check that did not happen, and
   the human can only conclude you skipped it.

   Known offenders, appended whenever the human points at one:

   | written | why it fails | plain |
   |---|---|---|
   | 安静地给出残缺结果 | "silently" rendered literally | 不报错，但输出的结果是残缺的 |
   | 更干净的编译 | "clean build" | 编译时不会再有那 33 条警告 |
   | 收盘 | stock-market close for "end of session" | 这次会话结束时 |
   | 各启动一次 | 各 needs a plural subject; 你 is singular | A 要做一次，B 也要做一次，每次都要你启动一遍 |
   | 引用版本 | noun pile from "reference version" — of what? | 引用到的程序集版本 |
   | 能编过 / 降级通过 | squeezed verb; says something changed, not what | 能编译通过 / 把程序集版本降到 6.0 后编译通过 |

   **This table grows only when the human points at a sentence.** Patterns already
   in it you will catch with the checklist; patterns that are not, you will not — so
   when the human flags one, fix the text and add the row in the same turn.

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
