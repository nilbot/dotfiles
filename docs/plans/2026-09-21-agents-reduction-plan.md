# `agents` Reduction Plan

> **SUPERSEDED 2026-09-21 by
> [`2026-09-21-simplification-plan.md`](2026-09-21-simplification-plan.md).**
> This plan was reviewed independently and returned with blockers: its deletion
> set left the module uncompilable (`internal/drift` imports `internal/layout`),
> its stated mechanism for removing existing hook entries cannot fire
> (`stripOurs` runs only inside the per-event loop), three of its premises were
> false (`guard` regenerates indexes; the repository is on v2; the hook entries
> live in `~/.claude/settings.json`), and it named files that do not exist. Kept
> as the record of what was proposed and why it did not hold. **Do not execute.**

**Status: superseded — see the banner above.**

**Goal:** Keep `agents` as the one tool that makes a repository immediately
usable by a harness — structure, skills, wiring — and delete every command whose
job is something else. The binary is Go because hook wiring is a reversible edit
to a file this repository does not own; nothing else about it needs to be Go,
and nothing else about it needs to exist.

**Why this is a deletion plan and not a rewrite.** An earlier audit asked whether
a binary is warranted at all for "an `AGENTS.md`, a skill, some folders and
READMEs", and whether POSIX shell (or fish) would do. The answer, measured:
the *deliverable* is trivial (`scaffold.CreateWithLayout` — a few hundred lines
of `MkdirAll` and `WriteFile`), but `wire` is not. It must recognise its own
prior entries in a user-owned `settings.json` with a deliberately narrow
predicate, delete only those, keep unknown shapes verbatim
(`internal/harness/harness.go`: `stripOurs`, `IsOwnedHookCommand`), and write the
result back as valid JSON. That code carries the record of this going wrong even
with types:

> A test that wired this repository with the ephemeral `agents.test` binary left
> entries that `ParseHookCommand` refused … They accumulated four per run and
> failed loudly at every session start while doctor reported the wiring exact.

So the language choice stands. What does not stand is the other 11,000 lines.

**Architecture:** Reduce to the commands that make a repository usable and
report on it. Everything else is either a second product (`layout` v2, the fleet
registry), a feature with its own storage (`trace`), or a diagnostic niche
(`drift`). The reduction is measured: 13,429 non-test lines today, of which
`internal/layout` (2,208), `internal/doctor` (1,611), `internal/drift` (1,296)
and `internal/trace` (867) are the bulk.

**Depends on:** [the boundary review](../design/2026-09-20-agents-and-bootstrap-boundary.md)
§2 (the two scopes), §6 rules 3 and 5 · the skills plan
[`2026-09-21-skills-single-home-plan.md`](2026-09-21-skills-single-home-plan.md),
which waits on this one for the fate of `agents-tool` and
`migrating-fleet-context`.

**Superseded scope, reused:** the earlier plan's "Deliberately out of scope"
listed *"retiring `agents`; rewriting `agents guard` as a repository script"*.
This plan takes the first item up, in the reduced form the audit supports, and
answers the second in Task 3.

---

## Global Constraints

1. **`wire` stays the narrow-predicate, reversible editor it is.**
   `IsOwnedHookCommand` must stay narrower than `ResemblesHookCommand`, and
   `stripOurs` must keep discarding only what it recognises. Deleting an
   unrecognised entry from a user's `settings.json` is the defect this pair
   exists to prevent, and no reduction is worth reopening it.
2. **Git-hook multicall stays working.** `agents` is installed under four hook
   names by `bootstrap.d/internal/phase/devtools.go`, and `main.go` dispatches on
   `filepath.Base(os.Executable())`. Whatever else changes, a symlink named
   `commit-msg` pointing at this binary must keep resolving to a sane command.
3. **`init` remains one command that reaches a usable state.** Whatever is left
   of it, `agents init` in a fresh directory must still leave the repository
   ready for a harness, and the trust steps a hook cannot take must still be
   printed rather than assumed.
4. **Help is generated from the command tree.** `commands.go` is the one place
   a command is declared; `agents help --render=markdown` feeds a README block.
   A command deleted from the tree must not survive in prose anywhere.
5. **Do not break the Homebrew path silently.** `script/sync-homebrew-formula.sh`
   installs `agents` and asserts `agents version`. `version` is therefore
   load-bearing for the release check even though it looks like a nicety.
6. **`doctor` may observe outside a checkout; it may not write there.** The
   boundary review's rule 3. Any check that survives must be read-only.

Verification commands, from `agents/`:

```sh
gofmt -d . && go vet ./... && go test -count=1 ./...
agents help                 # the surviving surface, and nothing else
```

---

### Decisions taken (owner, 2026-09-21)

Both were open when this plan was drafted and are now settled. The steps below
are written to these answers; where a step still says "decide", it means "record
the measurement", not "choose a direction".

| # | Question | Answer | Consequence |
|---|---|---|---|
| 1 | Does the session-record feature (`trace`) survive? | **No — delete it.** | Task 2 executes in full, including the `doctor` surgery it forces |
| 2 | Does `guard` survive as a subcommand? | **No — rewrite it as a script** under `git/hooks.d/`. | Task 3 Step 1 executes the script path; the binary loses `guard` and its 448 lines |

Two consequences that follow from these answers and are **not** yet owned by any
step — the review must place them:

- **The installed hook names are a contract with `bootstrap.d`.** With no
  `agents hook` and no `agents guard`, `git/install-hooks.sh`'s four symlinks
  (`pre-commit commit-msg post-merge post-checkout`, measured at line 61) point
  at a binary that answers none of them. Either the name set changes, or the
  binary keeps a dispatcher that routes them to the new script. This is Task 3
  Step 2 and the roadmap's step 3, and the two must not disagree.
- **Hook entries already written into harness configs must be removed by the
  same change.** Measured on this machine: `~/.claude/settings.json` carries
  `SessionStart`/`Stop`/`SubagentStart`/`SubagentStop` blocks calling
  `/opt/homebrew/bin/agents hook …`, and `wire` and `stripOurs` are what put
  them there. `wire`'s narrow predicate is the only thing allowed to take them
  out again.

---

### Task 1: Delete the second product

**The problem this solves.** `layout` and the fleet registry are a different
product wearing the same binary. The v2 layout — `agents.layout.json`, store-role
resolution, the v1→v2 migration with its journal and backup tag — is 2,208 lines
plus 1,603 of tests, and the migration is the single most dangerous code in the
repository: it `git mv`s a repository's documentation. The fleet registry
(`agents ls`, `agents update`, `internal/registry` 360 lines) exists so a machine
can rewire every repository it has touched at once.

**Files:**
- Delete: `cmd_layout.go`, `cmd_layout_test.go` (1,156 + 1,603)
- Delete: `cmd_fleet.go`, `cmd_fleet_test.go` (217 + 563)
- Delete: `internal/layout/` (2,208 + tests), `internal/registry/`
- Modify: `commands.go` (both entries), `cmd_help.go`, `cmd_init.go`
- Modify: `internal/scaffold/` — the pieces that exist only to emit a v2 manifest
- Modify: `agents/README.md` and any doc naming the deleted surface

---

- [ ] **Step 1: Decide what `init` becomes without a layout schema**

  Today `init` defaults to an implicit v1 layout (four stores under `docs/`) and
  accepts `--template`, `--stores`, `--archive` to create a v2 manifest instead.
  Without `layout`, `--template`/`--stores`/`--archive` have no consumer and
  `agents.layout.json` is never written. The question is whether the four-store
  convention stays *implicit* (it is what `scaffold` already creates by default)
  or becomes a flagless constant.

  Recommendation: implicit, with the store names as constants, and `scaffold`'s
  v2 asset tree deleted along with the manifest writer. `agents init` creates
  `docs/{design,plans,journal,qna}`, `.agents/`, the skill, and the wiring.

- [ ] **Step 2: Delete the layout and fleet commands, and what they drag in**

  Measured dependencies: `cmd_init` imports `drift`, `harness`, `layout`,
  `registry`, `repo`, `safetext`, `scaffold`, `exitcode`. `cmd_wire` imports two
  of those. After this step `cmd_init` must import `harness`, `repo`, `scaffold`
  and nothing else — that import list shrinking is the check that the deletion
  is complete, and it can be asserted in a test.

- [ ] **Step 3: Migrate this repository off `agents.layout.json`**

  This repository is on the v2 layout (the `docs/qna`, `docs/plans`,
  `docs/journal`, `docs/design` stores came from it). Deleting `layout` must not
  change where documents live: the paths stay, only the manifest that names them
  goes. Check whether `.agents/layout.json` exists here and whether anything —
  `doctor`, the scaffold assets, `.gitattributes` (`linguist-generated`) —
  reads it.

- [ ] **Step 4: Run the suite and the binary on this repository**

  `go test ./...`, then `agents init` in a scratch repository, then `agents wire`
  in this one, then `agents doctor`.

---

### Task 2: Delete the session-record feature

**Decided: it goes.** The owner's answer is that the record is not wanted. What
remains here is the *scope* of that deletion, which is larger than deleting
`trace`.

**The problem this solves.** `agents hook` is the harness lifecycle entrypoint,
and what it does is write session records: it imports `harness`, `lane`,
`machine`, `record`, `trace` — 867 lines of trace store plus the indexing — to
record that a session started, a subagent spawned, or a session stopped. The
record can then be queried (`agents trace ls`), read back (`show`), copied
(`cache`), and pruned. `agents save` exists to commit the `.agents/` paths those
records touch.

Measured, the `doctor` package imports `trace` and `record` and reads the store
directly (`trace.Query`, `checkRecording`, `checkPointers`, `LaneHealth`,
`--lane-*`, `--recording-freshness`). So the record is not a leaf — it runs
through the diagnostic too, and this task carries the largest single cost in the
plan.

**Files (if deleted):**
- Delete: `cmd_trace.go`, `cmd_trace_test.go`, `cmd_trace_migrate.go`,
  `internal/trace/`, `internal/record/`, `internal/lane/`, `internal/pointer/`
- Delete: `cmd_hook.go` or reduce it to a no-op, and `cmd_save.go`
- Modify: `internal/harness/{claudecode,codex}.go` — `Events()` becomes empty and
  `wire` stops writing hook entries
- Modify: `internal/doctor/` — `checkRecording`, `checkPointers`, `LaneHealth`,
  the `--lane-*` and `--recording-freshness` flags, and the `trace`/`record`
  imports go with it. This is the largest single cost of the decision, and the
  plan must say how much of `internal/doctor` (1,611 non-test lines) survives

---

- [ ] **Step 1: Establish what is in the store, so the deletion is honest**

  Before deleting, record what is being given up: does `~/.agents/` (the
  machine-local store) hold records on this machine, and how many? The two Q&A
  entries that motivated the feature are
  `docs/qna/which-harnesses-actually-lose-transcripts.md` and
  `docs/qna/why-are-subagent-transcripts-gone.md`. Read them, write one line in
  the journal entry saying what the feature was for and what was in the store at
  the moment it was removed. Deleting a capability whose purpose nobody wrote
  down is how it gets rebuilt by accident.

- [ ] **Step 2: If deleted, delete it whole**

  Half a record is worse than none: a hook that records without a query surface,
  or a query surface whose store nothing fills, is the "installed but never
  fires" failure this repository keeps writing down. Either `hook` + `record` +
  `trace` + `save` all go, or none does.

- [ ] **Step 3: Reconcile `wire` with an empty event list**

  `wire` writes hook entries per harness `Events()`. With no events it writes
  nothing into `.claude/settings.json` or `.codex/hooks.json` — which means
  `~/.claude/settings.json`'s existing `hooks` section (measured: a
  `SessionStart`, `Stop`, `SubagentStart`, `SubagentStop` block, each calling
  `/opt/homebrew/bin/agents hook …`) must be **removed by the same change**,
  using `wire`'s own narrow `stripOurs`, never by hand. A binary that stops
  supporting `agents hook` while leaving hook entries that call it turns every
  session start into an error.

- [ ] **Step 4: Keep what `.agents/` is still for**

  If the record goes, `.agents/` holds `AGENTS.md`, `hooks.json`, `skills/`, and
  the wire lock. Decide whether `agents save` still has a job; if not, delete it
  and leave `AGENTS.md` and `skills/` to normal commits.

---

### Task 3: `guard`, and the git-hook dispatcher

**The problem this solves.** `agents guard --staged` scans staged `.agents/`
content for secrets, regenerates generated indexes and compares them
byte-for-byte, and warns when a commit mixes agent context with code. It runs as
a git pre-commit hook because `agents` is installed under four hook names
(measured in `bootstrap.d/internal/phase/devtools.go`). If the generated indexes
go away with the layout, `guard`'s main job goes with them.

**Files:**
- Modify or delete: `cmd_guard.go` (66), `internal/guard/` (382)
- Modify: `git/hooks.d/` and `git/templates/hooks/` if the dispatcher changes
- Modify: `bootstrap.d/internal/phase/devtools.go` if the installed hook-name set changes

---

- [ ] **Step 1: Rewrite the check as a repository script** (decided)

  `guard`'s job, with the generated indexes gone, is: scan staged `.agents/`
  content for secrets, and warn when a commit mixes agent context with code. The
  written form goes under `git/hooks.d/`, where the other commit-time checks
  live, and this repository's own tests exercise it. Constraints carried over
  from the current implementation: it must be advisory by default (a warning
  must not abort a commit — `main.go` maps `guard`'s result to success for
  exactly this reason), and it must not depend on the binary being installed.

  The boundary rule this follows, from the superseded plan and the review: a
  pre-commit check that runs in one repository does not need to be a subcommand
  of a fleet tool.

- [ ] **Step 2: Keep the multicall intact**

  `main.go` dispatches on `filepath.Base(os.Executable())`, so a symlink named
  `commit-msg` must resolve to something. If `guard` becomes a script, the
  installed hook-name set in `devtools.go` changes with it — in the same commit,
  or `bootstrap apply` leaves hook names pointing at a binary that no longer
  answers them.

- [ ] **Step 3: Check the release path**

  `script/sync-homebrew-formula.sh` installs `agents` and asserts
  `agents version`. Whatever the surviving command set is, that assertion must
  still hold, and the formula carries no per-command list to update.

---

### Task 4: Whole-change verification

- [ ] **Step 1:** `cd agents && gofmt -d . && go vet ./... && go test -count=1 ./...`
- [ ] **Step 2:** `agents help` — the listing contains exactly the surviving surface, and `grep` for every deleted name across `agents/README.md`, `docs/`, and the generated README block finds nothing.
- [ ] **Step 3:** Fresh directory: `agents init` reaches a usable state and prints the trust steps.
- [ ] **Step 4:** This repository: `agents wire` then `agents doctor` — wiring exact, no reference to a deleted command.
- [ ] **Step 5:** `git commit` in a scratch repository still runs the hook chain end to end.
- [ ] **Step 6:** Measure the result: `find agents -name '*.go' -not -name '*_test.go' | xargs wc -l` against the 13,429 baseline, and record the delta in the journal entry.
- [ ] **Step 7:** `bootstrap.d` still builds an `agents` binary (`phase/devtools.go`) and `bootstrap check` still finds it on `PATH`.

---

## What this plan does not decide

- **The `migrating-fleet-context` skill.** The skills plan asks this plan whether
  `agents` survives; this plan says yes, in reduced form. The skill's fate
  belongs there, together with `agents-tool`, and this plan only removes the
  `agents layout migrate` command that the skill's text names.
- **Whether `doctor` keeps all 17 checks.** Some of them (`lane health`,
  `recording freshness`) belong to the record feature and go with Task 2; the
  rest are read-only reporting and cost little. Task 2 Step 4 says which.
- **The Homebrew distribution decision.** This plan keeps `agents`
  distributable and asserts only `version`.
