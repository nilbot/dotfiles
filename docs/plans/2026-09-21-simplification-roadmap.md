# Simplification Roadmap

> **SUPERSEDED 2026-09-21 by
> [`2026-09-21-simplification-plan.md`](2026-09-21-simplification-plan.md)**
> (which carries the sequence and the tasks in one file). This roadmap remains
> accurate about the *direction* — bootstrap last, the Go path orthogonal — and
> is kept for that record. It named two tests that do not exist and omitted the
> hook-ownership blocker (`--adopt-owned`). **Do not execute.**

**Status: superseded — see the banner above.**

**The question this answers:** `bootstrap` is going to be touched too — when?
Answer: **last, and not much.** It is a thin layer that builds and installs
`agents` and then leads the machine to Go. Almost everything decided about
`agents` and about skills changes what `bootstrap` does *downstream*, and only
one change touches `bootstrap` itself.

---

## Order

| # | Step | Waits on | Touches |
|---|---|---|---|
| 1 | **[`agents` reduction](2026-09-21-agents-reduction-plan.md)** | nothing — both decisions are taken (no `trace`, `guard` becomes a script) | `agents/`, `git/hooks.d/`, `git/install-hooks.sh` |
| 2 | **[Skills single home](2026-09-21-skills-single-home-plan.md)** | step 1 — `agents` survives in reduced form, so `agents-tool` and `migrating-fleet-context` can now be placed | `.agents/skills/`, `claude/skills/`, `gemini/skills/`, two manifest rows |
| 3 | **`bootstrap` follow-through** | step 1's surviving hook-name set | `bootstrap.d/internal/phase/devtools.go` |
| 4 | **The Go path** | nothing — orthogonal | `bootstrap` (the shim), `bootstrap.d/main_test.go` |

Steps 3 and 4 are the `bootstrap` work. Step 3 is small and mechanical. Step 4
is the only substantial piece, and it is already planned — it is Task 2 of the
superseded [self-sufficiency
plan](2026-09-21-bootstrap-self-sufficiency-plan.md), whose Task 1 (`linkdir`)
this roadmap replaces with deletion.

---

## Step 3 in detail: `bootstrap` follow-through

**Why it exists.** `bootstrap.d` does not know `agents`' command set. It knows
three things: it builds the binary (`go build -C <root>/agents -o
$HOME/bin/agents .`, with a `-X` stamp binding the binary to this checkout), it
installs it, and it hands the path to `git/install-hooks.sh`, whose `install`
subcommand **symlinks four hook names at it**. The third is the coupling:
`main.go` dispatches on `filepath.Base(os.Executable())`, so those names are a
contract.

- [ ] **Re-derive the hook-name set from the reduced binary.** If `agents hook`
  is deleted (reduction plan Task 2), the four symlinks
  (`commit-msg` and friends) must change in the same commit. A symlink named
  `commit-msg` pointing at a binary that no longer answers that name fails at
  commit time, in every repository, with a git error — and `bootstrap check`
  will still report `agents: ok`, because the check is `LookPath("agents")`.
  **That mismatch is the trap this ordering exists to avoid.**
- [ ] **Check whether the `agents` check in `bootstrap.d/internal/check` should
  verify behaviour rather than presence.** Today `agentsOnPath` proves the
  binary exists. If the hook names are now the contract, the check should prove
  one of them answers — the difference between "installed" and "wired".
- [ ] **Confirm nothing else in `bootstrap.d` names an `agents` subcommand.**
  Measured while writing this: `devtools.go` and the `check` package are the
  only non-test references, and neither names a subcommand.

## Step 4 in detail: the Go path

Independent of everything above, and the only place a machine is led to Go.

- [ ] **Do the four things the superseded plan's Task 2 lists:** move the Go
  check inside the build branch (a fresh cache should not need Go at all), look
  in six fixed locations before declaring Go missing, name an installer *this
  machine has* instead of branching on `uname`, and name a platform tarball when
  no installer exists.
- [ ] **Fix that plan's two test defects, already diagnosed:** its
  `TestGoOutsidePathIsFoundAndUsed` has no skip covering the whole candidate
  list, and its tarball-suffix test can never run in CI. The hermetic
  stub-`uname` + closed-`PATH` recipe in the earlier discussion replaces both.
- [ ] **Decide the release pipeline — and notice the direction is easy to get
  backwards.** spec 2 §2.2 says the *release* path is the seam that removes Go
  as a dependency; the shim's build-from-source branch is the fallback. So a
  hardened Go path does not license deleting the release pipeline, and deleting
  it would make Go permanent. Record the decision explicitly rather than
  inferring it from this plan's existence.

---

## Not in this roadmap

- **`bootstrap`'s phases other than `devtools`.** `config` is what the skills
  plan unblocks; `packages`, `fish`, `verify` are untouched by any of this.
- **`retiring agents`** as the superseded plan's out-of-scope list phrased it.
  The reduction plan answers it in the negative: `agents` survives, reduced.
- **`~/.codex/skills`, `~/.claude/skills`, `.gemini/skills` housekeeping.**
  Skills plan, Task 1 Steps 3–5.
