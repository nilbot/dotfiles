# Spec: CI Latency and Test Harness Optimization

**Date:** 2026-08-29  
**Status:** Approved design, entering implementation  
**Author:** Pair Programming Session (Superpowers)  
**Related Design Documents:**
- [Spec 2 — dotfiles hygiene](2026-08-07-spec-2-dotfiles-hygiene.md)
- [Spec 5 — verification gate](2026-08-11-spec-5-verification-gate.md)

---

## 1. Context & Problem Statement

Historical analysis across 29 successful GitHub Actions CI/CD runs (`verify.yml`) showed an average suite latency of **6.20 minutes (372s)**.

The critical path was dominated by three concurrent jobs taking ~5.5 to 5.8 minutes:
1. `hygiene` (avg 347.3s / 5.79m)
2. `test (ubuntu-latest, bootstrap.d)` (avg 338.0s / 5.63m)
3. `test (macos-latest, bootstrap.d)` (avg 328.1s / 5.47m)

### Root Cause
In `bootstrap.d/main_test.go`, integration tests invoke `./bootstrap` via `runShim` inside isolated `tempHome(t)` environments (`XDG_CACHE_HOME=tempHome/cache`). Because each test creates a fresh temporary directory, `./bootstrap` finds no pre-existing binary in `$XDG_CACHE_HOME/dotfiles-bootstrap/$key/bootstrap` and invokes `go build -trimpath -o "$binary" .`.

With ~45 test cases executing `runShim`, each `go build` takes ~3.5 seconds, accumulating ~150s per `go test` pass and ~160s per `go test -race` pass. In `hygiene`, the suite runs twice (once for `$HOME` containment check, once under restrictive `umask`), taking 347s.

Additionally, every matrix leg in `test` installed `gitleaks` via network download and checksum verification, despite `bootstrap.d` having no dependency on `gitleaks`.

---

## 2. Architecture & Design

### 2.1 Test Harness Cache Seeding (`bootstrap.d/main_test.go`)

To eliminate repeated `go build` invocations while maintaining hermetic test isolation:

1. **`TestMain` Pre-Compilation**:
   - `bootstrap.d/main_test.go` defines `TestMain(m *testing.M)`.
   - `TestMain` compiles the primary checkout's `bootstrap` binary once into an ephemeral shared temporary test cache (`sharedTestCacheDir`).
   - The compiled binary path and cache key are saved in package variables.
   - On completion, `TestMain` removes the temporary directory and exits with `m.Run()` exit code.

2. **Per-Test Cache Seeding in `runShimIn`**:
   - When `runShim` or `runShimEnv` executes against the main repository checkout, `runShimIn` checks if `$home/cache/dotfiles-bootstrap/$key/bootstrap` already exists.
   - If not present (and the test is not explicitly marked as cold), `runShimIn` seeds the binary from `sharedTestCacheDir` to the test's `$home/cache` with fresh file modification times.
   - `./bootstrap` detects the pre-existing, up-to-date binary and skips compilation, running `exec "$binary" "$@"` immediately.

3. **Explicit Cold-Build Separation**:
   - Dedicated tests that verify `./bootstrap` cold compilation, cache misses, or build failure handling (`TestShimCachesTheBuild`, `TestShimBuildFailureIsBlock`) use `runShimCold` / `runShimColdEnv`.
   - `runShimCold` bypasses cache seeding, ensuring `./bootstrap` executes its true compilation path and emits `bootstrap: building` to `stderr`.

4. **Multi-Checkout & Keying Invariants**:
   - Tests using `altCheckout(t)` (e.g. `TestCacheIsKeyedOnTheCheckout`, `TestMalformedManifestIsMalformedInput`) maintain distinct cache keys derived from their own checkout path (`BOOTSTRAP_ROOT`), ensuring no cross-contamination.

---

## 2.2 GitHub Actions Workflow Optimization (`.github/workflows/verify.yml`)

1. **Matrix Gitleaks Scoping**:
   - Add `if: matrix.module == 'agents'` to the `install gitleaks, checksum-verified` step in the `test` matrix.
   - Eliminates redundant tool downloads and sudo installations on both macOS and Ubuntu runners for `bootstrap.d`.

2. **Hygiene Job Latency Realization**:
   - With test cache seeding in place, the `hygiene` job steps (`suites must not write to $HOME` and `bootstrap.d under a restrictive umask`) will drop from ~345s to ~30-35s without weakening containment or umask assertions.

---

## 3. Performance Targets & Quality Invariants

| Metric | Baseline | Target |
| :--- | :--- | :--- |
| `bootstrap.d` `go test` duration | ~150s | < 15s |
| `bootstrap.d` `go test -race` duration | ~160s | < 15s |
| `test (ubuntu-latest, bootstrap.d)` CI job | ~338s (5.6m) | < 35s |
| `test (macos-latest, bootstrap.d)` CI job | ~328s (5.5m) | < 45s |
| `hygiene` CI job | ~347s (5.8m) | < 45s |
| Overall CI Critical Path Wall-Clock | ~372s (6.2m) | < 150s (2.5m) |

### Invariants Maintained:
- **Zero test coverage loss**: All existing test assertions remain intact.
- **Production shim script untouched**: `./bootstrap` logic remains standard without test-only backdoor branches.
- **Hermeticity**: Each test still operates within its own `tempHome(t)` with isolated `HOME` and `XDG_CACHE_HOME`.
