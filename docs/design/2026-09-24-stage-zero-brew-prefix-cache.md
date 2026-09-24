# Stage zero keeps Homebrew's prefix between runs

**Date:** 2026-09-24
**Status:** implemented in `.github/workflows/verify.yml`; §5 is the measurement
that decides whether it stays.
**Related:** [spec 5 — the verification gate](2026-08-11-spec-5-verification-gate.md),
[Debian container package cache fix](2026-08-29-ci-debian-package-cache-fix.md)

---

## 1. The measurement this came from

Measured on `verify` run 35975118898 — PR #56, which deleted four lines from
`fish/mypre.fish` and nothing else. The pull request was green in 2m50s
(08:25:08 → 08:27:58), and the gate opened only when its slowest job finished:

| job | duration | finished |
|---|---|---|
| `secrets` | 10s | 08:25:21 |
| `docs` | 20s | 08:25:31 |
| `test` (ubuntu, agents) | 52s | 08:26:03 |
| `test` (ubuntu, bootstrap.d) | 1m17s | 08:26:29 |
| `test` (ubuntu-24.04-arm, agents) | 34s | 08:25:57 |
| `test` (macos, agents) | 1m46s | 08:27:02 |
| `hygiene` | 1m26s | 08:26:37 |
| `linux-dotfiles` | 38s | 08:25:49 |
| `macos-dotfiles` | 43s | 08:26:00 |
| `linux-stage-zero` (archlinux:base) | 2m07s | 08:27:17 |
| **`linux-stage-zero` (debian:stable-slim)** | **2m41s** | **08:27:52** |
| `gate` | 3s | 08:27:58 |

Every Go job had finished 50 seconds before the gate opened. On this run, making
the Go jobs conditional would have saved **zero** wall-clock time: `gate` needs
all of them, and it waits for the last one.

Inside the Debian leg, the `apply workstation` step was 1m56s and broke down as:

| phase | seconds |
|---|---|
| `packages` — apt stage zero (cached archives) | 2 |
| `packages` — Homebrew installer | 17 |
| `packages` — `brew bundle` over the Brewfile's 30 formulae | 84 |
| `config` | <1 |
| `fish` — `source fish/mypre.fish; install_fisher` | 1 |
| `devtools` | 2 |
| `verify` | <1 |

101 of the leg's 161 seconds were Homebrew's prefix being built from nothing, and
that work is a function of `bootstrap.d/Brewfile` — not of any diff that leaves
the Brewfile alone.

## 2. What changed

`linux-stage-zero` now restores `/home/linuxbrew/.linuxbrew` before
`./bootstrap apply workstation` runs:

```yaml
      - name: the file the Homebrew cache key hashes is present
        run: test -s bootstrap.d/Brewfile || { echo "bootstrap.d/Brewfile is missing or empty; the Homebrew cache key would hash nothing" >&2; exit 1; }
      - name: cache the Homebrew prefix
        uses: actions/cache@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0
        with:
          path: /home/linuxbrew/.linuxbrew
          key: stage-zero-brew-${{ matrix.image }}-${{ hashFiles('bootstrap.d/Brewfile') }}
```

Both legs install to that same prefix — the installer log of each leg names
`/home/linuxbrew/.linuxbrew/bin/brew` — so one path serves both, and the image
name stays in the key because the poured bottles are not portable between
Debian and Arch.

The re-run is a supported path, not a workaround. Homebrew's installer, finding
the prefix, re-inits the repository, fetches, and checks out its latest stable
tag — the branch of `install.sh` that exists so a reinstall cannot produce merge
errors. `brew bundle` then finds the formulae already poured.

## 3. The key is strict: no `restore-keys`

The only edit that can regress "this list installs on a clean machine" is an edit
to the list. Hashing the Brewfile into the key is what makes that edit **miss**
the cache and install from nothing, in the very pull request that could regress
it. A `restore-keys` fallback would hand that pull request the previous run's
prefix instead: the property would be traded away silently, on the single run it
was needed, and the run would look faster than the check it replaced.

The cost of the strict key is a cold install on every Brewfile change. That is
the intended price and it is paid rarely.

## 4. The guard, and why it is not decoration

`hashFiles` returns the empty string when it matches nothing. An empty hash keys
every run alike: a renamed or emptied Brewfile would restore one stale prefix for
as long as the cache lived, and nothing downstream would notice — the
provisioning step **reports** `apply workstation`'s exit code rather than
asserting it (spec 5, 2026-08-17 narrowing), so a `brew bundle` that installed
nothing still exits the job green. The step above the cache is the only thing
that fails closed, and it is why the key can be trusted.

## 5. What this trades away, and how it is measured

**Traded:** a Homebrew prefix that is not built from nothing on runs whose
Brewfile did not change. The from-scratch property is exercised by Brewfile
changes, and by nothing else.

**Preserved, and slightly improved:** `apply workstation` aborts at the first
failing phase, so `config`, `fish` and `devtools` — which is where the fish phase
sources `fish/mypre.fish` and installs the plugins in `fish/fishfile` — only run
if `packages` succeeded. A warm bundle reaches them sooner rather than later, and
the job's exit-code-tolerant shape means the difference between "reached" and
"did not reach" is exactly this.

**Measurement protocol.** The first run against a new key is cold and saves; a
re-run of that same run restores from the cache its first attempt wrote, which is
the only way to time a restore before the entry reaches `master`. Both are read
from `GET /repos/{owner}/{repo}/actions/jobs/{id}` (step timings) and
`/actions/caches` (entry size):

- keep if the Debian leg drops well below its 2m41s baseline — the floor is
  `test (macos, agents)` at 1m46s, which becomes the critical path;
- revert if the restore plus re-run installer costs what the install cost, or if
  the entry is large enough to evict the archives and `setup-go` caches it sits
  beside.

**The cache budget is real.** At the time of writing `actions/cache/usage`
reports 11.27 GB (10.5 GiB) active across 73 entries against GitHub's documented
10 GB per repository, and the largest entries are the per-`run_id` stage-zero
archives (159 MB Debian, 189 MB Arch) that every run writes a fresh copy of. The
brew entry therefore has to be read on every run to stay resident, which it is:
both legs restore it before anything else. If it turns out to be evicted instead,
the entry is re-created on the next run and the leg costs what it costs today —
slower, not wrong.
