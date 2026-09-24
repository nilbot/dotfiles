# Stage zero keeps Homebrew's prefix between runs

**Date:** 2026-09-24
**Status:** in force. Implemented in `.github/workflows/verify.yml`; §5 is the
measurement that decided it — kept, at 2m50s → 1m41s per pull request.
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

## 5. What it measured

The first run against a new key is cold and writes the entry; a re-run of that
same run reads the entry its first attempt wrote, which is the only way to time a
restore before the copy on `master` exists. Both are run 35976648444.

| | Debian leg | Arch leg | pull request wall clock | critical path |
|---|---|---|---|---|
| baseline, run 35975118898 | 2m41s | 2m07s | 2m50s | `linux-stage-zero` (Debian) |
| attempt 1, cold | 3m34s | 2m14s | 3m41s | `linux-stage-zero` (Debian), **+73s writing the entry** |
| attempt 2, warm | **1m10s** | **54s** | **1m41s** | `test (macos-latest, agents)`, 1m27s |

Restore is 21s of the Debian leg and 10s of the Arch leg (615 MB and 576 MB
written). Inside the Debian leg's `apply workstation`:

| step | cold (baseline) | warm |
|---|---|---|
| apt stage zero | 2s | 2s |
| Homebrew installer over the restored prefix | 17s | 5s |
| `brew bundle` | 84s | 1s |
| `config` + `fish` (`source fish/mypre.fish; install_fisher`) | 1s | 1s |
| `devtools` | 2s | 1s |

**Kept.** The steady state is 1m09s faster per pull request, the critical path
moved off stage zero exactly as predicted, and the cold penalty lands on
Brewfile edits — the runs that have something to prove.

### 5.1 The entry has to reach `master` before a pull request can read it

Cache entries are scoped to a ref, and while a pull request reads the base
branch's entries it cannot read another pull request's. So this is cold twice
before it is warm for everyone: once in the pull request that writes the entry
its own re-runs read, and once in the first `master` run after the merge, whose
copy is what later pull requests restore. Both are paid after review except the
first, which is why the re-run above — not attempt 1 — is the number that
describes the steady state.

### 5.2 What would get this reverted

- A restore growing to the size of the install it replaces. It is 21s against
  101s.
- An entry large enough to evict what it sits beside. It writes 1.19 GB against
  a budget the API reports at 10.06 GiB across 68 entries — at GitHub's
  documented 10 GB per repository, with eviction therefore already active. What
  keeps the entry resident is that both legs read it at the top of every run,
  while the archives beside it are rewritten per `run_id`: the entry is never
  the least recently used. If it is evicted anyway, the run is slower, not
  wrong.
- A `brew bundle` that stops being a no-op on a warm prefix, which would mean
  the restored prefix had stopped matching what the Brewfile asks for.

The lever for the budget pressure is not this cache but the one under it: the
per-`run_id` archive entries write 348 MB on *every* run and can never be
restored by a later one, which is what fills a 10 GB budget with 68 entries.
That is a separate change to `stage-zero-<image>-<run_id>` and it is not made
here.
