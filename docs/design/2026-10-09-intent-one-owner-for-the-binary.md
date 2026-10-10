# Intent: one owner for the `agents` binary

**Date:** 2026-10-09
**Status:** revision intent, version two, reviewed by two lenses. **Superseded in its
conclusions** by `2026-10-09-the-personal-build-removal-analysis.md`, which folds the
same two lenses plus a lineage read and a dependency read; §2's inventory below
survives as plan material and is not repeated there. No design text has changed, no
code has changed.
**What this is for:** the owner has decided that `make agents` and the checkout build
go. This document states what that removes, what rests on it, the options with their
costs, and what the identity design's revision would then change. Version two folds a
blind inventory of dependents and an attack on the options, including two mechanisms
that were wrong rather than under-argued.

---

## 1. The premise being removed

The identity design's first substantive sentence is: *"There are two builds of
`agents` on a machine like this one"* — a personal build at `~/bin/agents`, compiled
from a checkout, and a released build from Homebrew at `/opt/homebrew/bin/agents`.
The design then spends its length reconciling their disagreement.

The owner's position is that the personal build should not exist. That decision was
taken earlier and **is recorded nowhere in this repository**: not in `docs/qna/`,
not in `docs/journal/`, not in the boundary document, which describes the personal
build at `:47`, `:57` and `:135` without ever questioning it. What the repository
records is the opposite — `Makefile`'s header calls the surviving target "a developer
convenience… a shortcut, not a separate mechanism", and
[spec 2](2026-08-07-spec-2-dotfiles-hygiene.md) lists "Makefile reduced to the
`agents` target" as a completed step. A reader of the tree finds a defended artifact,
which is why ten review passes over two documents did not question it.

**The route is already broken on the machine it runs on.** Measured 2026-10-09, on a
machine whose chain names `/opt/homebrew/bin/agents`:

```
$ bash git/install-hooks.sh preflight <root> "$HOME" "$HOME/bin/agents"
install-hooks: refusing: owned pre-commit hook … points to '/opt/homebrew/bin/agents',
  not '<home>/bin/agents'; … re-run with --adopt-owned to repoint it
exit=1
```

`bootstrap apply workstation` therefore exits 2, in the devtools preflight, before it
compiles anything and after packages, config and fish have run. The refusal is the
*difference* talking, not the absence: a machine with neither binary runs fine, and a
machine whose hooks name the personal build runs fine. Only the mixture fails.

## 2. What rests on it

Three groups. The first must go with it; the second must be edited in the same
change; the third is where a careless removal does damage.

**Deleted**

| what | where |
|---|---|
| the build target and its directory step | `Makefile:38-41` |
| the release target, which only runs `script/package-release.sh` | `Makefile:43-44` |
| the devtools build and its output path | `bootstrap.d/internal/phase/devtools.go:50`, `:69`, `:86-89` |
| its drift pin | `bootstrap.d/makefile_test.go:43-56`, `:94-133`, `:160-170` |
| its command pin | `bootstrap.d/internal/phase/devtools_test.go:22`, `:153`, `:20`, `:23`, `:268-275`; `preflight_test.go:300-303`, `:330` |

`makefile_test.go` is the hard one: it runs `make -n agents` inside the ordinary
`test` job and the `hygiene` containment leg, so deleting the target without editing
it turns CI red twice. Its own comment records that `-n` exists because the real
target writes `~/bin/agents` on the test machine.

**Edited**

| what | where | to what |
|---|---|---|
| the devtools installer invocations | `bootstrap.d/internal/phase/devtools.go:64`, `:99` | the resolved released binary, not a build; see §3 |
| the check's remedy sentence | `bootstrap.d/internal/check/checks.go:320-322` | it tells people to run the phase that no longer builds anything |
| doctor's remedy | `agents/internal/doctor/doctor.go:834` | `make agents` — **closed 2026-10-09:** the remedy is derived from the chain's own location instead |
| the workspace README's whole Makefile section | `README.md:47-62` | the build, the path, and the worktree hazard — **closed 2026-10-09:** instructions gone, hazard kept as a record |
| the tool README's three producers | `agents/README.md:20-29`, `:111-121`, `:164-180` | `:24` is an **unstamped** build, a different binary from the others; `:27-29` claims "two builders … run the same command with the operator-mode stamp added" — **closed 2026-10-09:** all three statements removed |
| the installer README's two routes and its files table | `git/README.md:12-25`, `:34-35`, `:67`, `:96` | Option 1 disappears; `:34-35`'s `$(command -v agents)` advice survives only for a shell — **closed 2026-10-09** |
| a doc comment | `agents/cmd_wire.go:13` | "the command to run after `make agents` moves the binary" — **closed 2026-10-09:** it now reads "after the `agents` binary moves" |
| spec 2's description of the phase | `docs/design/2026-08-07-spec-2-dotfiles-hygiene.md:189` | and spec 2 is missing from the identity design's own five-document list |
| a qna answer | `docs/qna/why-does-an-unstamped-or-homebrew-agents-binary-skip-dotfiles-checks.md:7` | names the build as a distribution route |
| a stale line to delete rather than edit | `docs/design/2026-08-11-spec-5-verification-gate.md:415` | `make agents && agents index` — `agents index` no longer exists — **done 2026-10-09** |
| `linux-stage-zero`'s probes | `.github/workflows/verify.yml:653-657` | checks gcc, file, curl, git — not `agents`; see §4 |

**Not a dependent, and the removal must not touch it:** `agents/install_hooks_test.go:298-338`
(`runHookSequence`) builds its own binary into a temporary home with no stamp. It is
the only test of the devtools sequence — preflight, build, install — so its shape
survives, with the build step removed.

`bootstrap.d/links.manifest` has no `bin` row; nothing in
`bootstrap.d/internal/check/` stats `$HOME/bin/agents`; the `agents` check is
`LookPath("agents")` and nothing else; `fish/mypre.fish:108` keeps `$HOME/bin` on
`PATH` whether or not anything is in it. The personal build is the only artifact this
repository writes that no phase reconciles and no check notices.

## 3. The options

**Option A — keep both routes.** Cost: the reconciliation machinery above; the
measured refusal; and the footgun `Makefile:29-35` documents — in a linked worktree
the target publishes a binary stamped to a temporary directory, and the failure is
silent, at exit 0. Benefit: a machine can be provisioned with no network and no tap,
and a developer can run the tree they are editing as the machine's binary.

**Option B — delete the build now, ahead of the identity change.** Not safe. The
checkout binding for an *installed* binary is the stamp or the environment variable
(`agents/main.go:50-53`), and the design deletes the variable, so removing the
builder first leaves nothing.

**Option C — delete it as part of identity stage 1, with the record.** The record
carries what the stamp carried: personal stages from the entry's `--checkout` instead
of `main.go:50-53`; machine checks from `core.hooksPath` and the record instead of the
root; a linked worktree better off, because `checkout=` can name the worktree without
publishing a binary stamped to a temporary path; two checkouts no problem, because the
chain names one binary and the record one checkout.

Two mechanisms, and version one had both wrong.

1. **Devtools must resolve the binary, not look it up on `PATH`.** `$(command -v agents)`
   is the repository's own advice in a *shell* (`git/README.md:34-35`), and it is wrong
   inside `bootstrap`: the packages phase installs Homebrew and then `agents` into the
   brew prefix, while `exec.LookPath` resolves against the inherited `PATH`, which
   never contains that prefix on a fresh machine — Homebrew's `shellenv` line is read by
   the *next* login shell. `packages.go:155-165` states this hazard and `resolveBrew`
   (`:182-204`) exists to work around it; no production code in `bootstrap.d` changes
   `PATH`. So the answer is a `resolveAgents` beside `resolveBrew`, probing
   `<prefix>/bin/agents` over the existing `brewLocations` (`packages.go:123-127`) and
   refusing with a named message when none exists. This is the fourth instance of the
   resolve-versus-`PATH` defect spec 2 records. Second failure of the same line: where
   `~/bin` precedes the brew prefix, the lookup returns the stale personal build and
   re-binds the chain to the binary this change deletes.
2. **The Brewfile entry is qualified.** `brew "nilbot/tap/agents"`, which
   `git/README.md:20` already writes. A qualified name taps on demand
   (`bundle/installer.rb:97`), so the separate `tap` line is optional; keeping an
   unqualified `brew "agents"` would resolve between the tap and homebrew-core, which
   is not decidable on a machine without core tapped.

**Option D — Option C, plus a build convenience that is not a route.**
**Recommended**: a documented `go build -o /tmp/agents .` (or `go run`) writing no
global path and installing nothing. The `Makefile` still goes, and nobody has to
rediscover how to try a change — with the qualification §4 gives.

## 4. What it costs

- **A machine whose chain names `$HOME/bin/agents` cannot convert itself.** Version
  one claimed the installer conversation re-points it; that is wrong.
  `owned_link_target` (`git/install-hooks.sh:202-208`) accepts a link only when its
  target *is* a keg path or resolves into one, and `~/bin/agents` is a plain
  executable outside any keg, so the link is foreign: `check_exact_symlink_or_absent`
  refuses at `:163` with **and** without `--adopt-owned`, exactly as the identity
  design's conversion table says. Nothing rebuilds that path afterwards, so such a
  machine is stuck at a refusal whose remedy no longer exists. Two ways out: give the
  installer a fourth owned shape — a link whose target is a regular executable named
  `agents`, the rule `isOwnedBinary` already uses
  (`agents/internal/harness/harness.go:174-176`) — or write the manual sequence into
  this intent and `git/README.md`: delete the four links, then run the installer,
  which sees four absent names and succeeds.
- **Homebrew becomes the only source, so the network is required.** A machine with a
  checkout, Go and no network can no longer provision a working `agents`.
- **`provenance:checkout`'s deletion leaves the recorded checkout unverified.**
  Version one called this the loss of provenance. The behavioural half is larger: the
  design moves "which checkout supplies personal stages" from the stamp to
  `chain.env`'s `checkout=`, and then requires only that the value be `-` or absolute
  (`§3.4 chain:record`). With the stamp gone and the check deleted, `doctor` cannot
  notice that the recorded directory no longer exists, while §3.6 promises "the
  checkout is gone … the entry, **and `doctor`**". The dispatcher treats a missing
  extras directory as "no personal hooks" and carries on
  (`agents/main.go:46-49`; `githook.go:122-125`) — the silent class this work exists
  to remove. **Keep the check and re-aim it at the record**: warn when `chain.env`'s
  `checkout` is neither `-` nor an existing directory. That needs no stamp.
- **The developer's workflow changes shape.** Option D's temporary binary is
  deliberately not the machine's binary, so testing a change *to the hook path* means
  running the installer against the built path and running it again to go back — and
  under the identity design's ownership rule each switch is an adoption. Option D
  should name the two commands.
- **Every provisioned machine tracks the tap at each `apply`**, and `linux-stage-zero`
  gains a third-party tap clone per run, cached inside `/home/linuxbrew/.linuxbrew` so
  a Brewfile edit re-seeds that cache.
- **An unreachable tap is loud for a person and invisible to the merge gate.**
  `brew bundle` failing makes the packages phase return the error and `apply` exits 2.
  On `linux-stage-zero` it is not: the job records `apply`'s exit code without
  asserting it and gates on `grep -q "stage zero"` (`verify.yml:636-641`), and the
  packages phase prints that line **before** `brew bundle` runs — so the job stays
  green with no `agents` installed, and devtools then fails for the reason in §3,
  also recorded and not asserted. Adding `command -v agents` to the job's probes makes
  the outcome observable.
- **Ordering.** Repointing `devtools`, qualifying the Brewfile entry and editing the
  build pins have to land in the same change as the deletion, or
  `bootstrap apply workstation` refuses on any machine whose chain names a binary this
  change stops producing.

## 5. What the identity design's revision would change

- §1's premise sentence and its table of "who answers what today" — one owner, not
  three.
- F2 and F4 stop being constraints: no build is bound to a checkout, and the variable
  has nothing left to override once the record names the checkout.
- §3.3: the stamp is deleted rather than kept as provenance. §3.4's
  `provenance:checkout` row is **re-aimed at the record**, not deleted — §4 of this
  document is the reason.
- §3.5, §5, §6: the developer-build paragraph; the ideal world's "no stamp at all",
  which becomes the present; the stage-1 bullet that renames `DotfilesRoot()`.
- §7: four enforcement rows become deletions rather than edits — the `Makefile`, its
  two drift-pin files, and doctor's remedy — and `§2`'s inventory rows above are added.
- §9: the open question about CI is answered by §4, not left open.

## 6. What the revision will not change

The record, the four entries, `agents githook`, the two stages, the move to
`~/.config/agents/hooks.d/`, the installer's ownership table, the transition window
and its two closers, the migration table, and the verification table. The removal
deletes work rather than adding it; the mechanism survives, with the two corrections
in §3 and §4.

## 7. Open questions

1. **`Makefile`'s release target**: `make release` runs `script/package-release.sh`.
   With the file deleted, does the script get documented in `README.md` or
   `agents/README.md`, or does the release workflow become the only documented entry?
2. **The Option-1 machine's conversion**: a fourth owned shape in the installer, or a
   documented manual sequence? The first is more code and a wider ownership rule; the
   second is two commands in `git/README.md` and a stuck machine until someone reads
   them.
3. **Option D's build line**: where does it live so that it is found —
   `agents/README.md` alone, or also a script?
