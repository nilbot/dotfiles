---
name: session-handoff
description: >-
  Enforces a structured, agent-and-human readable session handoff strategy. Automatically catalogs transient brain artifacts (walkthroughs, implementation plans, and tasks) into chronological session files in the project workspace.
---

# Session Handoff & Chronological Logging

## Overview
This skill provides a structured method for documenting agent and human session history inside project workspaces. By archiving Antigravity's transient brain files (`walkthrough.md`, `implementation_plan.md`, `task.md`) into your permanent project repository, it guarantees that no context, architecture logs, or verification results are lost across conversation turns or session resets.

## Dependencies
None. This skill is fully self-contained and operates entirely on the local filesystem using standard Python libraries.

## Quick Start
To initialize or catch up on the project status at the beginning of a session:
```bash
uv run python /Users/nilbot/.gemini/config/skills/session-handoff/scripts/handoff_manager.py start --workspace .
```

To archive completed work at the end of a session:
```bash
uv run python /Users/nilbot/.gemini/config/skills/session-handoff/scripts/handoff_manager.py record \
  --workspace . \
  --conv-id <current-conversation-id> \
  --title <short-hyphenated-title> \
  --prompt "<original-user-request>"
```

## Utility Scripts
The Python CLI helper script `handoff_manager.py` manages the scanning and file copying:

### Subcommands

#### 1. `start`
Scans `docs/sessions/` for the latest chronological file and prints its content, allowing the agent to instantly resume context.
- **Example**:
  ```bash
  uv run python /Users/nilbot/.gemini/config/skills/session-handoff/scripts/handoff_manager.py start --workspace /path/to/project
  ```

#### 2. `record`
Finds the active brain folder, extracts and compiles the generated walkthrough (and situational plan/task files) and writes them to a new, unique file: `docs/sessions/YYYYMMDD/{timestamp}-{title}.md`.
- **Link sanitation (automatic)**: brain artifacts routinely link with absolute `file:///…` URLs that break once archived and would resolve outside the repo. On `record`, the enforced invariant is **no link may resolve outside repo root** (so a fresh clone never hits a dead link): in-repo targets become clone-safe relative links; any out-of-repo file is **copied into the note's `assets/`** and linked to that copy; anything that can't be brought in (missing, a directory, or with `--no-archive-assets`) is demoted to plain text. In-repo `../` links (a note pointing up into `src/` or `.agents/`) are fine and expected; the only thing forbidden is a link that resolves *outside* repo root.
- **Example**:
  ```bash
  uv run python /Users/nilbot/.gemini/config/skills/session-handoff/scripts/handoff_manager.py record \
    --workspace /path/to/project \
    --conv-id 105dba66-76e2-4a0f-8846-d79ed396111a \
    --title implement-database-pooling \
    --prompt "Implement connection pooling for PostgreSQL using pg_pool."
  ```

## Workflow (For AI Agents)
As an Antigravity agent, you MUST follow these instructions automatically once this skill is loaded.

### Phase 1: Session Initiation (MANDATORY)
1. At the very first turn of a session, scan the active workspace for existing handoffs by running:
   ```bash
   uv run python /Users/nilbot/.gemini/config/skills/session-handoff/scripts/handoff_manager.py start --workspace <path-to-active-workspace>
   ```
2. Read the latest session log printed in stdout. Integrate its roadmap, architectural decisions, and current state into your immediate planning before writing any code.

### Phase 2: Execution
1. Follow your standard planning and execution loop.
2. Ensure you create or update `walkthrough.md` (and situational `implementation_plan.md`/`task.md`) in the current conversation's brain folder.

### Phase 3: Session Handoff & Recording (MANDATORY)
1. Before concluding your final turn, running walkthrough tests, or completing a task, run the `record` subcommand:
   ```bash
   uv run python /Users/nilbot/.gemini/config/skills/session-handoff/scripts/handoff_manager.py record \
     --workspace <path-to-active-workspace> \
     --conv-id <current-conversation-id> \
     --title <short-hyphenated-title> \
     --prompt "<original-user-request>"
   ```
2. Present a link to the generated file to the user so they can review their persistent project history.

## Common Mistakes
1. **Executing `record` before running/verifying the task**: If you run `record` before completing the task and writing a `walkthrough.md`, the script will fail loudly. Always verify your changes and generate `walkthrough.md` first.
2. **Hardcoding Conversation ID**: Never assume or hardcode a conversation ID. Always read the active conversation ID from the current session metadata or system prompt.
3. **Specifying an incorrect workspace path**: Make sure the workspace path provided is the absolute path to the active project repository, not the transient brain folder.
