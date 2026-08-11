# Checkout path and field defects — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Stop `agents` assuming the checkout is `~/dotfiles`, and close the two defects the first real provisioning run exposed.

**Architecture:** The checkout root is stamped into the `agents` binary at build time by every builder that has it, with an explicit fallback chain. The fish phase declares fisher's plugin record as a seeded machine-local file so `fisher install` can never mistake an installed plugin for a conflicting one. The login shell comes from the passwd database, with `$SHELL` demoted to a hint.

**Tech Stack:** Go 1.26, stdlib only. Two modules, `agents/` and `bootstrap.d/`, no `go.work`.

## Global Constraints

- **Stdlib only.** No new dependencies in either module.
- **Test command:** `cd agents && go test -count=1 ./...` and `cd bootstrap.d && go test -count=1 ./...`. **`-count=1` is mandatory** — several tests read tracked non-Go files, and a cached pass has already hidden a real break.
- **Run the `bootstrap.d` suite under `umask 077` as well as your own.**
- **`internal/phase` must import no I/O package** — not `os`, `os/exec`, `io/fs`, `net`. Enforced by an architecture test over the import closure.
- **Refuse, never clobber.** Seeded files are never overwritten.
- **Exit codes:** `0` ok, `1` advisory, `2` block/refuse, `3` malformed input, `4` not applicable.
- **Commit messages:** no AI attribution, no `Co-Authored-By` trailer.
- **Staging:** exact paths per commit. Never `git add -A`.
- **No test may execute** `fish`, `brew`, `sudo`, `chsh`, `tee`, `apt-get`, `pacman`, the Homebrew installer, or `git/install-hooks.sh` against the real machine.

---

## The two design decisions this plan encodes

**The root is stamped, not inferred.** `-ldflags "-X main.dotfilesRoot=<root>"` at every build site. Inferring it from `core.hooksPath` was rejected: `doctor`'s `git-hooks:global` check compares `core.hooksPath` against `HooksDir`, so deriving one from the other makes that check pass by construction — a guard that cannot fail. The stamp is an independent statement of what the binary belongs to, which is what makes the comparison meaningful. Fallback chain, in order: build stamp → `AGENTS_DOTFILES_ROOT` → `$HOME/dotfiles`.

**Fisher's plugin record becomes a seeded file.** The field failure was fisher refusing four plugins whose files were on disk because its own `fish_plugins` did not list them — then deleting that record, so every retry failed identically. `fish_plugins` is exactly the shape the placement rule already covers: a well-known path another program writes to. It becomes a `seed` row sourced from the tracked `fishfile`, so a machine always starts with a record that describes intent, and fisher reconciles from there. `jorgebucaran/fisher` moves into `fishfile` so the two lists are the same list.

---

### Task 1: Stamp the checkout root into `agents`

**Files:**
- Modify: `agents/main.go`
- Create: `agents/root.go`, `agents/root_test.go`
- Modify: `agents/internal/doctor/doctor.go`, `agents/internal/doctor/doctor_test.go`

**Interfaces:**
- Produces: `func DotfilesRoot() string` in package `main`; `doctor.DependenciesFor(root string) Dependencies`.

The `-X` linker flag can only set a string variable, and only in a named package. It goes in `main` because that is the only package both builders name.

- [ ] **Step 1: Write the failing test** — `agents/root_test.go` covering the chain in order: an empty stamp with `AGENTS_DOTFILES_ROOT` set returns the env value; both empty returns `$HOME/dotfiles`; a non-empty stamp wins over both. Use `t.Setenv`. The stamp is a package variable, so save and restore it rather than assuming it is empty.
- [ ] **Step 2: Run it, confirm it fails** — `cd agents && go test -count=1 -run Root ./...`
- [ ] **Step 3: Implement** `agents/root.go`:

```go
package main

import (
	"os"
	"path/filepath"
)

// dotfilesRoot is set at link time by every builder that knows the checkout:
//
//	go build -ldflags "-X main.dotfilesRoot=<root>"
//
// It is deliberately NOT inferred from core.hooksPath. doctor's git-hooks:global
// check compares the configured hooksPath against the root's git/hooks.d, so a
// root derived from that value would make the check pass by construction -- a
// guard that cannot fail is worse than no guard, because it reports success.
var dotfilesRoot string

// DotfilesRoot answers which checkout this binary belongs to.
//
// The fallback to ~/dotfiles is the historical assumption, kept last so an
// unstamped binary behaves as it always did rather than failing outright.
func DotfilesRoot() string {
	if dotfilesRoot != "" {
		return dotfilesRoot
	}
	if fromEnv := os.Getenv("AGENTS_DOTFILES_ROOT"); fromEnv != "" {
		return fromEnv
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "dotfiles")
}
```

- [ ] **Step 4: Give doctor the root instead of letting it guess.** Add `DependenciesFor(root string) Dependencies` carrying the current body with `dotfiles` replaced by the parameter. Keep `DefaultDependencies()` as `DependenciesFor(filepath.Join(home, "dotfiles"))` **only if** something outside `main` still calls it; otherwise delete it and update callers. Change the call in `main.go` to pass `DotfilesRoot()`.
- [ ] **Step 5: Extend `doctor_test.go`** with a case building dependencies for a root that is *not* `~/dotfiles` and asserting all four derived paths sit under it. The existing test pinning `git/gitconfig.shared` literally must keep passing.
- [ ] **Step 6:** `cd agents && go test -count=1 ./...`, `gofmt -l`, `go vet`. Commit.

### Task 2: Pass the stamp from both builders, and fix the silent ExtrasDir

**Files:** `agents/main.go`, `Makefile`, `bootstrap.d/internal/phase/devtools.go`, `bootstrap.d/internal/phase/devtools_test.go`, `bootstrap.d/makefile_test.go`

`agents/main.go:41` sets `ExtrasDir` to `$HOME/dotfiles/git/hooks`, and `githook.go` treats a missing extras directory as "no personal hooks" — so a relocated checkout silently runs none. That is the *silent* half of this defect and matters more than doctor's loud half.

- [ ] **Step 1:** Point `ExtrasDir` at `filepath.Join(DotfilesRoot(), "git", "hooks")`.
- [ ] **Step 2:** `Makefile` — add `-ldflags "-X main.dotfilesRoot=$(CURDIR)"` to the `agents` recipe.
- [ ] **Step 3:** `devtools.go` — add the same flag to the `go build` invocation, using `c.Root`. Update `devtools_test.go`'s expected argv; it asserts the exact command.
- [ ] **Step 4:** Extend `bootstrap.d/makefile_test.go` to assert the Makefile's `agents` target carries the `-X main.dotfilesRoot=` flag, so the two builders cannot drift apart silently.
- [ ] **Step 5:** Both suites, `-count=1`, plus `umask 077`. **Do not run `make agents`** — it overwrites `~/bin/agents`. Use `make -n agents`. Commit.

### Task 3: The login shell comes from the passwd database

**Files:** `bootstrap.d/main.go`, `bootstrap.d/main_test.go`

Go's `os/user.User` exposes no shell field, so this reads the platform's user database: `dscl . -read /Users/<name> UserShell` on darwin, `getent passwd <name>` on linux. Both run in `main.go`, which already owns environment access; `internal/phase` still receives a plain string.

- [ ] **Step 1: Write the failing test** — the resolver parses `dscl` output (`UserShell: /opt/homebrew/bin/fish`) and `getent` output (colon-separated, shell is field 7), and falls back to `$SHELL` when the command fails or the output does not parse. Drive it through an injected command runner; **no test may execute `dscl` or `getent`**.
- [ ] **Step 2:** Run it, confirm it fails.
- [ ] **Step 3:** Implement, with a comment stating why `$SHELL` is only a hint: it is inherited, so a process started under another shell reports that one, which made `check` report a false `login-shell` failure on a healthy machine.
- [ ] **Step 4:** Use it for `Context.Shell` in both places `main.go` builds a context. Fall back to `os.Getenv("SHELL")`.
- [ ] **Step 5:** Both suites, `-count=1`. Commit.

### Task 4: Refuse the collision fisher cannot reconcile, and seed its record

**Files:** `fish/fishfile`, `fish/mypre.fish`, `bootstrap.d/links.manifest`, `bootstrap.d/internal/manifest/manifest_test.go` (if it counts rows), `bootstrap.d/internal/check/checks.go` if a check enumerates manifest kinds

**Corrected premise.** This task was planned as "seed `fish_plugins` and the conflict goes away". That is wrong, and the correction is the point of the task. Fisher classifies each plugin as install-vs-update from the **universal variable** `_fisher_plugins`, never from the `fish_plugins` file:

```fish
set --local old_plugins $_fisher_plugins
contains -- "$plugin" $old_plugins && set --append update_plugins $plugin
                                   || set --append install_plugins $plugin
```

The conflict branch runs only for plugins in `install_plugins`. So the failure state is **files present on disk while `_fisher_plugins` is empty**, and seeding the file does not touch it. Confirmed in the field: the machine was recovered by deleting `~/.config/fish/{functions,conf.d,completions}`, not by restoring the file.

Seeding is still correct — `fish_plugins` is a well-known path another program writes to, exactly what the placement rule covers, and `fisher update` with no arguments reads it. It is just not the fix.

**The fix is to refuse before fisher runs.** When nothing installs, fisher reaches `command rm -f $fish_plugins` and deletes its own record, which is what made every retry fail identically. A guard that runs first never lets fisher get there, so the loop cannot start.

- [ ] **Step 1:** Add `jorgebucaran/fisher` to `fish/fishfile` as the first line, so the tracked list and fisher's record are the same list.
- [ ] **Step 2:** `install_fisher` becomes `fisher install $plugins` — it no longer names fisher separately, because `fishfile` now does.
- [ ] **Step 3:** Add the manifest row, in the same column shape as the others:

```
seed    fish/fishfile                     .config/fish/fish_plugins         *
```

- [ ] **Step 4:** Write the test first, before the row: a case asserting the manifest carries a `seed` row whose source is `fish/fishfile` and whose target is `.config/fish/fish_plugins`, and one asserting `fishfile` names `jorgebucaran/fisher`. Any test asserting a manifest row count must be updated in the same commit.
- [ ] **Step 5:** Confirm `Seed` does not substitute `@DOTFILES_ROOT@` into a file that has no such token — it should pass through unchanged. Add a case if none covers a token-free source.
- [ ] **Step 6: The guard.** In `install_fisher`, before calling `fisher`: if `_fisher_plugins` is empty **and** any of `functions/`, `conf.d/`, `completions/` under `$__fish_config_dir` holds a `.fish` file, print a refusal and return non-zero without invoking fisher. The message must name the three directories and say the files are fisher's to reinstall. Do **not** delete anything — refuse, never clobber.
- [ ] **Step 7:** Both suites, `-count=1`, plus `umask 077`. Commit.

**Deliberately not done: bootstrap does not remove the files itself.** Every file in those directories was plugin-owned on the machine that hit this, but that is a fact about one machine, not a guarantee. A hand-written function there would be destroyed with no way back, so the remediation is stated and left to the operator.

---

## Self-Review

**Coverage.** doctor.go:79 → Task 1; main.go:41 → Task 2; both build sites → Task 2; `$SHELL` → Task 3; fisher idempotence → Task 4.

**Corrected after Task 3.** This section originally claimed seeding `fish_plugins` fixed the observed conflict and left only a "stale file" case uncovered. Both halves were wrong: fisher reads `_fisher_plugins`, not the file, when deciding install-vs-update, so the file's contents never affect whether the conflict fires. Task 4 was rewritten accordingly — see its corrected premise.

**Not covered, deliberately.** Bootstrap does not remove the colliding files. Doing so would be the only thing that makes the failure self-healing, and it is also the one action that can destroy a hand-written fish function with no way back. The guard states the remediation and stops.

**Ordering.** Task 2 depends on Task 1's `DotfilesRoot()`. Tasks 3 and 4 are independent of both and of each other.
