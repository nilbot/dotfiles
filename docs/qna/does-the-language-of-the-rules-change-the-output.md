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

## Round 2: a non-technical task

Run the same day, because the human's reading of round 1 was that the technical
register has no good Chinese models to imitate and that a non-technical subject
would show a difference if one existed. The task was a message to a family group
about a National Day visit to a grandmother — same design, six subagents per
variant, same prompt, the variant delivered by swapping the file the symlinks point
at.

- **Quality: no difference again, and the sign flipped again.** Blind, on one
  scale: mixed 5.33 against English 6.00. Round 1 had mixed ahead by 0.50; round 2
  has it behind by 0.67. Pooled over both rounds the arms are 6.75 and 6.83.
- **Compliance did not replicate.** Both arms ran the self-review and reported it,
  6/6 each, where round 1 was 6/6 against 3/6. That behavioural gap was a property
  of the task, not of the rule's language.
- **What the task changed was what failed.** Every arm scored below its round-1
  counterpart (5.33–6.00 against 7.67–8.17), and the failures were no longer about
  wording: the judge penalised six runs for inventing commitments the material
  never contained — buying the handrail, splitting the shopping, telling the
  neighbour, promising photographs — and three for getting the timeline or the
  direction wrong (who drives down, who goes back, which day). Nothing in
  `global/AGENTS.md` forbids adding an action nobody agreed to. Rule 5 is about
  what a reader must be told, not about what a writer must not make up.

## What follows

Neither round supports a quality claim in either direction, and the compliance edge
did not survive a second task. What is left is the reason this design cannot
measure: the human reads and edits the file, and rules about Chinese prose written
in Chinese do not have to be translated back to be judged. The human's own reading
of round 1 — they preferred the mixed arm's outputs while the blind judge called it
a tie — is the signal that matters for that purpose, and it is not the signal these
two rounds collected.

**Both rounds were then superseded on method.** The human pointed out that a
subagent is still a product of this harness, so neither round controlled the whole
prompt the model saw. `why-does-the-model-translate-from-english-into-chinese.md`
does that with direct API calls and finds the mechanism these rounds were feeling
for: the language of the thinking follows the language of the context mass, and the
real DSH system prompt is 9084 characters of English against a thousand characters
of Chinese rules.
