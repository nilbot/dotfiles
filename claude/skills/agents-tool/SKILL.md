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

**Retiring, do not reach for these.** The `agents handoff` family and
`agents review` were the old path: `agents handoff draft` and
`agents handoff write` queued and committed notes into `.agents/`;
promotion wrote them into the tree; `agents handoff prune` bounded them. They still
exist in the binary and are being removed. The design that retires them, with
the evidence, is `docs/design/2026-08-19-knowledge-is-documentation.md` in the
dotfiles repository — the short version is that the queue solved a problem the
measurements do not support, and the store it fed was starved while a plain
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

## Recording and committing

`agents index` regenerates the generated indexes. The pre-commit guard runs it
too, so a stale index blocks a commit rather than landing.

`agents save` is an escape hatch for committing `.agents/` paths alone — the normal
path is promotion, which commits on its own.

Keep repository content and agent context in separate commits. The guard warns
when one commit touches both.

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
