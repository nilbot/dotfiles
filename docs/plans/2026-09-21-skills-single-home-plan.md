# Skills Single Home Implementation Plan

> **SUPERSEDED 2026-09-21 by
> [`2026-09-21-simplification-plan.md`](2026-09-21-simplification-plan.md).**
> This plan was reviewed independently and returned with blockers: its central
> measurement was false (`migrating-fleet-context` does not diverge — it is
> byte-identical to the v1 asset), it claimed no test needed changing while
> `agents/docs_test.go:145-148` hard-fails the moment `agents-tool` moves, its
> alias row would be refused by the existing `~/.claude/CLAUDE.md` symlink, and
> its Files list contradicted its own steps. Kept as the record. **Do not
> execute.**

**Status: superseded — see the banner above.**

**Goal:** Make `.agents/skills/` the only place this repository keeps a skill,
and make the machine-global instructions exist once instead of once per harness
— so that what a session reads is the same file no matter which harness it is,
and nothing has to be kept in sync by hand.

**Architecture:** This plan is almost entirely subtraction. Three things that
exist are removed, one directory becomes the source, and the per-harness
entry points that remain are symlinks that carry no content of their own.

- **Task 1** moves the four surviving skills that are not yet in
  `.agents/skills/` into it, deletes the two manifest rows that install them
  per-harness, and leaves `~/.claude/skills` and `~/.gemini/skills` to the
  machine and the product.
- **Task 2** moves the machine-global rules out of `~/.claude/CLAUDE.md` into a
  harness-neutral file, and makes every harness path a link to it. The rules
  themselves do not change; only where they live.
- **Task 3** deletes the two handoff skills and the duplicate root `CLAUDE.md`.
- **Task 4** verifies all of it on this machine.

**Target harness: DSH.** DSH already reads this repository's conventions
natively — `AGENTS.md`, `.agents/AGENTS.md`, `.agents/skills/`, `docs/` — with
no wiring step. That is why the work below is deletion rather than adaptation:
the conventions were never DSH-specific, the mirrors were.

Other harnesses stay supported only at the level that costs nothing — a path
alias, a symlink. Nothing in this plan adds a variant, and no task is written
for a harness that needs its own copy of anything.

**Deliberately out of scope:**
- The shim's Go handling (`bootstrap`'s four gaps: the check runs too early, it
  only looks on `PATH`, the hint names an installer this machine may not have,
  and the fall-through names no tarball). It is orthogonal to this plan and gets
  its own.
- Whether the `agents` binary keeps its own scaffolding behaviour. Task 1 reads
  that behaviour; it does not change it.
- `~/.codex/skills`. It is a real directory holding only the product's own
  `.system/`, no repository mirrors it, and the repository has no `codex/`
  source directory. Nothing to do.

**Supersedes:** [the bootstrap self-sufficiency
plan](2026-09-21-bootstrap-self-sufficiency-plan.md), whose Task 1 proposed a new
manifest kind (`linkdir`) so that a declaration could share a directory with a
program that writes there. This plan reaches the same blocker by deleting the
declaration instead. The superseded plan is kept as the record of why the kind
was once thought necessary.

**Spec / evidence:**
[spec 2 §6](../design/2026-08-07-spec-2-dotfiles-hygiene.md) (the manifest, one
owner per path) ·
[the boundary review](../design/2026-09-20-agents-and-bootstrap-boundary.md) §7
item 1 · [the incident
journal](../journal/2026-09-19-harness-skills-wrote-into-the-checkout.md) ·
[why is `~/.claude/skills` a real
directory](../qna/why-is-claude-skills-a-real-directory.md)

---

## Global Constraints

Properties the existing code and suite enforce. A change that breaks one is
wrong even if its own tests pass.

1. **`.agents/skills/<name>/SKILL.md`, unchanged in shape.** `agents` resolves
   skills by reading the direct subdirectories of `.agents/skills/` and
   stat'ing `SKILL.md` inside each (`internal/drift/drift.go`). Nothing
   validates the shape, so a rename or a regrouping is not an error — the skill
   simply stops being seen. Moving a skill means moving its directory whole;
   the file keeps its name. (The *content* of the two infrastructure skills is
   compared separately, by digest — constraint 5.)
2. **One owner per path.** A path another program writes into is not declared.
   `~/.claude/skills` is the product's: it holds that product's own skill
   catalogue and a lock file, and its contents change without this repository's
   involvement. This is spec 2 §6.1 applied, not an exception to it.
3. **A declaration and the file it names ship in the same commit.** Deleting a
   manifest row and deleting the source directory are one change, and so is
   adding a skill to `.agents/skills/`.
4. **The shim's contract is untouched.** One `exit`, one `exec`, no `${VAR:?}`
   — `TestShimHasExactlyTwoWaysOut` reads `bootstrap` as text. No task here
   edits `bootstrap`.
5. **The two infrastructure skills stay current.** `agents doctor` compares
   `recording-what-you-learn` and `migrating-fleet-context` against the text
   embedded in the installed binary. `recording-what-you-learn` currently
   matches the canonical asset exactly (md5 `c8505770…`); the consolidation
   must not disturb it. A divergence is reported as `carries repository
   customizations` at status OK — tolerated, not desired.
6. **An instruction file a harness reads must be verified, not assumed.**
   Whether a given harness reads a given path is a fact about that harness, and
   this machine's version is the only one that counts. Task 2 and Task 3 each
   contain a step whose whole purpose is to observe the behaviour before
   relying on it.

Verification commands, from the repository root:

```sh
cd bootstrap.d && gofmt -d . && go vet ./... && go test -count=1 ./...
agents doctor
./bootstrap plan dotfiles       # must exit 0
ls -a .agents/skills/           # the shape constraint, by eye
```

### Decisions this plan needs before it can be executed

Marked `[DECIDE]` again at each site, each with the plan's recommendation and
the evidence behind it. None of them change the target state; they change what
gets written down as the reason.

| # | Decision | Recommendation | Where |
|---|---|---|---|
| 1 | Which skills become global | Four: `agents-tool`, `codebase-memory`, `recording-what-you-learn`, `migrating-fleet-context`. `blog-editorial-generator` goes to the blog repository instead | Task 1 Step 1 |
| 2 | `migrating-fleet-context`'s divergence from the binary's asset | Keep the repository's customizations — this is the case the binary names and tolerates by name | Task 1 Step 1 |
| 3 | The links left inside `~/.claude/skills` | Remove the four links; leave the product's own files untouched | Task 1 Step 5 |
| 4 | Which global instruction path each harness reads | `$DSH_HOME/AGENTS.md` — the path DSH documents, not a guess | Task 2 Step 1 |

---

### Task 1: One home for skills

**The problem this solves.** `~/.claude/skills` is a real directory owned by
the product — it holds that product's synced catalogue and a lock file — and
`bootstrap.d/links.manifest` still declares it as `link`, which makes
`bootstrap plan|apply` refuse and stop at `config`. The path was declared back
when the repository's skills had nowhere else to live. They have somewhere else
now: `.agents/skills/`, which is what every command in the `agents` binary
reads, writes and validates.

**Why deletion rather than a new kind.** The blocker is that a declaration
claims a path another system writes into. Adding a kind that can express "we own
these entries inside someone else's directory" answers a different question —
*how may a shared directory be declared* — and leaves the declaration standing.
The question that removes the blocker is whether the repository needs to write
into `~/.claude/skills` at all. It does not: a skill belongs in
`.agents/skills/`, and a harness that wants to find skills there gets told once,
by the `agents` binary, at the path that harness reads.

**Files:**
- Move: `claude/skills/{agents-tool,codebase-memory,recording-what-you-learn}/`
  and `gemini/skills/blog-editorial-generator/` → `.agents/skills/`
  (the other two `claude/skills/`+`gemini/skills/` entries are Task 3's)
- Modify: `bootstrap.d/links.manifest` (two rows deleted)
- Modify: `bootstrap.d/internal/change/planner_test.go` (a stale comment)
- Modify: `docs/design/2026-08-07-spec-2-dotfiles-hygiene.md` (§6)
- Modify: `docs/design/2026-09-20-agents-and-bootstrap-boundary.md` (§7 item 1)
- Modify: `docs/qna/why-is-claude-skills-a-real-directory.md`

---

- [ ] **Step 1: Decide the final skill set, on paper, before moving anything**
  `[DECIDE 1]` `[DECIDE 2]`

  Measured while writing this plan — twelve `SKILL.md` files in the checkout,
  in four groups:

  | Location | Count | What it is |
  |---|---|---|
  | `.agents/skills/` | 2 | the live skills: `migrating-fleet-context` (367 lines), `recording-what-you-learn` (96) |
  | `agents/internal/scaffold/assets/skills/` | 4 | the **distribution** copies `agents init` writes into other repositories, versioned by layout schema (each of the two has a `v1/` variant) |
  | `claude/skills/` | 4 | `agents-tool` (194), `antigravity-handoff` (54), `codebase-memory` (63), `recording-what-you-learn` (96) |
  | `gemini/skills/` | 2 | `blog-editorial-generator` (96), `session-handoff` (82) |

  Two facts decide most of this step, both measured:

  - **`recording-what-you-learn` exists three times, and one pair is
    byte-identical.** `.agents/skills/recording-what-you-learn/SKILL.md` and
    `agents/internal/scaffold/assets/skills/recording-what-you-learn/v1/SKILL.md`
    are the same bytes (md5 `c8505770…`), and that is also what the installed
    binary embeds. The `claude/skills/` copy differs by frontmatter only. So
    the `.agents/skills/` copy is the one to keep and it must move without its
    content being touched — constraint 5.
  - **`migrating-fleet-context` is the opposite case.** It is 367 lines against
    an asset of a different digest, which `agents doctor` reports as `carries
    repository customizations` at status OK. Decision 2 is whether to keep that
    (recommended: the text is this repository's migration narrative, and a
    future `agents update --all --apply` is already guarded by the drift
    machinery) or re-align it with the asset. Either way, record which.

  Recommended outcome — four skills become the machine's global set, and they
  all live in `.agents/skills/`:

  | Skill | Comes from | Why it is global |
  |---|---|---|
  | `recording-what-you-learn` | already there | the rule that travels with every repository; also the binary's canonical text |
  | `migrating-fleet-context` | already there | `agents doctor`'s remedy text names it by name |
  | `agents-tool` | `claude/skills/` | describes a command available in every repository |
  | `codebase-memory` | `claude/skills/` | a graph tool that is not repository-specific |

  `blog-editorial-generator` is the odd one and the recommendation is **not** to
  make it global: it generates posts for one specific Hugo site. A global skill
  is advertised to every session in every repository — the cost of a wrong entry
  here is paid everywhere. It should move to the blog repository, and if that
  repository wants it visible, `agents init` there is the mechanism. Whether to
  do that in this plan or record it as a follow-up is the decision; the plan
  must not silently delete it.

  Output: the table above, confirmed or corrected, in place of this text.

- [ ] **Step 2: Move the survivors into `.agents/skills/`**

  Whole directories, `SKILL.md` in place, no content edits — so the move is
  verifiable by digest:

  ```sh
  md5 -q .agents/skills/recording-what-you-learn/SKILL.md   # before and after
  git mv claude/skills/agents-tool        .agents/skills/agents-tool
  git mv claude/skills/codebase-memory    .agents/skills/codebase-memory
  # claude/skills/recording-what-you-learn/ is NOT moved: .agents/skills/ already
  # has it, byte-identical to the binary's canonical text (constraint 5).
  ```

  Then `ls -a .agents/skills/` and check every entry is `<name>/SKILL.md`.

- [ ] **Step 3: Delete the two manifest rows, and their sources' siblings**

  Delete `links.manifest` lines 20 and 22 (`claude/skills` → `.claude/skills`,
  `gemini/skills` → `.gemini/skills`), and with them the now-emptied source
  directories. Same commit (constraint 3).

  Note what is *not* deleted here: `claude/skills/antigravity-handoff/` and
  `gemini/skills/session-handoff/` are Task 3's, so this step may leave those
  two directories standing under a `claude/skills/` that no manifest row names.
  That is fine for exactly one commit, and it is why Task 3 follows immediately
  rather than being folded in: each commit stays self-consistent, and the
  removal of a skill stays separable from the removal of a declaration.

- [ ] **Step 4: Confirm no test needs changing, and fix the one stale comment**

  Checked while writing this plan: `TestRealManifestIsWellFormed`
  (`internal/manifest/manifest_test.go:115`) asserts only that the manifest
  parses, is non-empty, and has no duplicate targets per platform — **no row
  count, no row identity**, so it passes unchanged. The row count at
  `manifest_test.go:22` belongs to a different test over a literal fixture.
  `TestLinkAcceptsADirectorySource` builds its own source and only *mentions*
  the deleted row in a comment; the test still stands, the comment goes stale.

  So this step is: correct that comment
  (`internal/change/planner_test.go:235`), and grep once more for the two paths
  in case anything was added between this plan and its execution.

- [ ] **Step 5: Remove the leftover global installs on this machine** `[DECIDE 3]`

  Measured state: `~/.claude/skills` is a real directory holding the product's
  own catalogue (`synced/` with its `manifest.json` and `.bucket-*` lock file)
  plus four per-skill links pointing into this checkout's `claude/skills/`;
  `~/.gemini/skills` is a whole-directory symlink to `gemini/skills`.

  After Steps 2–3, all five of the links this repository put there dangle.
  Recommended: remove those five, and only those five —

  ```sh
  # the four entry links, one per source skill
  rm ~/.claude/skills/agents-tool ~/.claude/skills/codebase-memory \
     ~/.claude/skills/recording-what-you-learn ~/.claude/skills/antigravity-handoff
  rm ~/.gemini/skills
  ls -a ~/.claude/skills/     # must still show synced/ and nothing of ours
  ```

  The product's own files under `~/.claude/skills/` are **not touched, not
  moved, not inspected further** — that directory is the product's, and this
  step is the last time this repository needs to look at it.

  This step is a prerequisite for Step 6, not a cleanup afterthought: a
  `~/.gemini/skills` that is a dangling symlink still *exists*, so a later
  `link` row would refuse it as an occupied target. Removing it here is what
  makes re-installing at a new path possible without a second manual repair.

- [ ] **Step 6: Run it on this machine**

  ```sh
  ./bootstrap plan dotfiles       # exit 0 — this is the blocker cleared
  ./bootstrap plan workstation    # must get past the config phase
  ./bootstrap apply workstation
  ./bootstrap check               # no manifest-kinds fault
  ls -a .agents/skills/           # <name>/SKILL.md, every entry
  agents doctor                   # the two infrastructure skills at expected status
  ```

  If `plan workstation` stops somewhere other than `config`, that is a
  different defect: record it and do not fix it inside this task.

- [ ] **Step 7: Record the decision**

  In the review, §7 item 1 is marked *handled as: the repository stopped
  claiming the path*. In the Q&A entry, a dated follow-up so it stops telling a
  reader that `plan` is blocked. The incident journal is not rewritten — its
  measurements are evidence.

  Two corrections belong in this step, both found while writing this plan:
  - The journal's third open question says *"`links.manifest` also declares
    `~/.gemini/skills` and `~/.codex/skills`"*. It does not declare
    `~/.codex/skills` and never did; there is no `codex/` source directory in
    the repository. Correct the sentence rather than deleting the paragraph.
  - The journal's "the writer is not identified with certainty" is **answered
    by this plan's outcome**: the writer never needed naming, because the
    repository stopped claiming the path. Say that, so the question does not
    stay open as unfinished work.

- [ ] **Step 8: Commit**

---

### Task 2: One home for machine-global instructions

**The problem this solves.** The rules that apply in every repository on this
machine live in `claude/CLAUDE.md`, reachable only at `~/.claude/CLAUDE.md`. A
session in another harness, in another repository, does not read them — so the
one rule described as having actually changed behaviour ("record when the work
hits something") is installed at exactly one harness's path. A second partial
copy sits at `~/.codex/AGENTS.md`, holding a language rule that is in no
dotfiles file at all and would not survive this machine.

**Ordering is a safety property, not a preference.** `claude/CLAUDE.md` is the
*only* copy of those rules today, so every step that removes it comes after the
new home exists **and has been observed to arrive**. A plan that empties the old
file early would uninstall the recording rule for the duration of Task 1 and
Task 3, and nothing would report it.

**Files:**
- Create: `global/AGENTS.md` in the checkout — the one copy of the rules, at a
  path whose name does not claim a harness
- Delete: `claude/CLAUDE.md` (Step 5, after the new home is verified)
- Modify: `bootstrap.d/links.manifest` (the Claude row re-points; two alias rows)
- Modify: `agents/install_hooks_test.go` — `TestTask18RetiresTemplateAndClaudeHookInstallers`
  reads `claude/CLAUDE.md` and fails with `t.Fatal` if it is absent; it must
  follow the file to its new path
- Modify: `git/README.md` (two places), `docs/design/2026-08-19-knowledge-is-documentation.md`,
  `docs/design/2026-08-11-spec-5-verification-gate.md`,
  `bootstrap.d/links.manifest`, and the file's own opening paragraph — every doc
  that names the path (the full list is produced in Step 2)

---

- [ ] **Step 1: Confirm the path DSH reads** `[DECIDE 4]`

  Answerable from the installed software rather than by experiment, so do that
  first. `@deepseek-ai/dsh-agent-instructions` renders a baseline from
  **`$DSH_HOME/AGENTS.md`** (the user-global file), then the project chain from
  the project root down to the session directory, where the candidates are
  `AGENTS.md` and `CLAUDE.md` plus the `.local.md` overlays. On this machine
  `DSH_HOME=/Users/nilbot/.dsh` and **`~/.dsh/AGENTS.md` does not exist** — so
  today a DSH session in any repository other than this one receives no global
  rules at all, which is the gap this task closes.

  The experiment is still worth running, because it is cheap and it is the
  general principle (constraint 6): put a marker in `$DSH_HOME/AGENTS.md`, open
  a session in a repository that is not this one, and confirm it arrives. Record
  the observed paths for the other harnesses beside it; they are recorded as
  observed, not as required.

- [ ] **Step 2: Create `global/AGENTS.md` and move the rules into it**

  Three sections moved verbatim from `claude/CLAUDE.md`: no AI attribution,
  recording what you learn, multi-line commit messages. This step moves text
  and changes nothing in it, so that the diff of the *rules* stays legible.
  Exactly one content edit is known to be needed, and it gets its own commit:

  - "Two stores, unless a repository's own **`CLAUDE.md`** names different ones"
    → `AGENTS.md`. The rule is about knowledge stores and has nothing to do with
    one harness. Nothing else changes — the attribution section names Claude
    Code on purpose and stays as written.

  **Why the source moves, and not just its name.** DSH loads the project
  instruction chain from the project root *down to the session directory*, so a
  global-rules file sitting in a nested directory is also, silently, a project
  instruction file for any session opened there. `global/` is chosen so that
  (a) nothing opens a session in it, and (b) its name does not claim a harness —
  `claude/CLAUDE.md` holding rules that every harness reads is the kind of
  contradiction this plan exists to remove.

  **The path is load-bearing in six files.** Measured while writing this plan:
  `git/README.md` (lines 4, 87, 93), `docs/design/2026-08-19-knowledge-is-documentation.md:182`,
  `docs/design/2026-08-11-spec-5-verification-gate.md:565,573`, the file's own
  opening paragraph, `bootstrap.d/links.manifest:21`, and — the one that is not
  docs — `agents/install_hooks_test.go:1205`, where
  `TestTask18RetiresTemplateAndClaudeHookInstallers` reads the file and calls
  `t.Fatal` if it cannot. That test is why this move cannot be a rename done
  quietly: it will fail loudly, which is the good outcome, but only if the path
  is swept in the same commit. Grep for `claude/CLAUDE.md` again at execution
  time; the list above is a measurement, not a guarantee.

- [ ] **Step 3: Fold in the language rule from `~/.codex/AGENTS.md`**

  It is the only rule on this machine that exists outside dotfiles: one
  paragraph, "if an instruction's core idea is expressed in language X, think
  and respond in X". It is stated harness-neutrally and belongs in the shared
  file, marked as such if it is not universally wanted; `~/.codex/AGENTS.md`
  becomes one of the aliases in the next step. Decide whether it applies to
  every harness or only where a harness has no language rule of its own, and
  write the answer down — it is the one rule whose scope is genuinely arguable.

- [ ] **Step 4: Add the alias rows, and apply**

  Each harness path becomes a `link` row whose target is the new file, so only
  the *target* column differs between rows — which is what makes the aliases
  obviously content-free. **No trailing comments**: `manifest.Parse` splits on
  whitespace and requires exactly four fields, so a fifth breaks the manifest
  with a syntax error. One comment per row is a manifest failure mode of its
  own, and the existing file's comments are all on their own lines.

  ```text
  link    global/AGENTS.md                  .dsh/AGENTS.md                    *
  link    global/AGENTS.md                  .claude/CLAUDE.md                 *
  link    global/AGENTS.md                  .codex/AGENTS.md                  *
  ```

  The `.claude/CLAUDE.md` row replaces the one that names `claude/CLAUDE.md`;
  `.dsh/AGENTS.md` is new and is the path DSH actually reads; `.codex/AGENTS.md`
  is the first time that path is declared, and the hand-made real file there
  must be moved aside first — a `link` refuses an occupied target rather than
  overwriting it (spec 2 §5). `~/.claude/CLAUDE.md` is already a symlink to this
  checkout, and pointing it at the new source means the link must exist as a
  link at apply time, which it does. Then `./bootstrap apply workstation` and
  confirm each alias resolves to the same file.

- [ ] **Step 5: Only now, retire the old path**

  With the new home verified in Step 4, `claude/CLAUDE.md` is deleted — its
  content now lives at `global/AGENTS.md`, and `~/.claude/CLAUDE.md` is a link
  to that. Do this **after** Step 4, never before: until `$DSH_HOME/AGENTS.md`
  exists and has been observed, `claude/CLAUDE.md` is the only copy of the
  recording rule on this machine, and nothing would report its absence. With
  `claude/CLAUDE.md` gone, `claude/` holds only skills by then — and after
  Task 3, nothing at all, which is itself worth checking before the directory
  is removed.

- [ ] **Step 6: Commit**

---

### Task 3: Delete what never had a home

**The problem this solves.** Three files that describe intentions nothing
carries out.

**Files:**
- Move to `docs/archive/skills/`: `claude/skills/antigravity-handoff/`,
  `gemini/skills/session-handoff/` (including `scripts/handoff_manager.py`)
- Delete: the repository-root `CLAUDE.md`
- Create: a `docs/journal/` entry recording the removal and why

---

- [ ] **Step 1: Archive the two handoff skills**

  `git mv claude/skills/antigravity-handoff/ docs/archive/skills/antigravity-handoff/`
  and the same for `gemini/skills/session-handoff/`, `scripts/handoff_manager.py`
  included. `docs/archive/skills/` is a new directory; these are its first
  entries, so add a one-line README there saying what the directory is for —
  skill text kept as evidence after the skill stopped being installed.

  There is no gap to fill. The handoff design triggered at "session end", a
  moment an agent cannot identify: in a multi-turn conversation every turn ends,
  and nothing tells the agent that the next one is not coming — so the trigger
  is not merely unreliable, it is unanswerable. Its observed successes were
  single-turn tasks where the harness documented its own completion, which is
  instruction following, not the mechanism. In practice the human decides when
  something is worth keeping, which is what `recording-what-you-learn` is
  written around.

  Note the one thing `antigravity-handoff` does that nothing else does —
  reading Antigravity's brain directory, cross-checking it against code and git
  — before archiving, and say in the journal entry whether that capability is
  wanted anywhere. Deleting a skill whose only job was to make a failing harness
  legible is easy to regret later if the reason was never written down.

- [ ] **Step 2: Observe whether Claude Code still honours `AGENTS.md` without
  the root `CLAUDE.md`**

  Two different questions, and only the second needs an experiment:

  - **DSH**: already settled, from its own package documentation — a project's
    `CLAUDE.md` and `AGENTS.md` are both candidates and *"siblings whose content
    is identical after whitespace removal are rendered once"*. So the duplicate
    is deduplicated today and its removal changes nothing for DSH.
  - **Claude Code**: the fallback is documented upstream but this machine's
    version is the one that counts. Test it in a scratch copy: remove the root
    `CLAUDE.md`, open a session there, and check the agent still reports the
    `docs/` pointer that only `AGENTS.md` carries. If it does not, keep the root
    file and record why — the file is 23 lines and correctness beats tidiness.

- [ ] **Step 3: Delete the root `CLAUDE.md`**

- [ ] **Step 4: Write the journal entry**

  What was removed, the reasoning above, and the accepted consequence: nothing
  is left that fires at session end, by design.

- [ ] **Step 5: Commit**

---

### Task 4: Whole-change verification

- [ ] **Step 1:** `cd bootstrap.d && gofmt -d . && go vet ./... && go test -count=1 ./...`
- [ ] **Step 2:** `cd agents && gofmt -d . && go vet ./... && go test -count=1 ./...`
- [ ] **Step 3:** `agents doctor` — every check OK or an explained Warn.
- [ ] **Step 4:** `./bootstrap plan dotfiles` exits 0; `./bootstrap apply workstation`; `./bootstrap check` has no `manifest-kinds` fault.
- [ ] **Step 5:** `./bootstrap plan workstation` — record where it stops. Anything other than the phases Task 1 unblocked is a new finding, not a regression to fix here.
- [ ] **Step 6:** Constraint 6's observations, repeated from a clean shell in a repository that is not this one: `$DSH_HOME/AGENTS.md` arrives (the rules are visible to the session), and `.agents/skills/` is visible to the harness in this repository.
- [ ] **Step 7:** Confirm `.agents/skills/<name>/SKILL.md` holds for every entry, and that `agents doctor` reports the two infrastructure skills at their expected status.

---

## What this plan does not decide

- **Which skills should exist at all.** Task 1 Step 1 lists them and the
  consolidation moves them; whether `codebase-memory` and `agents-tool` are
  still worth a skill is a content question, and this plan only guarantees they
  have one home.
- **Whether the `agents` binary should keep installing mirrors.** It has one
  today, with a per-harness adapter and a generated `.git/info/exclude` entry.
  That is a decision about the binary, and it is the one place a per-harness
  path is legitimate.
- **The Go path onto a machine.** See the superseded plan's Task 2.

## Accepted consequences

- **A freshly cloned repository exposes no skills to Claude Code or Codex until
  `agents wire` runs there.** Removing the machine-global installs means the
  project-local alias is the only route, and the binary owns it. This is the
  intended trade — one owner per path, and the owner is the tool that already
  knows each harness's directory name — but it is a behaviour change, not a
  pure deletion, and it is written down here so it is not discovered as a
  surprise.
- **Nothing fires at session end any more.** Removed in Task 3, deliberately:
  the moment was never identifiable by the agent that was supposed to notice it.

## Execution notes

When execution starts, the artifacts go under
`.superpowers/sdd/2026-09-21-skills-single-home-plan/` following the convention
the previous plans in this repository used — a `progress.md` ledger, a brief per
dispatched task, and a review diff per completed task.
