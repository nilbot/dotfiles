# Simplification Plan

**Status: EXECUTED 2026-09-21, with deviations recorded below.** The work is on
branch `simplify/agents-single-home` (PR #52). Read this section before the plan
itself: several steps landed differently from what is written further down,
because the writing was a plan and the doing found things a plan could not.

**What the execution taught, in the order it mattered:**

- **The mechanical probe replaced the plan's deletion list.** Deleting the six
  commands frees almost nothing on its own -- `go build` reported two undefined
  symbols, and only one internal package became unreachable. What frees the rest
  is rewriting `init`, `doctor` and the record half of `harness`, which is where
  the plan had guessed at command files.
- **`wire` had to be made strip-only, not merely re-ordered.** The plan's
  "strip before emptying the event list" cannot work: `renderHooksJSON` stripped
  and re-appended in one pass, so the strip is now driven by what a config holds
  rather than by what a binary declares. Details in Task 2's replacement commits.
- **A config whose only content is an empty hook container is not ours to
  delete.** `{"hooks":{"Stop":[]}}` holds no entry of this tool's, and the first
  removal rule deleted it. The rule now asks whether this run removed anything,
  which is the only question that separates "we emptied it" from "it was empty".
- **`checkAntigravityTrust` attached its instruction to a field the renderer
  never prints** (a remedy is printed only for a check that is not ok), so the
  manual step it exists to give reached no report. The step now travels in the
  detail.
- **The `commit-msg` guard stripped one of the two attribution URLs the global
  instruction file quotes.** Found by the acceptance test, fixed, pinned.
- **Skills: the tool installs `recording-what-you-learn` only.** The text it
  installs is the text this repository records, and the two dead asset trees are
  gone.
- **The CI `docs` job named 45 tests that no longer existed.** Its list moved to
  `agents/doctests.txt` so a diff to it is legible, and the job fails by name
  when a listed test is missing or does not run.

**Verification:** `script/accept-agents-simplification.sh` -- 42 checks, 0
failures, including a real commit through the installed hook chain. CI on PR #52
is the final gate.
**Supersedes:** [`2026-09-21-agents-reduction-plan.md`](2026-09-21-agents-reduction-plan.md),
[`2026-09-21-skills-single-home-plan.md`](2026-09-21-skills-single-home-plan.md),
[`2026-09-21-simplification-roadmap.md`](2026-09-21-simplification-roadmap.md),
and Task 1 of
[`2026-09-21-bootstrap-self-sufficiency-plan.md`](2026-09-21-bootstrap-self-sufficiency-plan.md).
Those four were reviewed independently and returned with 8 blockers and 15
cross-plan contradictions. This file replaces them; the superseded ones stay on
disk as the record of what was proposed and why it did not hold.

**Why it was rewritten rather than patched.** Each superseded plan mixed
*decisions* and *facts* in the same prose, and several facts were inference
written as measurement: a duplicate file that was a symlink, hook entries
attributed to the wrong settings file, a `guard` behaviour that had been retired,
two commands named that do not exist, a skill said to diverge when its digest is
identical, and a store path that was guessed. Every one of those is a claim a
single command would have settled. So the rule for this file:

> **Every factual claim is stated with the command that produced it, in the
> Verified Facts section. A claim that cannot be produced by a command is not
> stated here — it is a `[CHECK]` step.**

**Goal:** A repository that is immediately usable by a harness, one home for
every skill and instruction file, and a toolchain with no commands in it that
exist only because they once seemed useful.

---

## Decisions taken

| # | Decision | Taken |
|---|---|---|
| D1 | Session records (`trace`, `hook` recording, `save`) are deleted | owner, 2026-09-21 |
| D2 | `guard` **stays a Go subcommand** (reverses an earlier "make it a script") | owner, 2026-09-21 |
| D3 | The fleet registry and the v2 layout (`layout`, `ls`, `update`, `drift`) are deleted | owner, 2026-09-21 |
| D4 | `blog-editorial-generator` moves to the blog repository | owner, 2026-09-21 |
| D5 | `codebase-memory` is deleted | owner, 2026-09-21 |
| D6 | Machines get skills from `.agents/skills/`; no per-harness home-directory skill trees are maintained | owner, 2026-09-21 |
| D7 | DSH is the reference harness; other harnesses get path aliases only | owner, 2026-09-21 |

---

## Open questions

Marked `[DECIDE]` again at the step that needs them. Each has a recommendation
and the evidence behind it.

| # | Question | Recommendation | Evidence |
|---|---|---|---|
| Q1 | Do the three Codex transcripts in `trace-cache/codex/` get rescued before the store is deleted? | **Rescue them** — they are the only surviving copy | Verified Facts 7 |
| Q2 | Does `agents-tool` survive the reduction? | **Delete it.** It is a router for a large ambiguous command set; the surviving set is small and `agents help` renders the reference | Verified Facts 10 |
| Q3 | The hook chain has two owners (`/opt/homebrew/bin/agents` and `~/bin/agents`). Who owns it? | **Bootstrap**, by passing `--adopt-owned`; the Homebrew formula stops installing the hook chain | Verified Facts 8, 9 |

---

## Global Constraints

1. **`wire` stays a narrow, reversible editor.** `IsOwnedHookCommand` must stay
   narrower than `ResemblesHookCommand`, and `stripOurs` must keep discarding
   only what it recognises. This pair exists because a wider predicate deleted
   entries from a config this repository does not own.
2. **`stripOurs` only runs inside the events loop.** `renderHooksJSON`
   (`internal/harness/harness.go:518-540`) calls it per event; with an empty
   event list nothing is stripped and the old entries are written back. Any
   change that empties `Events()` must therefore strip **first**, in a separate
   deliberate step — verified in Facts 5. This is the constraint the superseded
   plan got wrong.
3. **Refuse, never clobber.** An occupied target is refused, not overwritten.
   This is why re-pointing an existing symlink needs an explicit `--adopt-owned`
   or an explicit `rm`, and why `~/.claude/CLAUDE.md` cannot simply be re-aimed
   by a manifest edit (Facts 11).
4. **`.agents/skills/<name>/SKILL.md`, unchanged in shape.** `agents` reads the
   direct subdirectories of `.agents/skills/` and stats `SKILL.md` inside each.
   Moving a skill means moving its directory whole.
5. **Every step ends green or is not a step.** `cd agents && go test ./...` and
   `cd bootstrap.d && go test ./...` are the gate. A step that leaves a test
   failing because a later step fixes it is not allowed — order the steps so
   each one lands green.
6. **The shim's contract is untouched.** One `exit`, one `exec`, no `${VAR:?}`
   (`bootstrap.d/main_test.go:1423-1477`).

---

## Verified Facts

Every line below was produced by the command shown, on this machine, on
2026-09-21. A later executor should re-run them rather than trust them; where an
output changed, the fact changed.

| # | Fact | Command |
|---|---|---|
| 1 | `agents/` non-test Go = **13,429 lines** | `cd agents && find . -name '*.go' -not -name '*_test.go' \| xargs wc -l \| tail -1` |
| 2 | Both suites are green today | `cd agents && go test ./...` · `cd bootstrap.d && go test ./...` |
| 3 | `.agents/layout.json` does **not** exist on disk or in git; the repository resolves the **implicit v1** layout | `ls .agents/layout.json` · `git ls-files \| grep -c layout.json` |
| 4 | Root `CLAUDE.md` is a **symlink** to `AGENTS.md`, not a duplicate file — and `agents doctor` has a `scaffold:symlink` check that requires exactly that | `ls -la CLAUDE.md` · `sed -n '1213,1227p' agents/internal/doctor/doctor.go` |
| 5 | Existing `agents hook` entries are **repo-local**: `.claude/settings.json` (4, `--harness claude-code`), `.codex/hooks.json` (4), `.agents/hooks.json` (1, antigravity). `~/.claude/settings.json` holds none (its SessionStart calls `cbm-session-reminder`) | `python3 -c "import json;d=json.load(open('.claude/settings.json'));[print(k,h['command']) for k,v in d.get('hooks',{}).items() for g in v for h in g.get('hooks',[])]"` |
| 6 | The machine-local store is `<git-common-dir>/agents` = **`.git/agents`**, not `~/.agents` | `sed -n '82,88p' agents/internal/repo/repo.go` · `ls .git/agents` |
| 7 | The store holds **922 records** (25 with a description) and 272 distinct transcript pointers, of which **165 point at files that are gone**. `trace-cache/codex/` holds 3 files (956 KB) with **no surviving original** under `~/.codex` | `cat .git/agents/traces/*.jsonl \| python3 -c "…"` · `find .git/agents/trace-cache -type f` |
| 8 | The hook-name set is `pre-commit commit-msg post-merge post-checkout`, in **three** places: `git/install-hooks.sh:61`, `agents/internal/githook/githook.go:24-27`, `agents/internal/doctor/doctor.go:685` | `grep -n hook_names git/install-hooks.sh` · `grep -n 'pre-commit' agents/internal/githook/githook.go` |
| 9 | `install-hooks.sh` accepts `--adopt-owned` and uses it to repoint links written for an earlier binary; `bootstrap.d/internal/phase/devtools.go:99` calls the installer **without** it | `sed -n '15,32p;139,168p' git/install-hooks.sh` · `sed -n '97,99p' bootstrap.d/internal/phase/devtools.go` |
| 10 | `claude/skills/agents-tool/SKILL.md` (194 lines) names 17 `agents` invocations, of which 12 are commands this plan deletes. `agents-tool` is **user-owned** (not in `bundledSkills`), and `agents/docs_test.go:145-148` hard-fails if the file moves | `grep -o 'agents [a-z-]*\( [a-z-]*\)\?' claude/skills/agents-tool/SKILL.md \| sort \| uniq -c` · `sed -n '118,141p' agents/internal/scaffold/scaffold.go` |
| 11 | `~/.claude/CLAUDE.md` is a symlink to `claude/CLAUDE.md`; `linkVerdict` refuses a symlink pointing elsewhere, so a manifest edit alone cannot re-aim it | `readlink ~/.claude/CLAUDE.md` · `sed -n '104,113p' bootstrap.d/internal/change/change.go` |
| 12 | `~/.claude/skills` holds **one** per-skill link (`antigravity-handoff`) plus the product's own `synced/`; the product's directory is not ours to manage | `ls -la ~/.claude/skills` |
| 13 | `git/hooks.d/` is generated and untracked (`.gitignore` = `*` + `!.gitignore`); `git/hooks/` is tracked and holds personal hook extras named `<name>.<hook>` (`recent.post-merge`, `recent.post-checkout`), appended by `githook.externalStages` | `cat git/hooks.d/.gitignore` · `git ls-files git/hooks/` · `sed -n '110,146p' agents/internal/githook/githook.go` |
| 14 | `guard` sets `Blocking: true` for unsafe paths and secret findings; `main.go:65-69` maps only OK/Advisory to success | `grep -n 'Blocking' agents/internal/guard/guard.go` · `sed -n '62,69p' agents/main.go` |

### Facts that were asserted in the superseded plans and are **wrong**

Recorded so the executor does not reintroduce them.

| Claim | Reality |
|---|---|
| This repository is on the v2 layout | Implicit v1; no manifest exists (Fact 3) |
| Root `CLAUDE.md` is a byte-identical duplicate | It is a symlink, and `doctor` requires it (Fact 4) |
| Hook entries live in `~/.claude/settings.json` | They are repo-local (Fact 5) |
| The machine-local store is `~/.agents/` and "nobody uses the record" | It is `.git/agents` with 922 records (Facts 6, 7) |
| `guard` regenerates generated indexes | `agents index` was retired 2026-08-20; `guard` does secret/conflict/mode checks |
| `agents fleet` and `agents migrate` exist and have zero references | Neither command exists; the names are `ls`, `update`, `layout migrate`, `trace migrate` |
| `agents-tool` and `migrating-fleet-context` diverge from their assets | Both are byte-identical to the v1 assets (`md5 e3fb5bf3…`, `c8505770…`) |
| `internal/layout` has 1,603 lines of tests | 3,562; 1,603 is `cmd_layout_test.go` |
| Re-pointing `~/.claude/CLAUDE.md` works by editing the manifest | `linkVerdict` refuses it (Fact 11) |

---

## Sequence

Each task lands green. The order is forced by three couplings: the hook chain is
a contract with the installer; `stripOurs` must run before `Events()` empties;
and `agents/docs_test.go` scans the skill trees.

```
Task 1  hook chain ownership        (unblocks every `apply workstation` step)
Task 2  delete the session record   (strip first, then empty Events)
Task 3  delete fleet registry + v2 layout + drift
Task 4  agent skills: what survives
Task 5  machine-global instructions
Task 6  the Go path
```

---

### Task 1: One owner for the hook chain

**Why first.** `bootstrap.d/internal/phase/devtools.go:99` calls
`git/install-hooks.sh install` without `--adopt-owned`. The four hook names in
`git/hooks.d/` point at `/opt/homebrew/bin/agents`, so the installer refuses
(§7 item 2 of the boundary review) and `bootstrap apply workstation` cannot
complete. Every later task's verification runs that command, so this goes first.

**Files:**
- Modify: `bootstrap.d/internal/phase/devtools.go` (pass `--adopt-owned`)
- Modify: `Formula/agents.rb` and `script/sync-homebrew-formula.sh` if the
  formula installs the hook chain

- [ ] **Step 1: Pass `--adopt-owned` from the devtools phase**

  Settled while writing this plan, so it is a change and not a question:
  `Formula/agents.rb` names no hook and no installer (`grep -n 'hook\|install-hooks'
  Formula/agents.rb` returns nothing), so **bootstrap is the only writer of the
  hook chain** and `install-hooks.sh` already supports adopting its own earlier
  links (Fact 9). The change is one argument where `devtools.go:99` calls the
  installer.

  Verify: `./bootstrap apply workstation` reaches `verify`, and the four names
  under the resolved `core.hooksPath` point at `~/bin/agents` rather than
  `/opt/homebrew/bin/agents`.

- [ ] **Step 2: Record the ownership in the boundary review**

  §7 item 2 is answered: bootstrap owns the hook chain, the formula does not.
  Update the review rather than leaving the item open. This is the same
  one-owner-per-resource rule the review's §6 already states, so the finding is
  that the machine had drifted from the rule, not that the rule was wrong.

---

### Task 2: Delete the session record

**Decided (D1).** The feature, its store and its query surface go. Ordering is
the whole risk: `stripOurs` runs only inside the per-event loop (Constraint 2),
so the existing entries must be removed while `Events()` is still non-empty.

**Files:**
- Delete: `cmd_trace.go`, `cmd_trace_test.go` (462 + 935),
  `cmd_trace_migrate.go` (122), `cmd_save.go` (125),
  `internal/trace/` (2,520 + 1,653), `internal/record/` (237 + 122),
  `internal/lane/` (102 + 60), `internal/pointer/` (167 + 110)
- Modify: `commands.go` (entries for `trace`, `trace migrate`, `save`),
  `cmd_hook.go`, `internal/harness/harness.go` (record half),
  `internal/harness/antigravity.go`, `internal/doctor/` (trace/record half),
  `cmd_init.go` (its "run `agents trace ls`" line and the recording TrustSteps)
- Modify: `agents/main.go` if the dispatcher names a deleted command

---

- [ ] **Step 1: Rescue what is irreplaceable** `[DECIDE Q1]`

  Three Codex transcripts in `trace-cache/codex/` have no surviving original
  (Fact 7). Recommended: move them somewhere durable before anything is deleted,
  and say in the journal entry what they are. Everything else in the store is a
  pointer index whose pointers are 60% dead — it goes with the code.

- [ ] **Step 2: Strip the existing hook entries while `Events()` still fires**

  Run `agents wire` **before** `Events()` is emptied. `stripOurs` removes the
  `agents hook` entries from `.claude/settings.json` and `.codex/hooks.json`
  because they match the narrow predicate; antigravity's
  `.agents/hooks.json` is stripped unconditionally
  (`antigravity.go:44-49`). Verify with the Fact 5 command that all three
  configs are clean **before** touching `Events()`.

  This step is the one the superseded plan got wrong: emptying `Events()` first
  makes `stripOurs` unreachable and the entries survive forever.

- [ ] **Step 3: Empty `Events()` and delete the record code**

  Now the loop has nothing to strip, which is correct because Step 2 already
  did. Delete the packages in the Files list, then `go build ./...` and
  `go test ./...`.

- [ ] **Step 4: Remove the store from this machine**

  After the code is gone: `.git/agents/` (922 records, the index, the queue).
  This is a machine step, not a commit — the store is untracked by design.
  Record in the journal what was in it and the count from Fact 7.

- [ ] **Step 5: Answer the two motivating Q&A entries**

  `docs/qna/which-harnesses-actually-lose-transcripts.md` and
  `docs/qna/why-are-subagent-transcripts-gone.md` explain why the feature was
  built. Add a dated follow-up to each saying it was removed and what replaced
  it (the recording skill writing knowledge into `docs/`), so the reasoning is
  not re-derived later.

---

### Task 3: Delete the fleet registry and the v2 layout

**Decided (D3).** Both are a second product. The registry writes machine state
outside a checkout (`cmd_init.go:207` writes `~/.local/state/agents/registry.json`),
which the boundary review's rule 3 forbids; the v2 layout is a store-role
manifest plus a `git mv`-based migration.

**Files:**
- Delete: `cmd_layout.go`, `cmd_layout_test.go` (1,156 + 1,603),
  `cmd_fleet.go`, `cmd_fleet_test.go` (217 + 563),
  `cmd_drift.go`, `cmd_drift_test.go` (180 + 304),
  `internal/layout/` (2,208 + 3,562), `internal/registry/` (927 + 567),
  `internal/drift/` (2,996 + 1,700), `internal/machine/` (289 + 177)
- Modify: `commands.go`, `cmd_init.go` (drop `--template`/`--stores`/`--archive`
  and the registry write), `internal/doctor/` (its layout/drift half,
  `checkScaffold`, `checkQNAFreshness`, the four `layout*Check` functions),
  `internal/scaffold/` (drop the v2 asset tree and manifest writer),
  `agents/README.md`, **root `README.md`** (the generated command block)
- Modify: `agents/docs_test.go` — it imports `layout`, `drift` and `scaffold`,
  keeps `livingDocuments`, and requires a three-level command to exist
  (`trace cache prune` is the only one and Task 2 deletes it)

---

- [ ] **Step 1: Decide what `init` creates now** `[CHECK]`

  Without a layout schema, `--template`/`--stores`/`--archive` have no consumer.
  Recommended: the four stores stay implicit constants (`docs/{design,plans,journal,qna}`),
  which is the implicit v1 layout the repository already uses (Fact 3). Write
  the answer in this file before Step 2.

- [ ] **Step 2: Delete the commands and packages**

  The check that the deletion is closed: after this step `go list -deps` on the
  binary must not mention `layout`, `registry`, `drift` or `machine`.

- [ ] **Step 3: Fix the tests that reference the deleted surface**

  Known set: `agents/docs_test.go` (imports + `livingDocuments` + the
  three-level-command requirement), `agents/commands_test.go` (exact command
  set), `agents/exitcode_doc_test.go` (help text for `trace`/`hook`),
  `agents/cmd_doctor_test.go`, `agents/cmd_drift_test.go` (deleted),
  `agents/internal/scaffold` layout tests, `agents/internal/registry` tests
  (deleted with the package), and the root README's generated block.

- [ ] **Step 4: Update the root README and the design docs**

  The generated block in root `README.md` is checked by
  `TestReadmeCommandBlockIsCurrent`; `.agents/AGENTS.md` requires it in the same
  change set. Then `docs/design/2026-08-07-spec-2-dotfiles-hygiene.md` and the
  boundary review for any prose naming a deleted command.

---

### Task 4: Which agent skills survive

**Decided (D4, D5, Q2).** After Tasks 2 and 3 the binary is `init`, `wire`,
`doctor`, `guard`, `hook`-less, `version`, `help`. The skills must match that.

**Files:**
- Archive: `claude/skills/antigravity-handoff/` (54),
  `gemini/skills/session-handoff/` (82 + `scripts/`),
  `claude/skills/agents-tool/` (194), `claude/skills/codebase-memory/` (63)
  → `docs/archive/skills/`
- Move: `gemini/skills/blog-editorial-generator/` (96) → the blog repository
- Keep: `.agents/skills/recording-what-you-learn/SKILL.md` (byte-identical to
  the binary's canonical text), `.agents/skills/migrating-fleet-context/SKILL.md`
- Modify: `agents/docs_test.go` — `TestHarnessSkillCoversAgentCommands`
  hard-codes `claude/skills/agents-tool/SKILL.md` and `t.Fatalf`s if it is
  absent; `TestLivingDocumentsNameOnlyRealCommands` scans both skill trees
- Modify: `bootstrap.d/links.manifest` (delete the `claude/skills` and
  `gemini/skills` rows)

---

- [ ] **Step 1: Confirm the survivor list against the reduced command set** `[DECIDE Q2]`

  Recommended: keep only `recording-what-you-learn` and
  `migrating-fleet-context`; archive the rest. The reasoning, recorded here so it
  is not re-litigated:

  - **`migrating-fleet-context`** — its own reason to exist was that a person
    (or an LLM) had to perform a migration by hand. `scaffold.RefreshInfrastructuralSkills`
    now writes the asset mechanically (`scaffold.go:381-413`) and
    `agents update` calls it, so the skill would be teaching an agent to do
    something the binary does without it.
  - **`agents-tool`** — it is a router: the test that guards it exists because a
    reference listing *what* the commands are did not prevent twenty sessions
    from producing no handoff, so it answers *which* command a situation needs.
    That judgement was worth a skill over a 17-command surface with a
    store-role manifest in it. Over the reduced surface, `agents help` renders
    the reference and the remaining choices are not ambiguous (Fact 10).
  - **`recording-what-you-learn`** — the one skill whose instruction is not
    about the binary at all, and the one measured as actually changing
    behaviour.

- [ ] **Step 2: Archive and move**

  Whole directories to `docs/archive/skills/`, with a one-line README saying what
  the directory is for. `blog-editorial-generator` goes to the blog repository —
  it generates posts for one Hugo site, and a global skill is advertised to every
  session in every repository.

- [ ] **Step 3: Delete the two manifest rows and the emptied sources**

  Rows 20 and 22 of `bootstrap.d/links.manifest`. Note that neither source
  directory is empty at this point — Task 4 Step 2 emptied them.

- [ ] **Step 4: Clean this machine's leftovers**

  `~/.claude/skills/antigravity-handoff` is the one per-skill link that exists
  (Fact 12); `~/.gemini/skills` is a symlink to a directory this task removes.
  Remove both. The product's own `~/.claude/skills/synced/` is not touched,
  inspected or reported.

- [ ] **Step 5: Update the tests and the records**

  `agents/docs_test.go`'s two skill tests; the incident journal's stale sentence
  claiming `links.manifest` declares `~/.codex/skills`; boundary review §7 item 1
  marked *handled as: the repository stopped claiming the path*; the Q&A
  `why-is-claude-skills-a-real-directory.md` gets a dated follow-up so it stops
  telling readers that `plan` is blocked.

---

### Task 5: One home for machine-global instructions

**Decided (D6, D7).** The rules that apply in every repository live in
`claude/CLAUDE.md`, reachable only at `~/.claude/CLAUDE.md`. DSH reads a
user-global file at `$DSH_HOME/AGENTS.md`, which does not exist — so a DSH
session in any repository but this one gets no global rules at all.

**Files:**
- Create: `global/AGENTS.md` (or keep `claude/CLAUDE.md` as the source and accept
  the name — Step 1 decides)
- Modify: `bootstrap.d/links.manifest`, `bootstrap.d/internal/change` (nothing,
  if the alias rows are links)
- Modify: `agents/install_hooks_test.go:1205` (reads `claude/CLAUDE.md` and
  `t.Fatal`s if it is gone), `git/README.md` (lines 4, 87, 93),
  `docs/design/2026-08-19-knowledge-is-documentation.md:182`,
  `docs/design/2026-08-11-spec-5-verification-gate.md:565,573`,
  `agents/docs_test.go:79` (assembles the path with `filepath.Join`)

---

- [ ] **Step 1: Decide the source path** `[CHECK]`

  Two options, and the second is cheaper:

  - Move the content to a harness-neutral `global/AGENTS.md`. Honest name, six
    files to sweep, and DSH's project chain never descends into `global/`.
  - Keep `claude/CLAUDE.md` as the source and document that it holds rules every
    harness reads. No sweep, but the name contradicts the content.

- [ ] **Step 2: Add the alias rows**

  `$DSH_HOME/AGENTS.md` is the path DSH reads; `~/.codex/AGENTS.md` currently
  holds a hand-made language rule that is in no dotfiles file at all and would
  not survive this machine — fold it in. **No trailing comments in
  `links.manifest`**: it parses exactly four whitespace-separated fields.

- [ ] **Step 3: Re-point `~/.claude/CLAUDE.md` explicitly**

  `linkVerdict` refuses a symlink pointing elsewhere (Fact 11), so this needs an
  explicit `rm` of the old link before `apply`, exactly as Task 4 Step 4 does for
  the skill links. A manifest edit alone will exit 2.

- [ ] **Step 4: Verify from a repository that is not this one**

  The whole point is cross-repository reach. A session in another repository must
  receive the rules; verify the same for the harnesses that have aliases.

- [ ] **Step 5: Only then retire the old copy**

  `claude/CLAUDE.md` is deleted after the new home is confirmed to arrive, never
  before — until then it is the only copy of the recording rule on this machine.

---

### Task 6: The Go path

Independent of Tasks 1–5; can run in parallel. This is Task 2 of the superseded
self-sufficiency plan, whose four gaps remain valid. Two corrections to it, both
measured:

- `TestMissingGoRefusesWithTheInstallCommand`
  (`bootstrap.d/main_test.go:1185-1205`) uses `runShimEnv`, which **warms** the
  cache. Reordering the Go check inside the build branch makes it exec the cached
  binary and exit 0, not the asserted 2. The test must become a cold-cache case.
- The proposed `TestGoOutsidePathIsFoundAndUsed` puts its stub at
  `${HOME}/.local/share/go/bin/go`, but `find_go` checks `/opt/homebrew/bin/go`
  first and that path exists here, so the stub is shadowed. The hermetic recipe:
  drive the shim with a stub `uname` and a `PATH` of
  `<stubdir>:/usr/bin:/bin`, which makes the fall-through arm reachable
  deterministically — measured, and it produces the expected hint with exit 2.

---

## What this plan does not decide

- **Whether `agents` should keep distributing into per-repo paths.** It has a
  per-harness adapter and a generated `.git/info/exclude` entry. That is the one
  legitimate place a harness-specific path exists.
- **The Homebrew formula's command surface.** It asserts only `agents version`
  and carries no per-command list, so it needs no change here.
- **`.codex/skills`.** A real directory holding only the product's own
  `.system/`; the repository has no `codex/` source directory. Nothing to do.

## Follow-up, 2026-09-21

**Two files this plan names were deleted while it was being executed.**

- `Formula/agents.rb` no longer exists in this repository. Task §"Step 1"
  (`:189`) and its quoted repro `grep -n 'hook\|install-hooks' Formula/agents.rb`
  therefore cannot run; the grep exits 2. The conclusion the step recorded is
  unaffected and is the reason it is safe to leave this as a record: the formula
  names no hook and no installer, so `bootstrap.d`'s devtools phase is the only
  writer of the hook chain. The formula now lives only in
  `nilbot/homebrew-tap`, and `script/sync-homebrew-formula.sh` points it at each
  release — spec 6 §5.2 is the authority.
- `script/sync-homebrew-formula.sh` still exists but does something different:
  it reads the tap's formula and rewrites its four `url` and four `sha256` lines
  instead of rendering a formula from this repository's copy and pushing it over
  the tap's.

The rest of the plan is a dated record and is not rewritten.
