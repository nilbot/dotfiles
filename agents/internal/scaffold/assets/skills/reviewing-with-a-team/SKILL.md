---
name: reviewing-with-a-team
description: Use when work needs independent verification — a design document, a migration, an audit, a policy, or any change whose failure would be expensive. Covers the three review lenses, the prompts that make them produce findings, and the delivery gate that decides when text is safe to ship.
whenToUse: When the artifact is long-lived, being wrong is expensive to undo, the evidence spans more than one system, or a previous attempt failed for a reason nobody can name.
---

# Reviewing with a team

## What this is

A procedure for having work checked by agents who were not present when it was
done. It assumes a harness with named teammates; the fallbacks for a thinner one
are in "Running it". The writing rules this procedure serves are in
`writing-for-humans`.

**What a review must establish, in this order.** First, that the content is true and complete, by the eight checks in
`writing-for-humans` Part 1. Second, that a reader can follow the argument.
Third, that the sentences do not tire them. Style findings are the easiest to
produce and the least valuable, so a review that returns only those has not
reviewed the content.

## When this is worth it

Use a team when at least two of these are true:

- The artifact is long-lived and other people will act on it.
- Being wrong is expensive to undo: a migration, a deletion, a release.
- The evidence is spread across more than one file, module or system.
- A previous attempt failed for a reason nobody can name.

Skip it when a single mechanical check settles the change and that check has been
shown to fail on what it guards: a test that has gone red on the bug, a compiler
error, a diff somebody else already read. Also skip a draft you are about to
rewrite: reviewing it wastes the reviewers.

## The premise pass, before any lens

Before dispatching, list what the artifact **assumes exists**: every build target,
path, command and mechanism it treats as background. For each, answer three
questions.

- **Who decided this** — a person, a document, or nobody.
- **Where is it written** — the path, or "nowhere".
- **Has it been executed** — or is it an approved change nobody has applied yet.

The expensive answer is "a person, in conversation, not yet executed". No lens will
find it, because the repository argues *for* the thing: whatever is about to be
deleted is usually well written and defends itself. A design built on that
assumption is coherent, reviewable, and aimed at a machine that is about to stop
existing.

Two rules follow.

**When a file argues for its own existence, that argument is a decision to
re-test, not a fact.** A header comment explaining why a target is "a shortcut, not
a separate mechanism" is among the most reviewable text in a repository, and every
lens reads past it.

**An approved change that removes a premise is not a job to schedule; it is a
premise to apply before the work continues.** Filing it behind the work it
invalidates is how an artifact gets reviewed ten times against a shape its owner has
already decided to delete.

Worked example. A design for a binary's identity assumed two builds exist on a
machine: one compiled from a checkout, one released. Three of its load-bearing
parts — a compile-time stamp, an environment variable and the checkout builder —
rested on that sentence, which was asserted as fact and never argued. Ten lens runs
found around eighty real defects and not this one. The owner had decided earlier
that the checkout build should go; the decision was written nowhere, and the build
target it deletes carries a comment defending itself. The symptom did surface — a
sequencing agent measured that `./bootstrap apply workstation` refuses on a machine
wired the other way — and it was handed to the owner as a question instead of being
traced to the premise.

## Three lenses, three questions

Each lens finds a class of defect the others cannot. Run them in parallel when the harness allows; the order to use when it does not is
in "Running it".

**1. Independent derivation — "produce the list yourself first."**
The list is the inventory of everything the work touches: files, tests, build
targets, configuration, documents. Write your own inventory first. Then hand the
reviewer the task and an empty page, without letting them read yours, and ask for
theirs in the same shape: a path and a one-line reason for every entry, with
anything they could not verify marked as unverified. Compare the two lists; every
entry only one of you has is the finding.

**2. Adversarial attack — "find what is wrong."**
Every finding names its evidence and says what breaks. For code, a path or a
command. For a plan or a policy, the step or the case that fails, quoted. Every
finding ends with **the alternative**: the fix you would apply, or the choice that
would have avoided the problem. Say that praise is not wanted.

Ask what state can exist that the artifact never considered. For a mechanism that
means concurrent writers, a deleted file, a half-written record, two versions of a
binary. For a plan it means an assumption that does not hold. For a policy it means
the case at the boundary.

Two warnings to give the attacker, both learned from a real incident:

- Reproductions happen **only under `/tmp`**, and scratch trees are deleted
  afterwards. One attacker ran a redirect through a symlink and overwrote an
  installed binary; the machine lost the tool for six minutes until it was
  reinstalled.
- Read-only on the real checkout. Say it explicitly, because "reproduce this" reads
  as permission to write.

**3. Cold reader — "you have never seen this repository."**
Hand them the text alone, not the surrounding tree, unless the task is specifically
to check whether a claim is evidenced elsewhere. Ask for every place the text
forced them to reconstruct context that is not on the page, with the sentence
quoted and a statement of what is missing. A request for "clarity feedback" returns
impressions; a request for quotes returns defects.

Ask them also to state in three sentences what the text said. Where their summary
differs from what you meant, that difference is a content defect, and it outranks
anything they report about the prose.

## The delivery gate

**Nothing is delivered until a cold reader has read the text that is actually being
delivered.**

The gate needs the third lens. Which of the other two you also run is a judgement
about risk, and the "when this is worth it" list above is that judgement.

The common way to fail the gate: reviewers read version one, you fold their
findings into version two and ship it. Version two has been read by nobody, and the
defects the rewrite introduced are the ones that survive.

**What counts as a rewrite.** A change to meaning, to structure, or to the prose
of a delivered section: a reordered section, a changed claim, an added or removed
constraint, a rewritten paragraph, a new example that carries weight. A typo fix, a broken link or a line-number update does not reopen
the review. When a rewrite does reopen it, re-run the cold reader, and re-run
whichever lens's findings forced the rewrite if that lens changed a mechanism.
Otherwise, deliver and say plainly that the text is unreviewed.

## Running it

- **One writer per file.** The author writes the artifact. Reviewers are read-only
  and report in their final message, which keeps the workspace clean and prevents
  two members editing the same text.
- **One task per lens, with a named owner.** The Lead creates the task
  (`team_task_create`), then names its owner in that member's spawn prompt; the
  member claims it with `team_task_get` and `team_task_update` using the current
  revision. A lens whose report never arrives is then visible on the board.
- **Fewer than three members, or no board.** Run the lenses one after another in
  this order: derivation, attack, cold read. Tell the human which lens has not
  reported, and treat an undelivered cold read as an unreviewed text.
- **Fold the findings yourself.** A reviewer produces the list; deciding what to
  accept, and writing the result, stays with the author.
- **Keep the artifact still while it is being read.** Editing the file under a
  reviewer invalidates their reading and their line references.
- **Tell the reviewer what not to do:** no summary of the document back to you, and
no restating the design.

## What each lens buys

The three find different things, which is why one reader asked all three questions
finds less than three readers asked one each. Independent derivation finds what is
missing. Adversarial attack finds claims that are false and mechanisms that do not
work. Cold reading finds an argument a reader cannot follow. Between them they
cover content first, then comprehension; the sentence habits in `writing-for-humans`
are yours to fix afterwards, and they are not what a reviewer should spend its
budget on.
