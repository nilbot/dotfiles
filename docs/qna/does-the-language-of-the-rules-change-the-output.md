# Does the language the rules are written in change the output?

## Context

2026-10-01: `global/AGENTS.md` was rewritten so that the rules about Chinese prose
are in Chinese — the `## 语言` section, rule 2's banned metaphors, rule 6's Gaokao
standard. The human asked what that actually does. The answer needed an experiment,
because the plausible arguments run in both directions.

## Method

Six subagents per variant, same model, same task, same prompt wording. Each was told
to read `~/.dsh/AGENTS.md` first, then write a 250–350 character Chinese explanation
of a release-process change for a colleague who had not been in the meeting, from
the same nine-bullet English fact sheet.

The variant was delivered through the real channel rather than pasted into the
prompt: the working-tree file the three symlinks point at was swapped between runs,
so each subagent genuinely read the version assigned to it — `79909ef:global/AGENTS.md`
for the all-English arm, the working tree for the mixed arm.
Nothing else differed between the arms, and the arms ran in separate time windows so
no run could read the other variant.

## What it measured

**Text quality: no detectable difference.** The twelve texts were judged blind, on
one scale, by a subagent that did not know the grouping: mixed 8.17 against English
7.67. Judged one batch at a time, the same texts gave 6.83 against 7.33 — the sign
flips with the sample, which is what noise looks like at n=6 per arm. The judge did
find real defects in both arms (a closing summary that turned checksum matching into
a release blocker; a "两处调整" followed by three items), so the scale was not
saturated.

**Compliance: the one thing that moved.** Rule 6 asks for the self-review pass and
for the sentences it changed to be named. That happened 6/6 with the Chinese rule and
3/6 with the English one. At this size that is the most extreme split the data can
show, one-sided p ≈ 0.09 — suggestive, not established.

**An observation that cuts the other way.** Rule 2 names five banned metaphors, and
runs in *both* arms removed 兜底 and 前置 from their drafts. A short list of specific
words does bite. What could not generalize was rule 6's long offender table, which is
why the table moved out of the file; this finding does not argue against that move.

## What follows

The case for writing those rules in Chinese is compliance and the human's reading
cost — not prose quality. "It makes the writing better" is not supported here.
Anyone repeating this should use more samples and a harder task: this one was a
translation of a fact sheet, which every arm handled competently.
