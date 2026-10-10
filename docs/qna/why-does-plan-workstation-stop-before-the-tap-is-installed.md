# Why does `./bootstrap plan workstation` stop before the tap is installed?

> **Corrected 2026-10-10: it does not stop any more.** `plan` on a machine with
> no `agents` binary is now a preview that names the path the chain would use and
> exits 0; `apply` still refuses, one step later, at the installer. CI run
> 38044462264 is what changed the answer: four plan tests failed on every Linux
> runner, because a runner has no `agents` and refusing on that made a query
> behave like a mutation. The reasoning below is kept as the record of why the
> stop existed, and the file it names is the one to read for the current
> behaviour (`resolveAgents` returns `""`; `devtools.go` names the path; the
> installer's `validate_binary` and `githook --probe` are what refuse).

## Context

Measured 2026-10-09 on this machine, while the personal `agents` build was being
removed and `bootstrap.d/Brewfile` gained `brew "nilbot/tap/agents"`.

`resolveAgents` (`bootstrap.d/internal/phase/packages.go:193`) walks the Homebrew
prefixes for an `agents` executable. Finding none, it returns:

```text
no agents binary at any Homebrew prefix: looked for /opt/homebrew/bin/agents,
/home/linuxbrew/.linuxbrew/bin/agents, … The packages phase installs it from
bootstrap.d/Brewfile (nilbot/tap/agents), so the git hook chain has nothing to
point at until the Brewfile carrying that row has been installed: run
'./bootstrap apply workstation', or 'brew bundle' against
bootstrap.d/Brewfile, and retry
```

The devtools phase calls it before the hooks preflight, because the binary is an
argument to both installer invocations. So `plan` — which is supposed to
preview — stops with the same error `apply` would.

## Answer

**The condition is "the Brewfile that carries `nilbot/tap/agents` has not been
installed yet", and that is wider than "this machine has never run apply".**

A machine that has run `apply workstation` under an older Brewfile has no
`agents` either — the tap row did not exist when it ran. This machine was in
exactly that state on 2026-10-09: `apply` had been run many times, and
`/opt/homebrew/bin/agents` did not exist until the Brewfile gained the row.

So the stop is a real stop with a real instruction, not a shortcut in the walk:
re-running `apply` installs the row in the packages phase, and the devtools phase
then finds the binary. `brew bundle` against `bootstrap.d/Brewfile` does the same
thing without the rest of the run.

Two consequences worth knowing before reading a failure as a bug:

- **`plan workstation` stops before previewing the phases after devtools.** That
  is accepted, and of the same shape `resolveBrew` documents: the alternative is
  recording the probe and planning against a binary that is not there, which is
  the silent success the phase exists to prevent.
- **`plan dotfiles` is unaffected.** It excludes the devtools phase, so it still
  previews on a machine with no released `agents`.

The rule behind it: the binary that owns the hook chain has to be the one the tap
installed, so the resolver probes prefixes rather than `PATH` — a `LookPath`
answering `$HOME/bin/agents` would re-pin the chain to the checkout build the
removal retired, and report success while doing it.
