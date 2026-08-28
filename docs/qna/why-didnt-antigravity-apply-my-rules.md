# Why didn't Antigravity apply my rules?

## Context

2026-08-28, reviewing a plan to add an Antigravity harness adapter. The plan
proposed an `agents doctor` advisory warning that a repository's
`.agents/AGENTS.md` is *directory-scoped* — read only inside `.agents/` — and
therefore not applied to work at the repository root. `autogo-mlx` keeps its
domain rules in exactly that file, so the advisory would have fired on it.

The claim traced to the `agy` binary's own embedded documentation:

> **Directory-Based Rules (`GEMINI.md` / `AGENTS.md`)**: Placed directly in any
> directory. The system walks up from the current working directory to the
> repository root and loads these files. They apply to the directory they reside
> in and all its subdirectories.

Walking *up* from the root never descends into `.agents/`, so the reasoning
looked sound. It was wrong, and the probe that caught it also caught a second
thing nobody was looking for.

## Answer

**Two independent causes, both measured on `agy` 1.1.22.**

### 1. Print mode loads no workspace rules

`agy -p` answers normally and applies nothing. Same fixture, same prompt, two
modes:

```
agy -p "Say hello in one short sentence."
  -> Hello! How can I help you today?              # neither marker

agy   (interactive, same directory, same prompt)
  -> Hello there! (QUARTZ-LANTERN-5183 BASALT-FERRET-7726)
```

`grep QUARTZ-LANTERN-5183` against the print-mode log returns **0** — the rule
text never reached the request. The turn itself ran: the log carries
`Creating new cascade trajectory`.

This is the same trap already recorded for hooks — *"print mode does not load
workspace hooks at all"*
([the scope re-test](is-antigravity-really-out-of-scope.md)) — now shown to
extend to rules. **Print mode is not an instrument for anything workspace-local.**
It is more dangerous here than for hooks, because a hooks probe returns silence
while a rules probe returns a fluent, plausible answer with nothing loaded.

One red herring worth naming, because it cost time. The print-mode log carries
**51** `not logged into Antigravity` errors and `Auth mode is unspecified`. They
come from `Cache(userInfo)`, `Cache(loadCodeAssistResponse)` and
`fetchBAICAdminControls` — entitlement lookups, not the turn. The turn ran and
answered anyway. Auth was not the cause; execution mode was.

### 2. `.agents/AGENTS.md` does load, repository-wide

Run interactively from the repository root, **both** markers applied. The
embedded doc quoted above is true and describes a *different* mechanism:
`.agents/` is Antigravity's **customization root**, and content there is loaded
as customization rather than found by the directory walk. The rule about walking
up governs `AGENTS.md` files sitting in arbitrary directories; it does not
govern this one.

So a repository that keeps its rules in `.agents/AGENTS.md` is fine on
Antigravity, and the proposed advisory was dropped. `autogo-mlx`'s domain rules
were live the whole time.

## How to probe this again

The fixture is three tokens and takes a minute:

| file | token |
|---|---|
| `<root>/AGENTS.md` | `QUARTZ-LANTERN-5183` |
| `.agents/AGENTS.md` | `BASALT-FERRET-7726` |
| *(none)* | `VELVET-COMPASS-3094` |

Each rules file says *"You MUST include the exact string `<token>` somewhere in
every reply."* Ask for anything trivial and read which tokens come back.

Two design choices carried the result. Rules that **instruct** rather than
tokens to find, so the probe tests rule *application* and cannot be satisfied by
the agent grepping the files. And the root marker as a **positive control** —
without it, "the nested file didn't load" is indistinguishable from "nothing
loaded," which is exactly the state print mode produces. The first run had both
markers absent, and only the control made that readable as a void run rather
than a negative result. The third token, present in no file, checks for
confabulation.

## What this does not settle

Whether **Claude Code** or **Codex** read `.agents/AGENTS.md` is unmeasured.
Claude Code reads `CLAUDE.md`; `CLAUDE.md` appears nowhere in the `agy` 1.1.22
binary. If the other two do not read it, rules placed there reach one harness of
three — a parity gap pointing the opposite way from the Claude-first assumption
[the scaffold still carries](../design/2026-08-07-agents-repo-context-design.md):
`CLAUDE.md` is the real file and `AGENTS.md` the symlink to it. The same fixture
answers it — run it under each harness.
