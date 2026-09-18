# Layout v2 Migration Playbook — paperbubble Pilot

**Date:** 2026-09-18
**Status:** **Proposed — do not execute.** This playbook runs only after the
design and implementation plan are approved, `v0.6.0` is released, and every
machine that can touch the fleet resolves that binary on `PATH`.
**Scope:** exactly one repository, `/Users/nilbot/gist/paperbubble`. It does not
migrate dotfiles, cowork, autogo-mlx, lewm-mlx, or desktop_pet.
**Design:** [layout manifest and store-root freedom](../design/2026-09-18-layout-manifest-and-store-root-design.md)
**Plan:** [layout manifest implementation plan](2026-09-18-layout-manifest-implementation-plan.md)

## 0. What the read-only preflight already established

Measured on 2026-09-18 before writing this playbook:

| Fact | Evidence |
|---|---|
| paperbubble is a git worktree with a clean tree | `git status --short --branch` → `## agents-editorial` |
| Its root router is canonical v1 | `agents drift --json` → `router_state: clean_current` |
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
6. **No `agents save` during the migration.** It commits only `.agents/` and
   would split the manifest from the moved stores.
7. **No fleet-wide `agents update --all --apply` as part of the pilot.** The
   update gate would touch the five v1 repositories too. Refresh paperbubble's
   bundled skills from the v0.6.0 assets on the pilot branch (Step 6) and leave the
   fleet alone.
8. **The vault is content, the stores are meta.** Never add vault notes to
   `.context/`, and never treat `.context/` as part of the vault's content
   graph.

## 2. Preconditions

Run these read-only checks and stop on any mismatch:

```bash
cd /Users/nilbot/gist/paperbubble
agents version    # expect v0.6.0; anything below the manifest's min_mut_ver_floor (0.6.0) stops here
git status --porcelain          # must be empty
git branch --show-current       # must be agents-editorial or a new branch made below
test -d .agents && test ! -d .context || { echo "unexpected layout; stop"; exit 1; }
agents layout validate; echo "exit=$?"   # expect exit 0, implicit v1
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
- `drift_before.json` is `clean_current`, all four `docs_stores` true,
  `misplaced_docs: []`.
- `docs_before.txt` is exactly the four README files.

Present these three files at the approval gate. They are the before image.

## 4. Step 2 — Branch and backup tag

```bash
cd /Users/nilbot/gist/paperbubble
git switch -c feat/layout-v2-migration
git tag -a pre-layout-v2-20260918 -m "paperbubble before .context migration"
git status --porcelain     # must still be empty
```

The tool also requires `--backup-tag` and creates it at HEAD; creating it here
first makes the rollback point visible before any tool runs. If the tag already
exists, choose a new name and use it consistently below.

## 5. Step 3 — Dry run and review

```bash
cd /Users/nilbot/gist/paperbubble
agents layout migrate \
  --profile content-vault \
  --store-root .context \
  --content-root . \
  --dry-run \
  --json > /tmp/paperbubble-migrate-plan.json

agents layout migrate \
  --profile content-vault \
  --store-root .context \
  --content-root . \
  --dry-run
```

Expected plan, based on the measured baseline:

```
layout migrate (dry run) — /Users/nilbot/gist/paperbubble
  from    agents.layout/v1  docs/{design,plans,journal,qna}
  to      agents.layout/v2  profile=content-vault  store_root=.context
  router  clean_current -> canonical v2 (deterministic swap)
  archive none
  git     branch feat/layout-v2-migration, tree clean

  move    docs/design   -> .context/design   (1 file)
  move    docs/plans    -> .context/plans    (1 file)
  move    docs/journal  -> .context/journal  (1 file)
  move    docs/qna      -> .context/qna      (1 file)
  keep    .agents/AGENTS.md  (user-owned; prose reviewed by the skill)
  remove  docs/ after the moves (no archive, no other tracked content)

  links   0 markdown links point into the moved stores
  result  4 move, 1 keep, 0 blocked, 0 link(s)
```

Review every line of the JSON plan:

| Field | Required value | Why |
|---|---|---|
| `blockers` | empty | any blocker stops the migration |
| `to.stores` | `.context/{design,plans,journal,qna}` | the requested layout |
| `to.content_root` | `.` | the vault is the content |
| `archive` | `""` | no archive exists; nothing may be moved or recorded |
| `router` | `clean_current -> canonical v2` | the router is proven boilerplate, so the swap is deterministic |
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
5. the rollback table from §10.

Then stop. Silence is not approval, and a clean dry run is not approval.

## 7. Step 5 — Apply

Only after explicit approval:

```bash
cd /Users/nilbot/gist/paperbubble
agents layout migrate \
  --profile content-vault \
  --store-root .context \
  --content-root . \
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
# M  AGENTS.md
# A  .agents/layout.json
# (plus the skill files changed in Step 6)

test ! -e docs && echo "docs/ removed"
cat .agents/layout.json
```

Expected manifest:

```json
{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
  "profile": "content-vault",
  "layout_status": "active",
  "content_root": ".",
  "store_root": ".context",
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

## 8. Step 6 — Refresh skills and reconcile prose

The migrated repository must end with the v0.6.0 bundled skills, not the v0.5.1
ones. This step deliberately avoids `agents update --all --apply`.

### 8.1 Verify nothing local would be lost

```bash
SRC=/Users/nilbot/dotfiles
PAPER=/Users/nilbot/gist/paperbubble

git -C "$SRC" show fc4903f:agents/internal/scaffold/assets/skills/recording-what-you-learn/SKILL.md \
  > /tmp/recording-v051.md
git -C "$SRC" show fc4903f:agents/internal/scaffold/assets/skills/migrating-fleet-context/SKILL.md \
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

### 8.2 Replace with the v0.6.0 assets

```bash
cp "$SRC/agents/internal/scaffold/assets/skills/recording-what-you-learn/SKILL.md" \
   "$PAPER/.agents/skills/recording-what-you-learn/SKILL.md"
cp "$SRC/agents/internal/scaffold/assets/skills/migrating-fleet-context/SKILL.md" \
   "$PAPER/.agents/skills/migrating-fleet-context/SKILL.md"

cmp "$SRC/agents/internal/scaffold/assets/skills/recording-what-you-learn/SKILL.md" \
    "$PAPER/.agents/skills/recording-what-you-learn/SKILL.md"
cmp "$SRC/agents/internal/scaffold/assets/skills/migrating-fleet-context/SKILL.md" \
    "$PAPER/.agents/skills/migrating-fleet-context/SKILL.md"
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
proved `clean_current`; do not hand-edit it. Confirm:

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
- `router_state: clean_current` against the v2 router;
- both embedded skills `ok`;
- `misplaced_docs: []`.

Archive and blob proof:

```bash
git log --follow --oneline -- .context/design/README.md | head -3
git diff --cached --stat
git diff HEAD --find-renames --name-status
```

Every store README must appear as a rename, never as a delete plus add.

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
git status --porcelain | grep -v '^R ' | grep -v '^M ' | grep -v '^A ' && echo "unexpected change; stop"
git diff --check
```

## 10. Step 8 — Commit, PR, merge

Stage exact paths, never `git add .`:

```bash
cd /Users/nilbot/gist/paperbubble
git add -- AGENTS.md .agents .context docs
git diff --cached --stat
git diff --cached --check

git commit -m "feat(context): move agent meta stores to .context"
git push -u origin feat/layout-v2-migration
gh pr create --fill
```

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
| before commit, on the migration branch | `git switch agents-editorial` then `git branch -D feat/layout-v2-migration` | the tree returns to the original branch; the backup tag remains |
| before commit, staying on the branch | `git restore --source=pre-layout-v2-20260918 --staged --worktree :/` plus removing the new `.context/` | only when the tree contains nothing but the migration |
| after commit, before merge | switch back to `agents-editorial` | the migration commit and tag remain as evidence |
| after merge | revert the migration commit on a new branch | the tag still names the pre-migration tree; never rewrite shared history |
| last resort, clean migration-only tree | `git reset --hard pre-layout-v2-20260918` | discards every uncommitted change by design; not the preferred path |

After any rollback, run `agents layout validate` and `git status --porcelain`
and record the result. A rollback that leaves a `migrating` manifest is not a
rollback; resume or restore the manifest explicitly.

## 12. Post-merge fleet state

After paperbubble is merged:

- paperbubble is the only v2 repository; `agents drift --json` reports it
  supported and clean;
- the other five repositories remain v1 in round 1; they are deferred, not
  excluded. A later code-repo migration can keep `docs/` and change only the
  manifest, router, and skills;
- after v0.6.0, the other repositories' `recording-what-you-learn` copies report
  `clean_legacy`, so `agents drift --all` exits 1 until each repository is
  migrated. That is the accepted advisory from design Decision 7, not a
  regression;
- do not run `agents update --all --apply` as a response to that advisory. It
  refreshes the migrating skill but does not update the user-owned recording
  skill and leaves uncommitted changes in repositories whose owners did not
  ask for them;
- the next repository migration is a separate decision with its own playbook
  section. Nothing in this document authorizes it.

## 13. Stop and ask when

- `agents version` is below the manifest's `min_mut_ver_floor` on any machine that can
  touch the repository;
- the dry run reports any blocker, any link candidate, or any path other than
  the four expected stores;
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
