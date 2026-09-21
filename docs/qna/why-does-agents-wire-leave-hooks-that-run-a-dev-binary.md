# Why does `agents wire` leave hook commands that run a dev binary?

## Context

Measured on 2026-09-20, after this session's verification work left dev builds in
`/tmp` under names like `agents-new` and `agents-old`. Wiring the repository gave
three warnings, one per harness:

```text
warn  wiring:claude-code   24 hook command(s) look generated but run a different binary, e.g. /tmp/agents-new hook session-start --harness claude-code
```

`agents wire` had run several times, each time from a differently named build.
Every run added its own commands and removed none, so the configs accumulated
36 entries pointing at `/tmp` binaries that no longer existed (or, worse, at the
`v0.5.99` control build). Every harness event then ran all of them: the correct
command plus each stale one.

## Answer

**`agents wire` recognises its own commands by the binary's name.** `isOwnedBinary`
accepts exactly three basenames:

```go
return base == "agents" || base == "agents-test-bin" || base == "agents-standalone"
```

A renamed dev build — `agents-new`, `agents-old`, `agents.test` from a `go test`
run — is therefore *shaped like ours but owned by nobody*. `stripOurs` leaves it,
and the refusal is deliberate, in the code's own words
(`agents/internal/doctor/doctor.go`):

> An entry shaped like ours but run from some other binary is owned by nobody:
> `agents wire` will not replace it, because replacing it would mean deleting a
> command we cannot prove is ours, and the harness runs it anyway — so it fails
> at every session start while this check says the wiring is exact.
> **Report it; never delete.**

Two tests hold that line. `TestWireLeavesAResemblingForeignHookAlone` fails with
*"wire deleted a hook it could not prove was ours; reporting is allowed, deleting
is not"*, and `TestCheckWiringReportsHookCommandsThatLookOursButRunAnotherBinary`
pins the report this entry is about. What *is* supported is a binary that moves
without being renamed: `TestClaudeCodeWireReplacesAMovedBinary` covers
`/old/path/agents` → `/new/path/agents`.

So this is a boundary, not a bug. The cost of widening it — deleting a hook
another tool owns — is higher than the cost of reporting a stale one, and a
report is something a human can act on.

## What to do

Delete the named commands by hand. `agents doctor` prints each offending binary
with its count and the file to edit:

```text
warn  wiring:codex   24 hook command(s) look generated but run a different binary: /tmp/agents-new (12), /tmp/agents-old (12)
      -> `agents wire` never deletes a command it cannot prove it wrote, so delete these from <root>/.codex/hooks.json by hand
```

The three files are machine-local and git-excluded. Two shapes:

| file | shape |
|---|---|
| `.claude/settings.json`, `.codex/hooks.json` | `hooks.<Event>[].hooks[].command` |
| `.agents/hooks.json` | `agents.<Event>[].command` (flat) |

Removing an entry that leaves a group empty means removing the group too, or the
harness keeps an empty matcher.

## How to avoid it

Wire with the binary you actually run. `agents wire` records
`os.Executable()`, so a dev build wires the repository to a path that will not
exist tomorrow. Run it from the installed `agents`, or from a build whose
basename is one of the three owned names — and delete the dev binaries when the
verification is done. Thirty-eight of them had accumulated in `/tmp` before this
was noticed.

**Since 2026-09-20** the remedy counts per binary instead of showing a single
example command: one example reads the same whether the entries came from one
renamed build or from several unrelated tools, and the deletion is per-file
either way.

## Follow-up, 2026-09-21

**The boundary is unchanged, and it was re-measured rather than assumed.**

A `.claude/settings.json` holding
`/tmp/agents-new hook stop --harness claude-code` was written into a throwaway
repository and put through the current binary:

```text
$ agents wire
wired claude-code -> /private/tmp/wire-probe/.claude/settings.json
$ agents doctor
warn  wiring:claude-code   1 entry(ies) in the generated shape but under a binary
                           this tool does not own: /tmp/agents-new hook stop --harness claude-code
```

The entry survived the strip and `doctor` reported it. `isOwnedBinary` still
accepts exactly the same three basenames, so a renamed dev build is still shaped
like ours and owned by nobody, and the "What to do" remedy is still the only one.

Two things in the Context have gone. The tests it names — the one that fails with
*"wire deleted a hook it could not prove was ours"* and the one that pins the
report — were both removed on 2026-09-21; the probe above is what re-checks the
property they held. And the accumulation they describe cannot happen again:
`wire` no longer writes hook entries at all, it only strips them, so no run adds
a command that a later run has to remove. The remedy is unchanged, and it is now
the whole of it: nothing stale will be replaced for you.
