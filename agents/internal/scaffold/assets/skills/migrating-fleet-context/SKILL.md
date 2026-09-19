---
name: migrating-fleet-context
description: Use when `agents doctor` reports a `scaffold:*` warning, `agents drift` exits non-zero, or a repository keeps domain rules in a root `AGENTS.md`/`CLAUDE.md`, lacks `.agents/AGENTS.md`, is missing the stores its layout declares, carries plans or designs in the wrong store, or has a bundled skill that no longer matches the installed binary.
---

# Migrating Fleet Context

Moves a repository onto the Two-Tier Agent Context architecture: a canonical
root router at `AGENTS.md`, domain rules in `.agents/AGENTS.md`, durable
knowledge in the four stores the repository's layout declares. In v1 those
stores are the four `docs/` directories; in v2 `.agents/layout.json` names
them, and they may live anywhere.

**The deterministic tools know the states; only you can read the prose.**
`agents drift` tells you exactly what is wrong and never guesses at meaning.
Your job is the part it cannot do — deciding which sentence is a repository's
own rule and which is scaffold boilerplate — and proving you moved every one.

**Nothing is staged or committed until a human approves the diff.**

---

## Step 0: Check whether you are stale

This skill is an `agents`-owned asset embedded in the binary. The copy you are
reading can be older than the tool you are about to run.

```bash
agents doctor
```

If `scaffold:skill-migrating` reports anything but `ok`, your instructions are
out of date. **Do not fix it yet** — the fix writes to the working tree, and
Step 2 needs a clean one. Note it and carry on to Step 2; Step 3.5 does the
refresh once you are safely on a branch.

---

## Step 1: Pick the mode and read the right JSON shape

The two invocations do not return the same type. A parser written for one
breaks on the other.

| Mode | Command | Returns |
|---|---|---|
| Single repository | `agents drift --json` | one **object** |
| Fleet | `agents ls`, then `agents drift --all --json` | an **array** of objects |

Fleet mode migrates one repository at a time, each on its own branch. Skip and
name in the final report, rather than migrating:

- registry entries reported `missing` or `unknown`
- any repository whose working tree is dirty
- any repository already `current` on every field

Fields to read from each report:

| field | use |
|---|---|
| `layout_version` | `v1`, `v2`, or `unknown` — see Step 1.5 |
| `layout_status` | `active` or `migrating` — see Step 1.5 |
| `min_mut_ver_floor` | the oldest binary allowed to mutate the layout |
| `unsupported` | non-empty means stop: the schema is unknown, the binary is below `min_mut_ver_floor`, or the manifest is invalid |
| `stores` | the resolved role→path map. `docs_stores` is the deprecated v1 shape |
| `router_state` | picks your procedure — see Step 4 |
| `symlink_state` | `ok` \| `not_symlink` \| `broken` \| `missing` — see Step 5 |
| `domain_state` | `ok` \| `missing` — whether `.agents/AGENTS.md` exists |
| `skills` | embedded skills only: `current` \| `known_legacy` \| `diverged` \| `missing` |
| `local_skills` | the repository's own skills. **Never touch these.** |
| `misplaced_docs` | plans and designs in the wrong live store |
| `diff` | unified diff of the root router against canonical |

---

## Step 1.5: Read the layout manifest

| manifest state | action |
|---|---|
| absent | v1; use `agents layout migrate --dry-run` to plan |
| active v2, supported | no layout move; reconcile prose and links only |
| `layout_status: migrating` | stop; run `agents layout migrate --resume --apply` |
| unsupported or unknown schema | stop; the binary is older than `min_mut_ver_floor` |
| `.agents/` is ignored (`--local`) | stop; v2 is unavailable here — a manifest would be machine-local |

`agents layout show` prints the resolved layout — schema, status,
`min_mut_ver_floor`, archive, and one path per role — so read it instead of
assuming a directory name. `agents layout path <role>` prints a single store and
nothing else, for use in a command substitution. The four roles are `design`,
`plans`, `journal`, and `qna`.

The layout move itself is deterministic and belongs to the tool:

```bash
agents layout migrate --dry-run              # the plan; moves nothing
agents layout migrate --apply --backup-tag pre-layout-v2-<date>
agents layout migrate --resume --apply       # continue a `migrating` manifest
agents layout migrate --abort --apply        # only from `planned`, nothing moved
```

`--dry-run` is the default and is the only form to run before the plan is
approved: it names every move and touches nothing. `--apply` is required to
move anything, and requires `--backup-tag` — the annotated rollback point the
tool creates at HEAD — except with `--resume` and `--abort`, where the manifest
already carries the migration. `--resume --apply` never re-plans: it reads the
journal in the manifest and reconciles the filesystem against its recorded
digests. Any phase later than `planned` resumes rather than restarts, and
`--abort --apply` exists only for a `planned` phase with nothing moved and a
manifest this migration created; it deletes the manifest and leaves the backup
tag as the record.

**The archive is never a move source or a destination.** The manifest records
it, every walk and every move list excludes it, and live knowledge found in it
is promoted by a human into a live store — never extracted by the tool.

The canonical text of this skill is selected by the resolved layout. The v1
copy of this skill cannot perform the flip: it predates `agents layout
migrate`, and a v1 repository's bundled skills are the frozen v1 texts. The CLI
flips the layout; this skill's v2 text is installed afterwards by the migration
playbook (`agents init` on a fresh clone, or `agents update --all --apply` once
the layout is v2).

---

## Step 2: Preflight

```bash
git status --porcelain
```

Any output at all: stop. Report it and let the human commit or stash. A dirty
tree makes the approval diff in Step 10 unreadable, which is the one artifact
this whole procedure exists to produce.

## Step 3: Branch isolation

```bash
git branch --show-current
git checkout -b feat/two-tier-context-migration
```

Never migrate on `master`, `main`, or any protected branch.

## Step 3.5: Refresh yourself, if Step 0 said you are stale

```bash
agents update --all --apply
```

`--all` is not optional: `agents update` refuses to run without it, and
`agents wire` does not refresh skills. There is no single-repository form, so
this rewrites the skill in **every registered repository**, not just this one.
It reads each repository's manifest first: unsupported and `migrating`
repositories are skipped, named, and left byte-for-byte unchanged. Two
consequences, both yours to handle:

- In this repository the refreshed skill is an uncommitted change on your
  branch. That is fine — it belongs in this migration's commit.
- In the others it leaves an uncommitted change nobody asked for. Say so in
  your Step 10 report, and leave them alone; each repository's own migration
  will carry its copy.

Then **re-read this file** and restart from Step 1. The instructions you have
been following up to this point are the stale ones.

---

## Step 4: Reconcile the root, one procedure per state

Four states, four different correct actions. Treating them alike is the single
most damaging thing you can do here — it partitions a file that has nothing to
partition, or invents content for a file that has none.

| `router_state` | What it means | What to do |
|---|---|---|
| `current` | matches the installed binary's canonical router for the resolved layout | nothing. Do not "improve" it. |
| `known_legacy` | a known older canonical template, **no repository content** | replace wholesale with the canonical router. There is nothing to extract — the digest already proved that. |
| `diverged` | canonical text plus, or reworded into, repository content | semantic reconcile, below |
| `missing` | no root `AGENTS.md` at all | do not invent one. Go to Step 5 and read `CLAUDE.md`. If that is absent too, **stop and ask** what this repository's rules are. |

### Semantic reconcile (`diverged` only)

Two inputs — the current root file and the canonical router — and one output
per block. This is not a three-way merge; there is no base. Read the `diff`
field to see what the repository added.

Classify **every** block:

- **Boilerplate**, to be replaced by the canonical router: pointer tables into
  the store roots, old single-line `agents doctor` instructions, references to
  retired commands (handoff writing, `save`, `index`, and the memory tooling —
  none of which the CLI defines any more).
- **Domain rules**, to be preserved in `.agents/AGENTS.md`: tech stack
  conventions, test mandates, safety constraints, architecture invariants, PR
  and workflow policy, commenting standards.

Write the domain rules to `.agents/AGENTS.md`. If it already exists, append
into the matching section without duplicating what is there. Preserve the
**per-role collaboration policy** while you are in that file: which stores
humans edit, which agents mostly write, and which are read-only for one side.
The manifest records where a role lives, not how the repository wants it used.
If the policy is missing and cannot be inferred from the repository's own
rules, **stop and ask** rather than inventing one.

Then restore the root `AGENTS.md` verbatim — with the tool, not from a template
in this file. `agents layout show --router` prints the canonical router for the
resolved layout and nothing else, so the bytes cannot have diverged from the
binary that will later classify them:

```bash
# Capture first, write only on success: a refused --router prints nothing, and
# an unguarded redirect would truncate AGENTS.md to zero bytes.
router="$(agents layout show --router)" || {
  echo "refusing to rewrite AGENTS.md: --router did not resolve the layout" >&2
  exit 1
}
printf '%s' "$router" > AGENTS.md
```

On a v1 repository that prints the router pointing at the four `docs/` stores;
on a v2 repository it prints the manifest-pointing router. Pasting either by
hand is how a repository ends up carrying a router the tool then reports as
diverged. This skill deliberately keeps no second copy: only the v1 text does,
because a v1 repository has no `agents layout` to ask.

---

## Step 5: The two root files, before any symlink

`CLAUDE.md` is about to become a symlink, which destroys whatever it holds.
`stat` both paths before you read or write either one.

```bash
ls -la AGENTS.md CLAUDE.md
```

| Topology | Meaning | Action |
|---|---|---|
| `AGENTS.md` regular, `CLAUDE.md -> AGENTS.md` | current | nothing to preserve |
| `AGENTS.md` regular, `CLAUDE.md` regular | both may carry rules | reconcile **both** as Step 4 sources; a legacy repository may hold its only copy of a rule in `CLAUDE.md` |
| `AGENTS.md -> CLAUDE.md`, `CLAUDE.md` regular | **inverted** — the pre-2026-08-19 topology | see below |

**The inverted case destroys the repository if handled blind.** Running
`rm -f` on `CLAUDE.md` and then `ln -s AGENTS.md CLAUDE.md` deletes the only real file
and leaves `AGENTS.md -> CLAUDE.md -> AGENTS.md`: a symlink loop, and every line
of context gone. Invert deliberately instead:

```bash
cat CLAUDE.md                 # read the real content FIRST
rm AGENTS.md                  # remove the symlink, not the file
# write reconciled content to AGENTS.md as a regular file
rm CLAUDE.md
ln -s AGENTS.md CLAUDE.md
```

Only create the symlink once the content has a destination.

---

## Step 6: Skills

**Embedded skills** (`skills` in the report) are the only ones you touch.

Each skill has one canonical text per layout, and the resolved layout selects
it. The flat embedded text is the v2 canonical text; the frozen v0.5.1 text is
the v1 canonical text.

| state | action |
|---|---|
| `current` | nothing |
| `missing` | populate this layout's canonical text: `agents init`, or `agents update --all --apply` for `migrating-fleet-context` |
| `known_legacy` | refresh it: an `agents`-owned skill is rewritten by `agents update --all --apply`; a user-owned copy that is this layout's older text stays as it is on v1 and is replaced on v2; a copy that equals the other layout's canonical text is replaced, below |
| `diverged` | three-way merge, below — except `migrating-fleet-context`, which is `agents`-owned: refresh it with `agents update --all --apply` and keep no local edits |

A user-owned copy whose digest equals **the other layout's canonical text** is
replaced with this layout's canonical text. The digest proves it is not a local
edit, so there is nothing to merge; only `diverged` goes to a three-way merge or
a stop-and-ask.

### Three-way merge (`recording-what-you-learn`)

Name the three inputs before you start; an unnamed merge is a guess:

- **upstream** — the version embedded in the running binary for the resolved
  layout
- **base** — the canonical or legacy template the local file last matched,
  identified by the digest catalog that produced the `known_legacy` state
- **local** — the working file on disk

Apply upstream's changes to local, keeping local's additions. If the digest
catalog cannot identify a **base**, there is no three-way merge to perform —
**stop and ask** rather than inventing one.

**`local_skills` are not yours.** They are the repository's own procedures,
which is exactly what `.agents/skills/` is for. Do not merge, rewrite, move or
delete them.

---

## Step 7: Stores and retired stores

Create any store missing from the resolved layout, with its `README.md`.
`agents init` scaffolds them non-destructively.

Relocate each entry in `misplaced_docs` into the role it belongs to:

```bash
git mv "$(agents layout path journal)/<file>-plan.md" "$(agents layout path plans)/"
```

Then fix relative markdown links inside the moved files.

**The archive is immutable and is never a source or a destination.** It holds
executed plans and retired specs, and a record edited to stay true is not a
record. `agents drift` does not report anything under it, and neither should
you relocate out of it.

### Retired stores

A repository predating 2026-08-19 may carry `.agents/memory/` and
`.agents/reports/`. Do not relocate them wholesale — much of it is
machine-generated and belongs nowhere. Triage each file:

| content | destination |
|---|---|
| a topic-indexed finding | the `qna` role |
| a design still in force | the `design` role |
| an unexecuted plan | the `plans` role |
| generated indexes, handoff scaffolding, trace pointers, stale summaries | drop |

Name everything you dropped in the Step 10 report.

---

## Step 8: Build the traceability table

Zero rule dropping is not verifiable by looking at the result. Before you ask
for approval, produce a row for every non-boilerplate block in every source file
you read:

```
source quote (verbatim)                          | classification | destination
-------------------------------------------------|----------------|---------------------
"All tests must pass before commit; use uv, not pip" | domain rule | .agents/AGENTS.md §2
"Run `agents doctor` early and surface what it says" | boilerplate | replaced by router
"docs/sessions/... halt and resumption plan"         | misplaced doc | the `plans` store
```

Every block gets exactly one destination. Then state the count: *N blocks in, N
blocks placed, 0 unaccounted*. That sentence is the evidence for the invariant.
Without the table the invariant is an assertion, and this is precisely the
failure deterministic tools cannot catch for you.

---

## Step 9: Verify

```bash
agents layout validate       # expect exit 0
agents drift                 # expect exit 0
agents doctor                # expect every scaffold:* check ok
```

Then the repository's own suite — `go test ./...`, `pytest`, `npm test`,
whatever it uses. A migration that breaks the build is not done.

---

## Step 10: Stop for the human

**Do not stage anything yet.** Present:

1. The traceability table from Step 8, with the *N in, N placed, 0 unaccounted* line.
2. `git status --porcelain` and `git diff --stat`.
3. Anything you dropped in Step 7, named.
4. Every stop-and-ask you resolved and how.

Then wait for an explicit approval. Silence is not approval, and neither is a
clean verification in Step 9.

## Step 11: Commit and open the pull request

Only after approval, and staging the exact paths you changed — never `git add .`
and never a broad directory that sweeps up unrelated work:

```bash
git add AGENTS.md CLAUDE.md .agents/AGENTS.md .agents/layout.json docs/
git diff --cached --stat
git commit -m "refactor(context): migrate to two-tier agent context and 4-store layout"
git push -u origin feat/two-tier-context-migration
gh pr create --fill
```

`docs/` is the v1 store root; on a v2 repository stage the root the manifest
declares instead. In fleet mode, repeat from Step 2 for the next repository.

---

## Stop and ask when

- A block cannot be confidently classified as domain rule or boilerplate.
- Two destinations are both plausible for the same block.
- `router_state` is `missing` and there is no `CLAUDE.md` to read either.
- `unsupported` is non-empty, or `layout_status` is `migrating`.
- `.agents/` is ignored, so v2 is unavailable here.
- The per-role collaboration policy is missing and cannot be inferred.
- A `diverged` skill has no identifiable **base**.
- The working tree is dirty, or the repository is mid-rebase or mid-merge.
- `misplaced_docs` names a file whose correct store is genuinely unclear.

Asking costs one message. Guessing costs a rule nobody notices is gone.

## Red flags

- About to remove `CLAUDE.md` without having `stat`-ed `AGENTS.md` first.
- About to run `agents update` without `--all`; the CLI rejects it.
- About to run `agents layout migrate --apply` without `--backup-tag`, or
  without an approved dry run.
- About to restart a migration instead of running
  `agents layout migrate --resume --apply`.
- About to apply the `diverged` procedure to a `known_legacy` router.
- About to paste the v1 router into a v2 repository instead of using
  `agents layout show --router`.
- About to commit before presenting the traceability table.
- About to touch a skill listed in `local_skills`.
- About to relocate something out of the archive.
- Parsing `agents drift --all --json` as an object.

## Where this comes from

This skill is owned and maintained by the `agents` CLI, not by the repository it
is sitting in. `agents update --all --apply` overwrites it from the installed binary,
so local edits here do not survive — if this repository needs different
behaviour, that belongs in `.agents/AGENTS.md`.

The text you are reading was selected by the repository's resolved layout
(design §0.8). A v1 repository reads the frozen v0.5.1 text, which cannot drive
a layout migration; the v2-aware text arrives after `agents layout migrate`
flips the layout.

**The tool is the authority on state, not this document.** `agents drift` and
`agents doctor` report what a repository actually is; where they and this skill
disagree, they are right and this copy is stale. Step 0 is how you find out.

The architecture rationale — why the router is a fixed template, why domain
rules live one level down, what each digest state proves — lives with the
`agents` project's own design documents, upstream. It is deliberately not
restated or linked here: this file is scaffolded into repositories that have no
copy of those documents, and a pointer to a path that does not exist is worse
than no pointer at all.
