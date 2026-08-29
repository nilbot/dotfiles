# Debian Container Package Cache Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure Debian `.deb` packages are retained in `/var/cache/apt/archives` during `linux-stage-zero` container runs so that `actions/cache` can save and restore them across workflow runs.

**Architecture:** In `.github/workflows/verify.yml`, remove `/etc/apt/apt.conf.d/docker-clean` and set `Binary::apt::APT::Keep-Downloaded-Packages "true"` in Debian's `prereqs` matrix string before executing `apt-get`.

**Tech Stack:** GitHub Actions YAML, Debian APT, `actions/cache@v6.1.0`

**Spec:** [docs/design/2026-08-29-ci-debian-package-cache-fix.md](file:///Users/nilbot/dotfiles/docs/design/2026-08-29-ci-debian-package-cache-fix.md)

---

### Task 1: Update Debian Prerequisites in `.github/workflows/verify.yml`

**Files:**
- Modify: `.github/workflows/verify.yml:295-310`

- [ ] **Step 1: Update Debian `prereqs` in `linux-stage-zero` matrix**

In `.github/workflows/verify.yml`:
```yaml
          - image: debian:stable-slim
            pkg_cache: /var/cache/apt/archives
            prereqs: rm -f /etc/apt/apt.conf.d/docker-clean && echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache && apt-get update -qq && apt-get install -y -qq sudo golang-go git ca-certificates curl
```

- [ ] **Step 2: Commit workflow changes**

```bash
git add .github/workflows/verify.yml
git commit -m "ci(verify): preserve Debian apt package archives in linux-stage-zero"
```

---

### Task 2: Verification

**Files:**
- Verification only

- [ ] **Step 1: Validate YAML and run local tests**
Run: `(cd agents && go test -count=1 ./...) && (cd bootstrap.d && go test -count=1 ./...)`
Expected: PASS
