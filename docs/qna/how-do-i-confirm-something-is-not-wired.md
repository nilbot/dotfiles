# How do I confirm something is *not* wired?

## Context

A session concluded that `agents guard --staged` was not wired into the
pre-commit path and said so in a handoff. It was wrong. `main.go:51-58` runs it
on every pre-commit and maps `Advisory` to `OK` so a warning does not abort the
commit, and two tests pin both halves.

The reasoning that produced the wrong answer looked thorough. `main.go` was read
to line 40 — stopping one line before the call — and the conclusion was then
"confirmed" by checking the hooks extras directory, `.git/hooks`, and the harness
configs. Three places, all empty, all agreeing.

## Answer

**Searching for more absences is not verification.** Every one of those three
places could only agree with the hypothesis; none could refute it. Accumulating
confirmations of "I did not find it here" is not evidence of "it is not there" —
it is the same non-observation repeated.

**Find the read that could disconfirm, and do that one.** For "is X called?", the
disconfirming read is the call site, and it is usually one file. Here it was one
file, twenty lines further into a file already open.

Two traps this repository sets specifically:

- `githook.builtin` returns 0 for pre-commit, because the wiring lives in
  `main.go` rather than in `internal/githook`. Reading the package alone shows
  the opposite of the truth. Anyone auditing reachability here has to read
  `runGitHook` to its end.
- An absence is cheap to observe and expensive to establish. Before writing
  "nothing does X", name the single place that would prove you wrong, and say in
  the note that you looked there.

Related: [can-this-check-actually-fail](can-this-check-actually-fail.md), which
is the same failure one level up — a check that cannot fail is a confirmation
you cannot learn from.
