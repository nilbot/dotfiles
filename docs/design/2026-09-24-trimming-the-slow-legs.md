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

The PR's own runs. The first meets three new keys cold (two archive entries, two
container build caches) and saves them; the second restores.

- keep if `hygiene` lands near its longest leg (about 39s) rather than its sum,
  and the archive post-save disappears from the step list;
- keep if the stage-zero `apply workstation` step loses about 9s to the restored
  build cache;
- revert the archive key if the restore key does not find the old entries, which
  would show as a cold `apt-get install` on the first run.

The numbers land below once the two runs are in.
