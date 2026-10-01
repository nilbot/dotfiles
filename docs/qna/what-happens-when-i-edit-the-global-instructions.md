# What happens when I edit the global instruction file?

## Context

2026-10-01: a session working in another repository edited `global/AGENTS.md` to
add the "Write so the reader can decode it" rule. It then could not tell what the
edit had set off: whether anything classified instruction files and would report
drift, whether the `agents` version had to be bumped along with it, or how a
change to a file that lives in one checkout reaches a session that is working in
another repository.

No living document answered any of it. Until this entry, `git log --oneline --
global/AGENTS.md` showed two commits — its creation (`9e39822`) and that edit —
and `grep -rln "global/AGENTS.md" docs/journal docs/qna` returned nothing. The
facts existed, scattered under headings keyed to other subjects: the file table
in `git/README.md`, the comments in `bootstrap.d/links.manifest`, and
`docs/plans/2026-09-21-skills-single-home-plan.md`, which is where the file was
decided and which is a dated record nobody re-reads.

## Answer

### Where it is, and when an edit takes effect

One tracked file, `global/AGENTS.md` in this checkout, with three symlinks to it
declared at `bootstrap.d/links.manifest:22-24`: `~/.claude/CLAUDE.md`,
`~/.codex/AGENTS.md` and `~/.dsh/AGENTS.md`. There is no second copy to keep in
step — `grep -rln "No AI attribution" .` matches only `global/AGENTS.md`.

Because those three paths are symlinks and not copies, an edit is live in the
next session on this machine, in every repository and under every harness, with
no distribution step. Other machines get it when their dotfiles checkout is
updated.

**Checking anything else out replaces the instructions too.** The symlinks point
at the working-tree file, not at a branch or a commit, so in this checkout `git
switch`, `git checkout <old commit>` and an interrupted merge all change what the
next session reads. Measured 2026-10-01: after merging a change here, a `git
switch master` left master one merge behind, and the rules were gone from the
working tree until `git fetch && git merge --ff-only origin/master` ran. Reach for
that rather than `git pull` when the working tree has unrelated changes: with
`pull.rebase` set, `git pull` refuses outright and leaves the checkout where it
was.

### What reacts to an edit

Nothing classifies instruction files: `agents drift` and `agents update` were deleted
on 2026-09-21, along with the fleet registry and `internal/drift`, so there is no
currency report to trip and no fleet to refresh. See
`why-does-agents-init-never-update-existing-instructions.md:77-103` and
Amendment 3 of `../design/2026-08-29-two-tier-context-and-llm-migration-architecture.md`.
`ls agents/internal` confirms the package is gone.

No production code reads the file either. The only command that inspects any
`AGENTS.md` is `agents doctor`, and it inspects the repository's own root
`AGENTS.md` (`agents/internal/doctor/doctor.go:329`) and `.agents/AGENTS.md`
(`:388`).

Four checks do read it, all four run in CI, and all four passed on the edit
above:

| Check | Where | What it enforces |
|---|---|---|
| `TestLivingDocumentsNameOnlyRealCommands` | the file is listed at `agents/docs_test.go:84`, the test is at `:107`, and the name is required by `agents/doctests.txt:43` | every code span naming an `agents` subcommand resolves to a command the registry defines |
| `TestLivingDocumentsNameOnlyResolvablePaths` | added 2026-10-01, in `agents/docs_test.go`, named in `agents/doctests.txt` | every inline code span naming a path resolves in the checkout |
| `TestTask18RetiresTemplateAndClaudeHookInstallers` | `agents/install_hooks_test.go:1211-1244` | the file does not advertise retired hook artifacts (`git/templates`, `check-commits.sh`, `~/.claude/commit-msg`, …) |
| `bootstrap check` | `bootstrap.d/links.manifest:22-24` | the declared links have sources in the checkout; on 2026-10-01 it printed `8 ok, 0 warn, 0 fail`, including `manifest-kinds 12 rows, each present and of its declared kind` |

The commands, in the order they are worth running:

```bash
(cd agents && go test -count=1 ./...)
(cd bootstrap.d && go test -count=1 ./...)
./bootstrap check
```

### It needs no `agents` release

The version is not a constant in the source. `agents/main.go:18` is
`version = "dev"`, and `script/package-release.sh:37` fills it from the tag the
release was run for (`-X main.version=${VERSION}`, reached from
`.github/workflows/release.yml:120`). The release artifact does not contain the
file either — `script/package-release.sh:43` packs `agents README.md` and nothing
else — and it could not be embedded: the repository's only two `//go:embed`
directives (`agents/internal/guard/guard.go:41`,
`agents/internal/scaffold/assets.go:5`) are inside the `agents` module, and
`global/` is outside it.

So tagging a release for a change to this file would publish a binary that
behaves exactly like the previous one, and `brew upgrade` would deliver nothing.
This file travels by `git pull` in the dotfiles checkout.

What does need a release is anything under `agents/`, including
`agents/internal/scaffold/assets/**`: the per-repository `.agents/AGENTS.md` and
the skill text that `agents init` writes are embedded in the binary, and they are
different files from this one.

### The edit lands like any other change

Branch and pull request. The repository's ruleset requires it and has no bypass
actors, so `git push origin master` is refused: `gh ruleset check master --repo
nilbot/dotfiles` lists `deletion`, `non_fast_forward`, `pull_request` and
`required_status_checks` (`gate`) from ruleset 21769474. The classic protection
API reports master as unprotected, so that API's answer is not the question to
ask; `how-do-github-rulesets-map-to-classic-branch-protection.md` has the
mapping.

### Where knowledge about the machine tier lives

This file has no `docs/` of its own — the two-tier design gives stores to
repositories, not to the machine tier — so this checkout is the only place a
machine-global decision can be recorded. The only pointer to it from another
repository is the header of `global/AGENTS.md` itself, which says the file is
tracked here. A session that greps this checkout for the path lands on this
entry; one that does not will re-derive everything above.

### A first draft that failed the rule it was adding

The rule's first version illustrated "name things" with `tools/doc_drift.py` and
`docs/CONTRIBUTING.md` §3 — paths from another repository that resolve in none of
the repositories reading this file. An example the reader cannot resolve is a
pointer again. The examples now name `bootstrap.d/links.manifest:22`, a section
of `global/AGENTS.md`, and `./bootstrap check`, all of which resolve inside this
checkout, which every reader has because the symlink that delivers the file
points into it.
