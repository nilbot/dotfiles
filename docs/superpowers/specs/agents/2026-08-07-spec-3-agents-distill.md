# Spec 3 — `agents distill`

**Status:** scope only — not designed, not implemented
**Depends on:** [spec 1](2026-08-07-agents-repo-context-design.md) for the pointer
format (§3.1) and the memory `sources:` schema (§3.4). Both were specified in spec 1
*specifically* so this could be built later without unrepairable data loss.

## Why it exists

Spec 1 §1 defines three tiers, and the third — machine-bound harness material — is
never authoritative and never tracked. It is *pointed at*. Spec 1 delivers two of
the three pointer operations (Check via `agents doctor`, Materialize via
`agents trace cache`). This spec delivers the third: **Distil** — turning
machine-bound raw material into curated, tracked, portable knowledge.

This is the half that makes the pointer story worth having. Without it, a repo can
tell you what it cannot reach but never do anything about it.

## The governing intent

Stated by the user during spec 1's design, and worth preserving verbatim in
substance: if work depends on machine-bound details, the agent should at least know
what is missing and prompt for a trip back to the owning machine to extract and
distil it — so the repo stays the place that focuses on the work, and past
experience gets raised into knowledge that is shareable across machines, agent
vendors, and model quirks.

## Scope

**Per-harness readers.**
- Codex: `~/.codex/memories/` — a git repository containing a large `MEMORY.md`,
  `raw_memories.md`, `memory_summary.md`, and `rollout_summaries/`. Populated
  automatically when `[memories] generate_memories = true`.
- Claude Code: the auto-memory directory, default
  `~/.claude/projects/<sanitized-cwd>/memory/`, relocatable via `autoMemoryDirectory`.

**A transcript reader** that mines the files the trace index points at.

> **Known hazard, inherited from prior art:** never dedupe by first match. Collapsing
> near-identical commands hides the case you most need — a first attempt that failed
> and a later corrected retry. Reconstruct the full chronological sequence *with
> results*, classify each attempt success/failure by scanning the result text, and
> only then decide which version is canonical.

**A draft-and-review flow** producing `.agents/memory/` entries with correct
`sources:` frontmatter, so the resulting entry records where it came from and stays
honest about what it summarizes.

**Runs on the machine that owns the material.** That is the entire point. `agents
doctor` on another machine reports the gap; this closes it, from the right place.

## Design constraints carried from spec 1

- `.agents/memory/` stays **curated**. This flow drafts *for review*; it never
  writes unreviewed model output straight into the tracked tree. Spec 1 rejected
  redirecting harness auto-memory into `.agents/memory/` for exactly this reason.
- A memory entry must never depend on a source being present to be correct
  (spec 1 §3.4). Distillation must produce entries that stand alone.
- The secret guard (spec 1 §7) applies to whatever this writes. Harness auto-memory
  is unreviewed model output and is the highest-risk input in the whole design.

## Open questions

- How much of the drafting is mechanical extraction vs. model summarization, and
  where is the review gate?
- Codex's `~/.codex/memories/` is itself a git repo. Does that history help
  (provenance, ordering) or is only the current state useful?
- Is there a useful `--since <last-distill>` watermark, and where is it stored?
