# Spec 4 (candidate) — the wiring DSL

**Status:** deferred, not rejected. Gated on trigger conditions below.
**Depends on:** [spec 1](2026-08-07-agents-repo-context-design.md) §5 (harness adapters).

> **Contingent requirement 2026-08-12 from
> [spec 7](2026-08-12-spec-7-capture-and-review.md).** *Only if spec 7 §3c is ever
> built.* That section's `Stop` gate blocks — it exits non-zero to ask the model
> for a draft — and a sibling option adds context without blocking; neither
> behaviour is known to be supported by every harness. The DSL would then need a
> per-hook capability ("this hook may block"), not just a command string, plus
> degradation on a harness that fails the positive control.
>
> Spec 7 ships its capture instruction in `CLAUDE.md` instead, which is scaffold
> rather than wiring, so **nothing in the wiring surface changes today.** Treat
> this as a requirement that may never arrive.

## What it is

A declarative source of truth for hook wiring — one tracked file describing *which
lifecycle moments this tool wants to act on*, compiled per harness into
`.claude/settings.json`, `<repo>/.codex/hooks.json`, and whatever comes next.

Spec 1 deliberately ships the compile step **without** the language: wiring lives in
Go, and `agents wire` emits both targets. That is the YAGNI reading, and it was the
right call for two targets whose schemas are ~90% identical.

## Build it when any of these becomes true

1. **A third target lands** — a new harness, or `agy` gaining workspace-local hook
   support (spec 1 records a re-test trigger for that).
2. **Per-repo wiring divergence appears** — a repo that needs hooks the others must
   not have. Today every repo wants identical wiring, which is why Go structs suffice.
3. **Capability requirements need to be declarative** — e.g. *"this hook requires the
   subagent-transcript-pointer capability; skip it on harnesses that lack it."* At
   that point wiring stops being configuration and becomes a small language with a
   type system, and hand-rolled conditionals in Go start losing.

## The reason to build it that is not cost-justified today

Worth writing down rather than losing, because it will not show up in any
cost/benefit table.

A DSL forces the **semantic event** to be named separately from each vendor's
encoding of it. "A subagent finished" is not `SubagentStop`, and it is not
`PostToolUse` matching `invoke_subagent` — those are two spellings of one idea, and
today that equivalence exists only implicitly, scattered across adapter code.

Writing the abstraction down turns the differences between vendors into **data** you
can compare, rather than implementation detail you re-derive each time. That
comparison is where insight about the vendors themselves would come from — which
contracts are genuinely richer, which are the same idea with different names, where
one vendor's model of an agent's lifecycle is actually different rather than merely
differently spelled.

That is a research payoff, not an engineering one. It is a legitimate reason to
build this even while the immediate output is two nearly identical config files —
but it should be undertaken **knowingly, for that reason**, and not smuggled in as
premature abstraction justified on engineering grounds it cannot currently support.

## Constraints if it happens

- Declarative schema in an existing format (TOML — precedent in Codex's own
  `config.toml`, and Go parses it for free). **Not** a hand-written grammar. If it
  needs its own lexer, the design went wrong.
- Must model **output disposition**, not just format: where each target's file
  lives, whether it is tracked or ignored, and whether it must be merged into a
  file holding unrelated settings (Claude Code) or owned outright (Codex).
- Must not swallow *content*. Skills, subagent definitions, and rules are
  markdown-with-frontmatter and already near-common across harnesses; they get
  **placed** (spec 1 symlinks them), never generated. The DSL covers wiring only.
