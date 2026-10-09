# Orthogonal minimal redesign: the plan

**Date:** 2026-10-09
**Status:** active. Implements two reviewed designs and one removal analysis.
**Designs:** [what a query is](../../../dotfiles.worktrees/what-a-query-is/docs/design/2026-10-09-what-a-query-is.md)
(branch `design/what-a-query-is`) and
[one record for binary identity](../../../dotfiles.worktrees/identity-design/docs/design/2026-10-07-one-record-for-binary-identity.md)
with [the removal analysis](../../../dotfiles.worktrees/identity-design/docs/design/2026-10-09-the-personal-build-removal-analysis.md)
(branch `design/one-record-for-identity`).

Every step below states what changes, the check that must pass before the next step
begins, and what a machine that is already wired experiences at that moment. A step
whose only verification is a later step failing is not a step.

---

## Step 1 — `bootstrap check` stops executing anything

**Why first.** It is fully verifiable today, needs no release and no machine change,
and it removes the auto-updating check (`checks.go:347`, reached under `plan` through
`verify.go:27`) from the loop used while converting the chain in steps 4–5.

**Changes.** The fifteen sites in the query design's §5: delete the `packages` check
(`bootstrap.d/internal/check/checks.go:326-352`); drop `Run` from `check.Machine`
(`check.go:54-60`) and rewrite the package comment; the `machineChecks` slice and the
two count comments (`:131-140`, `:116-120`); `TestMachineCannotMutate` 5→4 methods;
delete `TestPackagesFailsWhenTheBrewfileIsAbsent` and `…BrewBundleCheckFails`; seven
names in `TestAllReportsTheEightChecks`; the `packages` lines in the two
dotfiles-profile tests; `TestCheckOnABareHomeNamesTheMissingRows`; delete
`TestPlanAndCheckAgreeOnThePackagesVerdict`; rewrite `verify.go:18-26`. **Plus the
recorder test** the design's §6 row 2 specifies — one stub directory whose `brew`
appends its arguments to a file, proved before it is trusted, then `plan workstation`
and `check workstation` with that directory ahead of `stubToolDir(t)`.

**Verification.** `cd bootstrap.d && go test ./...`. Named failures:
`TestMachineCannotMutate` if `Run` survives; `TestAllReportsTheEightChecks` on an
eighth name; the recorder test if either query executes brew. The mutation in §6 row
1 — delete `Run`, add a call in a check — is a compile error. Locally,
`./bootstrap plan workstation` and `check workstation` print no `plan run: brew …`.
CI: `linux-dotfiles` still exits 0 (`n/a` machine checks under that profile);
`linux-stage-zero` is untouched.

**A wired machine.** Nothing moves. The four symlinks, `core.hooksPath`, the keg and
the hand-written fish line are untouched; the cached `bootstrap` binary is replaced on
the next run because the shim compares `.go` mtimes. The only visible change:
`check workstation` loses its `packages` row and stops printing
`==> Auto-updating Homebrew…`.

## Step 2 — Identity stage 1: the reader changes, the producers do not

**Changes.** The record, the four entries, `agents githook <name> --checkout <path>`,
the `chain:*` checks and `chain:checkout`, the `argv[0]` shim with its message, and
the reader-side stamp deletion: `agents/root.go`, `agents/main.go`, `agents/cmd_doctor.go:32`,
the entries' installer, `git/README.md`, the five design documents, `agents/doctests.txt`.
The builders keep passing `-X main.dotfilesRoot` — harmless, because a linker `-X`
naming a variable that no longer exists is accepted and ignored (measured) — and one
new check asserts no builder passes it. Stage 1 keeps the chain inside `git/hooks.d/`.

**Verification.** `cd agents && go test ./...`: `TestTheCommandSetIsExactlyThis` fails
until `githook` is registered; `TestReadmeCommandBlockIsCurrent` until the generated
block is regenerated; `install_hooks_test.go` covers the installer's ownership table
(§8 rows 1, 2, 4–13, 15–18); `TestLivingDocuments…` covers every amended document. CI:
`docs` runs that list by name; `hygiene` runs the suite with `HOME` at an empty
directory. Deliberately not verifiable here, and the design says so: §8 tests 3 and 14
are stage-2 subjects.

**A wired machine.** Still nothing. The installer is not run by this step, and a
premature `install --adopt-owned` writes nothing, because the installed release cannot
answer `githook` (measured: exit 3).

## Step 3 — Provisioning consumes the tap

**Changes.** `brew "nilbot/tap/agents"` in `bootstrap.d/Brewfile` (qualified; Bundle
taps on demand); `devtools` keeps calling the installer, loses the build, and hands it
the **resolved** released binary — a `resolveAgents` beside `resolveBrew`, probing
`<prefix>/bin/agents` over `brewLocations`, never `LookPath` — with `--adopt-owned`.
The two argv pins in `devtools_test.go:20,23` are re-aimed at the new string.

**Verification.** `cd bootstrap.d && go test ./...`; the argv pins fail until
re-aimed. CI: `linux-stage-zero` gains a tap clone, and its probes gain
`command -v agents` — without that the job cannot fail when the formula does not
install, because its only assertions precede `brew bundle`.

**A wired machine.** Nothing yet. The next `apply` installs the released binary into
the brew prefix, which the running process cannot see until a new login shell — which
is why devtools resolves it rather than looking it up.

## Step 4 — Convert this machine's chain

**Changes.** `bash <root>/git/install-hooks.sh install --adopt-owned <root> "$HOME" "$(command -v agents)"`:
four symlinks become four entries plus `chain.env`. Delete the hand-written
`AGENTS_DOTFILES_ROOT` line at `~/.config/fish/config.fish:19` by hand — no automated
path owns it, because the row is a `seed`.

**Verification.** `agents doctor` green on `chain:hooks-path`, `chain:entries`,
`chain:record`, `chain:checkout`, `chain:running`; `cat git/hooks.d/chain.env` shows
`binary=/opt/homebrew/bin/agents` — the **stable** path, never a `realpath`, because
`brew bundle` upgrades and deletes the old keg; a scratch-repo commit with a staged
secret is refused and a clean one succeeds; `git status --short` is empty. Rehearse the
stage-1 rollback once: delete the entries and `chain.env`, keep the directory and its
`.gitignore`, re-run the previous installer.

**A wired machine.** One `/bin/sh` per hook run, a binding a person can read, and a
guard that fails closed. `~/bin/agents` is still absent and `core.hooksPath` still
points into the checkout, so a deleted checkout still silently disables the guard —
which is what step 6 fixes.

## Step 5 — Delete the producers

**Changes.** The `Makefile` and its two targets; the devtools build and its output path;
`makefile_test.go`; the devtools pins (`:22`, `:139`, `:153`, `:58`); doctor's
`make agents` remedy; the check text that says the devtools phase builds the binary;
`README.md:47-62`; `agents/README.md:20-29`, `:111-121`, `:164-180`; `git/README.md`'s
Option 1, `:67`, `:96`; `cmd_wire.go:13`; spec 2 `:189`; spec 5 `:415` (a stale line to
delete); the qna that names the build as a distribution route. `runHookSequence`
(`install_hooks_test.go:298-338`) **stays**: it builds its own temporary fixture and is
the only test of the devtools sequence. Add the documented `go build -o /tmp/agents .`
line for the inner loop.

**Verification.** `cd bootstrap.d && go test ./...` and `cd agents && go test ./...`;
`make -n agents` is no longer invoked by any test; `grep -rn 'main.dotfilesRoot' .`
returns only the deletion's own records.

**A wired machine.** A stale `~/bin/agents`, if one exists, stops being rebuilt;
re-running the installer names the released binary instead.

## Step 6 — Identity stage 2: the chain becomes machine-owned

**Changes.** Move the chain to `~/.config/agents/hooks.d/`; the installer creates that
directory; `inspect_global_hooks_path` accepts `<root>/git/hooks.d` as a **value**;
repoint `core.hooksPath`; retire `git/hooks.d/.gitignore` **and remove the four
entries** — otherwise the checkout reports them untracked (measured in a scratch repo);
`doctor.go:78` and the `git-hooks:*` block move to `core.hooksPath`;
`install_hooks_test.go:280` and `:1153` are edited, not optionally; §8 tests 3 and 14
are written here.

**Verification.** `cd agents && go test ./...` with tests 3 and 14; `cd bootstrap.d &&
go test ./...`; `hygiene`'s `agents`/`home` leg fails if a test writes the new machine
directory.

**A wired machine.** `core.hooksPath` leaves the checkout. The guard now outlives a
moved or deleted checkout. Rollback is the measured sequence: recreate the four
symlinks first, then repoint, then delete the chain directory.

## Step 7 — The records

**Changes.** Amend the five design documents that restate the deleted contract; add
`git/README.md` to `livingDocuments` (`agents/docs_test.go:74-99`) — it passes as it
stands, and after step 2 it will name `agents githook`; correct or qualify
`git/README.md:14-17`, which promises a route that step 5 removes; record in
`docs/qna/` what depended on the personal-build route and what removing it took with
it, since the reason this was invisible for a month is that nothing was written.

**Verification.** `cd agents && go test ./... -run 'TestLivingDocuments'`; the `docs`
CI job runs that list by name.

---

## Standing rules for every step

- Work in a linked worktree; never move the shared checkout's tip.
- One writer per file; the Lead reviews the diff and runs the checks.
- A step is done when its verification has been run and its result recorded, not when
  the text says so. The 2026-09-21 plan is the worked example of the opposite.
