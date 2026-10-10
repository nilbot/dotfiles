# The personal build: what it is, and why it can go

**Date:** 2026-10-09
**Status:** analysis, three lenses folded (lineage, dependency, attack). No design
text changed, no code changed.
**Supersedes:** `2026-10-09-the-personal-build-is-a-fossil.md`, whose central claim
the lineage lens falsified.

---

## 1. What it is

| date | what entered | commit |
|---|---|---|
| 2016-10-13 | the `Makefile`, as the machine-provisioning entry point | `7e07231` |
| 2026-08-07 | `make agents` → `~/bin/agents`, appended to the Makefile, deliberately **not** added to `all:` | `ed1c4e3` |
| 2026-08-10 | the hook chain aimed at `$HOME/bin/agents` | `b87810f` |
| 2026-08-11 | the devtools phase, ported from the retired `githooks:` target — same three steps, same argument | `ababfd8` |
| 2026-08-28 | the release channel and the tap | `ddeebe4`, `1a4583f`, tap `4944805` |

The retrofit is literal. The commit that added the target carried the reason, since
deleted: *"The agents binary lives in dotfiles and is invoked by absolute path from
generated harness configs."* The release channel arrived **21 days later**, and
`devtools` is a port of the target it replaced, not a redesign.

## 2. It was not left unexamined — it was decided, marked done, and never done

Four revisits, every one keeping it:

1. **2026-09-20**, the boundary review names it as a leak — "Both are correct for
   their owner; whichever runs second fails" — and files it open as §7 item 2.
2. **2026-09-20**, the context design records it as "known gap, not closed here… a
   separate decision" (`2026-08-07-agents-repo-context-design.md:608`).
3. **2026-09-20**, `ab2a859` gives the installer `--adopt-owned` and records that
   this machine was repaired to name `/opt/homebrew/bin/agents` — while
   `devtools.go` is changed **by comment only**.
4. **2026-09-21**, the simplification plan answers the question —
   `docs/plans/2026-09-21-simplification-plan.md:89`: *"Who owns it? **Bootstrap**,
   by passing `--adopt-owned`"* — and Task 1 Step 1 specifies the change. The plan is
   marked **EXECUTED**.

It was never made. `git log --all -S'adopt-owned' -- bootstrap.d/` returns nothing on
any branch; `devtools_test.go:23` still pins the flagless command, so a test enforces
the state Task 1 exists to change. Corrected 2026-10-09 in `b301dbc`, which also
records that the check Task 1 specifies — `apply` reaches `verify` — was never run,
and would have shown the claim was false.

The same day, `f3b4a74` removed hook rendering, which was the target's original
justification, in a commit that never looked at either producer. So the build has
persisted since with a dead justification and an unexecuted decision behind it.

## 3. What depends on it: the binding, not the build

**The correctness class is empty.**

- **The guard** — rules embedded at build time (`agents/internal/guard/guard.go:41`),
  scanner from `PATH` (`:208-210`). A tap binary blocks the same secrets. The embedded
  ruleset is a *freshness* skew, not a dependency: the installed guard blocks with the
  last release's rules until a release lands, which is the same property every other
  `agents` change has.
- **The dispatcher** — name-based (`agents/main.go:24-28`), later
  `agents githook <name>`. It reads no checkout.
- **The personal hook stages and `doctor`'s checks** — a *binding* dependency
  (`agents/main.go:46-53`, `doctor.go:64-83`), satisfied today by the compile-time
  stamp or by `AGENTS_DOTFILES_ROOT`. Measured on this host with the released binary:
  **18 checks with the variable set, 13 without**; the five lost are `root:exists`,
  `git-hooks:global`, `git-hooks:effective`, `git-hooks:links`, `git-hooks:unmanaged`.
  Without either, the dispatcher runs no personal hooks and says nothing
  (`githook.go:122-124`, exit 0).

**Nothing automated writes that variable.** `~/.config/fish/config.fish` is a `seed`
row (`bootstrap.d/links.manifest:29`) and the tracked template has no such line, so on
this machine the binding is hand-written.

So the dependency is on the *binding*, and the binding is what identity stage 1 moves
into the record's `checkout=`. **Land the tap change after stage 1, or with it.** Land
it before, and every machine without the hand-written variable silently loses the
personal stages and those five checks.

## 4. Two traps

**`realpath $(command -v agents)` reproduces the 2026-09-20 silent guard loss.**
`brew bundle` installs *and upgrades* by default, and an upgrade deletes the old keg,
so a chain wired to a resolved keg path breaks on the next `apply`. Record the stable
path — what `git/README.md` already says and what `install-hooks.sh:158-160` enforces.

**`linux-stage-zero` cannot fail when the tap formula does not install.** Stage zero is
the *first* step of the packages phase and the tap install happens after it in the same
phase; the job asserts a log grep for a string printed before `brew bundle` and four
`command -v` probes, and records `apply`'s exit code without asserting it. The failure
lands past the only checkpoint it checks. Adding `command -v agents` to the probes
closes it.

Ordering that is satisfied, for the record: `packages` runs before `devtools`
(`phase.go:79-88`); a fresh machine runs stage zero, Homebrew's installer, then
`brew bundle`; a fully-qualified Brewfile entry auto-taps, and the formula covers all
four targets. The binary lands where the running process cannot see it — Homebrew's
`shellenv` is read by the next login shell — so `devtools` must resolve
`<prefix>/bin/agents` the way it already resolves `uv` (`devtools.go:28-40`).

## 5. What the removal fixes, and what it costs

It fixes the two-owner leak the boundary review recorded: today the provisioner and
the package manager write the same `core.hooksPath` with different targets, and
whichever runs second fails — measured here as `bootstrap apply workstation` exiting
2.

It costs: the binding must move first (§3); the network becomes required for a
machine that has none of the tap (a machine with a checkout and Go can build a guard
today and cannot after); and provisioning gains a third-party tap clone per fresh
machine, on a job that cannot see it fail.

## 6. The change, in order

1. **Identity stage 1** — the record exists, so the binding has somewhere to live
   that is not a build fact.
2. **Consume the tap** — `brew "nilbot/tap/agents"` in the Brewfile; `devtools` keeps
   calling the installer, loses the build, resolves the released binary, and passes
   `--adopt-owned`. This is where the 2026-09-21 decision finally lands, with the
   target corrected from `~/bin/agents` to the release path.
3. **Delete the producers** — the `Makefile` and its two targets, the devtools build,
   the drift pins in `makefile_test.go` and `devtools_test.go`, doctor's `make agents`
   remedy, the check's "the devtools phase builds it" text, and the README sections
   that describe Option 1. `agents/install_hooks_test.go:298-338` stays: it builds its
   own temporary fixture and is the only test of the devtools sequence.
4. **Keep a build that is not a route** — a documented `go build -o /tmp/agents .`,
   installing nothing, for the inner loop.
