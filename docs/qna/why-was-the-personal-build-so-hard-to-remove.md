# Why was the personal `agents` build so hard to remove?

## Context

On 2026-10-09 the owner said the checkout build of `agents` — the `Makefile`'s
`agents` target and `bootstrap`'s devtools build of `$HOME/bin/agents` — "should have
been retired long ago", and asked why deleting it looked difficult when CI, the
release workflow and the Homebrew tap had existed since 2026-08-28.

It looked difficult because a decision to remove it had already been taken, recorded
as executed, and never carried out. Nothing in the tree said so: `Makefile`'s header
calls the target "a developer convenience… a shortcut, not a separate mechanism", and
[the simplification plan](../plans/2026-09-21-simplification-plan.md) lists "Makefile
reduced to the `agents` target" as done. The owner's decision was written nowhere —
not in `docs/qna/`, not in `docs/journal/`, not in the boundary review.

## Answer

**The build is a port of a retired provisioning target, kept alive by an unimplemented
decision.** Three parts, each with its evidence.

**1. It is a retrofit of the old provisioning entry point.** The `Makefile` was how
this machine was provisioned before `./bootstrap` existed. When the Go binaries
arrived, `make agents` was appended to it (`ed1c4e3`, 2026-08-07) with the reason now
deleted: *"The agents binary lives in dotfiles and is invoked by absolute path from
generated harness configs."* The hook chain was aimed at that path three days later
(`b87810f`, 2026-08-10), and `bootstrap`'s devtools phase — added 2026-08-11
(`ababfd8`) — is a port of the Makefile's `githooks:` target: same three steps, same
argument. The release channel and the tap arrived 2026-08-28, twenty-one days after
the target.

**2. It was revisited four times, and three were deferrals.**
The boundary review of 2026-09-20 names the two-owner leak and files it open as §7
item 2; the context design records it as a "known gap, not closed here"; the same day
`ab2a859` gives the installer `--adopt-owned` and records that this machine was
repaired to name `/opt/homebrew/bin/agents` — while changing `devtools.go` by comment
only. Then on 2026-09-21 the plan answered the question and specified the change:

> | Q3 | The hook chain has two owners… Who owns it? | **Bootstrap**, by passing `--adopt-owned`… |

with Task 1 Step 1 giving the one-argument change and its verification.

**3. The decision was never carried out, and the record said it was.**
`git log --all -S'adopt-owned' -- bootstrap.d/` returns nothing on any branch.
`bootstrap.d/internal/phase/devtools_test.go:23` still pins the flagless command, so a
test enforced the state the task existed to change. The status line read
`EXECUTED 2026-09-21` from that day until 2026-10-09, when it was corrected in
`b301dbc` — with both of Task 1's checkboxes still unticked, two lines below the
claim. The verification Task 1 specified — that `apply` reaches `verify` — was never
run, and it is what would have caught the false claim.

The build's original justification died the same day, unnoticed: `f3b4a74` removed
hook rendering, so nothing this tool writes names a binary path any more.

## What this cost, and what removing it takes

**The cost of the missing record.** Two design documents were written, and ten review
lens runs spent across them, against a tree whose shape the owner had already decided
to change. The first lens to question the premise was a sequencing agent that measured
`bootstrap apply workstation` refusing on this machine — a symptom one step away from
the cause — and it was filed as a question for the owner instead of being traced back.

**What actually depends on the route** (measured 2026-10-09): the guard's rules are
embedded at build time and its scanner comes from `PATH`; the dispatcher is name-based.
So nothing needs the binary *built from the checkout*. What the build uniquely supplies
is the **binding** to a checkout, carried today by the compile-time stamp or the
hand-written `AGENTS_DOTFILES_ROOT` line in `~/.config/fish/config.fish:19`, worth five
`doctor` checks (18 → 13) and the personal hook stages. That binding moves to the
chain record in
[identity stage 1](../design/2026-10-07-one-record-for-binary-identity.md), which is
why the tap change lands with it or after it, never before.

**What it takes**: the `Makefile` and its two targets; the devtools build and its
output path; the drift pins in `makefile_test.go` and the stamp pins in
`devtools_test.go`; doctor's `make agents` remedy; the check text that says the
devtools phase builds the binary; the README sections describing the build-from-source
route; and `bootstrap.d/Brewfile` gains `brew "nilbot/tap/agents"` so provisioning
delivers the binary the builders used to produce. `runHookSequence`
(`agents/install_hooks_test.go:298-338`) stays: it builds its own temporary fixture and
is the only test of the devtools sequence.

## Follow-up, 2026-10-09

**The list above is executed, and the order it gives is the order it happened in.**

- **Everything on it landed the same day.** The devtools build and its output path
  went first, with `bootstrap.d/Brewfile` gaining `brew "nilbot/tap/agents"` and
  the phase resolving that binary and passing `--adopt-owned` to both installer
  invocations, so an existing chain is converted rather than refused. The
  `Makefile` and its two targets, the drift pins in `makefile_test.go`, the check
  text that said the devtools phase builds the binary, and the README sections
  describing the build-from-source route followed.
- **One item was fixed earlier than the removal, deliberately.** Doctor's remedy
  stopped naming the build before the build went, because a remedy has to keep
  working while the thing it repairs still exists: it derives the checkout from the
  chain's own location now.
- **Two corrections to the item list, worth having rather than the plan.** The
  stamp pins in `devtools_test.go` were not edited — the whole build step they
  pinned was deleted. And `runHookSequence` did stay, as predicted: it builds its
  own temporary fixture and is the only test of the devtools sequence, so its shape
  survived with the build step removed from it.

The answer itself is untouched, because the diagnosis was right: what made this
hard was never the deletion. It was the *binding* — a checkout recovered at run
time from a compile-time stamp or a hand-written environment line — and moving that
into the chain record is what turned a decision recorded on 2026-09-21 and never
executed into an afternoon's work.

## Correction, 2026-10-09 (same day, after the branch review)

Two citations in the answer above point at a tree that has since moved, and one
number was measured again:

- **`bootstrap.d/internal/phase/devtools_test.go:23`** — the file now pins the
  installer invocations **with** `--adopt-owned`, at `:20` and `:21`. The
  sentence above is the record of what it pinned before the fix; the line number
  is the one it had then.
- **`agents/install_hooks_test.go:298-338`** — `runHookSequence` is still there
  and still the only test of the devtools sequence; its line numbers moved with
  the stage-2 work, and the name is the citation that survives.
- **"worth five `doctor` checks (18 → 13)"** — re-measured on 2026-10-09 against
  the branch: 19 checks with a chain installed, 14 with none. The difference is
  still five, and it is now the chain record rather than the variable that
  carries the binding, so the count moves with the chain and not with
  `AGENTS_DOTFILES_ROOT` (which no longer has a reader).

Sibling entries in this store and in `docs/design/` still cite `Makefile:` line
numbers and `makefile_test.go`. Those are kept as records under the convention
[where else does this command name live](where-else-does-this-command-name-live.md)
gives; a dated answer is not rewritten, and a path that has gone is part of what
the answer was about.
