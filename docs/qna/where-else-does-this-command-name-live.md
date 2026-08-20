# I deleted a command — where else does its name still live?

## Context

2026-08-20, retiring the capture apparatus. `agents index` was deleted from the
binary. Before committing I grepped for the retired names and read every hit.
None was a live caller: the `docs/` hits were historical, and the Go hits were a
comment about a past measurement plus `guard_test.go` fixtures that now pin the
opposite property — a repository still holding a legacy `INDEX.md` must not be
blocked by it. `scaffold_test.go` already had `TestClaudeMDNamesNoRetiredCommand`
asserting the scaffolded template names none of `agents handoff`, `agents
review`, `agents index`. Both suites passed. The guard passed.

CI went red anyway. `.github/workflows/verify.yml` had a whole `hygiene` step
named "agents index is a pure function of tracked content", which built the
binary and ran the command. The binary answered `unknown command "index"` and
exited 3, so the step failed on malformed input rather than on anything it was
written to measure.

The grep was not shallow — it was scoped. `--include='*.go' --include='*.md'`
covers where a Go developer expects command names to appear, and CI is neither.

## Answer

**A command name is an interface, and CI is one of its callers.** `go test ./...`
cannot see it: the workflow invokes the built binary by name in a shell, so a
deleted command is a runtime error in a job, not a compile error in a package.
Nothing local fails. The full set of callers in this repository:

```bash
git grep -n 'agents <name>' -- .github/ Makefile git/ claude/ docs/ '*.go' '*.sh'
```

`claude/` matters as much as `.github/` — the skills there are symlinked into
`~/.claude/` and load in every session in every repository, so a stale command
name in a skill is a wrong instruction with a much longer half-life than a stale
CI step, which at least announces itself in red within ten minutes.

**Delete the check with its subject; do not repair it.** The step had nothing
left to hold pure once both `INDEX.md` generators were gone. The same rule the
tests followed. What is worth keeping from a retired check is its *reasoning* —
this one existed because "CI must not depend on the reviewer's machine being
provisioned", which outlives the command — so that argument stayed in spec 5
under a retirement note, and only the mechanism was cut.

**The failure mode to watch for is a scoped grep that feels exhaustive.** Every
hit was read and correctly judged; the hits that mattered were never returned.
When the question is "does this name still appear anywhere", scope the grep by
*path* (the whole tree, minus `docs/archive/`) rather than by file type.

Related: [how-do-i-confirm-something-is-not-wired](how-do-i-confirm-something-is-not-wired.md)
— establishing an absence needs the one read that could disconfirm it, and here
that read was the workflow file.
