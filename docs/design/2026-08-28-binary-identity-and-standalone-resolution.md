# Design: Explicit Binary Identity and Standalone Resolution

**Date:** 2026-08-28  
**Status:** Under Review  
**Applies to:** `agents` CLI, `DotfilesRoot()`, `agents doctor`, `githook` dispatcher, Homebrew packaging (`nilbot/tap/agents`)  
**Depends on:** [Spec 1](2026-08-07-agents-repo-context-design.md) (harness wiring), [Spec 5](2026-08-11-spec-5-verification-gate.md) (verification gate), [Spec 6](2026-08-11-spec-6-releases-and-distribution.md) (releases & binary distribution), [Contributor Guardrails](2026-08-28-contributor-guardrails-and-scaffold-decoupling.md) (2026-08-28)  
**Reads against:** [`docs/qna/can-this-check-actually-fail.md`](../qna/can-this-check-actually-fail.md)

---

## 1. Problem Formulation

The `agents` CLI operates in two distinct runtime paradigms:
1. **Dotfiles Operator Mode**: The binary is bound to the operator's personal `dotfiles` checkout. It validates machine-level global hooks (`~/dotfiles/git/hooks.d`), chains personal hooks (`~/dotfiles/git/hooks/*`), and asserts that global Git configuration matches the stamped dotfiles root.
2. **Standalone Mode**: The binary is distributed as a generic release artifact (e.g. via Homebrew or GitHub Releases) for external contributors and standalone repositories (e.g. `cowork`). It requires no dotfiles checkout and focuses exclusively on repository-local harness wiring, transcript caching, secret guarding, and repo-local diagnostics.

### The Defect: Accidental Heuristic in `DotfilesRoot()`

Previously, `DotfilesRoot()` in `agents/root.go` fell back to checking whether `$HOME/dotfiles` existed on disk:
```go
// Legacy implementation:
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
    fallback := filepath.Join(home, "dotfiles")
    if info, err := os.Stat(fallback); err == nil && info.IsDir() {
        return fallback
    }
    return ""
}
```

This heuristic introduced a critical flaw:
* On any machine where a directory named `~/dotfiles` exists (including the operator's machine or any developer with a dotfiles folder), an unstamped release binary (such as `/opt/homebrew/bin/agents`) automatically activated **Dotfiles Operator Mode**.
* When executing `agents doctor` inside a standalone repo (such as `cowork`), the Homebrew binary detected `~/dotfiles`, inspected `~/dotfiles/git/hooks.d/pre-commit`, and failed with:
  ```text
  FAIL  git-hooks:links              pre-commit does not resolve to the current binary
        -> run the reviewed global hook installer
  ```
* The check failed because the symlink in `hooks.d` pointed to `~/bin/agents` rather than the Homebrew Cellar binary (`/opt/homebrew/Cellar/agents/0.2.0/bin/agents`).

---

## 2. The Explicit Binary Identity Contract

To eliminate false mode inferences and guarantee predictable behavior across all platforms, `DotfilesRoot()` transitions from heuristic detection to an **explicit, deterministic contract**.

### 2.1 Resolution Rules

An `agents` binary is bound to a dotfiles checkout if and only if:
1. **Link-Time Stamp (Highest Precedence)**: The binary was compiled with `-ldflags "-X main.dotfilesRoot=<path>"`. This is set by `make agents` and `./bootstrap apply workstation`.
2. **Explicit Environment Variable**: `AGENTS_DOTFILES_ROOT=<path>` is non-empty in the process environment.
3. **Standalone Fallback (Default)**: In all other cases, `DotfilesRoot()` returns `""` (empty string).

```go
// DotfilesRoot answers which checkout this binary belongs to.
//
// A binary belongs to a dotfiles checkout if and only if:
// 1. Stamped at link time: go build -ldflags "-X main.dotfilesRoot=<path>"
// 2. Or explicitly configured via AGENTS_DOTFILES_ROOT.
//
// An unstamped binary without AGENTS_DOTFILES_ROOT operates in Standalone Mode
// and returns "" regardless of what exists in the user's home directory.
func DotfilesRoot() string {
	if dotfilesRoot != "" {
		return dotfilesRoot
	}
	return os.Getenv("AGENTS_DOTFILES_ROOT")
}
```

### 2.2 Mode Matrix

| Binary Provenance | Link Stamp (`main.dotfilesRoot`) | Environment (`AGENTS_DOTFILES_ROOT`) | Resolved `DotfilesRoot()` | Resulting Mode |
| :--- | :--- | :--- | :--- | :--- |
| **Homebrew Release** (`/opt/homebrew/bin/agents`) | `""` | Unset | `""` | **Standalone Mode** |
| **Homebrew Release** (Operator override) | `""` | `/Users/nilbot/dotfiles` | `/Users/nilbot/dotfiles` | **Dotfiles Operator Mode** |
| **Locally Compiled** (`make agents` $\rightarrow$ `~/bin/agents`) | `/Users/nilbot/dotfiles` | Any (stamp wins) | `/Users/nilbot/dotfiles` | **Dotfiles Operator Mode** |
| **CI / Container Test** | `""` | Unset | `""` | **Standalone Mode** |

---

## 3. Subsystem Behaviors

### 3.1 `agents doctor`

`agents doctor` uses `DependenciesFor(DotfilesRoot())` to configure its diagnostic suite:

* **When `deps.Root == ""` (Standalone Mode)**:
  * `rootChecks()`: Returns `nil` (skips `root:exists`).
  * `checkGitHooks()`: Skips `git-hooks:global` and `git-hooks:links`. Only runs `git-hooks:local` and `git-hooks:legacy`.
  * `checkGitAttributes()`: Skips checking the global `~/.gitattributes` link; checks the repo-local `.gitattributes` for `.agents/** linguist-generated=true`.
  * **All repo-local checks run in full**: `binary` (PATH resolution), `wiring:*`, `trust:*`, `recording:*`, `gitleaks`, `trace-index`, `pointers`, `scaffold:doctor-instruction`, `docs-freshness`, and `lane-health`.

* **When `deps.Root != ""` (Dotfiles Operator Mode)**:
  * Runs the complete suite including `root:exists`, `git-hooks:global` (verifying `core.hooksPath` points to `<root>/git/hooks.d`), `git-hooks:links` (verifying all 4 hooks symlink to the running executable), and global `~/.gitattributes`.

### 3.2 `agents hook` & Git Multi-Call Dispatcher

When invoked as a Git hook (`pre-commit`, `commit-msg`, `post-merge`, `post-checkout`):
* `ExtrasDir` is computed as `filepath.Join(DotfilesRoot(), "git", "hooks")`.
* In **Standalone Mode** (`DotfilesRoot() == ""`), `ExtrasDir` is ignored, and `agents` only executes repository hooks (`c.RepoHooksDir`) and built-in stages (e.g. `commit-msg` AI trailer stripper, `pre-commit` secret scanner).
* In **Dotfiles Operator Mode**, `agents` executes repository hooks, personal hook stages in `<root>/git/hooks/*`, and built-in stages in order.

---

## 4. Test Strategy & Verification

### 4.1 Unit Tests in `agents/root_test.go`
* `TestDotfilesRootReturnsEmptyWhenUnstampedAndEnvUnset`: Asserts `DotfilesRoot() == ""` when unstamped and env unset, even when `$HOME/dotfiles` exists as a directory.
* `TestDotfilesRootPrefersEnvironmentOverEmpty`: Asserts `AGENTS_DOTFILES_ROOT` is respected by unstamped binaries.
* `TestDotfilesRootStampWinsOverEnvironment`: Asserts a stamped binary ignores `AGENTS_DOTFILES_ROOT`.

### 4.2 Integration Tests in `agents/cmd_doctor_test.go`
* Update `TestIsolatedDoctorUnderUnstampedChildBinary`:
  * Explicitly test standalone repository execution (where dotfiles checks are cleanly skipped).
  * Explicitly test dotfiles operator execution using `AGENTS_DOTFILES_ROOT` or stamped binary fixture.

### 4.3 Clean Execution Verification
* Run `agents doctor` in `cowork` using the Homebrew binary: must report `ok` across all applicable repo checks with 0 failures.

---

## 5. Documentation Updates

1. **`docs/design/2026-08-11-spec-6-releases-and-distribution.md`**: Update Section 2 to document explicit resolution without `$HOME/dotfiles` directory fallback.
2. **`Makefile` & `bootstrap.d/internal/phase/devtools.go`**: Update code comments regarding unstamped binaries operating in Standalone Mode.
3. **`docs/qna/why-does-an-unstamped-or-homebrew-agents-binary-skip-dotfiles-checks.md`**: New Q&A documenting Standalone Mode mechanics and `AGENTS_DOTFILES_ROOT`.

---

## 6. Self-Review Checklist

- [x] **Placeholder Scan**: No TODOs, TBDs, or unspecified behaviors.
- [x] **Internal Consistency**: Mode matrix aligns with `doctor.go`, `cmd_hook.go`, and `main.go`.
- [x] **Scope Check**: Tightly scoped to `DotfilesRoot()` resolution, related tests, and documentation.
- [x] **Ambiguity Check**: Link-time stamp vs environment precedence is explicitly specified.
