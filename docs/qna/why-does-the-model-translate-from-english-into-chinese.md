# Why does the model translate from English into Chinese?

## Context

2026-10-01 and 2026-10-02. The human is bilingual, and their recurring complaint
is that the Chinese the agent writes reads like translated English: every word
correct, nothing anyone would say. Two rounds of subagent experiments had
compared an all-English `global/AGENTS.md` with the mixed one and found no
reliable difference (`does-the-language-of-the-rules-change-the-output.md`).

The human then raised the design flaw in those rounds: a subagent is a product of
this harness, so unless everything the model saw — system prompt, injected files,
tool schemas, system reminders — is recorded and controlled, the experiment does
not isolate language at all. They asked for two things: record what the model
actually sees in a DSH session, and run a controlled experiment against the model
directly, over an OpenAI-compatible endpoint.

## What a session actually shows the model

Recorded, not inferred. A session's event log is
`$DSH_HOME/sessions/<project>/<session-id>/session.v4.jsonl.zstd`, and the model's
messages are folded out of it:

```
zstdcat <log> | node --input-type=module -e '
import { readFileSync } from "node:fs";
import { foldSurface, deriveEventMessage, foldRequestHeader } from
  "<checkout>/packages/core/session/lib/index.js";
const [header, ...events] = readFileSync(0, "utf8").split("\n")
  .filter(l => l.trim()).map(l => JSON.parse(l));
const folded = foldSurface(events, []);
console.log(folded.nodes.map(s => deriveEventMessage(events[s], folded.projectedMessages))
  .filter(Boolean).map(m => m.role).join(","));
'
```

What that returns for a real session on this machine:

| message | size | language |
|---|---|---|
| `system/message` | 9084 chars | **0 Chinese characters** |
| `user/message` source=agent-instructions | 6127 chars | the injected `~/.dsh/AGENTS.md` — the only Chinese in the prompt |
| `user/message` source=runtime-context, skill-catalog, time-context | ~3.7k chars | English |
| `request/header` tools | 49 tool schemas | English |

So the harness's own prompt is monolingual English, and the only language
instruction anywhere in it sits in four tool-argument descriptions: *"Use the
language of the user's current request."* That sentence decides the language of
the **reply**. Nothing anywhere decides the language of the **thinking**.

A subagent's system prompt is byte-identical to its parent's
(`snapshots/session/text-turn/system-prompt.expected.md` and
`snapshots/sdk/subagent-send-message/system-prompt.1.expected.md` diff clean);
only the runtime-context message gains the delegation paragraph. So the earlier
subagent rounds were not contaminated by the harness — but they never had a
pure-English prompt either, because the task text was Chinese in both arms.

## The controlled experiment

Direct calls to `deepseek-flash` over the OpenAI-compatible endpoint, nothing else
in the request. Deliverable held constant: a 250–350 character Chinese message to
a family group about a National Day visit. Six samples per condition per
generation, two generations; judged blind in batches carrying two samples from
every condition, so a batch effect lands on all conditions equally.

The task text itself was written by the agent running the experiment, and it said
「发到家族群里」. 家族群 is a clan; the natural term for a family chat is 家庭群. No
output adopted the term — 0 of 114 texts contain either word — so the error stayed in
the prompt rather than spreading into the results, but it was the register the task
was framed in, and it was the experimenter's.

| # | system prompt | rules message | task | generation 1 | generation 2 |
|---|---|---|---|---|---|
| A | English, 2 lines | English AGENTS.md | Chinese | 7.33 | 7.50 |
| B | Chinese | Chinese | Chinese, Chinese facts | **8.17** | 7.17 |
| C | English, 2 lines | Chinese | Chinese | 7.83 | **7.67** |
| D | **the real 9084-char DSH prompt** | Chinese | Chinese | 7.08 | 5.83 |
| E | English, 2 lines | English AGENTS.md | English | 7.58 | 6.50 |

D is the configuration the machine actually runs. It came last in both
generations, and again in two further head-to-head re-judgings (6.42, 6.50). C
and B — the two conditions whose instructions are Chinese — are the pooled
leaders at 7.75 and 7.67.

The reasoning traces explain the gap. Every answer carried a `reasoning_content`
block, and the share of it written in Chinese tracks the condition:

| | A | B | C | **D** | E |
|---|---|---|---|---|---|
| Chinese share of the thinking | 42% | 52% | 55% | **15%** | 24% |
| reasoning length | 5.9k | 5.1k | 4.6k | 4.8k | 10.9k |

Across all thirty samples, the correlation between the Chinese share of the
reasoning and the judged naturalness of the answer is r = +0.38.

## What did not work

Three interventions, all measured, none of them a fix:

- **"Think in Chinese first"** added to the rules (same condition D otherwise):
  the Chinese share of the thinking rose from 15% to 33% and the judged score did
  not move — 6.5 against D's 6.4–6.5, with one sample returning a malformed tool
  call instead of a message.
- **A Chinese persona inside the real harness prompt** (replacing the English
  `personaPrefix`/`personaSuffix`): thinking share 15% → 27%, score 6.33 against
  D's 6.50.
- **The English fallback** — Chinese request, a rule requiring English replies,
  judged on one bilingual naturalness scale against D's Chinese replies:
  6.92 against 7.42. Answering in English did not buy a more natural text; it
  moves the reading burden, not the quality.

## Mechanism

The language of the model's internal work follows the language of the context it is
given, not the language of the request. A 9084-character English system prompt with
49 English tool schemas outweighs a Chinese question and a thousand characters of
Chinese rules; the thinking then happens largely in English and the Chinese answer
is a rendering of it. Rules can change what the model says about its thinking — the
"think in Chinese" line did — but they do not change the substrate that produces
the wording.

## The harness-side test, and a correction

The first version of this entry ended by pointing at the harness: short prompts beat
the real 9084-character one by about a point in the API experiment, so a lighter
profile looked like the lever. That inference was tested inside the harness and did
not survive.

Same task, same session machinery, same profile (`--profile headless`), same
injected `~/.dsh/AGENTS.md`, six runs per arm, run from an empty directory. The only
difference was a `--patch` overlay adding the real harness's own English to the
system prompt — measured afterwards from the session logs: **2794 characters in the
lean arm, 11373 in the bloated one**, and the injected text was confirmed present in
the prompt the model received.

Blind-judged together on the same scale: **6.92 against 6.92**. Eight and a half
thousand more characters of English changed nothing.

That same batch also produced the round's most consistent finding, and it is not
about language: the judge marked **all twelve** harness outputs as inventing an
action, a promise or a request the material never contained — "东西我这两天先看好，
回去就装", "跟李阿姨也说一声", "谁要是那几天也能回去，跟我说一声". In the API runs
that happened in some texts; here it happened in every one. A real agent loop with
tools, and with rules telling it to assume the reader has read nothing and to err
long, fills the gaps with commitments nobody made — and that is the defect the blind
judge punished hardest.

## A blind perceptual read, which the scores above were not

The human drew the line the earlier rounds missed: a reader is the right instrument for
naturalness, perception and aesthetics, and the wrong instrument for facts, counts and
length. Asking a reader to police invented content is how the +编 flag came to mark a
paraphrase; asking a script for beauty is how the counter came to mark nothing.

First attempt at a perceptual read: absolute 0–10 scores for naturalness, readability and
overall impression, no fact sheet, no invention penalty, thirty texts. **It saturated** —
twenty-five of thirty scored 9 on naturalness, including one the judge's own note quoted
「我2号有个推不掉的交期」 about and still scored 9. A reader asked for a number, with no
scale, has no scale.

So the design became a forced ranking: ten texts per batch, two from each condition, read
once, ranked from most natural to least with a one-line reason. Three batches, thirty
texts, generation 1 of the conditions in the table above.

| condition | texts | mean rank (1 = most natural) |
|---|---|---|
| B — all Chinese | 6 | **4.17** |
| D — the real DSH prompt | 6 | **5.00** |
| C — English persona, Chinese rules | 6 | **5.50** |
| A — all English | 6 | **6.00** |
| E — English task | 6 | **6.83** |

Three batches, three different winners (C, A, B), and every reason names a phrase:
「现在想起来都后怕」, 「那就依她，我们只把屋子弄安全些」, 「两个钟头」.

Two things follow. The ranking discriminates where the absolute scores did not, so this is
the shape to use for this question. And it does not reproduce what the table at the top of
this entry concluded: read by a reader ranking rather than a scorer awarding, D — the
configuration this machine actually runs — is second, not last. Six texts per condition,
one pass, one judge: a first read, not a verdict.

## Limits

Six samples per condition; one judge, the same model that wrote the texts, which
may share its own tastes; judge scores carry about a point of noise; one task type.
The correlation is 30 points and modest. None of this measures the human's own
reading, which is the thing that started the investigation — and on that, the human
preferred the Chinese-output arm by eye while the judge called it a tie.

**The absolute scores drift between judge calls, measurably.** The same six D texts
scored 6.42, 6.50 and 7.50 in three different batches. Every comparison above is
made *within* one batch with each condition weighted equally in every batch, so the
drift cancels inside a comparison; reading the numbers across sections as one scale
does not work, and the drift is larger than most of the effects being measured.

## What follows

Six interventions, measured, none of them a fix:

| intervention | thinking share | judged score |
|---|---|---|
| "think in Chinese first" | 15% → 33% | unmoved (6.5 against 6.4–6.5) |
| Chinese persona inside the real prompt | 15% → 27% | 6.33 against 6.50 |
| 8.6k more characters of English in the harness prompt | — | 6.92 against 6.92 |
| Chinese request, English answer | — | 6.92 against 7.42, one bilingual scale |
| `deepseek-v4-pro` instead of `deepseek-flash` | 15% (unchanged) | 3 of 6 outputs were tool-call markup; the 3 clean ones 6.67 against 7.58 |
| 18.8k characters of English tool output in the history | 15% → 39% | 6.92 against 7.00 |

The last row is the shape every real session has: the Chinese answer is written after
tens of thousands of characters of English tool output, and the suspicion was that
the history — not the system prompt — was doing it. It is not. The blob was real
output from this repository, inserted between the rules and the task; the thinking
share went *up*, and the judged score did not move.

The model was the last lever nobody had pulled, and it is not a fix either. Under
the identical prompt the pro model emitted raw tool-call markup — `<tool_calls>`,
`<invoke name="bash">` — in half its runs instead of a message, and the three runs
that did produce a message scored below flash's. Across every run in this
investigation (around 150 samples) the flash conditions produced that malformed
output 0 or 1 time out of 6; the pro condition, 3 out of 6. *(The pro endpoint was
removed from this machine's configuration on 2026-10-02, after the measurement; the
row is what was measured before that, and the instruction now is flash only.)*

**No rule change is supported by anything above** — the earlier claim that the harness's prompt mass
was the lever is withdrawn here. What is left is a structural choice rather than a
measured improvement: the human reads English natively and cannot decode rendered
Chinese, so answering in English removes the step that fails instead of making it
better. This entry says that plainly rather than dressing it as a quality win.

What does support a rule is a different measure, and it is not in this entry:
`does-naming-the-failure-modes-change-the-chinese.md` counts the three constructions the
human named, and a guide that names them cuts the rate from 16 of 16 texts to 10 of 24.

**The Chinese rules stay**, on the human's reading cost and because the
Chinese-instruction conditions led the API rounds — not on a claim that they improve
the writing.

**The next thing worth trying is not language at all** — that was the claim, and it did
not survive being checked either. Every harness output was flagged as inventing a
commitment, and `global/AGENTS.md` has no rule against that. A candidate rule was
measured — the same six runs, one line added:

> 材料里没有的，不要替读者加。对方给的材料里没有的行动、承诺、请求、分工，不要写进去
> ——「我先买好」「大家分一下」「跟李阿姨也说一声」这类都是替人做主。

In the first blind batch the baseline scored 7.50 and the rule 7.75, with invention
marks falling from 4 of 6 to 3 of 6. Doubled to nine judged samples per arm the gap
between the means looks wider — 6.44 against 7.33 — and that is an artefact: the
baseline's mean carries one malformed output that scored zero, and with malformed
outputs set aside the two are 7.25 and 7.33. Flagged invention falls from 11 of 15
texts to 8 of 15 when the two batches are pooled: a weak signal at this size.

**Then the flag itself was audited, and it does not mean what it looks like.** The
rubric told the judge to count any action, promise or request the material did not
contain as a defect, and the judge did exactly that. Reading all 155 unique flagged
sentences across every run and bucketing them by shape:

| what was flagged | count | example |
|---|---|---|
| paraphrase of a material bullet | 18 | 「再给她配一部掉在地上也够得着的手机」 — that is the material |
| social move in a family message | 33 | 「谁要是那几天也有空，言语一声」 |
| travel or timing detail the notes left open | 56 | 「交完就赶回去」 |
| errand or arrangement | 9 | 「也麻烦李婶先别提」 |
| residual, and the only real defects | 39 | one wrong number (three hours for two), 「死线」, four malformed tool-call markers |

The buckets come from keyword matching, so the split is approximate; the conclusion
is not. A flag rate is not a fabrication rate, the candidate rule's 11/15 → 8/15
measured the judge's strictness as much as the model's behaviour, and the honest
position is that this investigation has **no reliable measure of invented content at
all** — which is why no rule about it is proposed here, and why the line above stays
a candidate that the author of this file is free to reject outright.

**And the audit made the same mistake it was auditing.** It sorted sentences by *what
they added* and never asked whether they were Chinese. The human read the table and
stopped at the examples it had filed as innocent:

- 「二号有个推不掉的活，必须交，交完就往回赶」 — 交 belongs with 交工 and 交差; on
  its own it is not how anyone says "when the job is done". Filed as a travel detail.
  Judge score 7.7, and the same sentence form scored 8.3 in another run; a third run's
  「2号有工作死线」 — 死线, the literal rendering of "deadline" — scored 5.0.
- 「也麻烦李婶先别提」 — an objectless 提 wants its complement (提起), and in speech it
  is 先别说. Filed as an errand.
- 「再给她配一部掉在地上也能够得着的电话」 — 够得着 has no subject, so the nearest
  noun takes it and the *phone* acquires the feature of being reachable while lying on
  the floor. The material said "a phone she can reach from the floor", which names her.
  Filed as a paraphrase, which by content it is. Judge scores: 8.0 here, 8.3 for the
  「躺在地上也够得着的电话」 form.

The same model gets it right elsewhere — 「给她配一部电话，就算躺在地上也够得着，摔了能自己
打出去」 — so this is a rate, not an incapacity; and no judge in this investigation saw
it. Texts carrying 交完, 死线 and an unassigned 够得着 scored 7.3 to 8.3, the same range
as everything else. Every number in this entry is therefore "a model judging model
output", and the wording judgements in it come from the human, who is the only
instrument here that hears the difference.

## Rules 4 and 5 were rewritten, on accuracy rather than on a measured effect

The human read the rule list and rejected two of them on grounds that have nothing to
do with any measurement: the rules named the wrong thing.

- **"Err long"** is an idiom-plus-adjective ("err on the side of caution" with the
  noun replaced), and it points at the proxy rather than the property. What the rule
  wants is that every step the reader needs is on the page; length is a side effect
  of not compressing those steps away. A rule whose stated goal is *more words* is
  satisfied by padding, and padding needs material, which is a plausible route to the
  invented commitments the judge keeps flagging. It is also unverifiable: nobody can
  ask "am I long enough?" and get an answer, while "is every step on the page?" is
  checkable.
- **"Assume they have read nothing else"** states as a hypothesis what is a fact.
  The reader has not read what the session read — that is not a stance to adopt, it
  is the situation. "Assume" also oversteers towards explaining what the reader
  already knows, while the actual failure is a reply aimed at the context the *work*
  built up instead of at the question that was asked.

Both are now written that way in `global/AGENTS.md`: rule 4 says not to compress away
a step the reader needs and says outright that a short sentence carrying its steps
beats a long one that assumes them; rule 5 says the reader has not read what you
read, and ends by pointing at the drift — after a long stretch of work, a reply tends
to answer the question the work drifted into.

**The hypothesis that "err long" caused the invented commitments did not survive its
test.** Six runs with the file as it was against six with rules 4 and 5 replaced,
judged in one batch: 5.67 against 5.75, invented content flagged in six of six on
both sides, and the rewritten arm was not shorter (330 characters against 311). The
flag rate sits at 60–100% in every configuration measured — though, as the audit
above shows, most of what it counted is not invention — so a six-sample test cannot
separate small effects. What it does establish is that removing the length rule did
not change the output in any way this design can see.
