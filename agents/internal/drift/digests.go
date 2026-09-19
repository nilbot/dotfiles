package drift

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/nilbot/dotfiles/agents/internal/layout"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

type RouterState string

const (
	RouterCleanCurrent RouterState = "clean_current"
	RouterCleanLegacy  RouterState = "clean_legacy"
	RouterDrifted      RouterState = "drifted"
	RouterMissing      RouterState = "missing"
)

type ComponentState string

const (
	ComponentOK          ComponentState = "ok"
	ComponentCleanLegacy ComponentState = "clean_legacy"
	ComponentCustomized  ComponentState = "customized"
	ComponentMissing     ComponentState = "missing"
)

// LegacySingleBulletRouter represents the 2026-08-28 single-bullet doctor instruction template (with original trailing spaces).
const LegacySingleBulletRouter = `# Agent context

Durable context for this repo lives in ` + "`docs/`" + `. Read it before assuming;
it is the record, and this file is only the pointer to it.

- ` + "`docs/qna/`" + ` — answers indexed by the question you would ask again
- ` + "`docs/journal/`" + ` — dated record of what happened
- ` + "`docs/design/`" + ` — the design still in force

## Repository Architecture & Guidelines
- Domain engineering guidelines, commenting standards, and safety constraints 
  are defined in ` + "`.agents/AGENTS.md`" + `.
- Repo-specific procedures and skills are located in ` + "`.agents/skills/`" + `.

## Machine Wiring
` + "`.agents/`" + ` holds machine wiring and local skills. A hook cannot install itself 
and a missing hook fails silently, so an empty or stale ` + "`.agents/`" + ` means the setup 
is broken rather than that there is nothing to say — report it rather than 
working around it.

` + scaffold.LegacyDoctorInstruction + `

Recording is covered by the global instruction and the ` + "`recording-what-you-learn`" + ` 
skill; it is not repo-specific and is not restated here.
`

// LegacySingleBulletRouterTrimmed represents the single-bullet doctor instruction template with trimmed spaces.
const LegacySingleBulletRouterTrimmed = `# Agent context

Durable context for this repo lives in ` + "`docs/`" + `. Read it before assuming;
it is the record, and this file is only the pointer to it.

- ` + "`docs/qna/`" + ` — answers indexed by the question you would ask again
- ` + "`docs/journal/`" + ` — dated record of what happened
- ` + "`docs/design/`" + ` — the design still in force

## Repository Architecture & Guidelines
- Domain engineering guidelines, commenting standards, and safety constraints
  are defined in ` + "`.agents/AGENTS.md`" + `.
- Repo-specific procedures and skills are located in ` + "`.agents/skills/`" + `.

## Machine Wiring
` + "`.agents/`" + ` holds machine wiring and local skills. A hook cannot install itself
and a missing hook fails silently, so an empty or stale ` + "`.agents/`" + ` means the setup
is broken rather than that there is nothing to say — report it rather than
working around it.

` + scaffold.LegacyDoctorInstruction + `

Recording is covered by the global instruction and the ` + "`recording-what-you-learn`" + `
skill; it is not repo-specific and is not restated here.
`

// LegacyPrePlansRouter represents the 2026-08-28 two-bullet template pre-plans.
const LegacyPrePlansRouter = `# Agent context

Durable context for this repo lives in ` + "`docs/`" + `. Read it before assuming;
it is the record, and this file is only the pointer to it.

- ` + "`docs/qna/`" + ` — answers indexed by the question you would ask again
- ` + "`docs/journal/`" + ` — dated record of what happened
- ` + "`docs/design/`" + ` — the design still in force

## Repository Architecture & Guidelines
- Domain engineering guidelines, commenting standards, and safety constraints 
  are defined in ` + "`.agents/AGENTS.md`" + `.
- Repo-specific procedures and skills are located in ` + "`.agents/skills/`" + `.

## Machine Wiring
` + "`.agents/`" + ` holds machine wiring and local skills. A hook cannot install itself 
and a missing hook fails silently.
- If the ` + "`agents`" + ` CLI is installed, run ` + "`agents doctor`" + ` early and report any warnings before relying on this context.
- If ` + "`agents`" + ` is not installed on this machine, skip machine wiring checks and adhere directly to the repository instructions above.

Recording is covered by the global instruction and the ` + "`recording-what-you-learn`" + ` 
skill; it is not repo-specific and is not restated here.
`

// LegacyPrePlansRouterTrimmed represents the 2026-08-28 two-bullet template pre-plans with trimmed spaces.
const LegacyPrePlansRouterTrimmed = `# Agent context

Durable context for this repo lives in ` + "`docs/`" + `. Read it before assuming;
it is the record, and this file is only the pointer to it.

- ` + "`docs/qna/`" + ` — answers indexed by the question you would ask again
- ` + "`docs/journal/`" + ` — dated record of what happened
- ` + "`docs/design/`" + ` — the design still in force

## Repository Architecture & Guidelines
- Domain engineering guidelines, commenting standards, and safety constraints
  are defined in ` + "`.agents/AGENTS.md`" + `.
- Repo-specific procedures and skills are located in ` + "`.agents/skills/`" + `.

## Machine Wiring
` + "`.agents/`" + ` holds machine wiring and local skills. A hook cannot install itself
and a missing hook fails silently.
- If the ` + "`agents`" + ` CLI is installed, run ` + "`agents doctor`" + ` early and report any warnings before relying on this context.
- If ` + "`agents`" + ` is not installed on this machine, skip machine wiring checks and adhere directly to the repository instructions above.

Recording is covered by the global instruction and the ` + "`recording-what-you-learn`" + `
skill; it is not repo-specific and is not restated here.
`

// LegacyCaptureRouter represents the 2026-08-20 pre-antigravity / capture apparatus removal version.
const LegacyCaptureRouter = `# Agent context

Durable context for this repo lives in ` + "`docs/`" + `. Read it before assuming;
it is the record, and this file is only the pointer to it.

- ` + "`docs/qna/`" + ` — answers indexed by the question you would ask again
- ` + "`docs/journal/`" + ` — dated record of what happened
- ` + "`docs/design/`" + ` — the design still in force

` + "`.agents/`" + ` holds machine wiring, not knowledge: harness hooks, the trace
cache, and ` + "`.agents/skills/`" + ` for procedures specific to this repo. A hook
cannot install itself and a missing hook fails silently, so an empty or stale
` + "`.agents/`" + ` means the setup is broken rather than that there is nothing to
say -- report it rather than working around it.

` + scaffold.LegacyDoctorInstruction + `

Recording is covered by the global instruction and the
` + "`recording-what-you-learn`" + ` skill; it is not repo-specific and is not
restated here.
`

// LegacyInitialClaudeMDRouter represents the 2026-08-07 initial ClaudeMD scaffold.
const LegacyInitialClaudeMDRouter = `# Agent context

Durable context for this repo lives in ` + "`.agents/`" + `. Read it before assuming;
it is the record, and this file is only the pointer to it.

- ` + "`.agents/memory/INDEX.md`" + ` — curated knowledge about this codebase (generated)
- ` + "`.agents/reports/handoff/INDEX.md`" + ` — work in flight, by lane (generated)
- ` + "`.agents/reports/`" + ` — specs, plans, analysis, and trace pointers
- ` + "`.agents/skills/`" + ` — procedures specific to this repo

Run ` + "`agents doctor`" + ` early and surface what it says. A hook cannot install
itself and a missing hook fails silently, so an unreported failure here means
nothing is being recorded.

Write handoffs with ` + "`agents handoff write`" + `, not by hand. Commit ` + "`.agents/`" + `
changes with ` + "`agents save`" + ` so they do not ride along with code changes.
`

// LegacyRecordingSkill represents the 2026-08-20 version of recording-what-you-learn (which referenced CLAUDE.md instead of AGENTS.md).
const LegacyRecordingSkill = `---
name: recording-what-you-learn
description: Use when something worth keeping surfaces during work - the human says "save that", a run collapses, an approach is abandoned - or before asserting a claim about an area the repository may already have answered. Covers where knowledge goes, the shape it takes, and why it is documentation rather than a tool's private store.
---

# Recording what you learn

Knowledge about a codebase is **documentation**. It goes in the repository's
docs, in plain markdown, committed like anything else. There is no queue, no
promotion step and no separate store to sync.

Two stores, one retrieval axis each:

| | indexed by | answers |
|---|---|---|
| ` + "`docs/qna/`" + ` | topic | "how does X actually work / why did X happen" |
| ` + "`docs/journal/`" + ` | time | "what was I doing, and why did it go that way" |

A repository may name different directories in its own ` + "`CLAUDE.md`" + `. If it names
none and has a ` + "`docs/`" + `, use these. If it has no ` + "`docs/`" + ` at all, ask before
creating one.

## When to write

**The human notices.** "Save that", "good to know", "worth remembering" — that is
the trigger, and it is the strongest one available because a human just told you
this mattered. Write it while the context is still in front of you. Do not batch
it, and do not ask whether it is worth it: they already answered that.

**The work hits something.** A bug understood, a training run collapsed, a build
broken in a way that took real effort to diagnose, an approach abandoned and why.
These earn a ` + "`docs/journal/`" + ` entry at the moment they resolve, not at session end.

Nothing fires because a session ended. A session ending is not evidence that
anything happened.

## When to read

**Before asserting a claim about an area, grep for it.** Not "consider checking" —
grep. The failure this prevents is specific and measured: an agent wrote that a
CI leg was not reproducible locally while the repository's own spec recorded the
correction, one ` + "`grep`" + ` away. Confident recall is indistinguishable from knowledge
from the inside, which is why the check has to be mechanical rather than a
judgement about whether you are sure.

` + "```bash\n" + `grep -ril "<distinctive noun>" docs/qna/ docs/design/
` + "```\n\n" + `Distinctive nouns are what work: an image name, a flag, a tool, an error string.
Common words return everything and tell you nothing.

## The shape

` + "```markdown\n" + `# [The question you would ask when you hit this again]

## Context
[when and why it arose — with the concrete numbers]

## Answer
[the mechanism, with evidence]
` + "```\n\n" + `**Question-first.** Name the file for the question, not the conclusion — a reader
arrives with a question. ` + "`opponent-pool-contamination.md`" + `, not
` + "`pooling-is-harmful.md`" + `.

**Length is set by the finding, never by a rule.** Measured across two
repositories: entries written to a three-bullet bound clustered at 175-243 words;
entries sized by their content ranged 418-701. Uniform length is the tell of a
rule, not of a subject. Constrain shape; do not constrain length.

**Carry the numbers.** "Games got shorter" is not the entry. "Average game length
collapsed from 136.4 plies to 106.2, capture rate 29.9% to 22.9%" is. The numbers
are what make it checkable later, and what make it worth reading at all.

No frontmatter, no schema, no generated index. ` + "`ls`" + ` is the index and ` + "`grep`" + ` is
the query.

## Subagents will not do this

Measured on Claude Code: subagents inherit ` + "`CLAUDE.md`" + ` but do not act on it — 0
of 31 observed subagents followed an inherited directive. If you are dispatching
subagents, **the recording is yours**, from what their reports tell you. A child
that discovers something will report it and then forget it; nothing downstream
picks it up.

This is a real gap, not a formality. Work done through subagents is exactly the
work most likely to produce findings and least likely to record them.

## Where this comes from

` + "`docs/design/2026-08-19-knowledge-is-documentation.md`" + ` in the dotfiles
repository, with the evidence for each rule above. Read it before changing any
of this — several of these choices look arbitrary and are not.
`

// LegacyRecordingSkillV051 is the exact text of recording-what-you-learn as
// shipped in v0.5.1, which is also the v1 canonical text (design §0.8). The
// frozen v1 asset is pinned against it, so this constant and that file cannot
// drift apart: transcription is mechanical, and the pin is the check.
const LegacyRecordingSkillV051 = `---
name: recording-what-you-learn
description: Use when something worth keeping surfaces during work - the human says "save that", a run collapses, an approach is abandoned - or before asserting a claim about an area the repository may already have answered. Covers where knowledge goes, the shape it takes, and why it is documentation rather than a tool's private store.
---

# Recording what you learn

Knowledge about a codebase is **documentation**. It goes in the repository's
docs, in plain markdown, committed like anything else. There is no queue, no
promotion step and no separate store to sync.

Two stores, one retrieval axis each:

| | indexed by | answers |
|---|---|---|
| ` + "`" + `docs/qna/` + "`" + ` | topic | "how does X actually work / why did X happen" |
| ` + "`" + `docs/journal/` + "`" + ` | time | "what was I doing, and why did it go that way" |

A repository may name different directories in its own ` + "`" + `AGENTS.md` + "`" + `. If it names
none and has a ` + "`" + `docs/` + "`" + `, use these. If it has no ` + "`" + `docs/` + "`" + ` at all, ask before
creating one.

## When to write

**The human notices.** "Save that", "good to know", "worth remembering" — that is
the trigger, and it is the strongest one available because a human just told you
this mattered. Write it while the context is still in front of you. Do not batch
it, and do not ask whether it is worth it: they already answered that.

**The work hits something.** A bug understood, a training run collapsed, a build
broken in a way that took real effort to diagnose, an approach abandoned and why.
These earn a ` + "`" + `docs/journal/` + "`" + ` entry at the moment they resolve, not at session end.

Nothing fires because a session ended. A session ending is not evidence that
anything happened.

## When to read

**Before asserting a claim about an area, grep for it.** Not "consider checking" —
grep. The failure this prevents is specific and measured: an agent wrote that a
CI leg was not reproducible locally while the repository's own spec recorded the
correction, one ` + "`" + `grep` + "`" + ` away. Confident recall is indistinguishable from knowledge
from the inside, which is why the check has to be mechanical rather than a
judgement about whether you are sure.

` + "`" + `` + "`" + `` + "`" + `bash
grep -ril "<distinctive noun>" docs/qna/ docs/design/
` + "`" + `` + "`" + `` + "`" + `

Distinctive nouns are what work: an image name, a flag, a tool, an error string.
Common words return everything and tell you nothing.

## The shape

` + "`" + `` + "`" + `` + "`" + `markdown
# [The question you would ask when you hit this again]

## Context
[when and why it arose — with the concrete numbers]

## Answer
[the mechanism, with evidence]
` + "`" + `` + "`" + `` + "`" + `

**Question-first.** Name the file for the question, not the conclusion — a reader
arrives with a question. ` + "`" + `opponent-pool-contamination.md` + "`" + `, not
` + "`" + `pooling-is-harmful.md` + "`" + `.

**Length is set by the finding, never by a rule.** Measured across two
repositories: entries written to a three-bullet bound clustered at 175-243 words;
entries sized by their content ranged 418-701. Uniform length is the tell of a
rule, not of a subject. Constrain shape; do not constrain length.

**Carry the numbers.** "Games got shorter" is not the entry. "Average game length
collapsed from 136.4 plies to 106.2, capture rate 29.9% to 22.9%" is. The numbers
are what make it checkable later, and what make it worth reading at all.

No frontmatter, no schema, no generated index. ` + "`" + `ls` + "`" + ` is the index and ` + "`" + `grep` + "`" + ` is
the query.

## Subagents will not do this

Measured on Claude Code: subagents inherit ` + "`" + `AGENTS.md` + "`" + ` but do not act on it — 0
of 31 observed subagents followed an inherited directive. If you are dispatching
subagents, **the recording is yours**, from what their reports tell you. A child
that discovers something will report it and then forget it; nothing downstream
picks it up.

This is a real gap, not a formality. Work done through subagents is exactly the
work most likely to produce findings and least likely to record them.

## Where this comes from

` + "`" + `docs/design/2026-08-19-knowledge-is-documentation.md` + "`" + ` in the dotfiles
repository, with the evidence for each rule above. Read it before changing any
of this — several of these choices look arbitrary and are not.
`

// LegacyMigratingSkillV051 is the exact text of migrating-fleet-context as
// shipped in v0.5.1, which is also the v1 canonical text (design §0.8).
// TestMigrationSkillPastesTheCanonicalRouter reads this v1 text, not the
// repository copy, so the pasted router stays bound to DefaultAgentsMD.
const LegacyMigratingSkillV051 = `---
name: migrating-fleet-context
description: Use when ` + "`" + `agents doctor` + "`" + ` reports a ` + "`" + `scaffold:*` + "`" + ` warning, ` + "`" + `agents drift` + "`" + ` exits non-zero, or a repository keeps domain rules in a root ` + "`" + `AGENTS.md` + "`" + `/` + "`" + `CLAUDE.md` + "`" + `, lacks ` + "`" + `.agents/AGENTS.md` + "`" + `, is missing ` + "`" + `docs/` + "`" + ` stores, carries plans or designs in the wrong store, or has a bundled skill that no longer matches the installed binary.
---

# Migrating Fleet Context

Moves a repository onto the Two-Tier Agent Context architecture: a canonical
root router at ` + "`" + `AGENTS.md` + "`" + `, domain rules in ` + "`" + `.agents/AGENTS.md` + "`" + `, durable
knowledge in the four ` + "`" + `docs/` + "`" + ` stores.

**The deterministic tools know the states; only you can read the prose.**
` + "`" + `agents drift` + "`" + ` tells you exactly what is wrong and never guesses at meaning.
Your job is the part it cannot do — deciding which sentence is a repository's
own rule and which is scaffold boilerplate — and proving you moved every one.

**Nothing is staged or committed until a human approves the diff.**

---

## Step 0: Check whether you are stale

This skill is an ` + "`" + `agents` + "`" + `-owned asset embedded in the binary. The copy you are
reading can be older than the tool you are about to run.

` + "`" + `` + "`" + `` + "`" + `bash
agents doctor
` + "`" + `` + "`" + `` + "`" + `

If ` + "`" + `scaffold:skill-migrating` + "`" + ` reports anything but ` + "`" + `ok` + "`" + `, your instructions are
out of date. **Do not fix it yet** — the fix writes to the working tree, and
Step 2 needs a clean one. Note it and carry on to Step 2; Step 3.5 does the
refresh once you are safely on a branch.

---

## Step 1: Pick the mode and read the right JSON shape

The two invocations do not return the same type. A parser written for one
breaks on the other.

| Mode | Command | Returns |
|---|---|---|
| Single repository | ` + "`" + `agents drift --json` + "`" + ` | one **object** |
| Fleet | ` + "`" + `agents ls` + "`" + `, then ` + "`" + `agents drift --all --json` + "`" + ` | an **array** of objects |

Fleet mode migrates one repository at a time, each on its own branch. Skip and
name in the final report, rather than migrating:

- registry entries reported ` + "`" + `missing` + "`" + ` or ` + "`" + `unknown` + "`" + `
- any repository whose working tree is dirty
- any repository already ` + "`" + `clean_current` + "`" + ` with every other field ` + "`" + `ok` + "`" + `

Fields to read from each report:

| field | use |
|---|---|
| ` + "`" + `router_state` + "`" + ` | picks your procedure — see Step 4 |
| ` + "`" + `symlink_state` + "`" + ` | ` + "`" + `ok` + "`" + ` \| ` + "`" + `not_symlink` + "`" + ` \| ` + "`" + `broken` + "`" + ` \| ` + "`" + `missing` + "`" + ` — see Step 5 |
| ` + "`" + `domain_state` + "`" + ` | ` + "`" + `ok` + "`" + ` \| ` + "`" + `missing` + "`" + ` — whether ` + "`" + `.agents/AGENTS.md` + "`" + ` exists |
| ` + "`" + `skills` + "`" + ` | embedded skills only: ` + "`" + `ok` + "`" + ` \| ` + "`" + `clean_legacy` + "`" + ` \| ` + "`" + `customized` + "`" + ` \| ` + "`" + `missing` + "`" + ` |
| ` + "`" + `local_skills` + "`" + ` | the repository's own skills. **Never touch these.** |
| ` + "`" + `docs_stores` + "`" + ` | which of ` + "`" + `design` + "`" + `/` + "`" + `plans` + "`" + `/` + "`" + `journal` + "`" + `/` + "`" + `qna` + "`" + ` exist |
| ` + "`" + `misplaced_docs` + "`" + ` | plans and designs in the wrong live store |
| ` + "`" + `diff` + "`" + ` | unified diff of the root router against canonical |

---

## Step 2: Preflight

` + "`" + `` + "`" + `` + "`" + `bash
git status --porcelain
` + "`" + `` + "`" + `` + "`" + `

Any output at all: stop. Report it and let the human commit or stash. A dirty
tree makes the approval diff in Step 10 unreadable, which is the one artifact
this whole procedure exists to produce.

## Step 3: Branch isolation

` + "`" + `` + "`" + `` + "`" + `bash
git branch --show-current
git checkout -b feat/two-tier-context-migration
` + "`" + `` + "`" + `` + "`" + `

Never migrate on ` + "`" + `master` + "`" + `, ` + "`" + `main` + "`" + `, or any protected branch.

## Step 3.5: Refresh yourself, if Step 0 said you are stale

` + "`" + `` + "`" + `` + "`" + `bash
agents update --all --apply
` + "`" + `` + "`" + `` + "`" + `

` + "`" + `--all` + "`" + ` is not optional: ` + "`" + `agents update` + "`" + ` refuses to run without it, and
` + "`" + `agents wire` + "`" + ` does not refresh skills. There is no single-repository form, so
this rewrites the skill in **every registered repository**, not just this one.
Two consequences, both yours to handle:

- In this repository the refreshed skill is an uncommitted change on your
  branch. That is fine — it belongs in this migration's commit.
- In the others it leaves an uncommitted change nobody asked for. Say so in
  your Step 10 report, and leave them alone; each repository's own migration
  will carry its copy.

Then **re-read this file** and restart from Step 1. The instructions you have
been following up to this point are the stale ones.

---

## Step 4: Reconcile the root, one procedure per state

Four states, four different correct actions. Treating them alike is the single
most damaging thing you can do here — it partitions a file that has nothing to
partition, or invents content for a file that has none.

| ` + "`" + `router_state` + "`" + ` | What it means | What to do |
|---|---|---|
| ` + "`" + `clean_current` + "`" + ` | matches the installed binary's canonical router | nothing. Do not "improve" it. |
| ` + "`" + `clean_legacy` + "`" + ` | a known older canonical template, **no repository content** | replace wholesale with the canonical router. There is nothing to extract — the digest already proved that. |
| ` + "`" + `drifted` + "`" + ` | canonical text plus, or reworded into, repository content | semantic reconcile, below |
| ` + "`" + `missing` + "`" + ` | no root ` + "`" + `AGENTS.md` + "`" + ` at all | do not invent one. Go to Step 5 and read ` + "`" + `CLAUDE.md` + "`" + `. If that is absent too, **stop and ask** what this repository's rules are. |

### Semantic reconcile (` + "`" + `drifted` + "`" + ` only)

Two inputs — the current root file and the canonical router — and one output
per block. This is not a three-way merge; there is no base. Read the ` + "`" + `diff` + "`" + `
field to see what the repository added.

Classify **every** block:

- **Boilerplate**, to be replaced by the canonical router: pointer tables to
  ` + "`" + `docs/` + "`" + ` or ` + "`" + `.agents/memory/` + "`" + `, old single-line ` + "`" + `agents doctor` + "`" + ` instructions,
  references to retired commands (handoff writing, ` + "`" + `save` + "`" + `, ` + "`" + `index` + "`" + `, and the
  memory tooling — none of which the CLI defines any more).
- **Domain rules**, to be preserved in ` + "`" + `.agents/AGENTS.md` + "`" + `: tech stack
  conventions, test mandates, safety constraints, architecture invariants, PR
  and workflow policy, commenting standards.

Write the domain rules to ` + "`" + `.agents/AGENTS.md` + "`" + `. If it already exists, append
into the matching section without duplicating what is there. Then overwrite the
root ` + "`" + `AGENTS.md` + "`" + ` with the canonical router verbatim:

` + "`" + `` + "`" + `` + "`" + `markdown
# Agent context

Durable context for this repo lives in ` + "`" + `docs/` + "`" + `. Read it before assuming;
it is the record, and this file is only the pointer to it.

- ` + "`" + `docs/qna/` + "`" + ` — answers indexed by the question you would ask again
- ` + "`" + `docs/plans/` + "`" + ` — implementation plans
- ` + "`" + `docs/journal/` + "`" + ` — dated record of what happened
- ` + "`" + `docs/design/` + "`" + ` — the design still in force

## Repository Architecture & Guidelines
- Domain engineering guidelines, commenting standards, and safety constraints
  are defined in ` + "`" + `.agents/AGENTS.md` + "`" + `.
- Repo-specific procedures and skills are located in ` + "`" + `.agents/skills/` + "`" + `.

## Machine Wiring
` + "`" + `.agents/` + "`" + ` holds machine wiring and local skills. A hook cannot install itself
and a missing hook fails silently.
- If the ` + "`" + `agents` + "`" + ` CLI is installed, run ` + "`" + `agents doctor` + "`" + ` early and report any warnings before relying on this context.
- If ` + "`" + `agents` + "`" + ` is not installed on this machine, skip machine wiring checks and adhere directly to the repository instructions above.

Recording is covered by the global instruction and the ` + "`" + `recording-what-you-learn` + "`" + `
skill; it is not repo-specific and is not restated here.
` + "`" + `` + "`" + `` + "`" + `

---

## Step 5: The two root files, before any symlink

` + "`" + `CLAUDE.md` + "`" + ` is about to become a symlink, which destroys whatever it holds.
` + "`" + `stat` + "`" + ` both paths before you read or write either one.

` + "`" + `` + "`" + `` + "`" + `bash
ls -la AGENTS.md CLAUDE.md
` + "`" + `` + "`" + `` + "`" + `

| Topology | Meaning | Action |
|---|---|---|
| ` + "`" + `AGENTS.md` + "`" + ` regular, ` + "`" + `CLAUDE.md -> AGENTS.md` + "`" + ` | current | nothing to preserve |
| ` + "`" + `AGENTS.md` + "`" + ` regular, ` + "`" + `CLAUDE.md` + "`" + ` regular | both may carry rules | reconcile **both** as Step 4 sources; a legacy repository may hold its only copy of a rule in ` + "`" + `CLAUDE.md` + "`" + ` |
| ` + "`" + `AGENTS.md -> CLAUDE.md` + "`" + `, ` + "`" + `CLAUDE.md` + "`" + ` regular | **inverted** — the pre-2026-08-19 topology | see below |

**The inverted case destroys the repository if handled blind.** Running
` + "`" + `rm -f` + "`" + ` on ` + "`" + `CLAUDE.md` + "`" + ` and then ` + "`" + `ln -s AGENTS.md CLAUDE.md` + "`" + ` deletes the only real file
and leaves ` + "`" + `AGENTS.md -> CLAUDE.md -> AGENTS.md` + "`" + `: a symlink loop, and every line
of context gone. Invert deliberately instead:

` + "`" + `` + "`" + `` + "`" + `bash
cat CLAUDE.md                 # read the real content FIRST
rm AGENTS.md                  # remove the symlink, not the file
# write reconciled content to AGENTS.md as a regular file
rm CLAUDE.md
ln -s AGENTS.md CLAUDE.md
` + "`" + `` + "`" + `` + "`" + `

Only create the symlink once the content has a destination.

---

## Step 6: Skills

**Embedded skills** (` + "`" + `skills` + "`" + ` in the report) are the only ones you touch.

| state | action |
|---|---|
| ` + "`" + `ok` + "`" + ` | nothing |
| ` + "`" + `missing` + "`" + ` | populate from the binary: ` + "`" + `agents init` + "`" + `, or ` + "`" + `agents update --all --apply` + "`" + ` for ` + "`" + `migrating-fleet-context` + "`" + ` |
| ` + "`" + `clean_legacy` + "`" + ` | replace with the current version; the digest proved there are no local edits |
| ` + "`" + `customized` + "`" + ` | three-way merge, below — except ` + "`" + `migrating-fleet-context` + "`" + `, which is ` + "`" + `agents` + "`" + `-owned: refresh it with ` + "`" + `agents update --all --apply` + "`" + ` and keep no local edits |

### Three-way merge (` + "`" + `recording-what-you-learn` + "`" + `)

Name the three inputs before you start; an unnamed merge is a guess:

- **upstream** — the version embedded in the running binary
- **base** — the canonical or legacy template the local file last matched,
  identified by the digest catalog that produced the ` + "`" + `clean_legacy` + "`" + ` state
- **local** — the working file on disk

Apply upstream's changes to local, keeping local's additions. If the digest
catalog cannot identify a **base**, there is no three-way merge to perform —
**stop and ask** rather than inventing one.

**` + "`" + `local_skills` + "`" + ` are not yours.** They are the repository's own procedures,
which is exactly what ` + "`" + `.agents/skills/` + "`" + ` is for. Do not merge, rewrite, move or
delete them.

---

## Step 7: Docs stores and retired stores

Create any store missing from ` + "`" + `docs_stores` + "`" + `, with its ` + "`" + `README.md` + "`" + `. ` + "`" + `agents init` + "`" + `
scaffolds them non-destructively.

Relocate each entry in ` + "`" + `misplaced_docs` + "`" + `:

` + "`" + `` + "`" + `` + "`" + `bash
git mv docs/journal/<file>-plan.md docs/plans/
` + "`" + `` + "`" + `` + "`" + `

Then fix relative markdown links inside the moved files.

**` + "`" + `docs/archive/` + "`" + ` is immutable and is never a source or a destination.** It
holds executed plans and retired specs, and a record edited to stay true is not
a record. ` + "`" + `agents drift` + "`" + ` does not report anything under it, and neither should
you relocate out of it.

### Retired stores

A repository predating 2026-08-19 may carry ` + "`" + `.agents/memory/` + "`" + ` and
` + "`" + `.agents/reports/` + "`" + `. Do not relocate them wholesale — much of it is
machine-generated and belongs nowhere. Triage each file:

| content | destination |
|---|---|
| a topic-indexed finding | ` + "`" + `docs/qna/` + "`" + ` |
| a design still in force | ` + "`" + `docs/design/` + "`" + ` |
| an unexecuted plan | ` + "`" + `docs/plans/` + "`" + ` |
| generated indexes, handoff scaffolding, trace pointers, stale summaries | drop |

Name everything you dropped in the Step 10 report.

---

## Step 8: Build the traceability table

Zero rule dropping is not verifiable by looking at the result. Before you ask
for approval, produce a row for every non-boilerplate block in every source file
you read:

` + "`" + `` + "`" + `` + "`" + `
source quote (verbatim)                          | classification | destination
-------------------------------------------------|----------------|---------------------
"All tests must pass before commit; use uv, not pip" | domain rule | .agents/AGENTS.md §2
"Run ` + "`" + `agents doctor` + "`" + ` early and surface what it says" | boilerplate | replaced by router
"docs/sessions/... halt and resumption plan"         | misplaced doc | docs/plans/
` + "`" + `` + "`" + `` + "`" + `

Every block gets exactly one destination. Then state the count: *N blocks in, N
blocks placed, 0 unaccounted*. That sentence is the evidence for the invariant.
Without the table the invariant is an assertion, and this is precisely the
failure deterministic tools cannot catch for you.

---

## Step 9: Verify

` + "`" + `` + "`" + `` + "`" + `bash
agents drift          # expect exit 0
agents doctor         # expect all five scaffold:* checks ok
` + "`" + `` + "`" + `` + "`" + `

Then the repository's own suite — ` + "`" + `go test ./...` + "`" + `, ` + "`" + `pytest` + "`" + `, ` + "`" + `npm test` + "`" + `,
whatever it uses. A migration that breaks the build is not done.

---

## Step 10: Stop for the human

**Do not stage anything yet.** Present:

1. The traceability table from Step 8, with the *N in, N placed, 0 unaccounted* line.
2. ` + "`" + `git status --porcelain` + "`" + ` and ` + "`" + `git diff --stat` + "`" + `.
3. Anything you dropped in Step 7, named.
4. Every stop-and-ask you resolved and how.

Then wait for an explicit approval. Silence is not approval, and neither is a
clean verification in Step 9.

## Step 11: Commit and open the pull request

Only after approval, and staging the exact paths you changed — never ` + "`" + `git add .` + "`" + `
and never a broad directory that sweeps up unrelated work:

` + "`" + `` + "`" + `` + "`" + `bash
git add AGENTS.md CLAUDE.md .agents/AGENTS.md docs/
git diff --cached --stat
git commit -m "refactor(context): migrate to two-tier agent context and 4-store layout"
git push -u origin feat/two-tier-context-migration
gh pr create --fill
` + "`" + `` + "`" + `` + "`" + `

In fleet mode, repeat from Step 2 for the next repository.

---

## Stop and ask when

- A block cannot be confidently classified as domain rule or boilerplate.
- Two destinations are both plausible for the same block.
- ` + "`" + `router_state` + "`" + ` is ` + "`" + `missing` + "`" + ` and there is no ` + "`" + `CLAUDE.md` + "`" + ` to read either.
- A ` + "`" + `customized` + "`" + ` skill has no identifiable **base**.
- The working tree is dirty, or the repository is mid-rebase or mid-merge.
- ` + "`" + `misplaced_docs` + "`" + ` names a file whose correct store is genuinely unclear.

Asking costs one message. Guessing costs a rule nobody notices is gone.

## Red flags

- About to remove ` + "`" + `CLAUDE.md` + "`" + ` without having ` + "`" + `stat` + "`" + `-ed ` + "`" + `AGENTS.md` + "`" + ` first.
- About to run ` + "`" + `agents update` + "`" + ` without ` + "`" + `--all` + "`" + `; the CLI rejects it.
- About to apply the ` + "`" + `drifted` + "`" + ` procedure to a ` + "`" + `clean_legacy` + "`" + ` router.
- About to commit before presenting the traceability table.
- About to touch a skill listed in ` + "`" + `local_skills` + "`" + `.
- About to relocate something out of ` + "`" + `docs/archive/` + "`" + `.
- Parsing ` + "`" + `agents drift --all --json` + "`" + ` as an object.

## Where this comes from

This skill is owned and maintained by the ` + "`" + `agents` + "`" + ` CLI, not by the repository it
is sitting in. ` + "`" + `agents update --all --apply` + "`" + ` overwrites it from the installed binary,
so local edits here do not survive — if this repository needs different
behaviour, that belongs in ` + "`" + `.agents/AGENTS.md` + "`" + `.

**The tool is the authority on state, not this document.** ` + "`" + `agents drift` + "`" + ` and
` + "`" + `agents doctor` + "`" + ` report what a repository actually is; where they and this skill
disagree, they are right and this copy is stale. Step 0 is how you find out.

The architecture rationale — why the router is a fixed template, why domain
rules live one level down, what each digest state proves — lives with the
` + "`" + `agents` + "`" + ` project's own design documents, upstream. It is deliberately not
restated or linked here: this file is scaffolded into repositories that have no
copy of those documents, and a pointer to a path that does not exist is worse
than no pointer at all.
`

// DigestBytes returns the hex-encoded SHA256 digest of data.
func DigestBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// DigestString returns the hex-encoded SHA256 digest of s.
func DigestString(s string) string {
	return DigestBytes([]byte(s))
}

// CanonicalRouterDigest returns the SHA256 digest of scaffold.DefaultAgentsMD.
func CanonicalRouterDigest() string {
	return DigestString(scaffold.DefaultAgentsMD)
}

// CanonicalRouterDigestFor returns the digest a repository with this layout
// must match for RouterCleanCurrent. The resolved layout selects the canonical
// bytes (design §8.1): v1 keeps DefaultAgentsMD, v2 gets the manifest-pointing
// router.
func CanonicalRouterDigestFor(l layout.Layout) string {
	if l.Schema == layout.SchemaV2 {
		return DigestString(layout.V2AgentsMD)
	}
	return DigestString(scaffold.DefaultAgentsMD)
}

var (
	legacyRouterDigestsOnce   sync.Once
	cachedLegacyRouterDigests []string
)

// LegacyRouterDigests returns the known legacy router SHA256 digests.
//
// The canonical router is checked first wherever this list is consulted, so
// carrying the current v1 router never reads as legacy in a v1 repository. For
// a v2 repository the same bytes are a known template with no repository
// content — safe to replace — rather than an unclassifiable drift.
func LegacyRouterDigests() []string {
	legacyRouterDigestsOnce.Do(func() {
		templates := []string{
			LegacySingleBulletRouter,
			LegacySingleBulletRouterTrimmed,
			LegacyPrePlansRouter,
			LegacyPrePlansRouterTrimmed,
			LegacyCaptureRouter,
			LegacyInitialClaudeMDRouter,
			scaffold.DefaultAgentsMD,
		}
		cachedLegacyRouterDigests = make([]string, 0, len(templates))
		for _, t := range templates {
			cachedLegacyRouterDigests = append(cachedLegacyRouterDigests, DigestString(t))
		}
	})
	return cachedLegacyRouterDigests
}

// IsLegacyRouterDigest returns true if digest matches any known legacy router template.
func IsLegacyRouterDigest(digest string) bool {
	for _, d := range LegacyRouterDigests() {
		if d == digest {
			return true
		}
	}
	return false
}

// CanonicalSkillDigestFor returns the SHA256 digest of the canonical skill
// text for the resolved layout: the frozen v1 text for a v1 repository, the
// v2 text for a v2 one. The resolved layout selects the bytes (design §0.8).
func CanonicalSkillDigestFor(skillName string, l layout.Layout) (string, error) {
	assetPath, err := scaffold.SkillAssetPath(l.Schema, skillName)
	if err != nil {
		return "", err
	}
	content, err := scaffold.AssetsFS.ReadFile(assetPath)
	if err != nil {
		return "", err
	}
	return DigestBytes(content), nil
}

// LegacySkillDigests returns legacy SHA256 digests for a given skill.
func LegacySkillDigests(skillName string) []string {
	if skillName == "recording-what-you-learn" {
		return []string{DigestString(LegacyRecordingSkill)}
	}
	return nil
}

// IsLegacySkillDigest returns true if digest matches a legacy version of the skill.
func IsLegacySkillDigest(skillName, digest string) bool {
	for _, d := range LegacySkillDigests(skillName) {
		if d == digest {
			return true
		}
	}
	return false
}
