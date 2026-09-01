# 2026-09-01 — Why the migration skill shipped hollow

Reviewing [PR #40](https://github.com/nilbot/dotfiles/pull/40) after merge. The
Go side of the Two-Tier work — `internal/drift`, `agents drift`, five
`scaffold:*` doctor checks, the refined `agents update` — is sound and tested.
The one deliverable that is *prose*, `.agents/skills/migrating-fleet-context/SKILL.md`,
came out as an architecture summary with shell commands attached: it contradicts
the tooling it is supposed to drive, and it drops requirements the design and the
plan both stated.

The question this entry answers is not "what is wrong with the skill" — that was
enumerated separately. It is **where each defect entered**, and whether the
`superpowers:writing-plans` skill or the authoring harness (Antigravity) is
responsible.

## Where each defect entered

Read design = `docs/design/2026-08-29-two-tier-context-and-llm-migration-architecture.md`,
plan = `docs/plans/2026-08-31-two-tier-context-and-llm-migration-plan.md`,
both in commit `60e5d22`; skill authored in `285627e`, same day, same author.

| # | Defect | Design | Plan | Entered at |
|---|---|---|---|---|
| 1 | No fleet mode; never runs `agents ls` | §7.1 names it | Task 6 *Interfaces* names it, Step 1 body does not | plan step body |
| 1b | `--all --json` emits an array, `--json` an object | absent (§4.2 gives one object) | absent | Task 3 implementation, never reconciled with Task 6 |
| 2 | Four router states collapsed to one procedure | §4.1 defines all four | Task 6 names one behaviour | plan |
| 3 | `rm -f CLAUDE.md` can delete canonical content | absent | absent | design omission |
| 4 | "3-way merge" with undefined sources | used 4× undefined | repeated verbatim | design vocabulary |
| 5 | Archive relocation vs archive immutability | §7.1 says move from `docs/archive/plans/`; §7.2 + `.agents/AGENTS.md:10` say **STRICTLY IMMUTABLE** | Task 2 narrows the classifier to `docs/journal/` | design, then widened by implementation |
| 6 | Zero-rule-dropping has no verification | §7.2 asserts it | no test step at all | plan |
| 7 | Human approval gate and PR missing | §7 nodes 6–7 | Task 6 Step 1 **does** say "Interactive human diff review before commit/PR" | skill authoring |
| 8 | Skill can run while stale | absent | absent | — (and see below) |
| 9 | No triage for `.agents/memory/`, `.agents/reports/` | absent | absent | design omission |
| 10 | Canonical router pasted as 24 literal lines | §2.1 embeds it too | Task 6 says restore `DefaultAgentsMD` — a *symbol* | skill authoring |

Rows 7 and 10 are the only pure transmission losses: the plan carried the
requirement and the skill dropped it. The skill contains **0** occurrences of
`agents ls`, `--all`, `gh pr`, `approval`, or `approve`, and its Phase 5 ends by
staging four broad paths and committing.

Two of these are worth pinning with numbers.

**Row 5 is a three-way disagreement, currently latent.** `drift.go:151-155`
flags any `*-plan.md` outside `docs/plans/` and any `*-design.md` outside
`docs/design/`, walking all of `docs/` — including `docs/archive/`. The plan
(Task 2, Step 3) had specified only `docs/journal/`. It does not fire in
`dotfiles` today because the archived plans are named
`2026-08-07-agents-repo-context.md`, without the `-plan.md` suffix:
`agents drift --json` returns `"misplaced_docs": []`. Any fleet repo that
archived with the suffix would have the skill told to `git mv` out of a store
`.agents/AGENTS.md` declares immutable.

**Row 8 has a deterministic half that is also green when it should not be.**
The design's §6 table specifies `warn` for `scaffold:skill-migrating` when the
skill is "missing **or outdated**". `doctor.go:1120-1125` reports
`ComponentCustomized` as **`OK`** — "carries repository customizations". So a
stale or hand-edited authoritative skill passes doctor. The plan's Task 4 never
restated the design's remedy table; it said only "generate the 5 diagnostic
checks with descriptive status and remedies", and the implementation chose.

Row 10 is currently harmless and unbound: the 23-line router block pasted into
the skill is byte-identical to the live `AGENTS.md`, and no test asserts that it
stays so.

## The plan is its own control

The tempting attribution is "Antigravity wrote a sloppy plan". The plan itself
refutes that, because it holds the harness constant and varies only the
deliverable type:

- **5 of 7 tasks carry a red-first gate** (`Expected: FAIL` at lines 96, 200,
  278, 345, 404). Tasks 1–5 contain real Go, real assertions, real file:line
  targets.
- **Task 7 has no failing-test step but does invoke existing gates** —
  `docs_test.go` and `exitcode_doc_test.go`, which are genuine content
  assertions over prose.
- **Task 6 has neither.** Its Step 1 is a nine-bullet list of topics. Its Step 3
  runs `go test -v ./agents/...`, which proves only that the file embeds.

Same author, same session, same document. Output quality tracks whether the
`writing-plans` template supplied a shape, not who was driving. That is a
within-document control, and it points at the template.

The template's blind spot is structural, not accidental. Its entire Task
Structure example is `pytest`; its Bite-Sized Task Granularity section is
literally "write the failing test / run it / implement / run / commit"; and its
No Placeholders rule ends with "code blocks required for **code** steps" —
which by omission licenses a prose step that only describes. Its Self-Review has
exactly three checks: spec coverage, placeholder scan, type consistency. A
hollow prose task passes all three.

And it did. The plan's own Self-Review Checklist asserts:

> **No Placeholders:** All steps contain explicit Go code snippets, test
> assertions, file paths, and shell commands.

Task 6 Step 1 contains none of those. The self-review produced a false green on
the one task that needed it — the same failure shape already recorded in
[can this check actually fail](../qna/can-this-check-actually-fail.md).

## The repository already owns the fix, aimed elsewhere

Prose *can* carry a test cycle here; two already do.

`TestLivingDocumentsNameOnlyRealCommands` (`agents/docs_test.go:95`) walks
`.agents/skills/**/*.md`, so the migration skill **is** scanned — and passes,
because every command it names (`agents drift`, `agents doctor`, `agents init`)
exists. The check is one-directional: it catches invented commands, never
omitted ones. Defect 1 is precisely an omitted command, so this test could not
have caught it.

The other direction exists too. `TestHarnessSkillCoversAgentCommands`
(`docs_test.go:140`) asserts every `Audience: Agent` command appears in the
fleet skill — and `drift` is registered `Audience: []Audience{Human, Agent}`
(`commands.go:46`). But that test is hard-bound to
`claude/skills/agents-tool/SKILL.md`, which does name both `agents ls` (line
108) and `agents drift` (line 126). The reverse check works; it was simply never
pointed at the skill Task 6 produced.

Both tests pass right now (`go test . -run 'TestLivingDocuments|TestHarnessSkillCovers'` → `ok`),
and `agents doctor` reports all five `scaffold:*` checks `ok`. Every gate this
change owns is green while the skill is wrong. That is the finding.

## Verdict, and what would settle the rest

**The `writing-plans` template is the necessary condition.** It has no shape for
a deliverable that is prose, so Task 6 was written without content and without a
gate, and nothing downstream could detect either.

**The harness is not thereby exonerated.** Rows 7 and 10 are content the plan
explicitly carried and the skill silently dropped — an execution-side loss. The
template's blind spot is what removed the detector that would have caught it.

What is *not* settled is whether a different harness executing the same template
would have produced a hollow Task 6. This entry asserts a template blind spot
from a within-document control, not from a probe, and the repository's standard
for harness claims is higher than that — see
[why didn't Antigravity apply my rules](../qna/why-didnt-antigravity-apply-my-rules.md),
where a sound-looking inference about `agy` was wrong in two independent ways
and only a fixture with a positive control made the run readable.

The probe, if it is worth running: hand the same design §7 and the same
`writing-plans` skill to Claude Code, and read whether its Task 6 comes out with
a gate. The positive control is a code task in the same plan — if Tasks 1–5 also
degrade, the run says nothing about prose. Until that is run, "the template is
the culprit" is the best-supported reading of one document, not a measured fact
about two harnesses.

## What to change

1. Give `agents drift` and the skill one definition of misplaced: exclude
   `docs/archive/` in `drift.go`, or drop archive relocation from the skill.
   They disagree today.
2. Make `scaffold:skill-migrating` `warn` on `customized`, per design §6.
   Presence is not currency.
3. Point a reverse-coverage test at the migration skill, the way
   `TestHarnessSkillCoversAgentCommands` points at `agents-tool`.
4. Bind the pasted 24-line router to `scaffold.DefaultAgentsMD` with a test, or
   stop pasting it.
5. When `writing-plans` produces a task whose deliverable is a document, write
   the acceptance assertion into the task anyway. The template will not ask.

## Resolution, same day

All five items landed on `feat/migration-skill-rework`, each red before green.

`TestMigrationSkillCoversItsSpecifiedProtocol` is the gate that was missing.
Against the old skill it failed **nine** times — once per real defect, including
every omission `TestLivingDocumentsNameOnlyRealCommands` structurally could not
see. It asserts required substrings with the spec section that requires each,
forbids the literal `rm -f` on `CLAUDE.md`, and forbids a `git mv` with an
archive source. Two more bind the skill to its context: one asserts the
repository copy and the embedded asset are byte-identical, one asserts the
router pasted into the skill equals `scaffold.DefaultAgentsMD`. All three are
registered in the `docs` job of `verify.yml`, which fails if a named test stops
existing.

Both of the repository's existing prose gates caught real errors in the rewrite
draft — the living-documents scan rejected `agents handoff` in a code span
listing retired commands, and the new gate rejected my own forbidden literal in
a warning. The one-directional check is weak, not useless.

**A correction to the analysis above.** This entry implied the whole exercise
was a no-op on an already-migrated repository. That was wrong, and the reasoning
is worth keeping. `CanonicalSkillDigest` hashes the asset *in the running
binary*, and `LegacySkillDigests` returns `nil` for `migrating-fleet-context` —
only `recording-what-you-learn` has a legacy entry. So rewriting the embedded
asset makes every already-migrated repository report `customized`, and
`isDriftClean` requires every skill to be `ComponentOK`. Measured on the new
binary: `cowork` goes from exit 0 to **exit 1** with `migrating-fleet-context:
customized`, while its router, symlink, domain and all four stores stay clean.
The semantic un-nesting has nothing to do there; the staleness path fires. Those
are different claims and this entry ran them together.

**Two defects found while fixing, neither in the original review.**

*The inverted symlink.* `playground/desktop_pet` is on the pre-2026-08-19
topology: `AGENTS.md -> CLAUDE.md`, with 866 bytes over 18 lines in `CLAUDE.md`
as the only real file. The old skill's unconditional replacement would have
deleted it and left `AGENTS.md -> CLAUDE.md -> AGENTS.md`, a symlink loop with
every line of context gone. Defect 3 was worse than "may lose content": on this
one repository it is total loss plus an unreadable tree. Now design §7.4 with a
three-row topology table.

*The tool owned a directory it did not own.* `drift.go` classified every
directory under `.agents/skills/` and reported unrecognised ones as
`customized`, which `isDriftClean` treats as dirty. But §2 says `.agents/skills/`
is exactly where repository-specific skills belong, so a repository using the
feature as designed could never report clean and no migration could fix it —
there was nothing to fix. `playground/autogo-mlx` carries `human-ranked-sft` and
`llm-in-the-loop-rl-discovery` and was permanently dirty for it. The tool now
judges only what it embeds; the rest is listed in a new `local_skills` field and
never classified.

**One process note.** The control for `TestMigrationSkillPastesTheCanonicalRouter`
appeared to pass with the router deliberately sabotaged. It had not run: `go
test` served a cached result, because the file it reads lives outside the
package. `-count=1` showed both tests failing as they should. `verify.yml:101`
already carries the comment that `-count=1` is load-bearing for exactly this,
so CI was never exposed — only the local check was, and a control that silently
does not run is the failure this repository has recorded twice before.

## What running it found that reading it did not

The rewritten skill was executed against `cowork` on v0.5.0 within minutes of
release. Two defects surfaced immediately, and neither was reachable by review.

**The self-refresh deadlocked.** Step 0 said to refresh a stale skill with
`agents update`, and Step 2 said to stop if `git status --porcelain` produced
any output. The refresh writes a tracked file, so following the steps in order
produced a dirty tree that the next step rejected. Reading the skill does not
surface this; only running the steps in sequence does. Fixed by splitting
detection (read-only, Step 0) from the fix (Step 3.5, after branch isolation).

**The invocation did not exist.** Both the skill and the doctor remedy said
`agents update --apply`. The CLI answers:

```
agents update: --all is required; use `agents wire` for one repository
```

`RefreshInfrastructuralSkills` is called from exactly one place,
`cmd_fleet.go:150`, on the `--all` path. `agents wire` does not refresh skills,
so there is no single-repository form at all — the refresh necessarily rewrites
the skill in every registered repository. The design carried the same broken
invocation in its §6 remedy table from the start, so this one predates the
rewrite.

Three things about the gates are worth keeping.

*The bug was in the gate too.* `TestMigrationSkillCoversItsSpecifiedProtocol`
asserted the skill contains `agents update --apply` — it required the invalid
command. A test written from the same spec as the artifact inherits the spec's
errors, and a green suite proved only that the two agreed.

*Neither existing check could see it.* `TestLivingDocumentsNameOnlyRealCommands`
validates that a named command exists; `update` does, and `--apply` is a
registered flag. What was wrong is an absent **required** flag, which no
existence check detects. `TestLivingDocumentsSpellUpdateWithAll` now covers it:
a bare `agents update` is prose naming the command and passes, a flag-bearing
span without `--all` fails.

*The first version of that gate could not fail.* Its control passed with the
invalid command reintroduced, because the regex scanned inline backtick spans
only and the real command lives in a ```bash fence — the one place a reader
actually copies from. Both a fenced and an inline sabotage now fail it. This is
the third time in this entry that a check needed a control before it could be
believed, and the second time the control itself was the thing that was broken.
