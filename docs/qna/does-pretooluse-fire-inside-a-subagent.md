# Does `PreToolUse` fire inside a Claude Code subagent?

## Answer first

**Yes — for every tool call the child makes, and the payload says which child.**
Measured 2026-08-22 on Claude Code **2.1.237**.

A child's `PreToolUse` payload carries `agent_id` and `agent_type`. A parent's
does not carry either. That presence/absence is a per-invocation discriminator: a
hook can tell whether it is running inside a subagent from the payload alone,
with no state kept across invocations.

```
SessionStart
PreToolUse     Agent     <- parent dispatches; no agent_id
SubagentStart
PreToolUse     Bash      <- agent_id a7e4e3cfd606588ee, agent_type Explore
PostToolUse    Bash
PreToolUse     Read      <- agent_id a7e4e3cfd606588ee, agent_type Explore
PostToolUse    Read
SubagentStop
PostToolUse    Agent
Stop
```

**Coverage was complete, not partial.** The child's own transcript records
exactly two tool calls; both appear in the hook log, matched by `tool_use_id`
(`toolu_01Qe5Vp…` Bash, `toolu_01M6rzin…` Read). Two of two, no misses, no
extras.

**`session_id` and `transcript_path` stay the *parent's*** even inside the child.
This extends the per-event asymmetry spec 1 measured at `SubagentStart` /
`SubagentStop` to a third event: the child is identified by `agent_id`, never by
the session fields.

Payload keys, exactly as captured — the child's set is the parent's plus two:

```
parent: cwd, effort, hook_event_name, permission_mode, prompt_id, session_id,
        tool_input, tool_name, tool_use_id, transcript_path
child:  ... the same ten, plus agent_id, agent_type
```

Fixtures: [`cc-pretooluse-parent.json`](../design/fixtures/2026-08-07-claude-code-hook-payloads/cc-pretooluse-parent.json)
and [`cc-pretooluse-subagent.json`](../design/fixtures/2026-08-07-claude-code-hook-payloads/cc-pretooluse-subagent.json).

## Context

The [2026-08-19 redesign](../design/2026-08-19-knowledge-is-documentation.md)
named this question and said what rides on it:

> `PreToolUse` was not among them, so whether it fires inside a child is unknown
> — and that single fact decides whether the read trigger can be mechanised where
> instructions demonstrably fail.

It fails there measurably: 0 of 31 observed subagents acted on an inherited
bootstrap directive. Subagents *see* instructions and ignore them, so an
instruction-delivered read trigger cannot reach the place this repository does
most of its work. The redesign asked for the measurement "before building
anything" and then nothing ran it for three days, while five separate
measurements were taken of a different vendor's subagent hooks.

## Method

Spec 1's instrument, with the one event it omitted. Its fixture README is
explicit about the gap — "no `PreToolUse` hook was wired in this capture" — so
this is the same probe with `PreToolUse`/`PostToolUse` added, not a new method.

A throwaway git repo, a dump script wired to `<repo>/.claude/settings.json` for
six events, one headless `claude -p` dispatching one `Explore` subagent. No
permission- or trust-bypass flag; hooks fired on the first run.

Three things made a negative result readable as a negative:

1. **The instrument was proved to write before the run.** A hand-fed payload
   produced a log line, so an empty log afterwards would have meant a broken
   script, not an absent event. See [can this check actually fail](can-this-check-actually-fail.md).
2. **The parent's own `PreToolUse` is the positive control.** It fired on
   `Agent`. Had the child's calls been missing, the wiring would still be
   demonstrably live, so the absence would have been the finding rather than an
   artifact.
3. **The child got a uniquely named target** — `XYLOPHONE-QUASAR-7731.md`,
   a string appearing nowhere else — so a child tool call is identifiable by
   content, independently of any id field. The child's transcript then supplied
   ground truth for completeness.

## What this does and does not decide

**Decides:** the *moment* is reachable. A hook can run inside a subagent, at the
point a tool call is about to happen, knowing it is in a subagent and which one.

**Does not decide: whether anything can be said at that moment.** The read
trigger needs to put retrieved text in front of the model, and firing is not
injecting. [Spec 4](../design/2026-08-07-spec-4-wiring-dsl.md) §5.2 records
`result.inject-context` for Claude Code at `session.begin` only. Whether a
`PreToolUse` hook's output can add context is untested here and is the next
question — a mechanised read trigger needs both halves, and this measurement
bought one.

**Also unmeasured:** depth. One subagent at depth 1, of one agent type
(`Explore`). Whether a subagent's own subagent (`spawnDepth` > 1) is covered, and
whether `agent_id` then names the nearest parent or the root, is untested.

## The contrast worth keeping

The same question, asked of two vendors the same day, came back with mirror-image
answers:

| | fires in the child? | can a hook tell whose child? |
|---|---|---|
| Claude Code | yes, parent's hook, every call | **yes** — `agent_id` + `agent_type` |
| Antigravity | yes, the child's *own* hooks | **no** — `parent_conversation_id` exists in the runtime and appears in no payload |

Both harnesses reach the moment. Only one of them lets a hook say what it is
looking at. See [how does Antigravity expose subagents](how-does-antigravity-expose-subagents.md);
that missing field is one line of vendor JSON away from closing, which is why
it is worth reporting upstream rather than working around.

## Correction this produced

Spec 4 §5.2 listed `payload.parent-id` as "not needed — events are named" for
Claude Code. That is true at `subagent.begin`/`subagent.end`, where the event
name carries the meaning, and false at `tool.before`, where the *only* thing
distinguishing a child's invocation from its parent's is `agent_id` being
present. A capability the table dismissed as unnecessary turns out to be the
thing the whole read trigger rests on.
