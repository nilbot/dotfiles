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

## Follow-up, 2026-09-21

**This entry now contains one of the checks it warns about.**

```bash
H=$(mktemp -d); HOME=$H go test -count=1 ./...
test ! -e "$H/.local/state/agents"
```

`$H/.local/state/agents` was the machine-local registry — the fleet registry's
store, under `HOME`. It was deleted on 2026-09-21 with the fleet registry and the
rest of the capture apparatus, nothing writes that path any more, and so
`test ! -e` cannot fail. The command still exits 0, and it now exits 0 for a
reason that has nothing to do with containment: the path is absent because the
code that would have created it is gone.

Measured with `HOME` pointed at a fresh temporary directory, `go test
./internal/... -count=1` leaves Go build caches under `$H/Library/` and nothing
else. Containment is therefore not broken — it is held for free, because there is
no machine-local store left to leak. That is exactly the case where the rule from
[where else does this command name live](where-else-does-this-command-name-live.md)
applies: delete the check with its subject rather than repairing it. A
replacement that asserted *"nothing outside `$H/Library` was written"* would be
measuring a stronger property than the suite now has any way to violate.

The third lesson above is untouched and still the more general one: `HOME=$(mktemp
-d) go test ./...` exiting 0 still measures portability, and portability was
never containment. The portability half of the command still works.
