# Spec 6 — releases and binary distribution

**Date:** 2026-08-10 (as part of spec 5's scope note) / 2026-08-11 (split out)
**Status:** scope only — not designed, not implemented
**Depends on:** [spec 1](2026-08-07-agents-repo-context-design.md) for the Go
module, security boundaries, supported-OS assumptions, and the global hook
installation contract. Also on [spec 5](2026-08-11-spec-5-verification-gate.md):
a release pipeline on top of a repository with no automated gate publishes
unverified binaries on a schedule.
**Carries obligations from:** [spec 2](2026-08-07-spec-2-dotfiles-hygiene.md),
implemented — chiefly §2.2's seam.

---

## Why it was split from spec 5

The 2026-08-10 scope note covered pull-request verification, release
construction, publishing and installation, and version drift under one number.
Verification needed no open decisions; the rest was gated on ten. A single
implementation plan could not be written against the union, which is why it never
left "scope only."

Spec 5 kept verification and was designed. Everything below moved here intact.

**Decided 2026-08-11 (human): this spec publishes both binaries, and any later
one on the same terms.** `agents` and `bootstrap` are separate modules producing
separate binaries and only `bootstrap` is on the stage-zero path — but publishing
one and not the other would leave half this repository's executables installed by
a mechanism the other does not use, which is the two-entry-points problem spec 2
removed from provisioning. Verification, versioning, checksums and the upgrade
path are therefore per-binary concerns solved once and applied to each, not a
pipeline built around `agents` with `bootstrap` bolted on. A third binary should
need no new decisions.

**Decided 2026-08-11 (human): other people install these binaries.** Not only
this fleet. A tap, a formula, a versioning policy and a documented upgrade path
are therefore justified rather than speculative, and the audience for the install
instructions is not the author.

## The seam this spec fills

`./bootstrap` is a shell shim whose only job is to reach Go and hand over. When
`command -v go` fails it **refuses**, printing one platform-specific command and
exiting `2`. It installs nothing: the auto-install branch was deleted rather than
repaired, because `set -e` does not apply inside `$( )` and a failed install
reached the build as an empty command (spec 2, shim defect 1).

That refusal is deliberate and adequate, and it is not the end state.
[Spec 2 §2.2](2026-08-07-spec-2-dotfiles-hygiene.md#22-the-seam-where-spec-6-removes-go-as-a-dependency)
records the structure a release gives it:

1. A released binary matching this checkout is cached and verified → `exec` it.
2. Otherwise a release exists for this platform → download, verify checksum,
   cache, `exec`.
3. Otherwise `go` is on `PATH` → build from source as today.
4. Otherwise refuse.

Steps 3 and 4 are today's shim unchanged, so this is an extension rather than a
rewrite, and **spec 2 does not depend on this spec** — build-from-source remains
the floor.

**The property that must survive is the one to design against: the shim must
never `exec` a binary it cannot attribute to the current checkout.** Today
attribution is a cache key derived from the resolved repository root, which
exists because an unkeyed cache let a clone and a worktree share one binary and
run old code against a new tree, silently (spec 2, shim defect 2). Under a
release, the same property has to be carried by a checksum plus a version the
binary reports back.

## The hardest open problem: what a released binary stamps

**A binary built by CI belongs to no checkout, so it has no root to stamp.**

The `~/dotfiles` assumption was removed on 2026-08-11 by stamping the root at
build time, with an explicit fallback chain — `-X main.dotfilesRoot` →
`AGENTS_DOTFILES_ROOT` → `$HOME/dotfiles` — and both builders pass it. Inferring
the root from `core.hooksPath` was rejected deliberately: doctor's
`git-hooks:global` check *compares* those two, so deriving one from the other
would make the check pass by construction.

But a release falls through to `$HOME/dotfiles` — exactly the assumption that was
just removed. Three shapes of answer, none chosen:

- the installer supplies the root at install time,
- a distributed binary resolves the root at runtime, or
- `AGENTS_DOTFILES_ROOT` becomes a documented part of the install.

For the record, the failure the old assumption produced — `agents doctor` on a
correctly provisioned machine whose checkout was elsewhere:

| Check | Result | Because |
|---|---|---|
| `git-hooks:global` | fail | global `core.hooksPath` holds the real checkout's `git/hooks.d`; doctor compared it against `~/dotfiles/git/hooks.d` |
| `git-hooks:links` | fail | it looked for the four hook links in a directory that does not exist |
| `git-attributes` | fail | `core.attributesFile`'s origin is the real `gitconfig.shared` |
| `git-hooks:effective` | warn | same mismatch, reported as a shadowed hook directory |

A second instance was worse because it was silent: `ExtrasDir` pointed at
`$HOME/dotfiles/git/hooks`, and `githook.go:127` reads a missing extras directory
as "no personal hooks", so a relocated checkout ran none and said nothing.

Spec 5 §8 adds the doctor check that the resolved root still exists. That guards
the local case. It does not answer what a distributed binary should resolve.

## Scope

### Release construction

- Decide supported macOS and Linux architecture combinations from the actual
  fleet and cross-build constraints; Windows is out unless chosen deliberately.
- Define versioning and release triggers: tags, manual promotion, or another
  reviewed mechanism.
- Embed version and VCS provenance in both binaries.
- Reproducibility needs a falsifiable criterion or it does not belong in the
  spec. The obvious one: build twice in CI and require identical `sha256`.
- Publish checksums, and decide whether signing, attestations, and an SBOM belong
  in the first release boundary.

### Publishing and installation

- Evaluate a Homebrew formula and bottles as the primary installation and upgrade
  interface, including where the tap lives.
- Decide whether Homebrew builds from source, consumes GitHub release binaries,
  or uses CI-produced bottles.
- Replace the hard-coded assumption that the executable lives at `~/bin/agents`
  with a reviewed stable resolution contract that still prevents hook recursion,
  binary substitution, and accidental activation of a foreign executable.
- Preserve the installer's preflight, ownership, rollback, and
  no-partial-activation guarantees when installation and global-hook activation
  become separate steps.
- Preserve the shim's attribution property when a published binary is fetched
  rather than built.

### Version and drift diagnostics

- Add a content-safe version surface for users and support reports.
- Decide what `agents doctor` should compare: installed release, repository
  source, hook-link target, or an explicit combination.
- Make stale-binary reporting actionable without silently downloading,
  upgrading, rewiring repositories, or granting harness trust.

## Constraints carried from spec 1

- Release jobs must never approve or bypass Codex, Claude Code, Git, or other
  trust and security prompts.
- No generated binary is committed to the dotfiles tree.
- Release automation must not publish machine-local paths, registry contents,
  transcript pointers, hook trust state, or user configuration.
- The record type retains its structural inability to carry assistant messages,
  tool input, or tool response.
- **A failed update must leave the previously installed executable and the active
  global hook chain usable.**

## Explicitly out of scope

- Automatically granting harness or repository trust.
- Automatically running `agents init`, `agents wire`, or `agents update --apply`
  across the fleet after an upgrade.
- Publishing machine-local registries, traces, memories, or handoffs.
- Designing `agents distill`, the wiring DSL, or broader dotfiles cleanup.

## Open questions for the design phase

- Which operating systems and architectures are genuinely required?
- Should releases live in this dotfiles repository, a dedicated binary
  repository, or a dedicated Homebrew tap?
- Formula built from source, from release binaries, or from CI-built bottles?
- What event promotes a tested commit to a release, and how is rollback
  expressed?
- Should `make agents` remain a supported developer install, coexist with
  Homebrew, or become development-only?
- Does spec 2's build-from-checkout path stay a documented fallback once a
  release exists, or become development-only?
- How should the global hook installer locate a Homebrew-managed binary without
  weakening its exact-identity checks?
- Should doctor report only version mismatch, or also offer a non-mutating
  command that explains the reviewed upgrade path?
- What does a released binary stamp as its checkout root? See
  [the hardest open problem](#the-hardest-open-problem-what-a-released-binary-stamps).

## Answered, recorded so they are not reopened

- ~~Is `bootstrap` published at all?~~ **Both binaries are published, on the same
  terms, and so is any later one.** 2026-08-11.
- ~~Does the `~/dotfiles` assumption belong to this spec or a spec 1
  amendment?~~ **Neither — it was a defect and was fixed outright** by stamping
  the root at build time, in
  [checkout path and field defects](../../plans/2026-08-11-checkout-path-and-field-defects.md).
  What remains is the narrower question of what a binary belonging to no checkout
  should stamp.
- ~~Which Linux distributions does a runner actually verify?~~ **Answered by
  [spec 5 §7](2026-08-11-spec-5-verification-gate.md#7-linux):** Debian and Arch
  containers, the same two families spec 2 targets on paper.
