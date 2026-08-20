# Claude Code hook payloads — captured 2026-08-07

Real `stdin` payloads from Claude Code **2.1.224** on macOS, captured by wiring a
dump script to `<repo>/.claude/settings.json` in a throwaway git repo
(`/tmp/ccprobe`) and running one headless session that dispatched a single
`Explore` subagent:

```bash
claude -p "Use the Task tool to dispatch exactly one Explore subagent. Its task:
report how many lines are in README.md in this directory. Then tell me the number."
```

No permission-bypass or trust-bypass flag was used. Hooks fired on the first run.

These are the test fixtures referenced by
[spec 1 §11](../../2026-08-07-agents-repo-context-design.md#11-testing). They exist
so adapter tests assert against what Claude Code actually sends, not against a
hand-written guess at the schema. Until this capture, the Claude Code payload
shape in `claudecode_test.go` was reconstructed from the spec's record of prior
art; these files replace that reconstruction with a measurement.

## Files

| File | Event |
|---|---|
| `cc-session-start.json` | `SessionStart`, `source: startup` |
| `cc-subagent-start.json` | `SubagentStart` |
| `cc-subagent-stop.json` | `SubagentStop` |
| `cc-stop.json` | `Stop` |

## What the capture settled

Three things the plan had assumed, two of which were wrong:

1. **`SubagentStart` exists.** A payload arrived for it, so `claudeCode.Events()`
   keeps the event.
2. **There is no `turn_id`.** Claude Code names the per-turn identifier
   `prompt_id`; `turn_id` is Codex's spelling and appears in no Claude Code
   payload. The reconstruction had asserted `turn_id`. `harness.Payload` now
   decodes both and `Build` falls back to `prompt_id`.
3. **`SubagentStart` carries no `agent_transcript_path`.** Only `SubagentStop`
   does. At subagent-start time the sole path present is the *parent session's*
   transcript, so the pointer for that event cannot verify against `agent_id`.
   `pointer_verified: false` on a `subagent_start` record is therefore expected
   and correct, not a defect.

`description` is **not** in either subagent payload. It lives only in the
spawn-time sidecar `agent-<id>.meta.json` beside the transcript, whose measured
shape is `{"agentType","description","toolUseId","spawnDepth"}`. The adapter's
sidecar read stands.

## What was redacted, and why it matters

`last_assistant_message` is replaced with `"<REDACTED — see spec 3.2>"`, per the
same rule applied to the Codex fixtures. `tool_input` and `tool_response` do not
appear here because no `PreToolUse` hook was wired in this capture — the rule was
applied to all three regardless.

The Codex capture found **encrypted task blobs** (`gAAAAAB…`) in
`tool_input.message`. Nothing of that kind appears in these four payloads; every
retained field is pointer or label data: ids, paths, event names, effort level,
permission mode, cwd, and two empty arrays (`background_tasks`, `session_crons`).
Each file was read in full before being committed.

## These files are test input, not documentation

`agents/internal/harness/claudecode_test.go` **reads these four files directly**
at `../../../docs/design/fixtures/2026-08-07-claude-code-hook-payloads/`.
They are not a pasted-in copy of something asserted elsewhere: edit a value here
and the suite fails.

That is deliberate, and it is what makes the next paragraph true. Verified by
mutation: changing `prompt_id` and `agent_id` in `cc-subagent-stop.json` fails
`TestClaudeCodeBuildsVerifiedSubagentTrace` and
`TestClaudeCodeFixturesDecodeToMeasuredShape/SubagentStop`; deleting any file
fails whichever test loads it; replacing a redaction marker with real text fails
`TestClaudeCodeFixturesStayRedacted`.

Two things the fixtures deliberately do **not** cover, because they cannot:

- The redaction canary. These files are redacted, so no forbidden content is left
  in them to leak. `TestClaudeCodeForbiddenContentCannotReachTrace` keeps a small
  inline payload carrying `SECRET-LEAK` and a `gAAAAAB…` blob for that assertion.
- `agent_transcript_path` in `TestClaudeCodeBuildsVerifiedSubagentTrace`. The
  captured path names a machine-bound file that does not exist on another
  machine, so that one field is overwritten *after* decoding, with a temp path.
  Every other field in that test comes from the committed bytes.

## Reproducing

Re-capture on any Claude Code minor-version bump and overwrite these files; the
tests above are what will notice if the contract moved. If you do re-capture,
re-apply the redaction before committing — `TestClaudeCodeFixturesStayRedacted`
enforces it, but it is easier to redact than to rewrite history.
