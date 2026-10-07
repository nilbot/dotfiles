# How do you keep a copy-paste template from drifting away from the thing it copies?

## Context

`template/ci/verify.yml` is the generic GitHub Actions verification gate that
other repositories are meant to copy. On 2026-10-06 it was found describing a
gate this repository had stopped running. Its README taught classic branch
protection after the repository moved to Rulesets; it named the required check
`Verification Gate` where the ruleset names `gate`; its Go example recommended
`cache: true`, the pattern the implementation had already measured and rejected;
and its `quality` job echoed a message and exited 0.

Nothing had failed. The template was created on 2026-08-28 and last changed in
`ef4cdb7` (2026-08-29); the reference workflow, `.github/workflows/verify.yml`,
changed through `f274bc8` (2026-09-24), 26 days later. No code, test or workflow
read `template/ci/` — a grep found only the directory's own files and two design
documents. The plan that named the single adopter (`toolshed/cowork`) was never
executed: that repository has no `.github/workflows/verify.yml` today.

The pins were the first thing to diverge, and they diverged fastest. `ef4cdb7`
bumped `actions/checkout` from v4.4.0 to v7.0.1 and `actions/setup-go` from
v5.6.0 to v7.0.0 the day after the template was written. Nothing required the
README's Go example, which pins the same two actions, to move with them; it was
corrected by hand in the same commit, which is the only reason it was not wrong
immediately.

## Answer

**A copy-paste artifact has no consumer, so nothing fails when it goes stale.**
That is the entire mechanism. A README nobody reads and a template nobody copies
are checked by no test, no build and no run; the only signal is a reader who
happens to compare the two. "Nobody depends on it" is the finding, not a reason
the file can wait.

The repair is to give the comparable parts a consumer.
`bootstrap.d/ci_template_test.go` reads all three files — the reference
workflow, the template workflow, and the template README's YAML examples — and
fails when:

- any `uses:` is not a 40-character commit SHA;
- the same action is pinned to different commits in two of the files (this is
  the check that would have caught the `ef4cdb7` pins);
- the `gate` job carries a `name:`, which moves the required check's context off
  the `gate` a ruleset names;
- `gate`'s `needs` list does not name every job in the workflow, or `gate` lacks
  `if: always()`;
- a `paths:` filter appears;
- the template's `quality` job never exits non-zero.

Each check was mutated and watched to go red before it was kept, because a guard
that has only ever been green is not known to be a guard. The mutation to try
first is the one you expect to be caught by a *different* check: replacing the
first `exit 1` in a file tests the job that happens to come first, not the one
you were aiming at.

**What no test can compare needs a stated rule instead.** The template carries
four jobs and the reference carries eight, and comparing them job for job would
be wrong. So a change to a *generic* mechanism — an action pin, the secret scan,
the cache scheme, the job set `gate` aggregates — lands in `template/ci/` in the
same pull request, and a change specific to this repository does not.
`template/ci/README.md` states that rule where the next editor will meet it.

The `quality` placeholder is the second lesson and the older one: a step that
prints a message and exits 0 makes the gate green on a repository where nothing
was verified, which is [a check that cannot
fail](tests-that-pass-no-matter-what.md). The template shipped exactly that from
its first commit. A placeholder an adopter has to make fail is worth more than
one that passes by default.

Related: [can this check actually fail](can-this-check-actually-fail.md).
