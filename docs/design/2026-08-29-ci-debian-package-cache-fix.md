# Spec: Debian Container Package Cache Fix

**Date:** 2026-08-29  
**Status:** Approved design, entering implementation  
**Author:** Pair Programming Session (Superpowers)  
**Related Design Documents:**
- [CI Latency and Test Harness Optimization (Phase 2)](2026-08-29-ci-latency-and-test-harness-optimization-phase-2.md)
- [Spec 5 — verification gate](2026-08-11-spec-5-verification-gate.md)

---

## 1. Problem Statement

In `linux-stage-zero` container jobs in `.github/workflows/verify.yml`, `actions/cache` was configured to cache `/var/cache/apt/archives` on Debian and `/var/cache/pacman/pkg` on Arch Linux.

While Arch Linux successfully saved and restored ~195 MB of package archives, Debian saved only 394 bytes (an empty directory metadata tarball).

**Root Cause:**
Official Docker Debian images (`debian:stable-slim`) ship with a default `/etc/apt/apt.conf.d/docker-clean` configuration that hooks `DPkg::Post-Invoke` and `APT::Update::Post-Invoke` to automatically delete all `.deb` archives in `/var/cache/apt/archives/` after every `apt-get` execution.

---

## 2. Solution

In `.github/workflows/verify.yml`, update the Debian matrix entry under `linux-stage-zero` to remove `/etc/apt/apt.conf.d/docker-clean` and configure APT to keep downloaded archives:

```yaml
          - image: debian:stable-slim
            pkg_cache: /var/cache/apt/archives
            prereqs: rm -f /etc/apt/apt.conf.d/docker-clean && echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache && apt-get update -qq && apt-get install -y -qq sudo golang-go git ca-certificates curl
```

---

## 3. Invariants & Verification

1. **Clean Container Fidelity:** `stageZero` in `packages.go` continues to run real package installations and verify required tool binaries (`gcc`, `file`, etc.).
2. **Security & Pinning:** `actions/cache` remains pinned to `55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0`.
3. **Workflow Syntax:** Workflow YAML syntax validated.
