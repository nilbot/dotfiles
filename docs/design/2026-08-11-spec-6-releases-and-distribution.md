# Spec 6 — releases and binary distribution

**Date:** 2026-08-10 (as part of spec 5's scope note) / 2026-08-11 (split out) / 2026-08-28 (designed & implemented)  
**Status:** Designed & Implemented (2026-08-28)  
**Depends on:** [Spec 1](2026-08-07-agents-repo-context-design.md) (Go module, security boundaries, harness wiring), [Spec 5](2026-08-11-spec-5-verification-gate.md) (automated verification gate), [Knowledge is Documentation](2026-08-19-knowledge-is-documentation.md) (2026-08-19)  
**Carries obligations from:** [Spec 2](2026-08-07-spec-2-dotfiles-hygiene.md)  

---

## Amendment 1 — 2026-09-20

**§5.1 changed: the release workflow no longer re-runs the verification matrix.**
It requires, through the API, that the tagged commit already has a successful
`verify` run on `master`, and then builds that commit.

`needs: verify` re-ran all 11 jobs of `verify.yml` and then rebuilt the same tree.
Measured on the v0.5.0 and v0.5.1 release runs (`33498693632`, `33499733625`): 12
jobs, 13.8 to 14.1 job-minutes, 3.3 to 3.6 minutes wall clock, of which the 11
re-verified jobs are ~13.2 job-minutes (**94%**) and the job that actually
packages and publishes is 0.8. The re-run proved nothing the `master` push run
had not already proved about the same tree, and it made the release depend on the
flakiest legs of the matrix — two macOS runners and two container jobs — at the
one moment a human is driving the gate.

The tagged commit is the evidence. Every release tag points at a commit that was
pushed to `master`; that push ran `verify.yml`, and its `gate` job is the check a
pull request is gated on. All seven existing tags already satisfy this: each
resolves to a commit with exactly one successful `master` push run. So a green
run for that commit *is* the "green CI build for the tagging SHA" a release
needs, and reading it costs one API call instead of eleven jobs. The release
still builds from the tagged tree, because the version is not knowable earlier:
`-X main.version` and the archive names (`agents_<version>_<os>_<arch>.tar.gz`)
both come from the tag, so an archive built by the `master` push run could not
carry the version it would be released under without guessing it or changing how
the binary is stamped. The build is also the small term (0.8 of 14.0
job-minutes), so the release builds and then asserts what it built, rather than
transporting bytes between runs.

### Implementation constraints, each of which silently breaks the guard

- `head_sha` must be the **full 40-character** commit SHA. An abbreviated SHA
  returns HTTP 200 with `total_count: 0`, which reads as "not verified" for a
  commit that is. Verified against this repository: `a82681b` returns 0, and
  `a82681bd4975f48a813fab75ef97ec870e17e59b` returns 1.
- The workflow declares `permissions:`, and an unlisted scope is `none`, so the
  guard needs `actions: read` beside `contents: write`.
- The tag is peeled through `GET /repos/{owner}/{repo}/commits/{tag}`;
  `/git/ref/tags/{tag}` returns the tag object for the annotated tag that §5.1's
  `git tag -a` produces.
- The query also requires `event=push` and `branch=master`, so a green
  pull-request run for the same commit does not satisfy it: a release comes from
  the mainline, not from a branch that merely has green CI.
- No green `master` run means no build and no release. The
  failure is fail-closed and names the commit and the condition.

### `workflow_dispatch` resolves and checks out the tag

The version came from the `tag` input while `actions/checkout` defaulted to the
dispatched ref, so a manual run packaged the default branch's tree under the
requested version number. The amendment checks out the tag the run resolved. The
dispatch form also gains a `dry_run` input that stops before the release is
created, so the whole path can be rehearsed
against an already-released tag.

### §4 and §4.2 corrected

§4 said "three tier-1 cross-compilation targets … (Apple Silicon only)" and §4.2
said the archives contain `LICENSE`. `script/package-release.sh` has built four
targets since the pipeline landed — `darwin/arm64`, `darwin/amd64`,
`linux/arm64`, `linux/amd64` — and Intel macOS is not optional: the tap's syntax
job runs `brew readall --os=all --arch=all`, which evaluates every
macOS/architecture pair, and a formula with no `darwin/amd64` URL fails it
([qna](../qna/why-does-brew-readall-fail-when-macos-intel-is-omitted.md)). The
repository has no `LICENSE` file, and the archives contain `agents` and
`README.md`.

---

## 1. Executive Summary

This specification defines the build, packaging, release, and distribution pipeline for the `agents` binary, enabling external collaborators and automated environments to install and execute `agents` as a standalone tool without requiring a local clone of the operator's `dotfiles` repository.

---

## 2. The Hardest Open Problem — Resolved

**Historical Question (2026-08-11):** *"A binary built by CI belongs to no checkout, so it has no root to stamp."*

**Resolution (2026-08-28):**
Released binaries are built without a stamped checkout root (`main.dotfilesRoot == ""`).

`DotfilesRoot()` in `agents/root.go` resolves via an explicit 2-tier contract without `$HOME/dotfiles` existence heuristics:
1. **Link-Time Stamp (`main.dotfilesRoot`)**: Set by `make agents` and `./bootstrap apply workstation` to bind the binary to a specific dotfiles checkout root (activating Dotfiles Operator Mode).
2. **Environment Variable (`AGENTS_DOTFILES_ROOT`)**: Explicit runtime override for operators running unstamped or Homebrew-installed binaries who wish to bind them to their personal dotfiles.
3. **Standalone Fallback (`""`)**: An unstamped binary with no environment variable returns `""` and operates in **Standalone Mode** regardless of what exists in the user's home directory.

When `DotfilesRoot() == ""` (`deps.Root == ""`):
- `agents doctor` operates in **Standalone Repository Mode**:
  - Skips dotfiles-specific checks (`root:exists`, `git-hooks:global`, `git-hooks:links`).
  - Validates repository-level `.gitattributes` directly for `.agents/** linguist-generated=true`.
  - Validates repository-local git hooks (`git-hooks:local`).
  - Runs full harness wiring, trust, gitleaks, instruction, and documentation checks.
- Git hook multi-call dispatcher executes repository hooks and built-in stages, cleanly skipping personal hook chains (`<root>/git/hooks/*`).

---

## 3. Version Surface

### 3.1 Link-Time Variables

Build metadata is stamped at compile time via `-ldflags`:
- `main.version`: Semantic version tag (e.g. `v0.2.0` or `dev`).
- `main.commit`: Git commit SHA.
- `main.date`: UTC build timestamp (ISO 8601).

### 3.2 CLI Command & Flags

`agents version` outputs human-readable version provenance:
```text
agents v0.2.0 (commit: abc1234, built: 2026-08-28T17:00:00Z)
```
Top-level flags `--version` and `-v` are intercepted in `agents/main.go` and invoke `runVersion`.

---

## 4. Release Construction & Platforms

### 4.1 Cross-compilation targets

Four cross-compilation targets covering the macOS and Linux fleets (Amendment 1):
1. `darwin/arm64` (Apple Silicon macOS)
2. `darwin/amd64` (Intel macOS; the tap's `brew readall` requires it)
3. `linux/arm64` (ARM64 Linux)
4. `linux/amd64` (x86_64 Linux)

### 4.2 Packaging Format

Release archives are generated per platform:
- Archive name: `agents_<version>_<os>_<arch>.tar.gz`
- Contents: `agents` binary, `README.md`
- Manifest: `checksums.txt` containing SHA-256 hashes of all archives.

---

## 5. Publishing & Distribution

### 5.1 GitHub Actions Workflow (`.github/workflows/release.yml`)

- Triggered on tag push matching `v*` (e.g.
  `git tag -a v0.6.0 -m "…" && git push origin v0.6.0`), and by
  `workflow_dispatch` with a `tag` input for recovery and rehearsal.
- Requires a successful `verify` run on `master` for the tagged commit, read
  through the API rather than re-run (Amendment 1), then checks out that tag,
  builds binaries across all 4 targets, generates `checksums.txt`, and publishes
  a GitHub Release with assets attached.
- Requires `.github/release-notes/<tag>.md` to exist and be non-empty, and passes
  it as the release body, so a release cannot ship an empty body.
- Asserts before publishing that the built binary reports the tag version and the
  guarded commit, so the uploaded archives are provably built from the commit
  whose green run was read.

### 5.2 Homebrew Formula

**Amended 2026-09-21.** The formula lives in `nilbot/homebrew-tap` and is
maintained there. This repository used to carry a second copy of it; that copy
is deleted. What replaces it is not a copy but an editor.

The copy was seven releases stale (`v0.1.0` against the tap's `v0.6.0` — last
written by `3c6f6cb` on 2026-08-28 and never again) and a reader here could not
tell that the file they were looking at was not the one `brew` reads. A second
copy of a published artifact that no consumer reads is a trap, not a
convenience.

`script/sync-homebrew-formula.sh` reads the formula the tap already has through
the Contents API and rewrites only its four `url` and four `sha256` lines, so the
tap remains the single source of truth. `release.yml` runs it after publishing
the release, then runs it again with `--check` to assert the result.

**It matches digests by filename, never by position, and that is load-bearing.**
`checksums.txt` comes from `sha256sum agents_v<X.Y.Z>_*.tar.gz`, whose glob sorts
by collation: `darwin_amd64` is listed **before** `darwin_arm64`. The formula's
four slots read `darwin_arm64`, `darwin_amd64`, `linux_arm64`, `linux_amd64`. A
paste in line order therefore swaps every Intel digest with its ARM sibling, and
nothing catches it — the formula stays valid Ruby, and the tap's CI runs
`brew test-bot --only-tap-syntax`, which checks syntax rather than whether a
digest belongs to the URL above it. The mismatch would surface only in a user's
`brew install`. Keying each digest on the archive filename in the URL removes the
ordering question by construction.

The regression this guards against is measured, not hypothetical: the sync step
ran on every release from v0.2.1 to v0.6.0, each tap commit landing 1-35 seconds
after the release was published. Any version of this that leaves the tap behind
silently is a regression against that record.

Installation is unchanged for the user:
```bash
brew install nilbot/tap/agents
```

---

## 6. Verification & Safety Constraints

- **Non-destructive upgrades**: Updating `agents` does not overwrite or mutate existing `AGENTS.md` or repository files.
- **Redaction purity**: Release builds retain the structural redaction guarantee (unknown hook payload fields discarded before serialization).
- **Zero personal leaks**: Released binaries contain no hardcoded personal `$HOME` paths or machine identifiers.
