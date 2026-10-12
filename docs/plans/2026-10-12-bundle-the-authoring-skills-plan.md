# Bundle the two authoring skills

**Status:** active — for v0.9.0.

## What this is

`agents init` installs one bundled skill. Two more were written for this
repository and exist only on the author's machine, under `~/.dsh/skills/`:

- `writing-for-humans` — what a text must mean, the order a reader needs, and the
  sentence habits worth fixing last.
- `reviewing-with-a-team` — the three review lenses, the prompts that make each of
  them produce findings, and the delivery gate that decides when text is safe to
  ship.

Both are procedure rather than knowledge about one repository, which is the test
`recording-what-you-learn` passes and the reason it travels with every repository.
They belong in the binary for the same reason.

## What has to change

1. Copy each skill to `agents/internal/scaffold/assets/skills/<name>/SKILL.md`.
   They are already in the shape the embed expects; `recording-what-you-learn` is
   the model.
2. **Collapse the two registries before adding to them.** `bundledSkills`
   (`agents/internal/scaffold/scaffold.go:116`) is the ordered name list `init`
   iterates; `skillAssets` (`:141`) maps a name to its asset path. The two must
   agree and nothing checks that they do, so three skills would be six places to
   keep in step. Make one ordered list of name-with-path, derive the lookup from
   it, and export it, so the installer, the lookup and the guards all read one
   list. The comment above `bundledSkills` says "recording-what-you-learn is the
   only one" and goes with the change.
3. Widen the two guards that name one skill by hand.
   `TestInstalledSkillMatchesTheBinary` (`agents/docs_test.go:175`) and
   `TestInstalledSkillNamesNoDeletedCommand` (`:204`) each carry
   `"recording-what-you-learn"` as a constant, and neither can iterate the bundled
   set today: `bundledSkills` is unexported in `internal/scaffold`, `docs_test.go`
   is package `main`, and `SkillAssetPath` resolves a name without listing any.
   Both iterate the list exported in step 2 — that is what makes the collapse pay.
4. **Pin the assets with the two questions they actually raise**, not with the
   store walk. `assets/docs/plans/README.md:7` links `../design/`, relative to a
   repository being initialized, so a resolvable-path rule has the wrong referent
   for a template. Two pins instead:
   - **a bundled asset may not name a dated document.** The two dangling citations
     v0.8.1 removes were `docs/design/2026-08-19-knowledge-is-documentation.md` and
     `docs/design/2026-10-09-the-personal-build-removal-analysis.md`, and both
     existed only in this repository: the reader of an initialized repository has
     never had either. A `\d{4}-\d{2}-\d{2}-<slug>.md` match anywhere under
     `assets/` fails, and the remedy is to state the fact rather than cite it. No
     asset names one today, so the pin starts green and cannot be worked around in
     silence.
   - **every `agents` command an asset names must exist.** The command-span check
     already runs over the living documents and the two stores; point it at
     `assets/skills/**` as well.
5. `agents/README.md:9` names what `init` installs today, so it needs the three
   names. The root README's generated block (`README.md:102-112`) is a command
   table rendered from each command's `Summary`, so it will not carry them on its
   own — `init`'s Summary is `agents/commands.go:22`.

## Decisions this plan carries

- **One registry, exported** (step 2), rather than a second reader over the
  existing two. Two lists that must agree, with no check between them, is the same
  defect as two tests each hardcoding one skill's name.
- **Narrow pins, not a widened walk** (step 4). The dated-document rule is the one
  that would have prevented v0.8.1 in both of its cases.
- **`init`, not `wire`.** `init` writes content that belongs to the repository
  afterwards: it writes a skill only where the path is absent
  (`writeIfAbsentFromFS`, `scaffold.go:272`), and `doctor` reports a divergence
  from the binary as OK rather than as a fault — coherent only if the text is the
  repository's after install. `wire`'s subject is machine wiring, and its direction
  is now removal. Bundling through `init` also leaves the text under version
  control, where a `wire`-only install would put it in a harness directory outside
  git.
- **Nothing reaches an existing repository.** `init` writes only where the path is
  absent, so the two skills arrive on a fresh repository and on no existing one.
  The 0.9.0 notes say that; no migration is written for it.

## Verified by

`agents` 10 packages (8 with tests) and `bootstrap.d` 6, whole runs; and on a fresh
repository, `agents init` followed by a listing of `.agents/skills/` naming all
three.
