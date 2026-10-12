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
   `"recording-what-you-learn"` as a constant, and both should iterate the bundled
   set. That needs an exported way to enumerate it first: `bundledSkills` is
   unexported in `internal/scaffold` and `docs_test.go` is package `main`, and
   `SkillAssetPath` resolves a name without listing any.
4. **Ask the assets the three questions no guard asks.** `SkillCurrent`
   (`agents/internal/scaffold/scaffold.go:356`) reads an embedded skill, but only
   to compare its digest with the installed copy. Nothing opens `assets/**` to
   check what the text *says*: the backwards checks in `agents/docs_test.go` walk
   `.agents/skills/**` and the two stores, and no test, walk or CI step reaches the
   assets for path, command or citation validity. That is how
   `recording-what-you-learn` shipped pointing at
   `docs/design/2026-08-19-knowledge-is-documentation.md` after that design was
   deleted, and how v0.8.1 came to carry two more of the same. Widen a walk to the
   assets or pin each one, and ask of each: does every path it names resolve, does
   every command it names exist, and does every citation still point at something.
5. `agents/README.md:9` names what `init` installs today, so it needs the three
   names. The root README's generated block (`README.md:102-112`) is a command
   table rendered from each command's `Summary`, so it will not carry them on its
   own — `init`'s Summary is `agents/commands.go:22`.

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
