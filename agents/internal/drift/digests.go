package drift

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

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

// LegacyRouterDigests returns the known legacy router SHA256 digests.
func LegacyRouterDigests() []string {
	templates := []string{
		LegacySingleBulletRouter,
		LegacySingleBulletRouterTrimmed,
		LegacyPrePlansRouter,
		LegacyPrePlansRouterTrimmed,
		LegacyCaptureRouter,
		LegacyInitialClaudeMDRouter,
	}
	digests := make([]string, 0, len(templates))
	for _, t := range templates {
		digests = append(digests, DigestString(t))
	}
	return digests
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

// CanonicalSkillDigest returns the SHA256 digest of an embedded skill from scaffold.AssetsFS.
func CanonicalSkillDigest(skillName string) (string, error) {
	assetPath := fmt.Sprintf("assets/skills/%s/SKILL.md", skillName)
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
