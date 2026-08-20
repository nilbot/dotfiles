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
