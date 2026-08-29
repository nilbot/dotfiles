# Spec: CI Latency and Test Harness Optimization (Phase 2)

**Date:** 2026-08-29  
**Status:** Approved design, entering implementation  
**Author:** Pair Programming Session (Superpowers)  
**Related Design Documents:**
- [CI Latency and Test Harness Optimization (Phase 1)](2026-08-29-ci-latency-and-test-harness-optimization.md)
- [Spec 5 — verification gate](2026-08-11-spec-5-verification-gate.md)

---

## 1. Context & Problem Statement

Phase 1 reduced CI suite wall-clock latency from **6m 12s down to 3m 23s** (~45% reduction).

Profiling of the post-Phase 1 GitHub Actions run (`33264574909`) revealed the next two latency bottlenecks:
1. **Alt-Checkout Compilation Churn in `bootstrap.d` Tests:**
   Integration tests using `altCheckout(t)` to test manifests (`links.manifest`, `Brewfile`) or directory configurations (`TestCheckOnAMalformedManifestIsMalformedInput`, `TestCheckOnAnUnreadableManifestBlocks`, `TestPlanAndCheckAgreeOnThePackagesVerdict`, `TestMalformedManifestIsMalformedInput`, `TestRefusalFromAPhaseBlocks`) still incurred 7–8 separate full Go compilations (~5s each in CI), keeping `bootstrap.d` test steps at ~72s–82s in CI.
2. **`linux-stage-zero` Container Package Downloads:**
   `linux-stage-zero` spends ~110s–118s in Debian and Arch container environments downloading `build-essential` and `base-devel` packages over the network.

---

## 2. Architecture & Design

### 2.1 Universal Test Harness Cache Seeding (`bootstrap.d/main_test.go`)

To eliminate the remaining `go build` churn across all integration tests while preserving strict hermeticity and cold compilation coverage:

1. **`bootstrapCacheKey` Helper:**
   - In `bootstrap.d/main_test.go`, implement a helper function `bootstrapCacheKey(root string) string` that derives the POSIX `cksum` key expected by `./bootstrap` for any checkout path.

2. **Universal Seeding in `runShimIn`:**
   - In `runShimIn`, when `cold == false` and `sharedTestBinary != ""`:
     - Derive `key := bootstrapCacheKey(checkoutDir)`.
     - Seed the precompiled binary from `sharedTestBinary` into `$home/cache/dotfiles-bootstrap/$key/bootstrap`.
     - Touch file modification timestamps so `./bootstrap`'s staleness scan detects an up-to-date binary.

3. **Cold Test Preservation:**
   - Tests that modify Go code or explicitly test cold builds (`TestCacheIsKeyedOnTheCheckout`, `TestShimBuildFailureIsBlock`, `TestShimCachesTheBuild`) use `runShimCold` / `runShimColdEnv` to ensure the compilation pipeline and cache transitions remain fully tested.

---

### 2.2 Package Cache in `linux-stage-zero` (`.github/workflows/verify.yml`)

1. **GitHub Actions Cache Action Pinning:**
   - Use pinned `actions/cache@55cc8345863c7cc4c66a329aec7e433d2d1c52a9` (v6.1.0) with exact commit SHA and trailing comment.
   - Cache package archive paths:
     - Debian: `/var/cache/apt/archives`
     - Arch: `/var/cache/pacman/pkg`
2. **Fidelity Invariants:**
   - `stageZero` in `packages.go` continues to run real package installations and assertions (`command -v gcc`, `command -v file`, etc.), with package downloads served from runner cache.

---

## 3. Performance Targets

| Metric | Phase 1 Baseline | Target (Phase 2) |
| :--- | :---: | :---: |
| `bootstrap.d` `test` step (Ubuntu/macOS) | ~63s–72s | < 15s |
| `bootstrap.d` `test -race` step (Ubuntu/macOS) | ~64s–82s | < 20s |
| `hygiene` CI job | 194s (3.2m) | < 45s |
| `linux-stage-zero` CI job | 146s (2.4m) | < 60s |
| Overall CI Critical Path Wall-Clock | 203s (3.4m) | < 80s (1.3m) |
