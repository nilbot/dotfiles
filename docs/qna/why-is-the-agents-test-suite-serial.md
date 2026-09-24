# Why is the `agents` test suite serial, and what would it take to change that?

## Context

Asked on 2026-09-24 while looking for the next few seconds in the macOS Go leg,
right after its build caches were fixed (that leg went from 105s to 44s — see
[who owns the Go build cache](../design/2026-09-24-go-build-cache-ownership.md)).
Two changes were on the table: parallelising the suite, and merging the `go
build` invocations its fixtures make. Measured, neither is available as a
tweak.

The short answer: **the suite is 98 tests that each spawn real processes, the two
heaviest files cannot use `t.Parallel()` as written, and there are far fewer
in-test builds than there appear to be.**

## The numbers

Local, `go1.27.1` on arm64 macOS, warm build cache, `go test -count=1 ./...`:

| package | tests | sum of test time |
|---|---|---|
| `agents` (root) | 98 | 17.6s |
| `internal/guard` | 22 | 9.6s |
| `internal/repo` | 21 | 2.7s |
| `internal/githook` | 32 | 1.8s |
| `internal/scaffold` | 8 | 1.7s |
| `internal/harness`, `internal/doctor`, `internal/safeio` | 50 | 0.4s |

The root package's sum is its wall: **nothing in it runs concurrently**. The same
is true of `internal/guard`. Within the root package the time is spread thin —
the slowest single test is 4.09s, the next is 2.11s, and the rest is a long tail
of 0.2–0.5s cases in `install_hooks_test.go` (8.08s across the file) and
`githook_main_test.go` (5.29s). Those cases spawn `bash git/install-hooks.sh` and
real `git` repositories; they are bound by process spawns, not by computation.

## What blocks `t.Parallel()`

`t.Setenv` and `t.Parallel` are mutually exclusive — parallel tests may not set
the process environment. The root package calls `t.Setenv` **48 times**, in the
very files that hold the time:

| file | `t.Setenv` calls |
|---|---|
| `githook_main_test.go` | 30 |
| `root_test.go` | 7 |
| `cmd_init_test.go` | 6 |
| `cmd_guard_test.go` | 3 |
| `cmd_doctor_test.go`, `cmd_wire_test.go` | 1 each |

So parallelising the suite is not a flag; it is a refactor that hands each test's
environment to the subprocesses it spawns (`cmd.Env`), which most of them
already build explicitly. The prize is real but bounded: with the root package's
sum equal to its wall, running even half of it concurrently on a 3-vCPU runner
would take the package from ~16s to ~11s, and the same suite runs in five
places (three matrix legs, one hygiene leg, and the docs job's doctest subset).

## There are five in-test builds, not seventeen

`install_hooks_test.go` reads as if every fixture compiles the module: fourteen
tests call `runHookInstaller`. They do not build — they hand the installer the
stub shell script `newHookInstallFixture` writes. Exactly one helper,
`runHookSequence`, compiles anything, and it has one call site that runs it
twice. `githook_main_test.go`'s `buildTemporaryAgentsBinary` has three call
sites. Five `go build` invocations, ≈0.22s each warm: **about 1s of the 17.6s**.

Two of those five are not shareable in any case: `runHookSequence` builds from a
copy of the checkout whose path deliberately contains spaces, and the build is
part of what that test proves. A shared binary built once in `TestMain` would
take the other three (~0.7s) and change a property this suite exists to assert.

## What this means

The macOS leg's remaining time is the suite itself, and the suite is at its
serial floor. The one lever with real headroom is the `t.Setenv` refactor above;
it is worth doing for the local loop and for five CI legs at once, but it is not
a few-seconds tweak, and the leg it would help is no longer the gate's critical
path — `linux-stage-zero` is (see
[trimming the two slow legs](../design/2026-09-24-trimming-the-slow-legs.md)).

## Amended 2026-09-24 — it was done, and this is what it bought

The refactor above landed the same day, in two pieces:

- **`install_hooks_test.go` needed nothing but `t.Parallel()`**: its fixtures are
  per-test temp directories and it already hands every subprocess an explicit
  environment (`isolatedGitEnvironment`). 26 tests, none of their assertions
  changed.
- **`githook_main_test.go`'s three spawning tests** carry their environment in
  the fixture now — `liveHookRepo.env`, built by `childEnv` — instead of pinning
  the process with `t.Setenv`. That is the actual `t.Setenv` → explicit-env
  change, and it is also the more faithful test: a hook is a grandchild of the
  command that starts it, and
  `TestMulticallDispatcherRunsOrdinaryWrapperWithInheritedEnvironment` is about
  exactly that inheritance, which the process environment was hiding.

Measured on the same machine, whole root package, repeated runs:

| `go test -parallel` | before | after |
|---|---|---|
| 1 (the old behaviour) | 17.9s | 14.4s |
| 3 (the CI runner's core count) | 17.9s | **10.0–10.6s** |

The two watchdogs (the installer's and the dispatcher's) went from 2s to 10s:
they exist to catch a hang, and a hang detector that fires on a loaded runner is
a flake, not a finding.

**What stays serial, and why.** The three tests in `githook_main_test.go` that
call `runGitHook` in process, and the four `cmd_*` files, resolve their
repository from the process's working directory (`t.Chdir`) or from its
environment; two of them also mutate the `dotfilesRoot` build stamp.
`t.Parallel` and `t.Chdir` are as exclusive as `t.Parallel` and `t.Setenv`.
Making those parallel means running the built binary as a subprocess — a
different test, one that no longer exercises the in-process command — or
threading a root through the command, which is a different API. Neither is a
test-only change, and the 4.3s they hold between them is not worth either.
