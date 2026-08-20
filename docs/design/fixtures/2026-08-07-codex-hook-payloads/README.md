# Codex hook payloads — captured 2026-08-07

Real `stdin` payloads from Codex CLI **0.147.0** on macOS, captured by wiring a
dump script to `<repo>/.codex/hooks.json` in a throwaway git repo and running one
`codex exec` session that delegated to two subagents.

These are the test fixtures referenced by
[spec 1 §11](../../2026-08-07-agents-repo-context-design.md#11-testing). They exist
so adapter tests assert against what Codex actually sends, not against a
hand-written guess at the schema.

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
`"<REDACTED — see spec 3.2>"`.

This is not routine hygiene. The `PreToolUse` payloads for `spawn_agent` carried
**encrypted task blobs** (`gAAAAAB…`) in `tool_input.message`. That is a live
instance of exactly what spec 1 §3.2 forbids recording — found in the very first
capture, before any of this was implemented. The structural redaction rule in the
record type is validated by this directory, not merely motivated by it.

Everything retained is pointer or label data: ids, paths, event names, model,
permission mode, cwd.

## Reproducing

The capture procedure is described in
[spec 1 § Measured facts](../../2026-08-07-agents-repo-context-design.md#measured-facts-2026-08-07).
Re-capture on any Codex minor-version bump; the adapter's golden tests are the
thing that will notice if the contract moved.
