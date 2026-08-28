# Spec 6 — releases and binary distribution

**Date:** 2026-08-10 (as part of spec 5's scope note) / 2026-08-11 (split out) / 2026-08-28 (designed & implemented)  
**Status:** Designed & Implemented (2026-08-28)  
**Depends on:** [Spec 1](2026-08-07-agents-repo-context-design.md) (Go module, security boundaries, harness wiring), [Spec 5](2026-08-11-spec-5-verification-gate.md) (automated verification gate), [Knowledge is Documentation](2026-08-19-knowledge-is-documentation.md) (2026-08-19)  
**Carries obligations from:** [Spec 2](2026-08-07-spec-2-dotfiles-hygiene.md)  

---

## 1. Executive Summary

This specification defines the build, packaging, release, and distribution pipeline for the `agents` binary, enabling external collaborators and automated environments to install and execute `agents` as a standalone tool without requiring a local clone of the operator's `dotfiles` repository.

---

## 2. The Hardest Open Problem — Resolved

**Historical Question (2026-08-11):** *"A binary built by CI belongs to no checkout, so it has no root to stamp."*

**Resolution (2026-08-28):**
Released binaries are built without a stamped checkout root (`main.dotfilesRoot == ""`).
1. `DotfilesRoot()` in `agents/root.go` verifies `$HOME/dotfiles` existence before falling back to it. On a machine without `~/dotfiles`, it returns `""` (empty string).
2. When `deps.Root == ""`, `agents doctor` operates in **Standalone Repository Mode**:
   - Skips dotfiles-specific checks (`root:exists`, `git-hooks:global`, `git-hooks:links`).
   - Validates repository-level `.gitattributes` directly for `.agents/** linguist-generated=true`.
   - Validates repository-local git hooks (`git-hooks:local`).
   - Runs full harness wiring, trust, gitleaks, instruction, and documentation checks.

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

Three tier-1 cross-compilation targets covering macOS (Apple Silicon only) and Linux fleets:
1. `darwin/arm64` (Apple Silicon macOS)
2. `linux/arm64` (ARM64 Linux)
3. `linux/amd64` (x86_64 Linux)

### 4.2 Packaging Format

Release archives are generated per platform:
- Archive name: `agents_<version>_<os>_<arch>.tar.gz`
- Contents: `agents` binary, `README.md`, `LICENSE`
- Manifest: `checksums.txt` containing SHA-256 hashes of all archives.

---

## 5. Publishing & Distribution

### 5.1 GitHub Actions Workflow (`.github/workflows/release.yml`)

- Triggered on tag push matching `v*` (e.g. `git tag v0.2.0 && git push origin v0.2.0`).
- Runs verification tests, builds binaries across all 4 targets, generates `checksums.txt`, and publishes a GitHub Release with assets attached.

### 5.2 Homebrew Formula (`Formula/agents.rb`)

A Homebrew formula template enables installation via Homebrew:
```bash
brew install nilbot/tap/agents
```
The formula downloads the platform-specific release tarball from GitHub Releases, verifies its SHA-256 checksum, and links `bin/agents`.

---

## 6. Verification & Safety Constraints

- **Non-destructive upgrades**: Updating `agents` does not overwrite or mutate existing `AGENTS.md` or repository files.
- **Redaction purity**: Release builds retain the structural redaction guarantee (unknown hook payload fields discarded before serialization).
- **Zero personal leaks**: Released binaries contain no hardcoded personal `$HOME` paths or machine identifiers.
