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
spec 1 §11, in a design since deleted. They exist
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
| `cc-pretooluse-parent.json` | `PreToolUse`, parent — **captured 2026-08-22, 2.1.237** |
| `cc-pretooluse-subagent.json` | `PreToolUse`, inside a subagent — **captured 2026-08-22, 2.1.237** |

**Two captures live here, on two versions.** The four `Stop`/`Subagent*`/
`SessionStart` files are the original 2026-08-07 capture on **2.1.224**. The two
`PreToolUse` files were captured on **2.1.237** on 2026-08-22 to answer a
question the first capture left open, using the same instrument with that one
event added. The directory keeps its original date in its name because renaming
it would break the path the test suite hardcodes; read each file's own stamp
rather than the directory's.

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

`last_assistant_message` is replaced with `"<REDACTED — see spec 1 §3.2>"`, per the
same rule applied to the Codex fixtures. In the 2026-08-07 capture `tool_input`
and `tool_response` did not appear at all, because no `PreToolUse` hook was
wired — the rule was applied to all three regardless.

**The 2026-08-22 capture is where that mattered.** `PreToolUse` carries
`tool_input` for real, and the first draft of these two files was committed with
it raw: a `wc -l` command, a probe path, and the parent's full dispatch prompt.
Innocuous here, and exactly the judgment call the rule exists to remove — the
Codex capture found encrypted blobs inside `tool_input.message`. Both files now
carry the marker, so the key is present for shape assertions and the value is
gone.

The Codex capture found **encrypted task blobs** (`gAAAAAB…`) in
`tool_input.message`. Nothing of that kind appears in these four payloads; every
retained field is pointer or label data: ids, paths, event names, effort level,
permission mode, cwd, and two empty arrays (`background_tasks`, `session_crons`).
Each file was read in full before being committed.

## These files are captured input, and nothing reads them

`agents/internal/harness/claudecode_test.go` read these files directly and asserted
their decoded shape. It was deleted on 2026-09-21 (`f3b4a74`) with the trace machinery
it tested. The harness package now holds `claudecode.go` for wiring, and two tests —
`strip_test.go` and `wire_safety_test.go` — that do not touch this directory.

One document still uses these files as evidence:
[Does `PreToolUse` fire inside a Claude Code subagent?](../../../qna/does-pretooluse-fire-inside-a-subagent.md)
cites `cc-pretooluse-parent.json` and `cc-pretooluse-subagent.json` for its answer,
and that citation is what keeps the directory.

## Reproducing

Re-capture on any Claude Code minor-version bump and overwrite these files. Nothing
will notice if the contract moved — the tests that would have are gone — so compare
the new payloads against the adapter's expectations by hand.

If you do re-capture, re-apply the redaction before committing. No check enforces it
now. The rule is spec 1 §3.2, and these files carry the marker
`"<REDACTED — see spec 1 §3.2>"` so a reader can tell redacted from captured.
