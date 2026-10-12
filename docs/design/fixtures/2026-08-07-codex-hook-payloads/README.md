# Codex hook payloads — captured 2026-08-07

Real `stdin` payloads from Codex CLI **0.147.0** on macOS, captured by wiring a
dump script to `<repo>/.codex/hooks.json` in a throwaway git repo and running one
`codex exec` session that delegated to two subagents.

These were the test fixtures for spec 1 §11, a design since deleted. Nothing reads
them now: `agents/internal/harness/codex_test.go`, which asserted their shape against
what Codex actually sends rather than a hand-written guess at the schema, was deleted
on 2026-09-21 (`f3b4a74`).

## Files

| File | Event |
|---|---|
| `codex-session-start.json` | `SessionStart`, `source: startup` |
| `codex-pre-tool*.json` | `PreToolUse` ×6 |
| `codex-subagent-start*.json` | `SubagentStart` ×2 (two independent subagents) |
| `codex-subagent-stop*.json` | `SubagentStop` ×2 |
| `codex-stop.json` | `Stop` |

## What was redacted, and why it matters

`tool_input`, `tool_response`, and `last_assistant_message` are replaced with
`"<REDACTED — see spec 1 §3.2>"`.

This is not routine hygiene. The `PreToolUse` payloads for `spawn_agent` carried
**encrypted task blobs** (`gAAAAAB…`) in `tool_input.message`. That is a live
instance of exactly what spec 1 §3.2 forbids recording — found in the very first
capture, before any of this was implemented. While the tests existed, this directory
validated the structural redaction rule in the record type rather than merely
motivating it; nothing validates it now.

Everything retained is pointer or label data: ids, paths, event names, model,
permission mode, cwd.

## Reproducing

The capture procedure is described in spec 1's measured facts, in a design since
deleted. Re-capture on any Codex minor-version bump. Nothing will notice if the
contract moved: the golden tests that would have are gone.
