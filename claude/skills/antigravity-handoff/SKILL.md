---
name: antigravity-handoff
description: Pick up context from a Gemini Antigravity session to continue its work. Triggers on: hit quota/limit mid-flight, continue Antigravity work, resume from Antigravity, pick up the context, a supplied brain directory, ~/.gemini/antigravity/brain/, transcript_full.jsonl, an Antigravity handoff, "finish what the other agent started".
---

# Antigravity Handoff — Resume Work From a Gemini Antigravity Session

Antigravity runs a session in a **brain directory** and does the actual repo work in a **worktree**. Your job on handoff: recover its understanding cheaply, verify it against ground truth, and continue.

**Governing principle: the repo and the brain artifacts ARE Antigravity's externalized memory. Read the distillations, don't replay the episode — then cross-check them against code and git, because the agent drifts (it leaves work uncommitted, introduces broken links, and its prose goes stale).** Mismatches between prose, code, and git are the highest-value findings, not the agreements.

## Check the repo first — the handoff may already be archived

Antigravity's own `session-handoff` skill archives the brain's `walkthrough.md` / `implementation_plan.md` / `task.md` into the **repo** at `docs/sessions/YYYYMMDD/HHMMSS-title.md` at session end (its `record` step; `start` replays the newest one). So the distilled handoff often already lives in the worktree. **Read the newest `docs/sessions/**` file before parsing the raw brain transcript** — it's the curated version of exactly what you're reconstructing. Fall back to the brain directory / `transcript_full.jsonl` when `sessions/` is missing, stale, or the session was cut off *before* `record` ran (a mid-flight quota cutoff is common — so always still check `git status` for uncommitted work).

## The brain directory (what's inside)

`~/.gemini/antigravity/brain/<uuid>/`
- `.system_generated/logs/transcript_full.jsonl` — full reasoning trace, **one step per line**. Large (100KB+). **Never read linearly — grep/sed by step_index.**
- Root artifacts: `implementation_plan.md`, `task.md`, `walkthrough.md` — intent, current-state, and how-to deliverables.
- `scratch/` — throwaway analysis scripts. `evaluation_games/`, domain outputs (`*_analysis.md`).
- The brain references its repo via `workspaceUris` → a worktree under `~/.gemini/antigravity/worktrees/`.

### transcript_full.jsonl schema (per line)
`{step_index, source, type, status, created_at, content, thinking, tool_calls}`
- `source`: `USER_EXPLICIT` | `MODEL` | `SYSTEM`
- `type`: `USER_INPUT` (content wrapped in `<USER_REQUEST>…`), `PLANNER_RESPONSE` (has `thinking` + `tool_calls`), `VIEW_FILE`/`RUN_COMMAND`/`GREP_SEARCH`/`CODE_ACTION` (tool results), `CHECKPOINT`, `ERROR_MESSAGE`.
- **`CHECKPOINT` steps are gold** — they summarize truncated context: outstanding user requests, a session summary, the artifact list, and spawned subagents.
- **Finding the cut-off**: the tail often ends in repeated `ERROR_MESSAGE` (`error_code:429`) with empty `MODEL` steps. The last real intent is the last `USER_INPUT`; the last landed change is the last `CODE_ACTION`. Work often lands but is **never committed** — always check `git status`.

Useful probes (don't dump the whole file):
- `wc -l transcript_full.jsonl`; `grep -n '"type":"CHECKPOINT"'`; `grep -n '"type":"USER_INPUT"'` to map the spine.
- `grep -n '<step_index>' …` then `sed -n 'A,Bp'` to read a window around a point of interest.

## Read strategy — cheap spine first, drill on demand

| Tier | Read | How |
|---|---|---|
| **0 — spine (do unconditionally)** | **newest `docs/sessions/**` file** (archived handoff, above); brain `task.md`, `implementation_plan.md`; repo `docs/**/master_*history*.md` (pivot log); `git log --oneline -30`; `git status` | Read fully — cheap, recovers ~80% of orientation |
| **1 — procedural** | the project's own agent skills (`.agents/skills/`, `.claude/skills/`) | Skim |
| **2 — rationale (on demand)** | `docs/qna/*.md` or similar design docs | Index by title; read only the 2–3 touching the current task |
| **3 — ground truth (verify, don't absorb)** | the exact functions the docs/skills point at | Read the referenced function, not the whole file |
| **4 — episodic fallback** | `transcript_full.jsonl` | **grep only**, for a specific question Tiers 0–3 couldn't answer |

## Verification discipline (this is what beats Antigravity)

For anything load-bearing, triangulate three independent sources: **prose (docs) vs. code (functions) vs. timeline (git log + disk state)**. Antigravity accumulated context but couldn't cheaply self-check. When they disagree, the disagreement is the finding — surface it before continuing.

## Stopping rule (keeps context lean)

Stop reading once you can answer without guessing:
1. What are we ultimately building? 2. What state/phase are we in *now*? 3. What's the immediate next action? 4. What's been tried **and rejected** (don't repeat it)? 5. How could the next step break?

Pull context against a live question; never hoard it in advance. If a question survives Tiers 0–3, *then* grep the transcript.
