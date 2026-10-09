# What a query is

**Date:** 2026-10-09
**Status:** version six, read by an adversarial reviewer and by a cold reader, and
confirmed clean. No code changed.
**Scope:** the compiled verbs of `bootstrap.d`. The other tool in this repository,
the `agents` command-line program, appears only where its `doctor` command holds the
one runtime check that a package is installed. The bootstrap check whose name is
also `agents` is a different thing: it reports whether a program called `agents`
resolves on `PATH`, not whether this repository's `agents` is installed correctly.
**Supersedes in part:** [spec 2](2026-08-07-spec-2-dotfiles-hygiene.md) §10 item 8,
"`Brewfile` packages present". Its known gap at `:829` records the false failure
this change removes: on the stage-zero images Homebrew lives at
`/home/linuxbrew/.linuxbrew` and is not on `PATH`, so the check reported "Homebrew
is not installed" for a machine that had it.

---

## 1. What a query is

**A query is a compiled verb that reports on the machine and must not change it.**
Two of the four verbs are queries: `plan` says what would change, and `check`
reports whether the machine is in the state bootstrap would put it in. The other
two change it: `apply` brings the machine to that state, and `migrate` reconciles a
machine left in an older shape.

Both queries currently change the machine. On the author's machine on 2026-10-08,
`./bootstrap check workstation` — the profile that provisions everything —
printed
`==> Auto-updating Homebrew… Updated Homebrew from 7.0.8 (f534bc750b) to 7.0.9
(96d7c8ba12)` before its report, and `./bootstrap plan workstation` runs the same
command by the same route. This document explains the mechanism and proposes the
shape that removes the class of failure rather than the instance.

### Terms used here

- **Phase** — one unit of provisioning work: `preflight`, `packages`, `config`,
  `fish`, `devtools`, `verify`.
- **Profile** — a named subset of the phases. `workstation` is all six; `dotfiles`
  is `preflight`, `config` and `verify`, the three that need no privilege, no
  network and no package manager.
- **Stage zero** — the first step of the `packages` phase: installing Homebrew's
  own prerequisites (`build-essential` or `base-devel`, `curl`, `file`, `git`)
  through the machine's native package manager, before Homebrew exists.
- **Tap** — a Homebrew formula repository, by default one hosted on GitHub. Brew
  resolves what to install from taps, which is why its answer depends on the
  network rather than on the machine alone.
- **Applier** — the object that really runs commands and writes files.
- **Planner** — the object that records what a run *would* do. Its reads delegate
  to the real machine, and its writes answer as though they had happened: after a
  planned `Link`, `Lstat` on that path reports the link present.
- **`change.Interface`** — the type both of those satisfy. It declares `Run`, the
  single method that executes a command.
- **Check machine interface** — `check.Machine`, declared in
  `bootstrap.d/internal/check/check.go`. It is a subset of `change.Interface`,
  asserted as compatible by `check_test.go`'s `var _ check.Machine =
  change.Interface(nil)`, and it currently includes `Run`.
- **The fake** — the test double for the machine in the check package's tests. It
  records the calls a check makes and answers with what a test declares, so a test
  can say what the machine looks like without building one.

## 2. Constraints

**C1. The command the design reasoned about and the command that runs are not the
same program.** `bootstrap.d/internal/check/checks.go:326-332` justifies asking
Homebrew: *"`brew bundle check` reads and reports; it installs nothing, which is why
a check may run it."* That is true of the `brew bundle check` subcommand and false
of the `brew` wrapper that executes it. Homebrew lists `bundle` among the commands
that trigger a self-update — `AUTO_UPDATE_COMMANDS` in
`/opt/homebrew/Library/Homebrew/utils/auto-update.sh:144-150` on Homebrew 7.0.9 —
and `auto-update "$@"` runs before the command is dispatched (`brew.sh:668`, `:682`).
Nothing suppresses it: `grep -rn HOMEBREW_NO_AUTO_UPDATE .` over this checkout
returns nothing, and the variable is unset in the author's machine environment and
shell configuration.

Suppressing the self-update would not settle the deeper point. Brew decides what to
install by reading **taps** published on GitHub, so the same command on the same
machine can answer differently tomorrow, and no environment variable makes it a
question about the machine alone.

**C2. A query's promise is enforced at the wrong boundary.**
`bootstrap.d/internal/check/check.go` opens by stating that the package imports
nothing capable of I/O, so "a check cannot mutate" is a property of the import
graph. The graph is intact and the promise is broken twice, at places it cannot
see. `bootstrap.d/internal/check/checks.go:347` runs `brew bundle check`, through
the check package's own `Run`. `bootstrap.d/main.go:334` runs `dscl` on macOS or
`getent` on Linux to compute the login shell, which is the caller rather than the
package. The architecture test cannot close either:
`bootstrap.d/architecture_test.go` parses with `parser.ImportsOnly`, so it compares
import paths, and a method call adds no import — a limit
`bootstrap.d/internal/check/check.go:44-45` states itself.

**C3. `plan` reaches the same command, deliberately.**
`bootstrap.d/internal/phase/verify.go:27` builds a fresh Applier instead of using
the Planner its context holds. That is right, and the reason is the Planner's
overlay: after a planned `Link`, the Planner answers `Lstat` as if the link
existed, so a Planner-backed verify would report the state the plan intends rather
than the state the machine is in. The same choice puts Homebrew on `plan`'s path as
well as `check`'s. `bootstrap.d/main_test.go:146-160` records the consequence: the
packages check really executes brew, twice per suite run.

**C4. Correctness of the CLI call is decided at design time, and already is.**
`bootstrap.d/internal/phase/packages_test.go:47` pins the exact command:
`run <brew> bundle --file <checkout>/bootstrap.d/Brewfile`, through the resolved
brew path rather than the bare name. `TestBrewfileCarriesTheAudit` (`:530`) asserts
the formulae a phase resolves by name are present, with the reason for each.
Measured: emptying `bootstrap.d/Brewfile` fails three tests in that file —
`TestBrewfileCarriesTheAudit`, `TestBrewfileGuardsEveryCaskWithOSMac` and
`TestBrewfileDeclaresNothingTwice`.

**C5. CI's stage-zero job makes a liveness assertion that a state check cannot
replace, and its leniency is deliberate.**
`.github/workflows/verify.yml:589-604` records the narrowing of 2026-08-17: the job
gates stage zero, `apply workstation` is only the route to that code, and the phases
it drags in are unfinished on Linux — one Arch run failed inside `brew bundle` on a
corrupted partial download in Homebrew's own download cache, which is not this
repository's code. The exit code is reported and not asserted, on purpose.

## 3. Trade-off decisions

| what we choose | what we give up | what would let us revisit |
|---|---|---|
| a query reads the machine and executes nothing, with one declared exception | a standing answer to "are my packages still installed", except for `gitleaks`, which `agents doctor` still checks (`agents/internal/doctor/doctor.go:611-619`) | nothing, for the Brewfile. The question belongs to `brew bundle check`, which knows the answer and can state its own network assumptions |
| not to set `HOMEBREW_NO_AUTO_UPDATE` as a contract | the one-line suppression of the self-update that caused the incident | never as a contract. C1's second paragraph is why: it stops the self-update and leaves the answer coming from taps on the network. It remains a legitimate thing for a user to set |
| the package set is verified at design time, by tests that pin the command and the file | a run-time comparison of the machine against the Brewfile, including a missing or unparseable one | a machine whose packages diverge and somebody who needs bootstrap to say so |
| the login shell stays a declared read-subprocess | the unqualified sentence "a query executes nothing" | a way to read the user's shell from a plain file on both platforms |
| the claim is scoped to the compiled verbs | the same sentence applied to `./bootstrap`, whose shell shim runs `uname`, `find`, `cksum`, `tr` and a Go build before the verb is known | a shim that decides the verb before it builds anything |
| CI keeps its log grep and its four `command -v` probes, and its exit code stays reported rather than asserted | the assertion that would have caught the devtools bug — the phase ran `brew install uv` by bare name, which fails with "executable file not found in $PATH" on a machine that has just installed Homebrew and has not yet put it on `PATH` | a way to tell a repository failure from a Homebrew-side one — a retry around `apply`, or an integrity check on the restored prefix |

Named and given up, so they are not discovered later. **Casks**: `brew bundle check`
covered `cask "macfuse"` and a font cask, and after this change nothing does;
macfuse is a kernel extension whose absence stays invisible until something needs a
FUSE mount. **Taps** are uncovered by any file read, so what a machine is missing
from a tap is not something any check will tell you. **Runtime remnants**: the one
place that still asserts a package is installed is
`agents/internal/doctor/doctor.go:611-619`, which looks for `gitleaks` on `PATH` —
one formula out of the Brewfile's thirty, and neither cask.

## 4. What changes

1. **The `packages` check is removed, not repaired.** Its four arms are a `Lstat`
   of the Brewfile, a `LookPath` for brew, the brew call, and a success message.
   Remove the brew call and the check reports "every Brewfile entry is installed"
   having asked nothing about packages — a verdict it did not reach, which is the
   defect `bootstrap.d/internal/check/checks.go:13-15` names. The `Lstat` arm does
   not save it: the Brewfile is tracked (`git ls-files bootstrap.d/Brewfile` returns
   it), so its absence means a damaged checkout. Nothing else checks the file
   either — it is not a `links.manifest` row, so `manifest-owners` does not cover
   it — and after this change a missing or mistyped Brewfile surfaces when
   `brew bundle --file` fails at the next `apply`, on the same terms as any other
   edit to that file. The `LookPath` arm is the false failure spec 2 records on the
   stage-zero images.
2. **`Run` leaves the check machine interface.** With the brew call gone, no check
   needs it, and deleting it from `check.Machine` makes the guard a compile error
   rather than a promise an import scan cannot keep.
3. **Seven checks remain**, by the names the output uses: `platform`,
   `manifest-owners`, `manifest-kinds`, `fish-source`, `gitconfig-include`,
   `login-shell`, `agents`.
4. **The login shell is named as the one read-subprocess a query performs**, with
   its reason: on macOS the user's shell lives in the directory service rather than
   in a plain file. `check.Context.Shell` already carries the value in from the
   caller, so the check package cannot reach the database itself, and the
   declaration is this document plus the comment on that field.
5. **No new test is added for the Brewfile.** The empty-Brewfile mutation is already
   caught, by the three tests C4 names.
6. **CI does not change**, and the reason the defect survived is sharper than "no
   job runs `check workstation`". `linux-stage-zero` does run `apply workstation`,
   whose `verify` phase calls `check.All` with the `workstation` profile and so does
   reach the packages check. Two things in that check kept brew from running. The
   `LookPath("brew")` arm fails in those containers, because brew is at
   `/home/linuxbrew/.linuxbrew` and is not on the shell's `PATH` — the defect spec 2
   records. And the brew call names the **bare** `brew`
   (`bootstrap.d/internal/check/checks.go:347`), so `exec.Command` would perform its
   own `PATH` lookup and fail there too, even when the file exists on disk.
   **The bug was load-bearing.** A repair that made both arms work — resolving brew
   by prefix *and* passing that resolved path to `Run`, which is the pair
   `bootstrap.d/internal/phase/packages.go:54` performs — would have run
   `brew bundle check`, and Homebrew's auto-update, inside CI on every stage-zero
   run, against the warm prefix `verify.yml:568-572` restores.

### Migration: what this costs a machine that is already provisioned

Nothing on the machine migrates. The change adds no file, no record and no
configuration, and it removes none: it deletes a check from a program's behaviour
and a method from an interface. A machine that is already provisioned sees
`bootstrap check` stop asking Homebrew a question, and its `bootstrap` binary is
rebuilt by whatever route built it before — `bootstrap apply`, or a developer's
`make`-equivalent. Rollback is reverting the change and rebuilding. There is no
state to unwind, which is why this document has no rollback section — where the
sibling design for binary identity, in review on another branch, needs two, one for
each of its two stages.

Two things change for a person, and neither is on the machine. Anyone who ran
`./bootstrap check workstation` to ask whether their packages are installed loses
that answer, and has to ask `brew bundle check` instead — that is §3's first row.
And a Brewfile that is missing or mistyped is no longer noticed by `check`; it
surfaces when `brew bundle --file` fails at the next `apply`, which is §3's third
row.

## 5. What enforces the old behaviour today

Fifteen sites carry the behaviour being removed, or become wrong once it is gone.
Paths are relative to the repository root.

| site | what it says now | what it becomes |
|---|---|---|
| `bootstrap.d/internal/check/check.go:13-28`, `:50-51` | "a check that asks a question by running one — packages asks `brew bundle check`"; "Run stays because `brew bundle check` is how the packages check asks its question" | rewritten around what the interface protects once `Run` is gone |
| `bootstrap.d/internal/check/checks.go:326-332` | the justification C1 quotes as the design's error | deleted with the function |
| `bootstrap.d/internal/check/check.go:54-60` | the check machine interface has five methods, `Run` among them | four methods |
| `bootstrap.d/internal/check/check.go:131-140` | "machineChecks are the three that concern machine-wide state", and the slice carries a `packages` entry | two, and two entries |
| `bootstrap.d/internal/check/check.go:116-120` | "Checks 6-8 … report three problems that are not problems" | two problems |
| `bootstrap.d/internal/check/check_test.go:371-387` | `TestMachineCannotMutate` asserts five methods and names `Run` | four methods |
| `bootstrap.d/internal/check/check_test.go:460-473` | `TestPackagesFailsWhenTheBrewfileIsAbsent` pins the arm being removed | deleted |
| `bootstrap.d/internal/check/check_test.go:475-480` | `TestPackagesFailsWhenBrewBundleCheckFails` makes the fake fail on `brew` and requires `packages` to fail | deleted |
| `bootstrap.d/internal/check/check_test.go:106-115` | `TestAllReportsTheEightChecks` asserts the list including `packages` | seven |
| `bootstrap.d/internal/check/check_test.go:130-149`, `:151-165` | two dotfiles-profile tests call `assertStatus` on `packages`, which is fatal on a missing name, and both delete the Brewfile from the fake; both also say "three machine checks" in the comments at `:127-131` and `:148-149` | the `packages` lines, the deletes and the counts go |
| `bootstrap.d/main_test.go:520-537` | `TestCheckOnABareHomeNamesTheMissingRows` compares `checkStatus("packages")` to `n/a`, and its comment says "three checks" | the `packages` row and the count go |
| `bootstrap.d/main_test.go:646-687` | `TestPlanAndCheckAgreeOnThePackagesVerdict` runs a stub brew that exits 1 and requires both verbs to print the same verdict | deleted: the question no longer executes |
| `bootstrap.d/main_test.go:146-160` | explains the stub-brew harness by "the packages check really does execute `brew bundle check`" | the harness stays for other stubs; this paragraph goes |
| `bootstrap.d/main_test.go:389-390`, `:501` | cite the `packages` check as the worked example of a phase banner and of `checkStatus` | re-pointed at a surviving check |
| `bootstrap.d/internal/phase/verify.go:18-26` | justifies the fresh Applier by `brew bundle check` needing to execute | rewritten to the Planner-overlay reason C3 gives |

## 6. How we would know it works

| check | the mutation that must break it |
|---|---|
| no check can execute a command | delete `Run` from `check.Machine` in `check.go` and add a call to it in a check: the package fails to compile. **Do not** reach the same end by making `plan` hold an Applier instead of its Planner — that mutation executes every phase, and on 2026-10-09 it ran the fish phase's `sudo chsh` and changed the developer's login shell. The compile error is the safe mutation, and it is also the one the interface exists to produce |
| no query invokes Homebrew | one stub directory holding a `brew` that appends its arguments to a file, shown to work before it is trusted: invoke that directory's `brew` once directly, assert the file grew, truncate it, then run `plan workstation` and `check workstation` with that same directory on `PATH` ahead of `stubToolDir(t)` — which must stay behind it, so `dscl`/`getent` are still stubbed — and assert the file stays empty. The directory has to be one handle used for both halves: `stubToolDir` mints a fresh temporary directory on every call, so a second call would put a different script on `PATH` from the one whose recorder was proved |
| the packages check is gone | `check.All` returns seven results; the name-list test fails on the eighth |
| the empty-Brewfile mutation is caught | empty `bootstrap.d/Brewfile`; the three tests C4 names fail — already true today, and now named |
| the stage-zero gate still notices a phase that never ran | delete `{"packages", Packages}` from `All()`; the log grep fails. It is not the only net: `gcc` and `file` are installed by nothing but stage zero, so the probes fail too, and what the grep adds is that the phase reached the stage-zero branch and printed. `bootstrap.d/main_test.go:381` catches the same mutation earlier, in the unit suite |
| `plan` still reports the machine rather than the plan | swap `verify.go:27`'s Applier for the context's Planner and assert the verify section names the missing row. The setup is what makes the run reach verify: `altCheckout` copies only `bootstrap` and `bootstrap.d`, so the test writes a one-row `links.manifest` into the copy, creates that row's source file there, and points the target at a path absent from the temporary home — otherwise the config phase refuses the real manifest, `plan dotfiles` exits 2, and the assertion fails for an unrelated reason. Nothing asserts this today: the existing plan tests assert exit codes, phase banners and forbidden banners, so the swap would leave the suite green |

## 7. Open questions

1. Should every check report `n/a` for state the active profile does not provision,
   as a rule rather than per check? `managesMachine` applies it to the two machine
   checks that remain, `login-shell` and `agents`, and three tests already assert
   that behaviour — two in `bootstrap.d/internal/check/check_test.go:130-165` and
   one in `bootstrap.d/main_test.go:520-537` — but the general rule is asserted
   nowhere, and the verdict vocabulary is `OK`, `NA`, `Warn` and `Fail`.
2. Does `plan` need its own statement of the contract, or is one sentence covering
   both queries enough? This document takes the second position, and C3 is the
   reason: they share one code path.
3. Should `linux-stage-zero` assert `apply workstation`'s exit code? The narrowing
   of 2026-08-17 is why it does not, and the assertion would have caught the
   devtools bug. That job is the only one that reaches the devtools phase, and its
   exit code is reported rather than asserted, so the bug has no guard today.
