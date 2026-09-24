# Trimming the two slow legs

**Date:** 2026-09-24
**Status:** implemented in `.github/workflows/verify.yml`; §4 is the measurement
that decides whether it stays.
**Related:** [spec 5 — the verification gate](2026-08-11-spec-5-verification-gate.md),
[who owns the Go build cache](2026-09-24-go-build-cache-ownership.md)

---

## 1. Where the time was

Run 35979821906, warm, once the Go caches were fixed. The macOS leg had just
come down from 105s to 44s, and the gate opened on something else:

| job | time | what it spends it on |
|---|---|---|
| `linux-stage-zero` (debian) | 78s | container 4s, apt archives 4s, apt prerequisites 12s, checkout 1s, Homebrew restore 20s, `apply workstation` 21s, post-save 8s, teardown 8s |
| `hygiene` | 75s | both modules' containment suites 44s, then bootstrap.d again under umask 22s, overhead 9s |
| `test (ubuntu-latest, bootstrap.d)` | 55s | test 21s, race 23s, overhead 11s |
| `test (macos-latest, agents)` | 44s | test 13s, race 17s, overhead 14s |

Two of those had something structural in them rather than just work.

**Hygiene ran the suites twice for two unrelated properties.** Containment (the
suite must not write to `$HOME`) is one property; bootstrap.d surviving
`umask 077` is another. The job executed them in series, so the job cost their
sum instead of their longest.

**Stage-zero paid 8s to re-save a cache every run, and 9s to compile a module in
a container with no build cache.** The package-archive entry was keyed on
`github.run_id`, which is new on every run: the save happens in the job's post
step, inside the job, so every run spent 8s writing 159-189 MB that the next run
would have restored in 4s — and the storage it churned is what had the whole
repository sitting at its cache limit. Separately, `./bootstrap` compiles
bootstrap.d before it can do anything: in run 35980296271 the container logged
`bootstrap: building` at 09:17:56.0 and preflight at 09:18:04.9, so 8.9s of the
21s apply step was a cold Go build in a fresh container.

## 2. What changed

- **`hygiene` is three legs**: containment for `agents`, containment for
  `bootstrap.d`, and the umask property. The job id is unchanged, so the gate's
  `needs:` and assertion are too. Containment now costs its longest module
  rather than both in series, and a failure names the module. The gitleaks
  install rides on the agents leg only: bootstrap.d's suite passes without it,
  which the test matrix's bootstrap.d leg demonstrates on every run.
- **The package-archive key names a version, not a run**: `stage-zero-<image>-v1`
  with the old `<image>-` prefix as a restore key, so the first run restores an
  existing entry rather than starting cold. Bump the `v1` if the archive set ever
  needs re-seeding; unlike the Go caches there is no hash to rotate, because the
  step runs before the checkout and `hashFiles` would return the empty string.
- **The container's `GOCACHE` is cached**, at `/tmp/go-build-cache` (set on the
  job so the path is explicit), keyed on `.github/go-build-cache-epoch` with no
  restore-key. The guard step now checks both files whose hashes appear in this
  job's keys.

## 3. What was deliberately not done

- **Narrowing the Homebrew cache.** The obvious cut is `Homebrew/` — the git
  clone and vendored ruby — which the installer re-fetches anyway. But that is
  the entry's most reusable half, and dropping it trades a smaller restore (20s,
  probably 12s) for a much slower installer (5s → ~20s, git clone plus
  portable-ruby). The Cellar is the part worth restoring; there is nothing else
  big enough to cut.
- **Splitting containment *and* umask per module into four legs.** The umask
  property is asserted over bootstrap.d specifically; a second copy of it for
  agents would be a different property nobody asked for.

## 4. What it measured

Run 35981119794, both attempts. Attempt 1 met the two new keys cold (they saved)
and the archive key restored through its prefix; attempt 2 restored all three.

| job | before | attempt 1 | attempt 2 |
|---|---|---|---|
| `hygiene` | 75s | 41s (legs 31 / 41 / 22) | **41s** (legs 36 / 41 / 36) |
| `linux-stage-zero` (debian) | 78s | 72s | **67s** |
| `test (macos-latest, agents)` | 44–75s | 39s | 72s |
| `test (ubuntu-latest, bootstrap.d)` | 55s | 57s | 56s |

Inside the Debian leg, attempt 2:

| step | before | after |
|---|---|---|
| cache package archives | 4s | 4s, and the post step is **0s** where it was 5–8s |
| prerequisites (apt) | 12s | 12s |
| cache the Homebrew prefix | 20s | 18s |
| cache the container's Go build cache | — | 2s (saved 2s on attempt 1) |
| provision | 21s | **15s** |
| — of which `bootstrap: building` | 8.9s | **4.1s** |
| teardown | 8s | 7s |

The archive step is the one to read twice: it restored
`stage-zero-debian:stable-slim-35980845003` — an entry written before this change,
through the prefix restore key — and on the second run the post step did nothing
at all, where it used to spend 5–8s writing 159–189 MB that the next run would
have restored in 4s.

**What is left.** `linux-stage-zero` is now 67s: 5s of container, 12s of apt
prerequisites, 18s of Homebrew restore, 15s of apply, 7s of teardown. The
Homebrew restore and the apt prerequisites are 30s of that and neither is
structural — the first is the one entry that pays for itself seven times over
(101s of install against 18s of restore), the second is `apt-get update` plus six
packages a fresh container cannot do without. The macOS leg's 39–72s across runs
is runner variance, not work: the same steps, the same caches, different
neighbours.
