# Does `fish_add_path` need an existence guard?

## Context

Adding the bend CLI to `fish/mypre.fish` on 2026-09-22 under fish 4.9.3
(Homebrew). The block first landed with two tests in front of the call:

```fish
if test -d $HOME/.bend && test -e $HOME/.bend/bin/bend
    fish_add_path $HOME/.bend/bin
end
```

The question worth answering: which half of that guard does anything, given
that `fish_add_path` is documented to skip paths that are not there?

## Answer

The two halves are not equal. The shipped function carries its own existence
check, ahead of all dedup and scope logic:

```console
$ fish -c 'functions fish_add_path'
        # Ignore non-existing paths
        if not test -d "$p"
```

So `test -d $HOME/.bend` is redundant — no `~/.bend` means no `~/.bend/bin`,
and the call returns without touching `$fish_user_paths`. Measured in a
throwaway `HOME`:

| call | `$fish_user_paths` afterwards |
|---|---|
| `fish_add_path $HOME/nope/bin`, directory absent | unchanged, no entry created |
| `fish_add_path $HOME/.bend/bin`, directory present, no `bend` inside | the directory is added anyway |

Row 2 is what keeps `test -e $HOME/.bend/bin/bend` in the config: the built-in
check asks "is this a directory", not "is there a binary in it". Without the
guard, an empty `~/.bend/bin` left behind by an uninstall still lands in `PATH`.
The `test -d` guard on the `bun` and `lm-studio` blocks above it is redundant in
the same way, and harmless.

The call is idempotent — two identical calls leave one entry — and
fish_add_path(1) states it outright: when no directory is new, "the variable is
not set again or otherwise modified, so variable handlers are not triggered".
That is what makes it safe in `config.fish` on every session.

### What the guard does not cover

The guard stops an entry being created; it does not remove one. Measured: an
entry in `$fish_user_paths` sits in `$PATH` even when the directory does not
exist on disk, so deleting `~/.bend` does not take its `bin` back out. Removal
is a `set -e fish_user_paths` in the session, or `set -Ue` when the variable
lives in the universal store.

On this machine the entry is not sticky either way. From a clean `HOME`, fish
starts with a **global** `fish_user_paths` (holding
`/nix/var/nix/profiles/default/bin`), so `fish_add_path` edits that global list
and writes nothing to `~/.config/fish/fish_variables` — verified by grepping the
store after a call in a throwaway `HOME`. The `if` therefore runs again every
session, and the directory appears in `PATH` exactly when the binary is there.

### Probing this without touching the real store

`fish --private` is not isolation for universal variables. In 4.9.3 it means
only "fish will not access old or store new history" (`fish(1)`,
`-P/--private`); reads and writes of universal variables still go to the real
store, and a `set -U` probe under `--private` persisted its test path into
`~/.config/fish/fish_variables`, which put that path in `$PATH` of every new
shell. Use a throwaway `HOME` for any probe that writes universal variables:

```console
$ probe_home=$(mktemp -d)
$ HOME=$probe_home fish -c 'set -S fish_user_paths'
```
