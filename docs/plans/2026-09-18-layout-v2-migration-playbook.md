# Layout v2 Migration Playbook — paperbubble Pilot

**Date:** 2026-09-18
**Status:** **Approved as a document 2026-09-19 (design §12, gate 1); do not
execute.** This playbook runs only after gate 2 approves the implementation
plan for execution, `v0.6.0` is released, every machine that can touch the fleet
resolves that binary on `PATH`, and the §6 approval gate is passed.
**Scope:** exactly one repository, `/Users/nilbot/gist/paperbubble`. It does not
migrate dotfiles, cowork, autogo-mlx, lewm-mlx, or desktop_pet; §12 records the
measured state of each and why.
**Design:** [layout manifest and store-root freedom](../design/2026-09-18-layout-manifest-and-store-root-design.md)
**Plan:** [layout manifest implementation plan](2026-09-18-layout-manifest-implementation-plan.md)

## 0. What the read-only preflight already established

Measured on 2026-09-18 before writing this playbook:

| Fact | Evidence |
|---|---|
| paperbubble is a git worktree with a clean tree | `git status --short --branch` → `## agents-editorial` |
| Its root router is canonical v1 | `agents drift --json` → `router_state: clean_current` (v0.6.0 name: `current`) |
| It has the four-store `docs/` skeleton and no content there | `git ls-files docs` → exactly the four `README.md` files |
| It has no `docs/archive/` | `ls -la docs/` shows only `design`, `journal`, `plans`, `qna` |
| No markdown outside `docs/` links into the stores, except the root router | `rg` over `*.md` returns only `AGENTS.md:6-9` |
| `.agents/AGENTS.md` is real user-owned prose (Chinese writing rules), not the starter template | read-only inspection |
| The vault has no hidden meta store yet | no `.context/` in the repository root |

The four `docs/**/README.md` files are the empty shell this migration removes.
They contain the canonical store READMEs, which move with their directories.

`paperbubble` is the zero-content-cost pilot: nothing in the vault depends on
`docs/`, so a migration failure has no content to lose. That is why it goes
first and why no other repository is in scope.

## 1. Non-negotiables

1. **Deploy before flip.** Do not run `--apply` until every machine that can
   run `agents init`, `agents update --all --apply`, or `agents save` against
   the fleet resolves `v0.6.0` on `PATH` (`agents version` plus the `binary`
   doctor check). A v0.5.1 binary cannot see the manifest. Measured on a
   disposable fixture on 2026-09-18: it recreates the four `docs/` READMEs on a
   v2 repository and overwrites a v2-aware migration skill. There is no
   technical guard for a pre-manifest binary; this deployment gate is the
   guard.
2. **Frozen until approval.** No paperbubble write happens before Step 4's
   explicit human approval.
3. **Branch and tag first.** The migration runs on a dedicated branch, and the
   tool creates the annotated backup tag before its first write.
4. **No copies.** `git mv` is the only move mechanism. A copied store is a
   failed migration even if the result looks right.
5. **No archive movement.** There is no archive in paperbubble today. If one
   appears before execution, stop: the tool must record it and leave it alone.
   Immutability is content-blind (design §0.6): "the archive is a place, not a
   topic: everything under it is historical by definition, so nothing under it
   is ever moved, rewritten, or reclassified", whatever it happens to hold.
6. **No `agents save` during the migration.** It commits only `.agents/` and
   would split the manifest from the moved stores.
7. **No fleet-wide `agents update --all --apply` as part of the pilot.** The
   update gate rewires every registered v1 repository. Refresh paperbubble's
   bundled skills from the v0.6.0 assets on the pilot branch (Step 6) and leave the
   fleet alone.
8. **The vault is content, the stores are meta.** Never add vault notes to
   `.context/`, and never treat `.context/` as part of the vault's content
   graph.
9. **A stuck migration is resumed, never hand-fixed.** If apply fails, run
   `agents layout migrate --resume --apply`. `--abort --apply` is the only
   delete, and it succeeds only while the journal's phase is `planned` with
   every move still `pending` and `created_manifest` true; later phases refuse
   and name `--resume --apply`. There is no `--force`, and a refusal that names
   both paths is information, not an obstacle to route around.

## 2. Preconditions

Run these read-only checks and stop on any mismatch:

```bash
cd /Users/nilbot/gist/paperbubble
agents version    # expect v0.6.0; anything below v0.6.0 (the value this migration writes as min_mut_ver_floor) stops here
git status --porcelain          # must be empty
git branch --show-current       # must be agents-editorial or a new branch made below
test -d .agents && test ! -d .context || { echo "unexpected layout; stop"; exit 1; }
agents layout validate; echo "exit=$?"   # expect exit 0, implicit v1
if git check-ignore -q --no-index -- .agents/layout.json \
|| git check-ignore -q --no-index -- .agents; then
  echo ".agents/ is ignored (--local): a v2 manifest would be machine-local; stop"; exit 1
fi
```

If `git status --porcelain` is not empty, stop. A dirty tree makes the approval
diff unreadable, which is the artifact this procedure exists to produce.

## 3. Step 1 — Record the baseline (read-only)

```bash
cd /Users/nilbot/gist/paperbubble
agents layout show --json                  > /tmp/paperbubble-layout-before.json
agents drift --json                        > /tmp/paperbubble-drift-before.json
git ls-files docs                          > /tmp/paperbubble-docs-before.txt
find docs -name '*.md' -type f | sort      >> /tmp/paperbubble-docs-before.txt
shasum -a 256 AGENTS.md .agents/AGENTS.md  > /tmp/paperbubble-context-before.sha
```

Expected:

- `layout_before.json` is `agents.layout/v1` with stores `docs/{design,plans,journal,qna}`.
- `drift_before.json` is `current`, all four `docs_stores` true,
  `misplaced_docs: []`.
- `docs_before.txt` is exactly the four README files.

Present these three files at the approval gate. They are the before image.

## 4. Step 2 — Branch, and the backup tag name

```bash
cd /Users/nilbot/gist/paperbubble
git switch -c feat/layout-v2-migration
git status --porcelain     # must still be empty
```

Choose the tag name now — `pre-layout-v2-YYYYMMDD`, dated the day the migration
actually runs — and pass it as `--backup-tag` in Step 3 and Step 5. **Do not
create it.** The tool creates the annotated tag at `HEAD` as its first act,
before its first write (design §9.2), so the rollback point exists before
anything moves. Pre-creating it makes the apply refuse; measured on the pilot,
2026-09-20:

```text
agents layout migrate: --backup-tag "pre-layout-v2-20260920" already exists;
choose another name, or continue the migration it belongs to with `agents layout
migrate --resume --apply`
exit=3
```

That refusal is fail-closed — nothing was written — and it cost one round of
diagnosis, which is why this step no longer tells you to run `git tag`. If you
want the rollback point visible before the tool runs, `git rev-parse HEAD` names
the same commit the tag will point at.

## 5. Step 3 — Dry run and review

```bash
cd /Users/nilbot/gist/paperbubble
agents layout migrate \
  --template content-vault \
  --dry-run \
  --json > /tmp/paperbubble-migrate-plan.json

agents layout migrate \
  --template content-vault \
  --dry-run
```

Expected plan, based on the measured baseline:

```
layout migrate (dry run) — /Users/nilbot/gist/paperbubble
  from    agents.layout/v1  docs/{design,plans,journal,qna}
  to      agents.layout/v2  stores=.context/{design,plans,journal,qna}
  router  current -> canonical v2 (deterministic swap)
  archive none
  git     branch feat/layout-v2-migration, tree clean

  move    docs/design   -> .context/design   (1 file)
  move    docs/plans    -> .context/plans    (1 file)
  move    docs/journal  -> .context/journal  (1 file)
  move    docs/qna      -> .context/qna      (1 file)
  remove  docs/ after the moves (no archive, no other tracked content)
  links   0 markdown link(s) the move breaks and the skill must rewrite
  result  4 move, 0 keep, 0 blocked, 0 link(s)
```

`.agents/AGENTS.md` is not a plan row: `Counts.Keep` is 0 by ruling, so there is
no `keep` line and the result reads `0 keep`, not `1 keep`. The plan leaves that
file untouched, and Step 6 reviews its prose. Measured on the pilot, 2026-09-20.

Review every line of the JSON plan:

| Field | Required value | Why |
|---|---|---|
| `blockers` | empty | any blocker stops the migration |
| `to.stores` | `.context/{design,plans,journal,qna}` | the requested layout |
| `archive` | key absent | no archive exists; nothing may be moved or recorded. The field is `omitempty`, so "none" is the absent key rather than an empty string |
| `router` | `current -> canonical v2` | the router is proven boilerplate, so the swap is deterministic |
| `moves` | four directory moves | no file-level copy list |
| `link_candidates` | empty | if non-empty, the skill rewrites them after apply |

Dry run is the default; `--dry-run` is written explicitly here so the command
in the record says what it did. Do not proceed if `--dry-run` reports a
blocker, a target that already exists, or a below-floor refusal.

## 6. Step 4 — Human approval gate

Present, in one message:

1. `git status --porcelain` and `git branch --show-current`;
2. the baseline files from Step 1;
3. the dry-run output and the JSON plan;
4. the exact commands that will run in Step 5;
5. the rollback table from §11.

Then stop. Silence is not approval, and a clean dry run is not approval.

## 7. Step 5 — Apply

Only after explicit approval:

```bash
cd /Users/nilbot/gist/paperbubble
agents layout migrate \
  --template content-vault \
  --apply \
  --backup-tag pre-layout-v2-20260918
echo "exit=$?"
```

Expected after apply:

```bash
git status --porcelain
# R  docs/design/README.md -> .context/design/README.md
# R  docs/plans/README.md -> .context/plans/README.md
# R  docs/journal/README.md -> .context/journal/README.md
# R  docs/qna/README.md -> .context/qna/README.md
#  M AGENTS.md
#  M .gitattributes
# ?? .agents/layout.json
# (plus the skill files changed in Step 6)

test ! -e docs && echo "docs/ removed"
cat .agents/layout.json
```

Only the moves are staged: `git mv` stages them, and the tool leaves its own
writes for the migration commit — the router and `.gitattributes` unstaged, the
manifest untracked. `layout:committed` warns until Step 8 commits it, which is
the intended sequence rather than something to repair here.

Expected manifest:

```json
{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
  "layout_status": "active",
  "stores": {
    "design": ".context/design",
    "plans": ".context/plans",
    "journal": ".context/journal",
    "qna": ".context/qna"
  }
}
```

If apply fails mid-way, the manifest is left `migrating`. Do not hand-fix it;
run `agents layout migrate --resume --apply` and name the failed move in the
report.

Apply runs the recorded phases in order — `planned` → `moved` → `pruned` →
`router`, with the journal removed only when `layout_status` becomes `active`
(design §0.5). Three outcomes need different words in the report:

- **A `MoveError`.** Resume compares the filesystem and the recorded source
  digest per move. A mismatch prints the two paths and a remedy line naming
  which path to remove and how to `git restore --source=<tag>` the other. Hand
  that line to the human; do not resolve it yourself.
- **Nothing moved yet.** If the crash happened before the first `git mv`, the
  journal is still `phase: planned` with every move `pending`:
  `agents layout migrate --abort --apply` deletes the manifest this run created
  and leaves the backup tag. Any later phase refuses and names `--resume
  --apply`.
- **`docs_residue`.** The moves completed but `docs/` retained an entry that is
  neither a v1 store nor the declared archive. The layout is `active` and the
  exit is advisory: name each entry, remove or relocate it, and commit. Do not
  delete a path the tool did not name.

## 8. Step 6 — Refresh skills and reconcile prose

The migrated repository must end with the v0.6.0 bundled skills, not the v0.5.1
ones. This step deliberately avoids `agents update --all --apply`.

The v0.5.1 skill **cannot perform the flip**: a v1 repository's
`migrating-fleet-context` copy is the frozen v1 text, which predates
`agents layout migrate` and knows nothing about layouts. The flip was the CLI's
job and it is already done (Step 5); this step installs the v2 texts afterwards.
The same rule is why `agents update --all --apply` refreshes the agents-owned
skill only once the layout is v2, and why the fleet is left alone (§12).

### 8.1 Verify nothing local would be lost

```bash
SRC=/Users/nilbot/dotfiles
PAPER=/Users/nilbot/gist/paperbubble

git -C "$SRC" show v0.5.1:agents/internal/scaffold/assets/skills/recording-what-you-learn/SKILL.md \
  > /tmp/recording-v051.md
git -C "$SRC" show v0.5.1:agents/internal/scaffold/assets/skills/migrating-fleet-context/SKILL.md \
  > /tmp/migrating-v051.md

cmp "$PAPER/.agents/skills/recording-what-you-learn/SKILL.md" /tmp/recording-v051.md \
  && echo "recording: unmodified legacy (replace allowed)"
cmp "$PAPER/.agents/skills/migrating-fleet-context/SKILL.md" /tmp/migrating-v051.md \
  && echo "migrating: unmodified legacy (replace allowed)"
```

If either `cmp` fails, stop. A modified `recording-what-you-learn` is
user-owned prose and needs a three-way review, not a replacement; a modified
`migrating-fleet-context` is a stale agent-owned asset, but the divergence
still needs to be read before it is discarded.

These paths are read from the **v0.5.1 tag**, where the flat asset path holds
the v1 text. Design §0.8 does not change that reading: in the v0.6.0 tree the
flat path is the v2 text, and the frozen v1 text lives at
`agents/internal/scaffold/assets/skills/<skill>/v1/SKILL.md`.

### 8.2 Replace with the v0.6.0 assets

The two `git show` paths below are the **v2** canonical texts, which is what
paperbubble needs: Apply (Step 5) already made it a v2 repository. In the v0.6.0
tree each skill also carries a frozen v1 text at
`agents/internal/scaffold/assets/skills/<skill>/v1/SKILL.md`, which a v1
repository keeps as its canonical asset (design §0.8). This step therefore
takes the v2 text deliberately and is never a downgrade; if the repository
being migrated were still v1, taking the flat path would be wrong.

```bash
git -C "$SRC" rev-parse --verify v0.6.0^{commit}

git -C "$SRC" show v0.6.0:agents/internal/scaffold/assets/skills/recording-what-you-learn/SKILL.md \
  > /tmp/recording-v060.md
git -C "$SRC" show v0.6.0:agents/internal/scaffold/assets/skills/migrating-fleet-context/SKILL.md \
  > /tmp/migrating-v060.md

cp /tmp/recording-v060.md "$PAPER/.agents/skills/recording-what-you-learn/SKILL.md"
cp /tmp/migrating-v060.md "$PAPER/.agents/skills/migrating-fleet-context/SKILL.md"

cmp /tmp/recording-v060.md "$PAPER/.agents/skills/recording-what-you-learn/SKILL.md"
cmp /tmp/migrating-v060.md "$PAPER/.agents/skills/migrating-fleet-context/SKILL.md"
```

### 8.3 Reconcile user-owned prose and links

Read `.agents/AGENTS.md` in full. The baseline grep found no `docs/` path in it,
but the skill must still confirm that every sentence stays true when the meta
stores move to an invisible dot-directory. Classify every block that mentions
where knowledge lives:

```
source quote (verbatim) -> classification -> destination
```

If the plan reported link candidates, rewrite them now and record the mapping.
For paperbubble the expected count is zero. Any unclassifiable block is a
stop-and-ask, not a judgement call.

The root `AGENTS.md` was replaced deterministically because the pre-flight
proved `current`; do not hand-edit it. Confirm:

```bash
grep -n 'docs/' AGENTS.md && echo "STOP: v2 router names a store path"
grep -n 'agents.layout/v2' AGENTS.md
```

## 9. Step 7 — Verify

```bash
cd /Users/nilbot/gist/paperbubble
agents layout show --json                 # v2, active, .context/*, no problems
agents layout validate; echo "exit=$?"    # expect 0
agents drift --json                       # expect unsupported "", clean fields
agents doctor; echo "exit=$?"             # expect no new layout warnings
git status --porcelain
git diff --stat
```

Expected drift:

- `layout_version: v2`, `layout_status: active`, `unsupported: ""`;
- `stores` resolves to `.context/{design,plans,journal,qna}`;
- `router_state: current` against the v2 router;
- both embedded skills `current`;
- `misplaced_docs: []`.

Archive and blob proof:

```bash
git log --follow --oneline -- .context/design/README.md | head -3   # after Step 8
git diff --cached --stat
git diff HEAD --find-renames --name-status
```

Every store README must appear as a rename, never as a delete plus add. Before
the migration commit, `R100` from `--find-renames` is the proof, and
`git log --follow` on a new path is empty by construction — that path exists in no
commit yet. Both hold on the pilot, 2026-09-20.

Vault check (manual, read-only):

1. Open paperbubble in Obsidian.
2. Confirm `.context/` does not appear in Quick Switcher, Search, Graph View,
   or the file explorer.
3. Confirm the vault's content notes, links, and daily notes are unchanged:
   `git status` shows no vault content files.
4. Confirm the meta stores are still editable by the agent through the
   filesystem; Obsidian invisibility is not a permission change.

No repository test suite exists in paperbubble. The equivalent gate is:

```bash
# The tool stages the moves and leaves its own writes for the migration commit,
# so the expected set is exactly: staged renames, the two skill texts from
# Step 6, the router and attributes, and the untracked manifest.
git status --porcelain | grep -vE '^R  docs/.+ -> \.context/.+$|^ M (AGENTS\.md|\.gitattributes|\.agents/skills/(migrating-fleet-context|recording-what-you-learn)/SKILL\.md)$|^\?\? \.agents/layout\.json$' && echo "unexpected change; stop"
git diff --check
```

## 10. Step 8 — Commit, PR, merge

Stage exact paths, never `git add .`:

```bash
cd /Users/nilbot/gist/paperbubble
# Not `docs`: the migration's `git mv` already staged those renames, and the
# directory no longer exists, so naming it fails the whole command with
# `fatal: pathspec 'docs' did not match any files` (measured 2026-09-20).
# `.gitattributes` belongs in the set: the same reconcile added the manifest's
# linguist line, and leaving it out commits every migration output except one.
git add -- AGENTS.md .agents .context .gitattributes
git diff --cached --stat
git diff --cached --check

git commit -m "feat(context): move agent meta stores to .context"
git push -u origin feat/layout-v2-migration
gh pr create --fill
```

`--fill` titles the PR from the branch's commits. When the pilot branch carries
other work as well — paperbubble's carried ten commits, six of them editorial —
write the title and body explicitly instead, so the rename diff is what the
review names rather than whichever commit `--fill` picked. Check the repository's
visibility before pushing: the branch may carry work that is not on any remote yet.

The commit trips the pre-commit guard's `mixed-commit` advisory, because it
touches `.agents/` alongside other paths. It is advisory, and the design requires
the manifest and the moved stores to land in one commit, so this is the intended
shape rather than something to fix.

The commit message names the pilot and the design:

```
feat(context): move agent meta stores to .context

Paperbubble is the agents.layout/v2 pilot. docs/{design,plans,journal,qna}
became .context/{...}; the four store READMEs moved with git mv. No vault
content changed. Design: dotfiles docs/design/2026-09-18-layout-manifest-and-store-root-design.md
```

Merge only after CI (if configured) and a human review of the rename diff. The
backup tag stays until the merge is confirmed; delete it only when the operator
chooses to.

## 11. Rollback

| When | Command | Notes |
|---|---|---|
| nothing moved yet (journal `phase: planned`, every move `pending`) | `agents layout migrate --abort --apply` | deletes the manifest this run created and leaves the backup tag; any later phase refuses and names `--resume --apply` |
| before commit, on the migration branch | `git switch agents-editorial` then `git branch -D feat/layout-v2-migration` | the tree returns to the original branch; the backup tag remains |
| before commit, staying on the branch | `git restore --source=pre-layout-v2-20260918 --staged --worktree :/` plus removing the new `.context/` | only when the tree contains nothing but the migration |
| after commit, before merge | switch back to `agents-editorial` | the migration commit and tag remain as evidence |
| after merge | revert the migration commit on a new branch | the tag still names the pre-migration tree; never rewrite shared history |
| last resort, clean migration-only tree | `git reset --hard pre-layout-v2-20260918` | discards every uncommitted change by design; not the preferred path |

After any rollback, run `agents layout validate` and `git status --porcelain`
and record the result. A rollback that leaves a `migrating` manifest is not a
rollback: resume it with `agents layout migrate --resume --apply`, or — only
while `phase` is still `planned` and nothing has moved — abort it with
`--abort --apply`. Never hand-edit the journal, and never delete a manifest a
later phase still needs.

## 12. Post-merge fleet state

After paperbubble is merged:

- paperbubble is the only v2 repository; `agents drift --json` reports it
  supported and clean;
- the other five repositories remain v1 in round 1. The table below records the
  measured state and round-1 group of each; a later code-repo migration can keep
  `docs/` and change only the manifest, router, and skills;
- **v0.6.0 adds no new v1 advisory; `dotfiles`, `paperbubble`, `cowork`, and
  `lewm-mlx` stay `current`; `autogo-mlx` and `desktop_pet` continue to exit 1
  for pre-existing 2026-08-29 two-tier reasons.** Design §0.8 selects the
  canonical bundled-skill text by resolved layout, so a v1 repository keeps the
  frozen v0.5.1 text as its canonical asset and stays `current`. Do not run
  `agents update --all --apply` in response to anything in this section: it
  refreshes the agents-owned skill only, and only once a layout is v2;
- `agents drift --all` **already exited 1 before this release**, on v0.5.1, for
  those two repositories. That is the baseline, not a new advisory:

| Repository | Measured on 2026-09-18 with v0.5.1 (read-only; states named in the v0.6.0 vocabulary) | Round-1 group |
|---|---|---|
| paperbubble | v1; router `current`; domain ok; both skills current; 4/4 stores; `drift` exit 0 | **pilot** — this playbook |
| cowork | v1; router `current`; domain ok; both skills current; 4/4 stores; `drift` exit 0 | ready later — a v2 adoption moves nothing |
| lewm-mlx | v1; router `current`; domain ok; both skills current; 4/4 stores; `drift` exit 0 | ready later — a v2 adoption moves nothing |
| autogo-mlx | router **missing**; recording skill **missing**; 1/4 stores; 2 misplaced docs; `drift` exit 1 | **not ready** — blocked on the 2026-08-29 two-tier migration, not on this layout release |
| desktop_pet | router **diverged**; domain **missing**; recording skill **missing**; 0/4 stores; `drift` exit 1 | **not ready** — blocked on the 2026-08-29 two-tier migration |
| dotfiles | v1; router `current`; domain ok; both skills current; 4/4 stores; `drift` exit 0 | the development repository — keeps the frozen v1 text and stays `current` with no copy change |

- the debt this leaves is two repositories that are non-current for reasons the
  2026-08-29 two-tier design owns. Clearing them is that migration, not a layout
  change, and it is a separate decision;
- the next repository migration is a separate decision with its own playbook
  section. Nothing in this document authorizes it.

## 13. Stop and ask when

- `agents version` is below the manifest's `min_mut_ver_floor` on any machine that can
  touch the repository;
- `.agents/` is ignored (a `git check-ignore` rule matches it): the v2 path is
  unavailable, because a manifest there would be machine-local (design §0.7);
- the dry run reports any blocker, any link candidate, or any path other than
  the four expected stores;
- the dry run reports `docs_residue`: an entry under `docs/` that is neither a
  v1 store nor the declared archive;
- resume or apply reports a `MoveError`: a digest mismatch, both paths present,
  or neither present. Report the remedy line verbatim; do not resolve it;
- `docs/archive/` exists, or any tracked file under `docs/` is not one of the
  four store READMEs;
- either bundled skill differs from the v0.5.1 asset before replacement;
- `.agents/AGENTS.md` contains a block about where knowledge lives that cannot
  be confidently classified;
- Obsidian shows `.context/` in any vault surface, or a vault content file
  changes;
- the rename diff shows a delete/add pair where a rename is expected.

Asking costs one message. A copied store, a moved archive, or a silent skill
downgrade costs a record nobody notices is gone.
