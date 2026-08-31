---
name: agents-tool
description: Use when working in a repository that has a tracked .agents/ directory - reading a past session back from a transcript, checking or repairing harness wiring, or bounding the trace cache. For recording what you learn, use recording-what-you-learn instead; this tool no longer owns that.
---

# The `agents` tool

`agents` keeps durable context in the repository instead of in a harness
directory that does not travel. This skill is about **when** to reach for each
command. For **what** each one does, ask the binary: `agents help` lists the
commands a person invokes, and `agents help <command>` opens any one of them at
any depth, down to `agents trace cache prune`. Prefer that over guessing at a
flag — the help text is rendered from the same declarations dispatch uses, so
it cannot describe a command the binary does not have.

Every command answers with the same six codes: `0` ok, `1` advisory (it
finished, read the output), `2` block (the only code that stops work), `3`
malformed input, `4` not applicable here, `5` could not complete. A non-zero
code is not automatically a failure — `1` is how a command makes you read
what it printed.

## Start of a session

Run `agents doctor` early and report any warnings before relying on repository
context. A hook cannot install itself and a missing hook fails silently, so an
empty or stale `.agents/` means the setup is broken rather than that there is
nothing to say. Report that rather than working around it.

Knowledge lives in the repository's documentation, not in `.agents/`. Read
`docs/qna/` and `docs/design/` before assuming, or whatever the repository's own
`CLAUDE.md` names. `.agents/` is machine wiring: hooks, the trace cache, and
`.agents/skills/` for procedures specific to that repository.

If the repository has no `.agents/` at all, `agents init` scaffolds it and
registers it in the machine's fleet. It exits `1`, not `0` — the trust steps it
prints are still outstanding, and reporting a working setup that is not yet
working is the failure this code exists to prevent.

## Recording is not this tool's job any more

Use the `recording-what-you-learn` skill. Findings go to `docs/qna/`, work
records to `docs/journal/`, written directly and committed — no queue, no
promotion step.

The commands that used to do this — a handoff family, a draft queue and a
promotion step — were removed on 2026-08-20. If you meet them in an older
document, that document predates the change. The design that retired them, with
the evidence, is `docs/design/2026-08-19-knowledge-is-documentation.md` in the
dotfiles repository; the short version is that the queue solved a problem the
measurements do not support, and the store it fed stayed starved while a plain
`docs/` directory in another repository filled up on its own.

## When a question needs evidence rather than recall

`agents trace` is the family that answers from the record rather than from
memory — the pointer index, the transcripts behind it, and the cache that keeps
them reachable.

`agents trace ls` finds the sessions that touched a lane or a module.
`agents trace show` reads one transcript back — the harness's own copy if it
still exists, otherwise the cached one. Reach for these when the answer is
*what actually happened* and grep over the current tree cannot say, because the
tree no longer contains the attempt that failed.

`agents trace cache` copies reachable transcripts into the machine-local store
so they survive the harness pruning its own. It is the difference between a
pointer and a record.

**Cache everything reachable; never select by age.** Claude Code prunes subagent
transcripts *during* the session that produced them, and the losses are not
age-ordered in either direction — measured both ways, in two sessions that
disagreed about everything else. Any policy shaped as "cache things once they get
old" salvages the wrong set, and a `--since` window silently spans a period in
which most of the content is already gone. Reachability is the only signal that
means anything. See `docs/qna/why-are-subagent-transcripts-gone.md` in the
dotfiles repository for the census.

`pointer_verified: true` says the path existed **when the pointer was written**.
It is not a claim about now, and the gap between the two can be minutes.

The cache lives in the git **common** directory, so every worktree shares one and
it outlives any of them — and it is unstageable structurally, because git does
not track its own directory, so no ignore rule has to be remembered.

`agents trace cache prune --lane <name>` is dry-run unless `--yes` and removes
copies only, never records. Never infer that a lane is prunable from its branch
or worktree being gone: a deleted branch is usually a merged one, and a throwaway
worktree is often where the interesting work happened.

## Committing

`agents save` commits `.agents/` paths and nothing else. It was an escape hatch
around promotion; promotion is gone, so for the rare commit that touches only
harness wiring it is now simply the direct way.

Knowledge is not committed this way — it is documentation, so it goes in an
ordinary commit alongside everything else.

Keep repository content and agent context in separate commits. The guard warns,
without blocking, when one commit touches both, and names `agents save` in the
message.

## Wiring, across this repository and the fleet

`agents wire` regenerates the harness configs for the repository you are in. It
merges and never overwrites, so a hand-edited config keeps its edits.

`agents ls` lists the repositories registered on this machine. Reach for it
when a question is about the fleet rather than about the checkout you are in —
whether a repository was ever initialized, or whether a path you were given is
one the tool already knows. Its `--prune` form drops entries whose directories
are gone, which is worth doing before trusting the list.

`agents update` rewires every registered repository on the machine. It is a dry
run unless told otherwise, and the dry run is the point: read what it intends
to change before letting it change anything. This is the command to reach for
after the `agents` binary itself has been rebuilt or moved, when the wiring in
other checkouts still points at where it used to be.

## Checking version and provenance

`agents version` prints the binary version, git commit SHA, and build timestamp. Reach for it when diagnosing environment differences or verifying which build of the tool is active.

## Inspecting context and drift

`agents drift` inspects the repository or fleet for context layout drift, canonical router diffs, domain context, skills, and misplaced documentation. It is non-mutating.
