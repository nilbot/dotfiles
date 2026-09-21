# `agents` and `bootstrap`: the boundary

**Date:** 2026-09-20
**Status:** Review recorded, no code changed. §7 lists the follow-ups and none of
them is implemented here — this document exists so the decisions are made in the
open rather than accumulated by adding another entry point.
**Depends on:** [spec 1](2026-08-07-agents-repo-context-design.md) (repository
context), [spec 2](2026-08-07-spec-2-dotfiles-hygiene.md) (machine hygiene and
bootstrap), [spec 5](2026-08-11-spec-5-verification-gate.md) (the gate).

---

## 1. Why this document exists

A review of the hook installer asked whether `agents install-hooks` should exist,
so that installing the global Git hooks stops being "a shell script with three
positional arguments that nothing calls". The concern raised was that such a
subcommand would overlap `bootstrap.d`. It would — and the overlap is not
hypothetical. Two owners already disagree about the same `$HOME` paths on the
machine this was written on, and one of those disagreements is an incident
(§4b).

## 2. The two scopes, from first principles

| | `bootstrap.d` | `agents` |
|---|---|---|
| unit of change | **the machine** | **one repository** |
| writes | `$HOME` paths, packages, login shell, global Git config, the global hook chain | files under a checkout: `.agents/`, the doc stores, instructions, skills, `.claude/settings.json`, `.codex/hooks.json` |
| reads | its own manifest, PATH, the package manager | a repository, and — read-only — machine state (`~/.gitconfig`, `~/.gitattributes`) |
| entry points | `./bootstrap plan\|apply\|check <profile>` | `agents <command>`, `agents hook` as a Git-hook multicall |
| profiles | `workstation` = every phase; `dotfiles` = `preflight+config+verify`, the one with no sudo, no network, no package manager and no login-shell change | n/a |

**The rule:** bootstrap reconciles the machine; `agents` reconciles a repository
and reports on the machine. Every `agents` write lands inside a checkout — the
only `$HOME` references in the binary are reads performed by `doctor` so it can
report on machine state it does not own.

## 3. The crossing: `core.hooksPath`

Exactly one resource is machine-global *and* speaks `agents`: the hook chain.
`core.hooksPath` lives in `~/.gitconfig` and its content is four symlinks to the
`agents` binary.

| artifact | role |
|---|---|
| `git/install-hooks.sh` | the installer. Shell, deliberately: it must work when no `agents` binary exists yet — a fresh machine, a failed build, or a Homebrew-only user who never runs bootstrap |
| `bootstrap.d/internal/phase/devtools.go` | builds `~/bin/agents`, then calls the installer at **that** path |
| `agents hook` / `agents doctor` | the runtime, and read-only verification. The only `agents`→installer link in the codebase is `doctor` *printing* the repair command |

The call direction is one-way: bootstrap calls the installer, bootstrap builds
`agents`, and `agents` never calls either. That is the property to preserve.

## 4. Three leaks, measured on this machine

### (a) Two owners, two targets, one key

`devtools` installs the hooks at `~/bin/agents`, the binary it just built. This
machine's hooks name `/opt/homebrew/bin/agents`, because they were installed from
the package manager instead. Both are correct for their owner; whichever runs
second fails. Today `bootstrap apply workstation` refuses here, naming the
existing links, because `devtools` passes no `--adopt-owned` — a refusal that
predates this review and that the installer now explains better.

### (b) A `$HOME` path the system writes to, aimed at content this repo publishes

`links.manifest:20` declares `~/.claude/skills` as a symlink to the checkout's
`claude/skills`. The harness wrote its own skill content (`synced/`) **through
that link**, into the tracked working tree — 200+ uncommitted changes by the
operator's account, discovered and repaired by hand on 2026-09-19, and not
written down until now. The machine-readable shape it left behind is a real
directory holding per-skill links, which `bootstrap plan workstation` refuses:

```text
bootstrap: config: refusing: /Users/nilbot/.claude/skills
  problem: exists and is not a symlink
  remedy:  move it aside deliberately, then retry
```

The refusal is correct — it protected the checkout. **The declaration is what is
wrong.** This is the third instance of one defect class, and spec 1 §8.4 already
records the first two: `~/.gitconfig` was a symlink to `git/gitconfig.symlink`, so
every `git config --global` wrote into tracked public content; `~/.claude` was
symlinked wholesale from the checkout for the same reason and with the same
result. The rule those two produced — *a `$HOME` path the system writes to must
never be aimed at content this repository publishes* — was applied to
`~/.gitconfig` and to `~/.claude`, but not to the subdirectory the harness
writes into.

Consequence today: `bootstrap plan workstation` exits 2 at `config` and nothing
after it runs, so the machine cannot be provisioned through that profile at all
until the declaration is decided.

### (c) Two writers of `~/.gitconfig`

The installer writes `core.hooksPath` and refuses a config that is a symlink or
hard-linked; bootstrap's `config` phase seeds `gitconfig.local.template` and the
shared include. Neither knows the other's rules, and the installer's refusal can
fire on a file the config phase has just merged.

### Two mechanical duplications behind those leaks

- **The four hook names exist in three places** — `git/install-hooks.sh:61`,
  `agents/internal/doctor/doctor.go:685`,
  `agents/internal/githook/githook.go:23` — with no test tying them together, so
  adding a fifth hook name is a three-file edit that nothing checks.
- **Two different "is this ours" rules.** The installer asks whether a link
  resolves into a Homebrew keg for agents; `agents` asks whether a command's
  binary is named `agents*`. They answer adjacent questions and share a word,
  which is how a reader concludes one of them is wrong.

## 5. Verdict on `agents install-hooks`

**Do not add it.** Three reasons, in order of weight:

1. **It would be a third entry point for a resource that already has two owners
   who disagree** (§4a). The failure mode of two entry points is already on this
   machine; a third does not resolve it.
2. **It cannot be the only entry point, and must not pretend to be.** The
   installer has to run when no `agents` binary exists — the stage-zero argument
   this repository already makes for `./bootstrap` itself. A Go subcommand would
   be a second door into a stage-zero job, and it would carry a different default
   target (`$(command -v agents)`) than bootstrap's (`~/bin/agents`), which is
   precisely how §4a came about.
3. **The friction it answers is smaller than it looks.** `doctor` already prints
   the exact command, and the remaining cost is typing three paths.

**What to do instead** if the friction still bites: make the installer's three
positional arguments optional — root from the script's own location, home from
`$HOME`, binary from `command -v agents` — so the whole thing is one line:

```bash
bash ~/dotfiles/git/install-hooks.sh install --adopt-owned
```

That keeps one entry point, adds no capability, and leaves bootstrap's explicit
four-argument form untouched.

## 6. Rules this review proposes

1. **One owner per machine-global resource, and the owner is bootstrap** — except
   the hook chain, where the installer is the mechanism and bootstrap is only a
   caller.
2. **A declaration must not claim a path another system writes into.** §4b is the
   worked example; the test to apply is "does anything but this repository write
   here?", not "is it a subdirectory?".
3. **`agents` writes only inside a checkout.** Needing a machine write is the
   signal that the operation belongs to bootstrap or to the installer.
4. **One source of truth per constant.** The hook names belong in one Go list,
   with a test asserting the shell copy matches it; the shell copy stays, because
   stage zero cannot import Go.
5. **No second entry point for an existing operation without deleting the
   first.** If an operation genuinely needs two, they must share one default
   target, not two.

## 7. Deferred, in the order they should be taken

1. **Decide the `~/.claude/skills` declaration** (§4b). It is a decision, not a
   patch: the manifest can stop claiming the path, or the harness side can be
   told to stop writing into it, but the current pair cannot both stand. Until it
   is decided, `bootstrap plan|apply workstation` is blocked on this machine.
2. **Settle hook ownership when bootstrap and a package manager are both
   present** (§4a): whether `devtools` should pass `--adopt-owned`, defer to
   links it did not write, or refuse with an explicit instruction.
3. **Optional installer arguments** (§5) — the ergonomics fix that does not move
   the boundary.
4. **A test tying the shell's hook names to the Go list** (§4).
5. **A Homebrew caveat** printing the install line at upgrade time, so the
   instruction arrives where the breakage happens.
