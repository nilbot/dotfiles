# Spec 5 — CI, releases, and binary distribution

**Status:** scope only — not designed, not implemented
**Depends on:** [spec 1](2026-08-07-agents-repo-context-design.md) for the Go module,
security boundaries, supported operating-system assumptions, and global hook
installation contract.
**Carries obligations from:** [spec 2](2026-08-07-spec-2-dotfiles-hygiene.md), now
implemented. See [What spec 2 hands over](#what-spec-2-hands-over). These are scope
items, not a dependency inversion: spec 2 runs today without any of them, and its
build-from-checkout floor stays whatever this spec decides.

The number is a stable catalog identifier, not a promise that Specs 2–4 must be
implemented first. This spec can be the next one started because its only dependency
is the implemented Spec 1 foundation.

## Why it exists

Spec 1 deliberately completed the local loop first: `make agents` builds the
current checkout to `~/bin/agents`, and `git/install-hooks.sh` safely activates that
exact file. That proves the binary and installer, but it leaves source verification,
publishing, installation, and upgrades coupled to one local checkout.

`make githooks` was the installer's caller when that was written. Spec 2 reduced the
Makefile to the `agents` target alone and moved hook installation into bootstrap's
devtools phase, which invokes the same `git/install-hooks.sh` with the same
arguments. The mechanism did not change; only its caller did.

CI cannot be designed as merely "run `go build`." The published format determines
how users install, verify, upgrade, roll back, and point the global hook chain at the
binary. A GitHub Actions artifact that nothing safely consumes would not close the
loop.

The likely direction is a Homebrew-installable release. That is a design input, not
yet a settled formula, tap, bottle matrix, or release policy.

Spec 2's implementation made this spec's debt concrete rather than anticipated. It
deleted the only automated Go gate this repository had, added a second Go module, and
made a Go toolchain a stage-zero requirement for provisioning any machine at all.
What it handed over is recorded below, before the scope it feeds.

## Current baseline

- The repository has no `.github/`, GitHub Actions workflows, PR checks,
  build artifacts, or releases. The directory does not exist.
- `Makefile` builds the current checkout with `go build -trimpath` directly to
  `~/bin/agents`, and that one target is now the whole file. Provisioning is
  `./bootstrap`'s; the Makefile is a developer inner-loop convenience, and
  `bootstrap.d/makefile_test.go` pins it to that.
- Two Go modules — `agents/` and `bootstrap.d/` — and deliberately **no `go.work`**
  (spec 2 §11). Two `go test ./...` invocations, two independent build graphs.
- `git/install-hooks.sh` validates and activates an already-present executable; it
  neither downloads nor updates one. Its only caller is bootstrap's devtools phase,
  `bootstrap.d/internal/phase/devtools.go`, which runs it in `preflight` mode, builds
  `agents`, then runs it in `install` mode.
- `./bootstrap` builds `bootstrap.d/` from source into a per-checkout cache on every
  machine, and refuses when Go is absent.
- The binary embeds Go/VCS build metadata, but the CLI has no version or update
  command and `agents doctor` does not compare an installed release with source.
- The existing Go suite, race suite, vet, formatting, diff, and gitleaks gates are
  run manually rather than by GitHub. Since spec 2 removed `git/hooks/go.pre-commit`,
  no automated Go gate exists anywhere in this repository.

## What spec 2 hands over

Six items spec 2's implementation established or exposed. Each is recorded as scope —
what CI must answer — not as an answer. Where spec 2 measured something, the
measurement is cited rather than generalised.

### The Go dependency `./bootstrap` cannot remove by itself

`./bootstrap` is a shell shim whose only job is to reach Go and hand over. When
`command -v go` fails it **refuses**, printing one platform-specific command —
`brew install go` on Darwin, `sudo apt-get install -y golang-go` (or
`sudo pacman -S go`) on Linux, `https://go.dev/dl/` otherwise — and exits `2`. It
installs nothing: the auto-install branch was deleted rather than repaired, because
`set -e` does not apply inside `$( )` and a failed install reached the build as an
empty command (spec 2, shim defect 1).

That refusal is deliberate and adequate, and it is not the end state.
[Spec 2 §2.2](2026-08-07-spec-2-dotfiles-hygiene.md#22-the-seam-where-spec-5-removes-go-as-a-dependency)
records the seam this spec fills: a released binary already cached and verified is
`exec`ed; otherwise a release for this platform is downloaded, checksum-verified,
cached and `exec`ed; otherwise Go on `PATH` builds from source as today; otherwise
refuse. The last two steps are today's shim unchanged, so this is an extension.

The property that must survive is stated there and is the one to design against:
**the shim must never `exec` a binary it cannot attribute to the current checkout.**
Today attribution is a cache key derived from the resolved repository root, which
exists because an unkeyed cache let a clone and a worktree share one binary and run
old code against a new tree, silently (spec 2, shim defect 2). Under a release, the
same property has to be carried by a checksum plus a version the binary reports back.

**Decided 2026-08-11 (human): this spec publishes both binaries, and any later one
on the same terms.** They are separate modules producing separate binaries, and only
`bootstrap` is on the stage-zero path — but publishing one and not the other would
leave half the repository's executables installed by a mechanism the other does not
use, which is the two-entry-points problem spec 2 removed from provisioning.

Verification, versioning, checksums and the upgrade path are therefore per-binary
concerns to be solved once and applied to each, not a pipeline built around
`agents` with `bootstrap` bolted on. A third binary should need no new decisions.

### The Go gate that no longer exists anywhere

`git/hooks/go.pre-commit` was removed on spec 2's branch in `c0e3bb1`, whose message
ends "This is CI's job. See spec 5." It is recoverable with
`git show ac5286a:git/hooks/go.pre-commit`. So that this spec records what it is
replacing rather than an intent, what the hook did:

- Ran only in a repository with `.go` files **at the root** (`ls | grep '\.go$'`),
  which is neither of this repository's modules.
- Ran `go build -n`, then `go test`, then `go fmt`, then `go vet` — each without
  `./...`, so each covered one package.
- Redirected every command to `>/dev/null 2>&1` and printed a fixed message on
  failure, so a failure carried **no diagnostic**.
- `go fmt` rewrote files mid-commit without staging them, so a formatting fix
  silently failed to be committed.
- Nothing installed it. `git/install-hooks.sh` links four fixed hook names —
  `pre-commit`, `commit-msg`, `post-merge`, `post-checkout` — at the `agents` binary
  and never names this file, so it had been inert.

The gate CI owes is therefore not a port of that hook. It is the first build, test,
`gofmt` and `vet` gate this repository has ever actually run automatically.

### Two modules, and the test cache that can hide a broken one

`agents/go.mod` and `bootstrap.d/go.mod`, no `go.work`. Spec 2 §11 chose this so a
bootstrap failure can never block an `agents` release, and its
[Rejected alternatives](2026-08-07-spec-2-dotfiles-hygiene.md#rejected-alternatives)
rejected a tracked `go.work` specifically because it alters module resolution and
would hand this spec's release path a `GOWORK=off` footgun. CI must build and test
both, separately, and must not reintroduce a workspace to make that convenient.

**The concrete gotcha the branch hit.** `go test` caches a result keyed on Go inputs.
Several tests here assert against **tracked non-Go files**:
`bootstrap.d/makefile_test.go` reads the `Makefile`, and
`agents/internal/doctor/doctor_test.go` reads `git/gitconfig.shared`. Editing one of
those files does not invalidate the cached pass, so a real breakage stays green.
`doctor_test.go:378` records it in the tree:

> Reading a tracked file means the Go build cache does not know when the answer
> changes: run this module's tests with `-count=1` after any rename.

This is not hypothetical, and it was watched happening. When spec 2's Task 15 deleted
the Makefile's `githooks` target, two tests in `agents` that drove it began failing —
but `go test ./...` in that module reported `ok (cached)` because no Go input had
changed. Only `-count=1` revealed it. The break was real and was fixed in `06ff3c9`;
what leaves no artifact is the stale pass that hid it, which is precisely why the
requirement belongs in this document. The `gitconfig.symlink` → `gitconfig.shared` rename shipped
on this branch with `doctor.go` still naming the old file, which made the
`git-attributes` check fail on every healthy machine; only `install_hooks_test.go`
noticed. Spec 2's plan prescribes `go test -count=1 ./...` at four separate points
for this reason. The required gate must run with caching defeated, and that belongs
in this spec's requirements rather than in a contributor's memory.

### Linux, which only CI can reach

Spec 2's [Known gaps](2026-08-07-spec-2-dotfiles-hygiene.md#known-gaps-2026-08-11)
states it without hedging, and it is repeated here at the same strength:

> **Linux is untested.** No phase in this design has run on Linux. The stage-zero
> package selection, the Homebrew prefix at `/home/linuxbrew/.linuxbrew`, and `chsh`
> under a different `/etc/shells` convention are all unexercised.

Its risk table records the same thing as **not mitigated**, and says end-to-end
verification is owed before the Linux path is described as supported. No Linux
machine was available during implementation, and none is expected to be. CI is the
only realistic route to that coverage, which promotes a Linux runner from a
nice-to-have to **the thing that closes spec 2's largest known gap**.

Two measured details constrain what such a job can be:

- `Applier.run` leaves `cmd.Stdin` nil, so stage zero's `Sudo` calls need cached sudo
  credentials or fail with "no tty present".
- `plan` exits `2` on a machine that lacks Homebrew or fish rather than previewing
  (Known gaps 2), so a bare-runner smoke test cannot assume `plan` is the safe verb
  everywhere.

Spec 2's `dotfiles` profile — preflight, config and verify, with no sudo, no network,
no package manager and no login-shell change — exists precisely so it is safe inside
a container. Whether Linux coverage starts there or at the full `workstation` profile
is this spec's decision, as is which distributions are targeted: spec 2's stage-zero
commands are written for Debian/Ubuntu and Arch/Manjaro and neither is verified.

### The shared exit-code table is an interface CI reads

`0` ok, `1` advisory, `2` block/refuse, `3` malformed input, `4` not applicable.
[Spec 1 §6](2026-08-07-agents-repo-context-design.md#6-binary-surface) defines it;
spec 2 §10 adopts it verbatim, and `bootstrap.d/main.go` names the same five
constants. One vocabulary across both binaries in this repository.

This is not an implementation detail once CI keys off it. Spec 2 already fixed a
defect of exactly that shape: the shim exited `1` on a Go compile error — "advisory",
and the most likely real failure — so **every** shim failure now exits `2`
deliberately, "which is how a CI job keying off codes would read a hard stop"
(`bootstrap`, header comment; spec 2, shim defect 3). A second instance was found in
the same file, where `${HOME:?}` exited `1` where a block was meant.

So CI must treat these codes as a contract it consumes: decide which codes fail a
job, which annotate, and which are skips — and any later change to the table becomes
a change to a CI interface, not a local refactor.

### The checkout is not necessarily `~/dotfiles` — **resolved 2026-08-11**

**Fixed before this spec was started, in the plan
[checkout path and field defects](../../plans/2026-08-11-checkout-path-and-field-defects.md).**
The root now comes from a build-time stamp with an explicit fallback chain —
`-X main.dotfilesRoot` → `AGENTS_DOTFILES_ROOT` → `$HOME/dotfiles` — and both
builders pass it. Inferring the root from `core.hooksPath` was rejected
deliberately: `git-hooks:global` *compares* those two, so deriving one from the
other would make the check pass by construction.

The description below is kept because it names what this spec still has to
decide. **What remains open for CI is narrower than it looks:** a released
binary is not built from anyone's checkout, so it has no root to stamp, and the
fallback it lands on is `$HOME/dotfiles` — exactly the assumption that was just
removed. Deciding what a *distributed* binary stamps, or whether it should
instead resolve at runtime, is this spec's problem and is not solved by the fix
above.

One live hazard it introduced, documented in the Makefile and the README:
`make agents` stamps `$(CURDIR)` and writes the single global `~/bin/agents`, so
running it from a linked worktree publishes a binary stamped to a path that will
not survive. A doctor check on whether the resolved root still exists is the
obvious guard and belongs here or in a spec 1 amendment.

For the record, the failure this produced — `agents doctor` on a correctly
provisioned machine whose checkout was elsewhere:

| Check | Result | Because |
|---|---|---|
| `git-hooks:global` | fail | global `core.hooksPath` holds the real checkout's `git/hooks.d`; doctor compares it against `~/dotfiles/git/hooks.d` |
| `git-hooks:links` | fail | it looks for the four hook links in a directory that does not exist |
| `git-attributes` | fail | `core.attributesFile`'s origin is the real `gitconfig.shared`, not `~/dotfiles/git/gitconfig.shared` |
| `git-hooks:effective` | warn | same mismatch, reported as a shadowed hook directory |

`agents/main.go:41` carries a second instance —
`ExtrasDir: filepath.Join(os.Getenv("HOME"), "dotfiles", "git", "hooks")` — whose
failure is worse because it is silent: `githook.go:127` treats a missing extras
directory as "no personal hooks", so a relocated checkout runs none and says nothing.

This is recorded here rather than in spec 2 because spec 2 is what makes "the
checkout can live anywhere" a **promise**: the shim resolves its root from `$0` and
refuses a root that is not the tree it lives in, the build cache is keyed on that
root so a worktree and a clone cannot collide, and the fish stub names the clone
location exactly once so everything downstream is relative. Doctor is the last place
that still assumes the old answer. Resolving it belongs to this spec or to a spec 1
amendment — spec 1 owns doctor and the hook-installation contract — but it must not
be left unowned, and it is a prerequisite for any CI job that runs `agents doctor` in
a checkout the runner placed.

## Scope

### Pull-request and branch verification

- Define GitHub Actions triggers and path filters without allowing a docs-only
  change to masquerade as source verification.
- Pin the Go and gitleaks toolchains deliberately.
- Run the complete required gate: normal tests, the ambient-Go-cache-cleared test
  arm, race tests, vet, formatting, diff checks, and secret scanning.
- Run it over **both** modules, `agents/` and `bootstrap.d/`, independently and
  without introducing a `go.work`.
- Defeat test-result caching in the required gate. Several tests assert against
  tracked non-Go files, whose edits do not invalidate a cached pass.
- Decide whether Linux verification lives in the same required gate or a separate
  job, and at which profile. This is the only path to spec 2's untested Linux
  support.
- Keep all tests isolated from global Git, hook, trust, and machine-registry state.

### Release construction

- Decide supported macOS and Linux architecture combinations from the actual fleet
  and cross-build constraints; Windows remains out of scope unless chosen
  deliberately.
- Decide whether `bootstrap` is published alongside `agents`, or only `agents`. Only
  `bootstrap` is on the stage-zero path that spec 2 §2.2's seam wants to shorten.
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
- Preserve the shim's attribution property if a published binary is fetched rather
  than built: it must never `exec` a binary it cannot attribute to the current
  checkout. Spec 2 §2.2.

### Version and drift diagnostics

- Add a content-safe version surface for users and support reports.
- Decide what `agents doctor` should compare: installed release, repository source,
  hook-link target, or some explicit combination.
- Decide what a **released** binary stamps as its checkout root. The
  `~/dotfiles` assumption was removed on 2026-08-11 by stamping the root at
  build time, but a binary built by CI belongs to no checkout, so it falls back
  to `$HOME/dotfiles` — the very assumption that was removed. Either the
  installer supplies the root, or a distributed binary resolves it at runtime,
  or `AGENTS_DOTFILES_ROOT` becomes part of the documented install.
- Decide whether doctor should verify the stamped root still exists. Nothing
  does today, and a binary stamped to a deleted worktree runs no personal git
  hooks at exit 0.
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
- ~~Is `bootstrap` published at all?~~ **Answered 2026-08-11: both binaries are
  published, on the same terms, and so is any later one.** What remains open is
  whether spec 2's build-from-checkout path stays as a documented fallback once a
  release exists, or becomes development-only.
- Which Linux distributions does a CI runner actually verify? Spec 2 targets
  Debian/Ubuntu and Arch/Manjaro on paper and leaves the question open; a runner
  image answers it in practice, which makes this spec's choice the de facto one.
- ~~Does the `~/dotfiles` assumption in `agents` belong to this spec or to a spec 1
  amendment?~~ **Answered 2026-08-11: neither — it was a defect and was fixed
  outright** by stamping the root at build time. What this spec inherits is the
  narrower question above: what a binary that CI built, belonging to no checkout,
  should stamp instead.
