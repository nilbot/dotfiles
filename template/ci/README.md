# Authoritative CI Verification Gate Template

This directory contains the canonical, hardened GitHub Actions verification workflow template (`verify.yml`) for projects and collaborator repositories.

## Overview

The Authoritative CI Verification Gate serves as the non-bypassable server-side enforcement layer on GitHub Actions for pull requests and branch pushes, ensuring that:

1. **Zero Secrets Leakage (`secrets` job)**: Scans the full commit history using checksum-verified Gitleaks (`8.30.1`) to prevent credential leakage.
2. **Context & Guardrail Integrity (`context` job)**: Verifies the existence of non-empty `AGENTS.md` (or `CLAUDE.md`) instructions and validates `.gitattributes` configuration (`.agents/** linguist-generated=true`) when an `.agents/` directory is present.
3. **Code Quality & Testing (`quality` job)**: Runs project linting, type checks, and automated test suites.
4. **Unified Gate Status (`gate` job)**: Aggregates all parallel checks into a single required status check (`gate`) for GitHub Branch Protection.

---

## Adopting the Template

### 1. Copy Workflow File

Copy `verify.yml` into your repository's `.github/workflows/` directory:

```bash
mkdir -p .github/workflows
cp template/ci/verify.yml .github/workflows/verify.yml
```

### 2. Verify Repository Context

Ensure your repository meets the context integrity checks:
- Maintain a non-empty `AGENTS.md` or `CLAUDE.md` in the repository root.
- If an `.agents/` directory exists, ensure `.gitattributes` contains:
  ```gitattributes
  .agents/** linguist-generated=true
  ```

### 3. Configure the `quality` Job

Update the `quality` job in `.github/workflows/verify.yml` for your project's technology stack (see examples below).

### 4. Configure GitHub Branch Protection

1. In GitHub, navigate to **Settings** > **Branches** > **Branch protection rules**.
2. Add or edit a rule targeting your default branch (`master` or `main`).
3. Enable **Require status checks to pass before merging**.
4. Enable **Require branches to be up to date before merging**.
5. In the search box, search for and select **`Verification Gate`** (or `gate`).
6. Save the branch protection rule.

> **Why gate?** By requiring only the aggregate `gate` check, you can add, split, or rename individual verification jobs in `verify.yml` without modifying repository settings or breaking pull request gating.

---

## Quality Job Configuration Examples

### Go Project

For Go applications and modules:

```yaml
  quality:
    name: Code Quality & Tests (Go)
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: '1.26.6' # Pin exact patch version
          cache: true
      - name: build
        run: go build ./...
      - name: gofmt
        run: test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }
      - name: vet
        run: go vet ./...
      - name: test
        run: go test -count=1 ./...
      - name: test -race
        run: go test -count=1 -race ./...
```

### Python Project

For Python applications with Ruff and Pytest:

```yaml
  quality:
    name: Code Quality & Tests (Python)
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: actions/setup-python@42375524e23c412d93fb67b49958b491fce71c38 # v5.4.0
        with:
          python-version: '3.12'
          cache: 'pip'
      - name: install dependencies
        run: |
          python -m pip install --upgrade pip
          pip install ruff pytest
          if [ -f requirements.txt ]; then pip install -r requirements.txt; fi
      - name: ruff check (linter)
        run: ruff check .
      - name: ruff format (formatter)
        run: ruff format --check .
      - name: pytest
        run: pytest
```

### TypeScript / JavaScript / Web Project

For Node.js / TypeScript frontend and web applications:

```yaml
  quality:
    name: Code Quality & Tests (TypeScript/Node)
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: actions/setup-node@1e60f620b9541d16bece96c5465dc8ee9832be0b # v4.0.3
        with:
          node-version: '20'
          cache: 'npm'
      - name: install dependencies
        run: npm ci
      - name: typecheck
        run: npx tsc --noEmit
      - name: lint
        run: npm run lint
      - name: test
        run: npm test
```

For static web applications without build steps (e.g. static HTML/CSS/JS):

```yaml
  quality:
    name: Code Quality & Tests (Web Static)
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - name: validate static structure
        run: |
          set -euo pipefail
          test -f index.html || { echo "index.html is missing" >&2; exit 1; }
```

---

## Security & Hardening Standards

1. **Commit-Pinned Actions**: Every `uses:` step is pinned to an exact, immutable commit SHA rather than a mutable tag. This prevents upstream action compromises from silently affecting CI runs.
2. **Checksum Verification**: Binaries downloaded outside official package managers (such as Gitleaks) are verified against hardcoded SHA256 checksums before installation and execution.
3. **Least Privilege**: Workflows declare `permissions: contents: read` globally.
4. **Full History Scanning**: Secret scanning checks out with `fetch-depth: 0` so all branch commits in a pull request are inspected.
