---
name: writing-for-humans
description: Use before writing or revising text a person will read twice — a design document, a plan, a reply in a session, a commit message, a pull request body. Covers what the text must mean, the order a reader needs, and the sentence habits worth fixing last.
whenToUse: Any document or reply longer than a few lines, and any substantive revision of one. Skip for single-line answers, status pings, and notes nobody else reads.
---

# Writing for humans

## What this is

Rules for text a person reads. The author was present for the work and the reader was not; the problems below come from that difference.

**Work in this order.** Content, then flow, then sentences.

1. **What the text means** — whether it is true, complete, and about the thing that
   was asked. Documents fail here most expensively, and sentence-level rules are blind to it.
2. **How it reads** — the order, the headings, the joins between sections.
3. **The sentences** — the habits that tire readers. Fix these last, and never at
   the cost of the first two.

That order rests on one measurement rather than on proof. A design document in the
repository this skill came from was rewritten with the rules in Part 3 below:
em-dashes went from thirteen to zero and the mean sentence from 24.8 words to 18.4,
and the rewrite introduced nine content defects, one of them a count in its first
sentence that contradicted the section's own contents. Counting habits is the easy
half, and a writer who optimises the count ships the errors the count cannot see.

## Part 1: what the text means

Nine checks. Six have a test you can run against the page; the other three are
questions to hold against the request that produced the text.

- **Every claim carries its evidence.** Name the file, the command or the
  measurement. Where you have none, mark the claim unverified or cut it. A sentence
  that cannot be false — "a decision with no stated cost is either trivial or
  dishonest" — is decoration: replace it with the check the reader can run.
- **The strength of a claim matches its evidence.** Such words as "all", "never", "exactly", "every", "always", "nobody", "only"
  and "none": check each against the evidence. An unmeasured "exactly" overstates
  what the text knows.
- **The numbers agree with the page.** Recount every count and every list against the items present, and check every date
  against the event it describes. A mismatch blocks the text: a first sentence
  promising four items above a list of five is a defect, not a typo.
- **Every item sits in the right category.** Ask whether everything beneath a
  heading belongs there, and whether the introduction's promise covers all the
  contents. This is a check on meaning; whether a heading carries its point is a
  check on structure, and Part 2 owns that one. Categories carry meaning: a rule this repository chose,
  filed under facts about the platform, tells the reader the reverse of the truth.
- **Terms are defined before use, and used the same way twice.** Check the first
  appearance of each, and check that the second use means the first.
- **The text answers the question that was asked.** Read the original request.
  Long work drifts into answering the question the work turned into.
- **Nothing load-bearing is missing.** Ask what a reader must supply in order to
  act: a decision, a path, a threshold, an owner, a date. Each missing item is a gap.
- **A decision carries its options, the case against, and a recommendation.** A
  question handed to a reader without the alternatives, what each costs, and which
  you would choose is work you have moved rather than done. If you call a choice
  small, say what makes it small; if you cannot say, you have not analysed it. And a
  premise raised a second time is not analysis: say what changed since the last
  time, or the reader is being asked to re-read rather than to decide.
- **The scope is stated.** Say what the text does not cover, because silence reads
  as coverage.

## Part 2: how it reads

**Scope.** This order is for a document or a plan. A reply uses the parts that
apply; a commit message carries one paragraph and none of the headings.

Open with two or three plain sentences: what the thing is, and why it matters.
Then these five sections. Each of the first four answers the question the one
before it raises, so a reader who stops early still has an argument rather than a
fragment; the fifth is where the chain stops.

1. **Constraints.** What is true of the world that we cannot change, such as a
   platform behaviour, a measured failure, or a limit somebody else owns. Name the
   file, command or measurement that proves each one. Say which items are facts and
   which are rules you have chosen to adopt; a reader who cannot tell them apart
   cannot tell what the design is free to change.
2. **Trade-off decisions.** Given those constraints: what we choose, what we give
   up, and what would let us revisit it. Present it as a table with those three
   columns. If you cannot fill the middle column, you have not found the trade-off.
3. **The plan and the order of changes.** What happens, in what sequence, and why
   that sequence.
4. **Verification.** How we will know it works, then the specific checks. For
   anything mechanical, state each check beside the mutation that must make it fail
   — a deliberate change to the code or the data — and run that mutation once, so
   that the claim rests on an observed failure rather than on the check being new.
   For a claim that is not mechanical, state the evidence that would falsify it.
5. **Open questions, then the ideal shape.** What the first four sections could not
   settle. Then what the thing becomes if some of the constraints disappear, naming
   which ones; the ideal belongs last because the reader now knows what blocks it.

The first four are a chain: constraints force a trade-off, the trade-off forces a
plan, the plan raises "how would we know?". The fifth is where the chain stops.

**Section shape.** In a document of more than a page, give each section the same
three beats: the claim, the evidence for it, and what follows from it. That last
beat is also the join to the next section. The final section is the exception:
open questions assert no claim, and the ideal shape states no consequence.

**Joins.** End each section by naming what follows from it, so the reader is not
left to infer the connection. A section that is five unconnected verdicts carries no
argument, however true each verdict is.

**Terms the reader cannot look up.** A metaphor you coined ("the anchor", "the
guardrail", "the landing zone") is not shared vocabulary. A reader who cannot look
a term up stops; this blocks understanding, and it belongs here rather than among
the sentence habits. Say what you mean, or define the shorthand where it first
appears.

**Unstated subjects.** "It was decided", "the check fails", "this is required" —
who decided, which check, required by what? Name the actor and the object. An
unstated actor makes a sentence unactionable, which is comprehension again.

**Headings.** Read only the headings. If together they do not carry the argument,
rewrite them or move the sections until they do. A heading that names a role
("The fact that decides everything") rather than a point carries nothing.

## Part 3: the sentences

These matter, and they matter third. Fix them after Parts 1 and 2 are sound. The
two comprehension blockers — a term the reader cannot look up, and an unstated
subject — are in Part 2, because they stop a reader rather than tire one.

**Antithesis and correlative pairs.** The shapes: "not X but Y", "X rather than
Y", "X, not Y", "either X or Y", "the more X, the less Y", "not only A but also
B", and a pair of clauses asserting opposite directions ("it keeps the stamp and
gives up the check"). Each asserts a position by excluding its opposite, so a run
of them reads as a run of verdicts and the reader has to extract the facts again.

Count by reading: a search pattern misses "X, not Y" and the paired clauses, and
on one measured section found 1 where a reader found 4. A *place* is a headed
section in a document, and a paragraph in a reply. More than two in one place, and
you rewrite that place until the count is two or fewer.

- Tired: "It is not that the check is missing, but that nothing can report it."
- Direct: "Nothing can report the check's absence, because whatever would report
  it lives inside the check."

**Cramming.** A sentence carrying three or four facts, held together with dashes,
colons and semicolons. The reader suspends each clause while the next arrives. Cut
it into sentences; a short one that carries its steps beats a long one that assumes
them.



## The checks you can run yourself

Your reading of your own text is worth less than you think, because you hold the
context that makes it obvious. Four checks survive that limit:

1. **The mechanical half of Part 1.** Recount the numbers and the lists, ask what
   sits under each heading, and check the first appearance of every term. These
   catch defects no amount of rewriting the prose will fix.
2. **Headings only.**
3. **First paragraph only** — if a section's first paragraph does not say what the
   section is about, rewrite that paragraph.
4. **Count the antithesis**, by reading. More than two in a place is a rewrite.

The reader's comprehension is the check you cannot run. Hand the finished text to
somebody who was not there, or to a fresh agent in the cold-reader role described
in the `reviewing-with-a-team` skill. That skill owns both what to do when no
reader is available and what happens after an edit, including what counts as a
rewrite.
