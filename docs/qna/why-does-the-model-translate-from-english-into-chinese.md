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

The language of the model's internal work follows the language of the context
mass, not the language of the request. A 9084-character English system prompt
with 49 English tool schemas outweighs a Chinese question and a thousand
characters of Chinese rules; the thinking then happens largely in English and the
Chinese answer is a rendering of it. Rules can change what the model says about
its thinking — the "think in Chinese" line did — but they do not change the
substrate that produces the wording.

The lever that did show an effect is the size of the English context: the short
prompt conditions beat the real harness prompt by roughly a point, holding the
rules and the task fixed.

## Limits

Six samples per condition; one judge, the same model that wrote the texts, which
may share its own tastes; judge scores carry about a point of noise; one task
type. The correlation is 30 points and modest. None of this measures the human's
own reading, which is the thing that started the investigation — and on that, the
human preferred the Chinese-output arm by eye while the judge called it a tie.

## What follows

- **No rule change is supported.** Adding a line about thinking in Chinese, or
  about where the persona sits, moved the measurement it was aimed at and not the
  one that matters.
- **The lever is the harness.** A writing task in this session carries ~9k
  characters of English scaffolding it does not need. A lighter profile — fewer
  tools, shorter system prompt — is the change the data points at, and it is a
  DSH-side change, not a dotfiles one.
- **The Chinese rules stay**, on the human's reading cost and because the
  Chinese-instruction conditions were the pooled leaders, not on a claim that they
  make the writing better.
