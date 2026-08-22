# How does Antigravity expose subagents to hooks?

## Context

Claude Code and Codex both notify the **parent**: `SubagentStart` and
`SubagentStop` fire in the parent's context, and `agent_transcript_path` hands
the parent the child's transcript. That shape is what
`agents/internal/harness` models, and spec 1 measured its per-event asymmetries
on both harnesses.

Antigravity has subagents (`invoke_subagent`) but no event of either name, so
spec 4 carried a hypothesis: `PostToolUse` matched on `invoke_subagent` is
probably `subagent.end` under a different spelling. Measured 2026-08-22 on a
live `agy` session dispatching one subagent.

## Answer

**The hypothesis was wrong, and it was wrong in an instructive direction.
Antigravity inverts the model: the child runs its own hooks.**

All five events — `PreToolUse`, `PostToolUse`, `PreInvocation`,
`PostInvocation`, `Stop` — fire **inside the subagent**, carrying the child's own
`conversationId` and its own `transcriptPath` under
`~/.gemini/antigravity-cli/brain/<child-id>/.system_generated/logs/transcript_full.jsonl`.
The parent is told nothing: `PostToolUse` on `invoke_subagent` does fire when the
call returns, but its payload carries only the *parent's* ids. The child's
identity and transcript are absent from it.

That is **more** coverage than the named-event model gives, not less. Every
moment inside a child is observable.

**But the child cannot tell that it is a child.** The five payload key sets are
structurally identical between parent and child; only the `conversationId` value
differs, and nothing marks which one is root:

```
PreInvocation   artifactDirectoryPath conversationId initialNumSteps
                invocationNum modelName transcriptPath workspacePaths
Stop            artifactDirectoryPath conversationId error executionNum
                fullyIdle modelName terminationReason transcriptPath workspacePaths
```

`parent_conversation_id` and `parentConversationId` both exist in the runtime —
including a label `antigravity.google/parent_conversation_id` — and **appear in
no hook payload**. So `subagent.end` and `turn.end` are the same event with no
field separating them.

## What follows

**A third reason a capability cell can be empty.**
[Spec 4](../design/2026-08-07-spec-4-wiring-dsl.md) already distinguished *the
harness lacks the capability* from *the need is undemonstrated*. This adds
*the event fires but the payload cannot disambiguate which moment it is* — and
the three call for different responses. Only the first is a vendor gap worth
reporting; the second is not a gap at all; the third is a gap in the payload,
not the event model, and could be closed by one field.

**It answers an open question in the 2026-08-19 redesign, on one harness.** That
document left this hanging:

> Whether a write-time read hook can cover subagents is unmeasured … that single
> fact decides whether the read trigger can be mechanised where instructions
> demonstrably fail.

It was measured against Claude Code, where 0 of 31 subagents acted on an
inherited directive. On Antigravity the answer is **yes**: `PreInvocation` and
`PreToolUse` both fire inside children, and `PreInvocation` can return
`injectSteps` with an `ephemeralMessage`. The harness this repository excluded
for fifteen releases is the only one where the redesign's unmechanisable trigger
is mechanisable. Nothing is built on that yet, and it should not be built without
its own falsifier — but the question is now answerable rather than academic.

**It does not change the transcript cache.** Antigravity
[does not prune](which-harnesses-actually-lose-transcripts.md), so there is
nothing to rescue regardless of which events exist.

## Method caveat

Measured on the **CLI** with `--dangerously-skip-permissions`, not on the app and
not without a bypass flag. Hook *firing* semantics are unlikely to differ between
products that share a harness, but this was not shown. The same run's claim about
how the app grants trust is separately unsupported — see
[spec 4 §9](../design/2026-08-07-spec-4-wiring-dsl.md), which records why.
