# Which wordings did the human call translationese?

## Context

2026-10-01: three versions of one rule in `global/AGENTS.md` inside a single day.

It began as "re-read for translationese, sentence by sentence". The very next
document shipped 「A 和 B 都需要你各启动一次游戏」 — the failure that phrase named —
so the phrase was a no-op: it names a feeling, and a feeling is not a step anyone
can be caught failing to take. It became a six-item checklist with a table of
known offenders.

The human rejected that as well, on two grounds. A list of examples cannot be
enumerated, so the pattern that bites next is by construction the one not in the
table. And every line of that file is read by every session in every repository,
which makes a growing table the most expensive place in the system to keep
examples.

What stands in the file now is a method instead of a list: read the draft back as
the student who scores 130+ on the Chinese Gaokao language paper, and rewrite
everything awkward, everything that stops being fluent, and everything that jumps
context without saying how the two things connect. This entry keeps the
observations, where they cost the shared file nothing.

## Answer

The wordings the human has pointed at, with the plain rewrite:

| written | why it fails | plain |
|---|---|---|
| 安静地给出残缺结果 | "silently" rendered literally | 不报错，但输出的结果是残缺的 |
| 更干净的编译 | "clean build" | 编译时不会再有那 33 条警告 |
| 收盘 | stock-market close for "end of session" | 这次会话结束时 |
| 各启动一次 | 各 needs a plural subject, and 你 is singular | A 要做一次，B 也要做一次，每次都要你启动一遍 |
| 引用版本 | noun pile from "reference version" — of what? | 参考版本 |
| 能编过 / 降级通过 | squeezed verb; says something changed, not what | 能编译通过 / 把版本降到 6.0 后编译通过 |

The shapes those are instances of, which is what the rejected checklist asked
about, for a reader who wants a diagnostic rather than a standard:

- **Pronouns** — 这 / 那 / 它 / 其 / 该 / 此 must point at something the previous
  sentence named. If it does not, write the name.
- **Quantity words** — 各 / 均 / 分别 / 共同 need a plural subject.
- **Noun piles** — 引用版本, 版本标记状态, 语料规模. Expand to a clause.
- **Squeezed verbs** — 编过, 跑过, 挂掉, 降级通过. Write the verb out: 编译通过, 运行过,
  崩溃, 把版本降到 6.0 后编译通过.
- **Register** — borrowed vocabulary (收盘 for "end of session") reads as a
  different subject than the one being written about.

**This table grows when the human points at a sentence.** The row goes here, in
the same turn, not into `global/AGENTS.md`: the file carries the method, this
entry carries the instances.
