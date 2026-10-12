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
2. Register both in the two places that hold the current one: `bundledSkills`
   (`agents/internal/scaffold/scaffold.go:116`), which `init` iterates, and
   `skillAssets` (`:141`), which resolves a name to an asset. The comment above
   `bundledSkills` says "recording-what-you-learn is the only one" and has to go
   with the change.
3. Widen the two guards that name one skill by hand.
   `TestInstalledSkillMatchesTheBinary` (`agents/docs_test.go:175`) and
   `TestInstalledSkillNamesNoDeletedCommand` (`:204`) each carry
   `"recording-what-you-learn"` as a constant. Both should iterate
   `bundledSkills`, so a third skill cannot be installed unguarded.
4. **Close the gap that let v0.8.1 exist.** No guard reads `assets/skills/**`:
   the backwards check in `agents/docs_test.go` walks `.agents/skills/**` and the
   two stores, and nothing in the tree reads the assets at all. That is how
   `recording-what-you-learn` shipped pointing at
   `docs/design/2026-08-19-knowledge-is-documentation.md` after that design was
   deleted — the pointer was in an asset no check opened. The same three questions
   apply to the two new files: does every path they name resolve, does every
   command they name exist, and does every citation still point at something.
   Widen the walk to the assets, or pin each asset directly.
5. `agents/README.md` and the generated block in the root `README.md` say what
   `init` installs. Both need the three names.

## Two things to settle first

- **Nothing reaches an existing repository.** `init` writes a skill only when the
  path is absent, so these arrive on a fresh repository and on no existing one.
  That is today's behaviour for the one skill; the release notes should say so
  rather than a migration being written.
- **`init` or `wire`.** `wire` links `.claude/skills` and `.codex/skills` at
  `.agents/skills`, so bundling through `init` is what makes the text reachable by
  a harness in a repository that has been initialized. Confirm that is the intent
  before the assets move.

## Verified by

`agents` 8 packages and `bootstrap.d` 6 packages, whole runs; and on a fresh
repository, `agents init` followed by a listing of `.agents/skills/` naming all
three.
