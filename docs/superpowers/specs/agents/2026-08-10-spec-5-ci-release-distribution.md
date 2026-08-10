# Spec 5 — CI, releases, and binary distribution

**Status:** scope only — not designed, not implemented
**Depends on:** [spec 1](2026-08-07-agents-repo-context-design.md) for the Go module,
security boundaries, supported operating-system assumptions, and global hook
installation contract.

The number is a stable catalog identifier, not a promise that Specs 2–4 must be
implemented first. This spec can be the next one started because its only dependency
is the implemented Spec 1 foundation.

## Why it exists

Spec 1 deliberately completed the local loop first: `make agents` builds the
current checkout to `~/bin/agents`, and `make githooks` safely activates that exact
file. That proves the binary and installer, but it leaves source verification,
publishing, installation, and upgrades coupled to one local checkout.

CI cannot be designed as merely "run `go build`." The published format determines
how users install, verify, upgrade, roll back, and point the global hook chain at the
binary. A GitHub Actions artifact that nothing safely consumes would not close the
loop.

The likely direction is a Homebrew-installable release. That is a design input, not
yet a settled formula, tap, bottle matrix, or release policy.

## Current baseline

- The repository has no `.github/workflows/`, GitHub Actions workflows, PR checks,
  build artifacts, or releases.
- `Makefile` builds the current checkout with `go build -trimpath` directly to
  `~/bin/agents`.
- `git/install-hooks.sh` validates and activates an already-present executable; it
  neither downloads nor updates one.
- The binary embeds Go/VCS build metadata, but the CLI has no version or update
  command and `agents doctor` does not compare an installed release with source.
- The existing Go suite, race suite, vet, formatting, diff, and gitleaks gates are
  run manually rather than by GitHub.

## Scope

### Pull-request and branch verification

- Define GitHub Actions triggers and path filters without allowing a docs-only
  change to masquerade as source verification.
- Pin the Go and gitleaks toolchains deliberately.
- Run the complete required gate: normal tests, the ambient-Go-cache-cleared test
  arm, race tests, vet, formatting, diff checks, and secret scanning.
- Keep all tests isolated from global Git, hook, trust, and machine-registry state.

### Release construction

- Decide supported macOS and Linux architecture combinations from the actual fleet
  and cross-build constraints; Windows remains out of scope unless chosen
  deliberately.
- Define versioning and release triggers: tags, manual promotion, or another
  reviewed mechanism.
- Produce reproducible-enough binaries with embedded version and VCS provenance.
- Publish checksums and decide whether signing, attestations, and an SBOM belong in
  the first release boundary.

### Publishing and installation

- Evaluate a Homebrew formula and bottles as the primary installation and upgrade
  interface, including where the tap lives.
- Decide whether Homebrew builds from source, consumes GitHub release binaries, or
  uses bottles produced by CI.
- Replace the hard-coded assumption that the executable lives at `~/bin/agents`
  with a reviewed stable resolution contract that still prevents hook recursion,
  binary substitution, and accidental activation of a foreign executable.
- Preserve the installer's preflight, ownership, rollback, and no-partial-activation
  guarantees when installation and global-hook activation become separate steps.

### Version and drift diagnostics

- Add a content-safe version surface for users and support reports.
- Decide what `agents doctor` should compare: installed release, repository source,
  hook-link target, or some explicit combination.
- Make stale-binary reporting actionable without silently downloading, upgrading,
  rewiring repositories, or granting harness trust.

## Constraints carried from Spec 1

- CI and release jobs must never approve or bypass Codex, Claude Code, Git, or other
  trust and security prompts.
- No generated binary is committed to the dotfiles tree.
- Release automation must not publish machine-local paths, registry contents,
  transcript pointers, hook trust state, or user configuration.
- The record type must retain its structural inability to carry assistant messages,
  tool input, or tool response.
- The embedded gitleaks configuration and tracked canonical configuration remain
  byte-for-byte pinned.
- A failed update must leave the previously installed executable and active global
  hook chain usable.

## Explicitly out of scope

- Automatically granting harness or repository trust.
- Automatically running `agents init`, `agents wire`, or `agents update --apply`
  across the fleet after an upgrade.
- Publishing machine-local registries, traces, memories, or handoffs.
- Designing `agents distill`, the wiring DSL, or broader dotfiles cleanup.

## Open questions for the design phase

- Which operating systems and architectures are genuinely required?
- Should releases live in this dotfiles repository, a dedicated binary repository,
  or a dedicated Homebrew tap?
- Formula build from source, release binaries, or CI-built bottles?
- What event promotes a tested commit to a release, and how is rollback expressed?
- Should `make agents` remain a supported developer install, coexist with Homebrew,
  or become development-only?
- How should the global hook installer locate a Homebrew-managed binary without
  weakening its current exact-identity checks?
- Should Doctor report only version mismatch, or also offer a non-mutating command
  that explains the reviewed upgrade path?
