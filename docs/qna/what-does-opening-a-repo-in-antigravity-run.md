# What does opening a repository in the Antigravity app actually run?

## Context

The CLI gates workspace hooks behind trust: `agy` logs
`loaded 0 named hooks from 0 hooks.json file(s)` until the folder is trusted, and
`trustedWorkspaces` in `~/.gemini/antigravity-cli/settings.json` is where that
lives. That key appears **zero** times in the desktop app's `language_server`, so
the app's gate — if it has one — is something else.

Measured 2026-08-22 by opening a brand-new workspace in the app while an
independent observer snapshotted `~/.gemini` from another window. The workspace
was a fresh `git init` containing a README and a `.agents/hooks.json` that
appended every event and payload to a log.

## Answer

**There is no gate. Opening the folder is the trust act, and it runs an agent
turn — executing the workspace's hooks — before any prompt is typed.**

```
18:30:43  ~/.gemini/config/projects/<uuid>.json created   (folder opened)
18:31:27  PreInvocation   ┐
18:31:30  PostInvocation  ├─ a complete turn, no user input
18:31:30  Stop            ┘
18:33:48  PreInvocation      ← the first actual user message, 2m21s later
```

No trust prompt appeared. Nothing resembling a trust record was written:
`~/.gemini/antigravity/antigravity_state.pbtxt` was byte-identical across the
whole run, and the only file created under `config/` was the project manifest:

```json
{
  "id": "2f31c8bc-e3c0-4371-8e43-a1a8e08004a7",
  "name": "antigravity-trust-probe",
  "projectResources": { "resources": [ { "gitFolder": {
      "folderUri": "file:///Users/nilbot/antigravity-trust-probe",
      "defaultBranch": "main" } } ] },
  "settings": {},
  "updatedAt": "2026-08-22T17:30:43.242164Z",
  "isWorkspaceOnly": false
}
```

`settings` is empty — no permission grants, no trust flag. The manifest records
*that* the workspace exists, not that it is trusted. Registration and execution
are the same event.

Opening also initializes a full session on disk: a conversation database, a brain
directory, and both `transcript.jsonl` and `transcript_full.jsonl`, all before
the first message. Payloads confirm the app's own data root —
`~/.gemini/antigravity/brain/<id>/…`, not `antigravity-cli/`.

## What follows

**A `.agents/hooks.json` in a repository is executable content that runs on
open.** Hook commands run through `sh -c`. Cloning a repository and opening it in
the app executes whatever it contains, with no prompt and no user action beyond
opening the folder. That is a materially different threat model from Claude
Code's project-trust prompt and Codex's hash-based hook review, and it is the
reason [spec 4](../design/2026-08-07-spec-4-wiring-dsl.md) §5.4's `tracked`
question resolved the way it did.

**`.agents/hooks.json` is now git-ignored here**, alongside `.claude/` and
`.codex/`. Those two are ignored because a generated config carries this
machine's absolute binary path. This one has that reason *and* a second: this
repository is public, and a tracked hook config would be an armed trap for anyone
who clones and opens it. The ignore rule landed before any adapter exists, so
there was never a window in which one could be committed.

## Caveats

**The probe was designed around a gate that does not exist.** Its instructions
said to open the workspace and stop at "whatever trust or permission prompt
appears" — presuming the app mirrored the CLI, on no evidence. The operator
reports plainly that the app shows no trust or permission prompt of any kind.
That is a direct observation of their own screen rather than a fact read off
disk, but the disk agrees: hooks fired, and nothing resembling a trust record
was written anywhere under `~/.gemini`.

The flawed premise also cost the experiment's phase separation. Waiting for a
prompt that never came, the operator sent a message, so the "before trust"
snapshot was taken after the first user turn. Its A→B / B→C attribution is
therefore unreliable and was corrected by re-diffing the raw snapshots. The
timeline above comes from the hook log's own timestamps, which are independent
of the snapshots and unaffected.

This is the same error [the re-test](is-antigravity-really-out-of-scope.md)
records twice already, in a third costume: reasoning from one product to another
and building the instrument around the conclusion. The instrument still worked,
because it logged timestamps rather than only comparing phases — a probe that
had relied on the phase boundaries alone would have produced a confident wrong
answer.

This says nothing about the CLI, which
[does gate on trust](is-antigravity-really-out-of-scope.md) and was measured
separately. Two products, one harness, different gates.
