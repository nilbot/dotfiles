# Who owns the Go build cache

**Date:** 2026-09-24
**Status:** in force. Implemented in `.github/workflows/verify.yml`; §4 is the
measurement that decided it.
**Related:** [spec 5 — the verification gate](2026-08-11-spec-5-verification-gate.md),
[stage zero keeps Homebrew's prefix](2026-09-24-stage-zero-brew-prefix-cache.md)

---

## 1. The measurement

Run 35977829906, one commit, one workflow, four `setup-go` legs. Same steps
everywhere; the only difference is what each leg's go-build cache held:

| leg | cache restored | `build` | `vet` | `test` | `test -race` | leg |
|---|---|---|---|---|---|---|
| macOS arm64 (agents) | 8.9 MB | 8s | 4s | 19s | 39s | 105s |
| Linux x64 (agents) | 17.3 MB | 0.4s | 1.8s | 12.0s | 26.1s | 53s |
| Linux arm64 (agents) | 33.7 MB | 0.3s | 0.7s | 8.4s | **9.3s** | 36s |

The race step is where it shows. On arm64 the step took 9.3s and its long pole,
the root package, took 9.05s — the leg had nothing left to compile. On x64 the
same step was 26.1s against a 13.2s pole, and on macOS 39s against 25.9s: about
thirteen seconds each of recompiling race-instrumented std that a complete cache
already contained. macOS paid the same toll three more times — 3.1s inside the
plain step, 8s in `build`, 4s in `vet`.

Measured locally with an isolated `GOCACHE`, for the same module (Go 1.27.1 on
arm64, the CI pin is 1.26.6):

| step | cold cache | warm cache |
|---|---|---|
| `go build ./...` | 3.7s | 0.2s |
| `go vet ./...` | 1.5s | 0.1s |
| `go test -count=1 -run '^$' ./...` (compile only) | 1.9s | 0.5s |
| `go test -count=1 -race -run '^$' ./...` (compile only) | 8.6s | 1.5s |

A complete cache for this module is 253 MB on disk and 50.4 MB as the zstd
archive the cache service stores — against the 8.9, 17.3 and 33.7 MB the three
legs were restoring.

## 2. Why the snapshots were small

`actions/setup-go@b7ad1dad` (v7.0.0) does three things that together make a
stale key permanent:

- `cache-restore.ts` derives `setup-go-<platform>-<arch>-<image>-go-<version>-<hashFiles(cache-dependency-path)>`
  and calls `cache.restoreCache(cachePaths, primaryKey)` — **with no
  restore-keys**. A key that misses is a full rebuild; there is no fallback to
  the previous entry.
- `cache-save.ts` returns early: `if (primaryKey === state) { ...not saving
  cache }`. An exact hit is never re-saved, so an entry can never grow.
- The key is therefore seeded once, by **the first job to finish** holding that
  key — and every job on the same OS and architecture held the same one, because
  all five named `agents/go.mod`.

That made the seeding job the fastest, not the one that builds the most:

| OS / arch | jobs sharing the key | first to finish | seeded with |
|---|---|---|---|
| macOS arm64 | `test (macos, agents)` 105s, `macos-dotfiles` 43s | `macos-dotfiles` | 8.9 MB of bootstrap.d artifacts |
| Linux x64 | agents leg 53s, bootstrap.d leg, `docs` 20s, `hygiene` | `docs` | 17.3 MB of doctest build |
| Linux arm64 | agents leg only | itself | 33.7 MB, complete |

And it could never repair itself: `agents/go.mod` is three lines with no
dependencies, last changed 2026-09-21, so the hash in the key is a constant.

## 3. What changed

- **Each job names what it builds.** `${{ matrix.module }}/go.mod` for the test
  matrix — which also stops the bootstrap.d leg from keying on the agents module
  — `agents/go.mod` for `hygiene` (it runs after the matrix, so it restores
  rather than seeds), `agents/doctests.txt` for `docs` (the key names the list
  that job exists to run), `bootstrap.d/go.mod` for `macos-dotfiles` (what
  `./bootstrap plan` compiles).
- **`.github/go-build-cache-epoch` rotates every key at once.** It is listed in
  every `cache-dependency-path`, so bumping it moves all of them off a snapshot
  that has gone stale. A one-line guard in each of those four jobs fails closed
  if the file is missing: `hashFiles` returns the empty string for a path that
  matches nothing, and setup-go throws only when *every* named path matches
  nothing, so a deleted epoch file would otherwise fall back to the pre-epoch
  key in silence. The guard names `$GITHUB_WORKSPACE` and not a relative path:
  the `test` job runs its `run:` steps from the module directory
  (`defaults.run.working-directory`), and the first version of this guard failed
  on all four legs of run 35979538540 for that reason. Rotating the epoch was
  the repair; the run that failed had already let the three jobs that outlived
  it seed the new keys from their own partial builds.
- **The toolchain is cached.** `macos-dotfiles` and the macOS matrix leg both
  pay 23s in `setup-go` — 5s downloading Go and 13s copying the extracted tree
  into the runner's tool cache. An `actions/cache` step on
  `${{ runner.tool_cache }}/go/${{ env.GO_VERSION }}`, run before `setup-go`,
  lets it find the version it was about to install. Keyed on OS, architecture and
  version only; sharing is the point, because the toolchain is the same for every
  job on that platform.
- **`GO_VERSION` moved to the workflow `env`.** `setup-go`'s input, the toolchain
  cache's key and the path it restores all read it, so a bump is one edit rather
  than four that can disagree.

## 4. What it measured

Run 35979821906, both attempts. Attempt 1 met every new key cold and saved;
attempt 2 restored them.

| leg | before (35977829906) | cold, attempt 1 | warm, attempt 2 |
|---|---|---|---|
| `test (macos-latest, agents)` | **105s** | 72s | **44s** |
| `test (ubuntu-latest, agents)` | 53s | 50s | 28s |
| `test (ubuntu-24.04-arm, agents)` | 36s | 53s | 32s |
| whole run, to the gate | 2m00s | 1m27s | **1m24s** |

The macOS steps, which is where the money was:

| step | before | cold | warm |
|---|---|---|---|
| `setup-go` | 23s | 3s | 2s |
| `build` | 8s | 6s | 1s |
| `vet` | 4s | 3s | 0s |
| `test` | 19s | 16s | 13s |
| `test -race` | 39s | 27s | 17s |

`setup-go` now logs `Found in cache @ /Users/runner/hostedtoolcache/go/1.26.6/arm64`
instead of installing Go, and the go-build cache it restores is the agents leg's
own: 35.7 MB, where the macOS entry had been 8.9 MB seeded by `macos-dotfiles`.
That 35.7 MB is not the 50.4 MB §1 compresses to locally — that measurement is
`go1.27.1` on this machine and the runner pins `go1.26.6`, so the sizes are not
comparable across Go versions. What is comparable is the work: `build` 1s and
`vet` 0s on a warm macOS runner, where the entry used to leave 12s of compiling
behind it.

**The critical path moved.** With the macOS leg at 44s the gate now waits on
`linux-stage-zero` (78s: 22s restoring the Homebrew prefix, 21s applying, 13s of
apt) and on `hygiene` (75s: 44s running both suites under a synthetic `$HOME` and
22s more running bootstrap.d under a restrictive umask). The next lever is
there, not on macOS — and the two Go legs that still outrun the macOS one,
bootstrap.d at 55s and hygiene, are the ones that spend their time spawning
`./bootstrap` rather than compiling.

## 5. What would revert it

- The toolchain step: if `setup-go` stops finding the restored toolchain and
  installs anyway, the step is dead weight and comes out.
- The epoch: if a rotation does not produce a bigger entry than the one it
  replaced (8.9 → 35.7 MB on macOS here), the seeding story in §2 was wrong and
  the per-job keys are not doing what they were added for.

The costs are one cold run per rotation and about 200 MB of cache storage
(three toolchain entries, read on every run, plus one go-build entry per key).
Both are cheaper than the ~20s per run that recompiling std was costing.
