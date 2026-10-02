# Does naming the failure modes change the Chinese?

## Context

2026-10-02. `why-does-the-model-translate-from-english-into-chinese.md` ends by
withdrawing its own scores: the judge scoring those texts was not a wording measure —
it passed 「二号有个推不掉的活，必须交，交完就往回赶」 at 7.7 and
「再给她配一部掉在地上也能够得着的电话」 at 8.0. The human read the same sentences and
named what was wrong with them, and then asked whether those findings could guide the
writing rather than only the judging.

They can, and this is the first intervention in the investigation that moves a wording
measure — because the measure is a counter, not an opinion.

## The three modes, as the human named them

1. **A verb whose subject is missing, so the nearest noun takes it.** 「掉在地上也够得着的
   手机」 makes the phone the thing that reaches; the material said "a phone **she** can
   reach from the floor".
2. **A verb that needs its complement, or is borrowed from another domain.** 提 wants
   提起 and in speech is 别说; 交 belongs with 交工 and 交差, so 「必须交，交完就往回赶」
   is not how anyone says the job is done; 死线 and 交期 are renderings of "deadline".
3. **A term from the wrong register.** 家族群 is a clan; a family chat is 家庭群.

## The counter

Four patterns, roughly forty lines:

| detector | fires on |
|---|---|
| subject capture | `够得着/够得到/能自己打的(电话\|手机)` — reachability as a property of the handset |
| objectless verb | `别提` immediately before punctuation |
| domain verb | `必须交/要交/得交` before punctuation, or `交完` not followed by 活/差/工 |
| loan rendering | `死线`, `交期` |
| wrong register | `家族群` |

**A person check was added to the first detector and then removed again, and that
mistake is the most useful thing in this entry.** The first version fired on
「她躺在地上也够得着的手机」, which was taken to be the repair; the check suppressed it.
The human then pointed out that the sentence is illogical on its own terms: it puts the
phone on the floor and her on the floor, and reaching something on the ground from the
ground is the easy case, so the 「也」 marks the wrong condition as the hard one. What the
material describes is a situation — after a fall, unable to get up, she has to be able to
call for help — and any 「…的 + 手机」 converts that situation into a feature of the
handset. Inserting the subject does not fix the frame; it only hides it.

So the strict counter is right and the person check was a false negative that flattered
the arm it was written for. The subject-inserted form stays counted.

Run over the earlier corpus of 114 generated texts, **86 carry at least one of these
constructions**. That is the number the judge could not see.

## The test

Same task as the earlier rounds — a 250–350 character Chinese message to a family chat,
from the same eight English notes — with one correction: the task text now says 家庭群,
where the experimenter had written 家族群. Same real 9084-character DSH system prompt,
same rules message (this repository's own `global/AGENTS.md`), `deepseek-flash`,
250–350 characters asked for. Two guides. Modes two and three are identical in both:

> 二、**动词要带足搭配。** 单一个「提」「交」不成话：是「提起」「别说」，是「做完」不是「交完」。
> 三、**别把别的领域的词搬进来。** 工作上的「死线」「交期」，或行政口径的「家族群」，写家里
> 的事时都不该出现。
>
> 这三条是换说法，不是加话：改完不该比原来长。

They differ in one sentence, the repair instruction for mode one:

> 一、**动词的主语不要省。** 省掉之后，最近的那个名词会把它抢走：「掉在地上也够得着的手机」
> 读起来是手机够得着，不是她够得着。**把主语写出来，或者改成一个独立分句。** ← guide v1
>
> 一、……**把主语写进那个词组里**：「她躺在地上也够得着的手机」。 ← guide v2, and wrong

The second instruction produces a sentence that is still illogical, for the reason the
human gave when they read it: it puts the phone on the floor and her on the floor, and
reaching something on the ground from the ground is the easy case, so the 「也」 marks the
wrong condition as the hard one. What the material describes is a situation, and the only
repair that leaves the situation outside the noun phrase is v1's.

| arm | texts | uses the `…够得着的(手机\|电话)` frame | count, strict | over 350 characters | mean length |
|---|---|---|---|---|---|
| baseline, no guide | 16 | 14 | **16 (100%)** | 1 | 312 |
| guide v1, repair as a separate clause | 16 | 7 | **9 (56%)** | 11 | 387 |
| guide v2, repair inside the phrase | 24 | 15 | **17 (71%)** | 6 | 344 |

Baseline against v1: Fisher exact p = 0.007. Baseline against v2: p = 0.03. **v1 is the
better guide**, which is the opposite of what this entry first reported: with the person
check active, v2 looked like 42% against v1's 44%. It was not the guide that improved v2
— it was the counter, which had been taught to look away from exactly the construction
v2's wording produces. Most of the v2 outputs that looked like repairs are examples of
the defect: 「再给她配一部她躺在地上也够得着的手机」.

v1 still pays for its result in length, overshooting the stated 250–350 in eleven texts
of sixteen (mean 387, longest 666) where v2 overshoots in six of twenty-four.

## What this does not show

- **The counter and the guide were written from the same three modes**, so this measures
  suppression of those modes, not Chinese quality. A text can pass the counter and still
  be awkward; only the human hears that.
- One task, one material, 16 to 24 samples per arm. The baseline sits at 100%, which
  makes the effect easy to see and says nothing about texts that are already clean.
- The reading of the repairs — 「再给奶奶配一部手机，万一她再摔一次，躺在地上也能够得着」
  against 「再给她备一个从地上也够得着的手机」 — is the agent's, and the agent's reading is
  the instrument that failed earlier in this investigation.

**And the old judge, shown the same thirty-two texts blind, scored the guided arm lower**
— 5.88 against 6.94 — because it was penalising length, which the guide had inflated.
It also flagged invented content at 13 of 16 against 14 of 16, i.e. not at all. An
instrument that punishes the fix and cannot see the defect is the reason this entry
counts constructions instead of asking for a score.

## What follows

The v1 wording is the one worth considering: three modes, seven lines of Chinese, and
repairs written as separate clauses rather than as subjects pushed inside a phrase. It is
**not** in `global/AGENTS.md` — that file's space belongs to its author, and this evidence
is one task wide. What the evidence does support is the shape: name the failure mode, say
that the repair is a rephrasing rather than an addition, and keep the repair out of the
noun phrase.

The larger lesson is about the instruments, and it is now twice over. A judge scored the
guided arm lower while being unable to see the defect. A counter, written by the same
agent that wrote the guide, was then relaxed until it stopped seeing the construction that
guide produced. Both times the instrument agreed with the thing being measured. The only
check that has held up in this investigation is the human reading the output — the
counter is useful for counting what the human has already named, and for nothing else yet.
