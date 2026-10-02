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

Five interventions, measured, none of them a fix:

| intervention | thinking share | judged score |
|---|---|---|
| "think in Chinese first" | 15% → 33% | unmoved (6.5 against 6.4–6.5) |
| Chinese persona inside the real prompt | 15% → 27% | 6.33 against 6.50 |
| 8.6k more characters of English in the harness prompt | — | 6.92 against 6.92 |
| Chinese request, English answer | — | 6.92 against 7.42, one bilingual scale |
| `deepseek-v4-pro` instead of `deepseek-flash` | 15% (unchanged) | 3 of 6 outputs were tool-call markup; the 3 clean ones 6.67 against 7.58 |

The model was the last lever nobody had pulled, and it is not a fix either. Under
the identical prompt the pro model emitted raw tool-call markup — `<tool_calls>`,
`<invoke name="bash">` — in half its runs instead of a message, and the three runs
that did produce a message scored below flash's. Across every run in this
investigation (around 150 samples) the flash conditions produced that malformed
output 0 or 1 time out of 6; the pro condition, 3 out of 6.

**No rule change is supported**, and the earlier claim that the harness's prompt mass
was the lever is withdrawn here. What is left is a structural choice rather than a
measured improvement: the human reads English natively and cannot decode rendered
Chinese, so answering in English removes the step that fails instead of making it
better. This entry says that plainly rather than dressing it as a quality win.

**The Chinese rules stay**, on the human's reading cost and because the
Chinese-instruction conditions led the API rounds — not on a claim that they improve
the writing.

**The next thing worth trying is not language at all.** Every harness output invented
commitments the material never contained, and `global/AGENTS.md` has no rule against
that: rule 5 asks what a reader must be told, nothing asks what a writer must not
make up. A candidate rule was measured — the same six runs, one line added:

> 材料里没有的，不要替读者加。对方给的材料里没有的行动、承诺、请求、分工，不要写进去
> ——「我先买好」「大家分一下」「跟李阿姨也说一声」这类都是替人做主。

In one blind batch the baseline scored 7.50 and the rule 7.75, with invention marks
falling from 4 of 6 to 3 of 6. The direction is the one the judge punishes hardest
and the size is inside the noise, so it is recorded as a candidate, not adopted on
this evidence.
