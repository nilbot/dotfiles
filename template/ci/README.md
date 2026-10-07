# Authoritative CI Verification Gate Template

`verify.yml` in this directory is a reusable GitHub Actions workflow that puts a
server-side verification gate on every pull request. A local pre-commit hook can
be bypassed (`git commit --no-verify`) or never installed at all, so the gate is
the only check a contributor cannot skip.

This directory is a **generic starter for other repositories**. It is not a copy
of this repository's own gate: the reference implementation is
[`.github/workflows/verify.yml`](../../.github/workflows/verify.yml), which runs
eight job definitions against this checkout, including jobs that provision
Linux and macOS runners and cannot apply to another project. The template keeps
the parts that apply anywhere; the hardening rules in the last section are
distilled from that implementation.

## What the workflow does

Four jobs run in parallel; one aggregate check decides the merge.

| Job | Check context | What it proves |
|---|---|---|
| `secrets` | `secrets` | No credential anywhere in the commit history (full-history checkout with `fetch-depth: 0`), scanned by a checksum-verified Gitleaks binary. |
| `context` | `context` | A non-empty `AGENTS.md` or `CLAUDE.md` exists; if `.agents/` is present, the root `.gitattributes` carries the exact line `.agents/** linguist-generated=true`. |
| `quality` | `quality` | The project's lint, format and test commands. **Ships as a placeholder that fails closed** — see step 3 below. |
| `gate` | `gate` | Every job above reported `success`. |

`gate` runs with `if: always()` and asserts each dependency's result explicitly,
so a dependency that failed, was cancelled or was skipped cannot leave the gate
green.

The job **id** is the check context, so the `gate` job deliberately has no
`name:`. See step 4.

## Adopting the template

### 1. Copy the workflow

```bash
mkdir -p .github/workflows
cp template/ci/verify.yml .github/workflows/verify.yml
```

### 2. Satisfy the context checks

- Keep a non-empty `AGENTS.md` (or `CLAUDE.md`) in the repository root.
- If `.agents/` exists, add this exact line to the root `.gitattributes`:

```gitattributes
.agents/** linguist-generated=true
```

The line must match exactly. `agents doctor` and the `context` job both reject
near misses such as `.agents/** linguist-generated` or a trailing suffix.

If the repository's secret scan needs an allowlist — a synthetic fixture in a
test, a path that only ever contained an example key — put the config at
`.gitleaks.toml` in the repository root and the `secrets` job picks it up
automatically. This repository keeps its own at
[`git/gitleaks.toml`](../../git/gitleaks.toml), which allowlists one archived
plan document that quotes the secret-scan fixtures.

### 3. Replace the `quality` job

The `quality` job ships as a placeholder that **exits 1**. That is deliberate. A
placeholder that echoed a message and exited 0 would make `gate` green on a
repository where nothing had been verified, which is a check that cannot fail —
the exact defect the gate exists to catch. Expect red CI until you replace it
with the real commands; the examples below are written to be replaced, not
appended to.

### 4. Require `gate` in a repository ruleset

GitHub's current mechanism is **Rulesets** (`Settings` → `Rules` → `Rulesets`),
not the older classic branch protection rules. Create a ruleset that targets the
default branch and requires one status check, with the context exactly:

```
gate
```

The context is the job id, not a display name. Because the template's `gate` job
has no `name:`, its context stays `gate` in every adopter. Naming it would change
the context, and a ruleset naming a context that no job produces waits forever.

Ruleset JSON for "require a pull request, and require a passing `gate`":

```json
{
  "name": "Protect default branch",
  "target": "branch",
  "enforcement": "active",
  "conditions": {
    "ref_name": { "include": ["~DEFAULT_BRANCH"], "exclude": [] }
  },
  "rules": [
    { "type": "deletion" },
    { "type": "non_fast_forward" },
    { "type": "pull_request", "parameters": { "required_approving_review_count": 0 } },
    {
      "type": "required_status_checks",
      "parameters": {
        "strict_required_status_checks_policy": true,
        "required_status_checks": [ { "context": "gate" } ]
      }
    }
  ]
}
```

**Why require only `gate`?** It is the single check the ruleset names, so jobs
can be added, split, renamed or matrixed inside `verify.yml` without touching
repository settings.

---

## Quality job examples

Each example replaces the template's `quality` job. The `quality` job runs after
`actions/checkout`, and every `uses:` must be pinned to a commit SHA with the
release in a trailing comment, for the reason in the workflow header.

### Go

```yaml
  quality:
    name: Code Quality & Tests (Go)
    runs-on: ubuntu-latest
    timeout-minutes: 15
    env:
      # A stray go.work -- a contributor's -- must not change what CI resolves.
      GOWORK: off
      # One source for the version: setup-go's input and the toolchain cache key
      # below both read it, so a bump is one edit instead of two that disagree.
      # Never go-version-file: `go 1.26` in a go.mod floats to whatever the
      # runner happens to ship.
      GO_VERSION: '1.26.6'
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      # Fails closed when the epoch file is missing. `hashFiles` returns the
      # empty string for a path that matches nothing, and an empty hash keys
      # every run alike -- so a deleted epoch file would silently fall back to a
      # frozen cache instead of rotating it.
      - name: the cache key's epoch file is present
        run: test -s "$GITHUB_WORKSPACE/.github/go-build-cache-epoch" || { echo ".github/go-build-cache-epoch is missing or empty; every Go cache key would fall back to a frozen snapshot" >&2; exit 1; }
      # Restoring the extracted toolchain lets setup-go find the version it was
      # about to install, instead of downloading and unpacking it on every run.
      # Keyed on OS, architecture and version only: the toolchain is the same
      # for every job, so sharing it here is the point.
      - name: cache the Go toolchain
        uses: actions/cache@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0
        with:
          path: ${{ runner.tool_cache }}/go/${{ env.GO_VERSION }}
          key: go-toolchain-${{ runner.os }}-${{ runner.arch }}-${{ env.GO_VERSION }}
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: ${{ env.GO_VERSION }}
          # NOT `cache: true`. See "The Go build cache" below.
          cache-dependency-path: |
            go.mod
            .github/go-build-cache-epoch
      - name: build
        run: go build ./...
      - name: gofmt
        run: test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }
      - name: vet
        run: go vet ./...
      # -count=1 is load-bearing. Several tests legitimately assert against
      # tracked non-Go files, and editing those does not invalidate a cached
      # pass, so a cached result can be green for the wrong tree.
      - name: test
        run: go test -count=1 ./...
      - name: test -race
        run: go test -count=1 -race ./...
```

#### The Go build cache

`setup-go`'s `cache: true` keys its cache on `hashFiles(cache-dependency-path)`,
which defaults to `go.sum` — and it then restores with **no restore-key** and
refuses to save when the primary key already exists. An unrotated key is
therefore frozen at whatever the first job to finish holding that key had in
`GOCACHE`, and it can never grow. This repository measured the consequence on
2026-09-24: a macOS leg restored 8.9 MB seeded by a faster job, where a complete
cache for the module is 50.4 MB compressed, and paid 39s in `test -race` on a
leg that should have taken 9s. The measurements and the mechanism are in
[who owns the Go build cache](../../docs/design/2026-09-24-go-build-cache-ownership.md).

Two rules follow, and both are in the example above:

1. **Each job names the files that define its own cache** via
   `cache-dependency-path`, rather than relying on the default. In a
   multi-module repository, name the module the job actually builds; a key
   shared between two jobs is seeded by whichever finishes first.
2. **A rotating epoch file** (`.github/go-build-cache-epoch`, listed in every
   `cache-dependency-path`) moves every key at once when a stale entry needs to
   be abandoned. Because `hashFiles` returns the empty string for a missing
   path, the file's presence is asserted by a step that fails closed.

Do not turn caching off instead: `setup-go` caches the module cache as well as
the build cache, and losing it costs several seconds per job in recompilation.

### Python

For Python applications with Ruff and Pytest:

```yaml
  quality:
    name: Code Quality & Tests (Python)
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: actions/setup-python@5fda3b95a4ea91299a34e894583c3862153e4b97 # v7.0.0
        with:
          python-version: '3.14'
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

### TypeScript / JavaScript / Web

For Node.js / TypeScript applications:

```yaml
  quality:
    name: Code Quality & Tests (TypeScript/Node)
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: actions/setup-node@820762786026740c76f36085b0efc47a31fe5020 # v7.0.0
        with:
          node-version: '24'
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

For static sites with no build step:

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

## Hardening rules

Each rule is here because this repository hit the failure it prevents. The
evidence is in the reference workflow's comments and in `docs/design/`.

1. **Commit-pinned actions.** Every `uses:` is an immutable commit SHA with the
   release name in a trailing comment. A tag is a mutable pointer, so a tag
   reference lets the gate's own definition change without a commit.
2. **Checksum-verified downloads, plus provenance.** A binary fetched outside a
   package manager is verified against a hardcoded SHA256 *and* the installed
   binary is checked to resolve from the intended path and to report the
   expected version. A checksum proves the archive, not the binary the step runs.
3. **Least privilege.** `permissions: contents: read` at the workflow root. A
   job that needs more names it explicitly.
4. **Full-history secret scanning.** `fetch-depth: 0`, because `gitleaks git`
   walks commits and a shallow checkout sees only the tip.
5. **No `paths:` filters.** A required check that a filter skips stays
   "Expected" on the pull request and blocks it; it also skips verification for
   the edits a cached test run is least likely to notice.
6. **Cache-key inputs are asserted present.** `hashFiles` returns the empty
   string for a path that matches nothing, and an empty hash keys every run
   alike — so a renamed or deleted input silently freezes a cache. The step that
   asserts the file exists is the only part that fails closed.
7. **Each job names what it builds.** A cache key shared between jobs is seeded
   by whichever finishes first, which is the fastest, not the one that builds
   the most.
8. **`-count=1` on test runs whose assertions read tracked non-Go files.** Go's
   test cache is keyed on Go sources, so a cached pass can be green for a tree
   whose fixtures changed.
9. **A placeholder is not a check.** The `quality` job fails closed until it is
   replaced, because a step that prints a message and exits 0 makes the gate
   green while proving nothing.
10. **`shell: bash` on container jobs.** A job with `container:` does not
    default to bash; on `debian:stable-slim` `/bin/sh` is dash, which lacks
    arrays and `${PIPESTATUS[0]}`.

## Keeping the template current

Nothing runs this directory, so it drifted once already: its last change was
`ef4cdb7` (2026-08-29), while `.github/workflows/verify.yml` changed through
`f274bc8` (2026-09-24), and the one documented adopter,
`/Users/nilbot/devel/nilbot.net/toolshed/cowork`, has no
`.github/workflows/verify.yml`. Two mechanisms keep it honest now.

**A test, for the parts that can be compared.**
`bootstrap.d/ci_template_test.go` reads the reference workflow, this template,
and this README's YAML examples. It fails when a `uses:` is not a 40-character
commit SHA; when the same action is pinned to different commits in two of those
files; when the `gate` job carries a `name:`; when `gate`'s `needs` list misses a
job or `gate` lacks `if: always()`; when a `paths:` filter appears; or when the
template's `quality` job exits 0.

**A stated rule, for the parts that cannot be compared.** A change to a
*generic* mechanism — an action pin, the secret scan, the cache scheme, the job
set `gate` aggregates — updates `template/ci/verify.yml` and this README in the
same pull request. A change specific to this repository (a Linux provisioning
job, a doctest list) does not.

The test cannot see `actions/setup-python` and `actions/setup-node`: they appear
in this README and nowhere else, so there is no second pin to compare them
against. Re-resolve one by hand with:

```bash
gh api repos/<owner>/<repo>/commits/<tag> --jq .sha
```

Last swept 2026-10-07. Already current: `actions/checkout` v7.0.1,
`actions/setup-go` v7.0.0, `actions/cache` v6.1.0, gitleaks v8.30.1. Moved on
that sweep: `actions/setup-python` v5.4.0 → v7.0.0 and `actions/setup-node`
v4.0.3 → v7.0.0; `node-version` 20 → 24, because Node 20 reached end of life on
2026-04-30; `python-version` 3.12 → 3.14, because 3.12 left bugfix support on
2025-04-02.

`go-version` stays at the reference workflow's `1.26.6` on purpose: Go 1.26 is
still supported, the pin exists to be exact rather than newest, and this
repository's `GO_VERSION` is the single source for its own CI.
