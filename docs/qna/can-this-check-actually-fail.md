# How do I know a verification command in this repo can actually fail?

## Context

Two commands written as verification could not fail, and running them was the
only way that became apparent.

- `AGENTS_DOTFILES_ROOT=/nonexistent agents doctor` is a no-op. The installed
  binary is stamped at build time and **the stamp beats the environment**, so
  output is byte-identical with and without the variable.
- `agents index && git diff --exit-code` means "the index is stale" in CI, but
  locally it reports every unrelated working-tree edit. Scope it to
  `-- .agents/` or it fails for reasons that have nothing to do with the index.

A third came from the test suite itself. `HOME=$(mktemp -d) go test -count=1
./...` exits 0, which was read as proof the suite was self-contained. It is not:
the suite still **writes** into whatever `HOME` it is handed. Exit 0 measured
portability; nobody had measured containment.

## Answer

**Run it against the state it is supposed to catch.** Reasoning about a check
tells you what it should detect; running it tells you what it does. Both of the
commands above survived review because they looked like verification.

For the containment case the check that works is not the run's exit code but
what the run left behind:

```bash
H=$(mktemp -d); HOME=$H go test -count=1 ./...
test ! -e "$H/.local/state/agents"
```

Portability and containment are different properties and the obvious command
only tests the first. When a check and its subject can both be satisfied by
something other than the thing you care about, the check is decorative — break
the production line it names, watch it go red, then restore.
