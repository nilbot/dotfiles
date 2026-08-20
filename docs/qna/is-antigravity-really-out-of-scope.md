# Is Antigravity out of scope for hooks?

## Answer first

**No, and it has not been since `agy` 1.1.1.** A workspace-local
`<workspace>/.agents/hooks.json` loads once the folder is trusted. Measured on
1.1.16, 2026-08-20:

```
17:39:19  [pid 1]    loaded 0 named hooks from 0 hooks.json file(s)   # startup, untrusted
17:39:22  [pid 326]  loaded 1 named hooks from 1 hooks.json file(s)   # after trusting
```

Spec 1 says Antigravity is out of scope. That section is a re-test note, not a
verdict; this is the re-test.

## Context

Spec 1 measured `agy` **1.1.0** on 2026-08-07 and recorded, correctly, that it
did not read `<workspace>/.agents/hooks.json` — `loaded 0 named hooks from 0
hooks.json file(s)` with the workspace trusted. From that one probe the design
concluded "Antigravity (`agy`) and Gemini CLI support — out of scope."

The binary's own changelog:

> **1.1.1** — "Fixed workspace-local hooks defined in
> `<workspace>/.agents/hooks.json` not loading after trusting a folder by
> reloading hooks whenever workspaces change."

The bug was real, upstream-known, and fixed in the next patch. Spec 1 wrote its
own re-test trigger — "any `agy` upgrade" — and nothing watched it. Fifteen
releases later the decision was still being cited as settled.

## What the probe actually cost, and what it nearly concluded

Two methodological traps, both nearly fatal to the answer:

**`-p "/hooks"` is not an instrument.** 1.1.12 added print-mode answers for
read-only slash commands "without starting an agent turn, spending quota or
leaving a conversation behind", which makes it a tempting free probe. It printed
nothing, every time, trusted and untrusted alike. But its own log lines show
`loaded 0 named hooks from 0 hooks.json file(s)` on every print-mode run:
**print mode does not load workspace hooks at all.** An empty answer from it is
an artifact of the probe. `/help` printed a full list from the same workspace,
so print-mode slash commands do work — the control proved the mechanism and
still could not license the conclusion.

**The first config was written in the wrong dialect.** Antigravity keys
`hooks.json` on a *hook name* at top level; Claude Code's shape is
`{"hooks": {...}}`. Writing the Claude shape loads nothing. This also re-reads
the original 1.1.0 evidence: `loaded 0 named hooks from **0 hooks.json
file(s)**` — the second number says the file was never found, so that was the
loading bug rather than a format error.

The thing that settled it was neither command output nor reasoning. It was
`hooks_manager.go:53` counting what it loaded, with the workspace named on an
adjacent line.

## What Antigravity actually offers

`.agents/` is its **customization root** — rules, skills, hooks and plugins all
live there, which is why a repository set up for Antigravity looks like one set
up for this tool. Rules load from `GEMINI.md`, `AGENTS.md` and
`.agents/rules/*.md`; skills auto-load from `.agents/skills/<name>/SKILL.md`.

Hook events are `PreToolUse`, `PostToolUse`, `PreInvocation`, `PostInvocation`
and `Stop`. **There is no `SessionStart`, `SubagentStart` or `SubagentStop`.**
That is the load-bearing gap: the transcript cache exists because Claude Code
deletes subagent transcripts mid-session, and Antigravity offers no event to
hang it on. Antigravity natively supports the instruction half of the design and
structurally cannot support the part kept mechanical. `Stop` hooks are also
newer than they look — before 1.1.10 they sat "unreachable behind the built-ins"
and did not run at all.

One further caveat on scope: a Google engineer states publicly that Antigravity
2.0 and the CLI "have the same harness", and that the older IDE is "close". That
is a claim, not a measurement — everything above was measured on the CLI, and
whether it transfers to the app is untested.

Related: [how-do-i-confirm-something-is-not-wired](how-do-i-confirm-something-is-not-wired.md)
and [can-this-check-actually-fail](can-this-check-actually-fail.md) — this is
both of them at once, plus a third failure they do not cover: a measurement that
was true when taken and expired without anyone noticing.
