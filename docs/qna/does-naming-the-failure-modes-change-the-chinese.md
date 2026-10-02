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

## One measure, re-run on every arm

The human said they would take longer output in exchange for something coherent to read,
which removes the reason the task ever specified 250–350 characters. Dropping that line and
running both arms again, twelve samples each. Every number below is the **strict** counter,
which counts the construction regardless of who stands in front of the verb; the column
"as first reported" is the loosened counter this entry used earlier, and the difference
between the two columns is the subject of the next section.

| arm | texts | strict | as first reported | mean length |
|---|---|---|---|---|
| baseline, 250–350 asked | 16 | **16 (100%)** | 16 | 312 |
| guide v1, 250–350 asked | 16 | **9 (56%)** | 7 | 387 |
| guide v2, 250–350 asked | 24 | **17 (71%)** | 10 | 344 |
| baseline, no length asked | 12 | **10 (83%)** | 6 | 342 |
| guide v1, no length asked | 12 | **8 (67%)** | 6 | 369 |
| baseline, need material, no length asked | 12 | **1 (8%)** | 1 | 322 |

What the strict column supports:

- **The budget on its own is a minor factor**: 16 of 16 to 10 of 12, Fisher p = 0.28.
- **The guide helps only under the budget**: 16 → 9 of 16 there (p = 0.007), 10 → 8 of 12
  without it (p = 0.65). A guide that says "keep the repair out of the noun phrase" matters
  to a writer who is squeezing a requirement into a character count, and not otherwise.
- **The material dominates**: the same twelve samples with the bullet rewritten as a need
  drop from 10 of 12 to 1 of 12 (p = 0.0004), and the `…够得着` family goes to zero.

Every text in every arm still mentions both agreed items, so no arm scores clean by
dropping the requirement.

## The counter had two versions, and the looser one was in the tables

The first version was a bare pattern. It fired on 「**她**躺在地上也够得着的手机」, which the
agent believed was the repair, so a person check was added: skip any modifier whose verb has
她/他/奶奶/自己/老人/让 in the eight characters before it. That check is what produced every
number in this entry's earlier drafts — and it is wrong twice over. It has no principled
window (a person nine characters away does not count, one eight away does), and it was added
because the instrument disagreed with the hypothesis, which is the definition of fitting the
measure to the answer. The human then read the sentence and pointed out it is illogical on
its own terms, so the loosened version was counting nothing at all: nine of the twenty-four
v2 texts passed it wrongly, and four of the twelve no-budget baseline texts did.

Consequences, stated plainly: the row "baseline, no length asked, 50%" that an earlier
version of this entry reported was really 83%, so the budget was never the whole story, and
the guide's apparent effect without the budget was never there. The strict column above is
the one to read, and it is the same code path in every row.

## The material was the experimenter's, and it named an implementation

The bullet was 「a phone she can reach from the floor」, written by the agent running the
experiment. It names an implementation where the material's own logic — she lay on the
floor for two hours before a neighbour heard her — calls for a need: when she falls, she
has to be able to call for help. The human's reading of the intended idea is a health
monitor that raises an alarm on a fall: 「能在老人跌倒的时候自动发出报警的健康监控设备」.

Rendered as that need, the Chinese is ordinary — 「一摔倒就能叫人」. Rendered
word-for-word, no phrasing of the phone clause can be coherent, because the sentence would
have to describe a handset that is easy to reach from the floor, which is either a
tautology or a product feature nobody sells. Every repair in this entry, including the one
the human demolished, was an attempt to make a broken concept sound natural. That is the
actual finding: **when the source names an implementation instead of the need, the correct
move is to resolve the need, not to rephrase the sentence** — and no counter built on
phrase shapes can see the difference.

To measure that rather than assert it, one line of the material changed and nothing else:
「a phone she can reach from the floor」 became 「a way for her to call for help if she falls
and cannot get up」. Twelve samples, current rules, no length asked for:

| setup | texts | strict flagged | the `…够得着` family | other families |
|---|---|---|---|---|
| old material, 250–350 asked | 16 | **16 (100%)** | 14 | 2 |
| old material, no length asked | 12 | **10 (83%)** | 10 | 0 |
| need material, no length asked | 12 | **1 (8%)** | **0** | 1 |

The construction this entry spent its length counting does not appear at all once the
material states the need, and what comes out is ordinary Chinese:
「再给她弄个摔倒起不来时能喊人的办法」, 「再想个办法，让她万一摔倒起不来的时候能喊到人」,
「再给她配上摔倒了能喊人的东西——真起不来的时候，她不能只能躺在地上等人听见」.

So the material is the variable that matters, the budget is a minor one, and the guide
matters only under the budget. What is left of the defect rate under a fair setup is one
text in twelve, of the register-borrowing family, which the need-framing does not touch.

## What this does not show

- **The counter and the guide were written from the same three modes**, so this measures
  suppression of those modes, not Chinese quality. A text can pass the counter and still
  be awkward; only the human hears that.
- One task, one material, 12 to 24 samples per arm; the need-material arm is twelve texts
  and one flagged, which bounds nothing tightly.
- The need-framing result says the *source* decides whether the sentence can be coherent.
  It does not say the agent's own prose — written from its own plan rather than from a
  bullet — has the same cause, which is the question that started all of this and is still
  not measured.

**And the old judge, shown the same thirty-two texts blind, scored the guided arm lower**
— 5.88 against 6.94 — because it was penalising length, which the guide had inflated.
It also flagged invented content at 13 of 16 against 14 of 16, i.e. not at all. An
instrument that punishes the fix and cannot see the defect is the reason this entry
counts constructions instead of asking for a score.

## What follows

**Nothing from this line goes into `global/AGENTS.md`.** The guide's measured effect was
the length budget in disguise, its mode-one repair instruction produces a sentence the
human rejected as illogical, and the sentence it was repairing came from the experimenter's
own material. Three strikes, and the file's space belongs to its author.

Two things do transfer, and neither is a phrase-shape rule:

- **Do not compress a requirement into a modifier to fit a length.** Under a tight budget
  the model put the requirement inside the noun phrase and produced the tautology; with
  room it wrote a clause and the defect disappeared. That is the same principle rule 4 was
  rewritten to state, arrived at from the other side.
- **When the source names an implementation, find the need before writing.** The material
  wanted "she can call for help after a fall"; it said "a phone she can reach from the
  floor". Resolving that is a reading step, and it is upstream of every wording rule.

The instrument lesson is now three times over. A judge scored the guided arm lower while
unable to see the defect. A counter, written by the same agent that wrote the guide, was
relaxed until it stopped seeing the guide's output. And the comparison that looked
significant was driven by a line in the task text rather than by the thing being tested.
The only check that has held up in this investigation is the human reading the output; the
counter is good for counting what the human has already named, and for nothing else yet.
