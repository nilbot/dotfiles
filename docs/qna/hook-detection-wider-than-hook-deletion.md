# Why is hook detection wider than hook deletion?

## Context

`TestInitDoesNotPointAtTheRetiredTrackedTracePath` called `runInit` with no
`t.Chdir`, so it wired **this** repository with the ephemeral `agents.test`
binary path. The damage compounded because `stripOurs` only deletes commands
whose basename is exactly `agents`: the real hook was stripped, the test one
added, and nothing could ever remove it. Four accumulated per `go test` run,
erroring at session start — while `agents doctor` reported the wiring exact.

## Answer

The conservative deletion rule is right and stays. The defect was that
**detection inherited deletion's narrowness**. They are now split:

- `ResemblesHookCommand` — wide, and report-only.
- `ParseHookCommand` — narrow, and the only thing deletion consults.

**Widening what you report is free; widening what you delete is not.** A
detector that misses a malformed entry leaves a puzzle. A deleter that matches
too much removes someone else's hook. Those failure modes are not symmetric, so
the two questions must not share a predicate.

`TestMain` now chdirs out of the checkout before any test runs, which is what
stops a test wiring the developer's own repository. That cost three call sites
that resolved repository paths from the working directory — `task18RepoRoot` and
two `go build` invocations — which now use `packageDir`, captured before the
chdir. Worth knowing before adding a test here that reads a tracked fixture:
resolving from cwd will not work.

## Follow-up, 2026-09-21

**The answer holds; the entry's own example has expired.**

`ResemblesHookCommand` and `ParseHookCommand` both still exist, still differ in
width, and the asymmetry between reporting and deleting is still the point.
`doctor` now leans on it a second way: the same predicate that decides what
`wire` may strip also separates an entry this tool owns from one that merely
looks like ours.

What has gone is `TestInitDoesNotPointAtTheRetiredTrackedTracePath`. The tracked
trace path in its name was deleted on 2026-09-21 with the trace store, and the
test went with its subject — the rule from
[where else does this command name live](where-else-does-this-command-name-live.md),
applied to a test rather than a CI step. The defect it documented is not
reopened: `TestMain`'s chdir and `packageDir` are both still there, and so is
the reason for them.
