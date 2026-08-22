# Does the Antigravity app share the CLI's hook machinery?

## Context

Everything in
[is Antigravity really out of scope](is-antigravity-really-out-of-scope.md) was
measured against `agy`, the CLI. The operator uses **the app**, not the CLI and
not the IDE. A Google engineer's public claim that Antigravity 2.0 and the CLI
"have the same harness" was recorded there explicitly as *a claim, not a
measurement*.

Wiring the app on the strength of CLI measurements would be the same error the
linked entry documents: an accurate measurement of one thing, cited as settled
about another.

## Answer

**Yes, for the part that matters.** Measured 2026-08-22 against the app's own
core binary, with `agy` at 1.1.18 for the CLI side:

```
/Applications/Antigravity.app/Contents/Resources/bin/language_server   145 MB

423 matches on  PreInvocation | PostInvocation | hooks_go_proto
"failed to parse hooks.json: %w"
"failed to read hooks.json: %w"
"The `hooks.json` file is a JSON object where each top-level key is a hook..."
"Hooks are configured in a single `hooks.json` file placed in your customization"
"pattern matching is not supported for hooks"
```

The app carries the same hook proto types, the same five events, the same
top-level-key-is-a-hook-name schema, and the same embedded documentation as
`agy`. The parity claim is now a property of the shipped binary rather than a
statement by an engineer.

Two things this does **not** establish, both cheap to check and neither checked:

**Trust.** `~/.gemini/antigravity/` has **no `settings.json` at all** — no
`trustedWorkspaces` key, nothing. The CLI's trust gate was the thing that had to
be cleared before workspace-local hooks loaded (`0 named hooks from 0 files`
untrusted → `1 from 1` after trusting). Whatever the app uses instead is
unknown, and it is the gate, so it decides whether any of this fires.

**Subagent events.** The matrix cell is still blank. The app has subagents
(`invoke_subagent`, `SubagentManager`, `KillSubagent`), and its tooling says
*"logs and artifacts are preserved"* when a subagent tree is killed — while a
`Failed to prune trajectory` path also exists. Whether it destroys trajectories
mid-session, the way Claude Code does, is
[the measurement that decides how much of our machinery applies](which-harnesses-actually-lose-transcripts.md).

Method note, since the linked entry is largely about bad probes: this one is
string extraction from a binary. It proves the code is *present and documented*,
not that it *runs*. It is strong enough to stop treating parity as an open
question and to start writing the adapter; it is not strong enough to skip the
live trust-and-fire probe before claiming the app is wired.

One more thing the app and CLI share: `~/.gemini/config/`. `agy` moved its
`/hooks` output there "ensuring hooks remain synchronized between the TUI and
the backend", `~/.gemini/antigravity/mcp_config.json` symlinks into it, and it
holds the project registry naming workspaces by `gitFolder` URI. It does **not**
hold `trustedWorkspaces` — that key exists only in the CLI's own settings — so
finding the shared directory does not answer the trust question above.
