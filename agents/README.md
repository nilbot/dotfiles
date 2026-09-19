# agents

A developer harness manager, repository context framework, and transcript recorder for AI coding agents (Claude Code, Codex, Antigravity, Cursor).

---

## Features

- **Multi-Harness Wiring**: Automatically configures and keeps in sync hook configurations for Claude Code (`.claude/settings.json`), Codex (`.codex/hooks.json`), and Antigravity (`.agents/hooks.json`).
- **Two-Tier Context & Drift Detection**: Enforces clean separation between canonical machine routing (`AGENTS.md`, `CLAUDE.md`) and repository domain guidelines (`.agents/AGENTS.md`). `agents drift` inspects context layout, canonical diffs, domain context, bundled skills, and misplaced documentation across repositories. Repository-specific skills under `.agents/skills/` are listed as `local_skills` and never classified as drift.
- **Layout Manifest Resolution**: `agents layout show`, `agents layout validate`, and `agents layout path` read `.agents/layout.json` and report the layout a repository resolves to — schema, status, minimum mutating version (`min_mut_ver_floor`), and the store each role names — without writing anything. A repository with no manifest keeps the implicit v1 `docs/` layout. `agents layout migrate` adopts an existing v1 repository into a v2 layout: it freezes a resumable journal before the first move, moves each store with `git mv`, and replaces the router.
- **Fleet Maintenance & Skill Refresh**: `agents update` rewires machine hooks across registered repositories, refreshes the authoritative `migrating-fleet-context` skill, and emits advisory notices if any repository exhibits context drift.
- **Durable Transcript Caching**: Captures and preserves subagent conversation transcripts before harnesses delete them, storing them in `.agents/transcripts/` with retention and size bounding.
- **Repository Guardrails & Pre-Commit Secret Scanning**: Integrates `gitleaks` into `agents guard --staged` to catch secret leaks before commit.
- **Commit Message Sanitization**: Built-in Git `commit-msg` hook automatically strips AI attribution footers and co-author tags to keep git histories clean.
- **Self-Diagnostic Tooling**: `agents doctor` inspects harness wiring, trust permissions, transcript indices, repository hygiene, and 5 granular `scaffold:*` diagnostics without mutating your configuration.

---

## Installation

### Homebrew (macOS & Linux)

```bash
brew install nilbot/tap/agents
```

To upgrade:
```bash
brew update && brew upgrade nilbot/tap/agents
```

### Pre-built Binary (GitHub Releases)

Download pre-compiled binaries for Darwin (Apple Silicon / Intel) or Linux (x86_64 / ARM64) from [GitHub Releases](https://github.com/nilbot/dotfiles/releases).

Extract and place the binary on your `$PATH`:
```bash
tar -xzf agents_*_darwin_arm64.tar.gz
sudo mv agents /usr/local/bin/
```

### From Source (Go 1.26+)

```bash
go install github.com/nilbot/dotfiles/agents@latest
```

---

## Quickstart

Initialize any Git repository to track agent context, scaffold Two-Tier instructions, and wire harness triggers:

```bash
cd my-project
agents init
```

`agents init` creates the implicit v1 layout, whose four stores are
`docs/{design,plans,journal,qna}`. To create an `agents.layout/v2` layout
instead, pass a template: `agents init --template content-vault` puts the same
four stores under `.context/`, and `agents init --template code-repo` keeps them
under `docs/`. `--stores <role>=<path>` overrides an individual role and
`--archive <path>` records the immutable archive. A repository that already has
`AGENTS.md` or `docs/` is refused: adopting it is `agents layout migrate`'s job.

Run diagnostics to verify that harnesses, hooks, and scaffold integrity are intact:

```bash
agents doctor
```

Inspect repository or fleet-wide context layout and router drift:

```bash
# Check context layout and detect drift in current repository
agents drift

# Output full JSON drift report for AI agents and automation
agents drift --json

# Inspect a specific repository or check all registered fleet repositories
agents drift --repo /path/to/repo
agents drift --all
```

Update fleet wiring and refresh embedded migration skills:

```bash
# Dry run update across all registered repositories
agents update --all

# Apply harness rewiring and skill refresh across the fleet
agents update --all --apply
```

Resolve where the documentation stores live (read-only, never writes):

```bash
# The resolved layout: schema, status, min_mut_ver_floor, stores, archive
agents layout show

# One store path by role, for use in a shell command substitution
qna=$(agents layout path qna)

# Validate the manifest; exits 1 for problems or an unsupported version floor
agents layout validate
```

Adopt an existing v1 repository into an `agents.layout/v2` layout (dry run
first; `--apply` requires a backup tag):

```bash
# The plan: what moves where, what blocks it, and the links it would break
agents layout migrate --template content-vault --dry-run

# Apply it on a migration branch, with the rollback point as an annotated tag
agents layout migrate --template content-vault \
  --apply --backup-tag pre-layout-v2-20260918

# Continue a migration that stopped mid-way; --abort deletes a manifest from
# phase `planned`, while nothing can have moved
agents layout migrate --resume --apply
```

Inspect session transcripts and agent activity:

```bash
# List recorded sessions and subagent runs
agents trace ls

# View the full transcript of a specific turn or subagent
agents trace show <session-id>

# List recent recorded sessions and subagent runs with limit
agents trace ls --limit 10
```

---

## Operating Modes

`agents` operates in two modes:

### 1. Standalone Mode (Default)
When installed via Homebrew or downloaded from releases, `agents` operates as a standalone repository tool.
- Requires no external dotfiles clone.
- `agents doctor` checks repository-local harness wiring, repo `.gitattributes`, secret scanner presence, documentation freshness, and 5 granular `scaffold:*` diagnostics:
  - `scaffold:router`: Validates that root `AGENTS.md` matches the canonical router template without unpartitioned domain drift.
  - `scaffold:symlink`: Verifies that `CLAUDE.md` is a valid relative symlink to `AGENTS.md`.
  - `scaffold:domain`: Confirms presence of `.agents/AGENTS.md` for repository-specific domain rules.
  - `scaffold:skill-recording`: Checks status and customization state of `.agents/skills/recording-what-you-learn/`. This skill is repository-customizable, so local edits are reported without warning.
  - `scaffold:skill-migrating`: Checks that `.agents/skills/migrating-fleet-context/` matches the installed binary. This skill is `agents`-owned, so any divergence is staleness and warns; run `agents update --all --apply` to refresh it.
- Git hook dispatching executes repository-level hooks and built-in guards.

### 2. Dotfiles Operator Mode
For developers managing a centralized `dotfiles` checkout with machine-level Git hook chaining:
- **Build with Link Stamp**:
  ```bash
  go build -trimpath -ldflags "-X main.dotfilesRoot=$HOME/dotfiles" -o ~/bin/agents .
  ```
- **Or Set Environment Variable**:
  ```bash
  export AGENTS_DOTFILES_ROOT="$HOME/dotfiles"
  ```
- In Operator Mode, `agents` validates global `core.hooksPath` symlinks (`~/dotfiles/git/hooks.d/`) and chains personal hook scripts from `~/dotfiles/git/hooks/*`.

---

## CLI Reference

<!-- BEGIN GENERATED: agents help --render=markdown -->
| Command | What |
|---|---|
| `agents help` | print the listing, or one command's page |
| `agents init` | create .agents/, triggers, wiring, fleet entry |
| `agents wire` | regenerate harness configs (merges, never overwrites) |
| `agents doctor` | report wiring, trust evidence, reachability, and lane health |
| `agents drift` | inspect context layout and router drift |
| `agents layout` | inspect the resolved layout, or migrate v1 to v2 |
| `agents layout show` | print the resolved layout, or the canonical router |
| `agents layout validate` | check the layout against every validation rule |
| `agents layout path` | print one store path by role |
| `agents layout migrate` | plan, apply, resume, or abort a v1 to v2 migration |
| `agents save` | commit .agents/ paths and nothing else (escape hatch) |
| `agents trace` | query records; read one back; copy reachable ones |
| `agents trace ls` | query records |
| `agents trace show` | read one transcript back |
| `agents trace cache` | copy reachable transcripts into the store |
| `agents trace cache prune` | remove cached copies, never the records |
| `agents trace migrate` | move a tracked index into the machine-local store |
| `agents ls` | list the fleet on this machine |
| `agents update` | rewire every registered repo (dry run by default) |
| `agents version` | print binary version and build provenance |
| `agents guard` | pre-commit checks (the only command that blocks) |
| `agents hook` | harness hook entrypoint |
<!-- END GENERATED -->

### Layout commands

`agents layout` reads `.agents/layout.json`; `show`, `validate`, and `path` are
read-only, and `migrate` is the family's only mutation. `show` prints the
resolved layout — `--json` for the normalized object (prose-free, with any
problems carried inside the object), `--router` for the canonical root router
and nothing else. `path <role>` prints one repository-relative store path
(`design`, `plans`, `journal`, `qna`) and nothing else. `validate` runs the
layout validation rules and exits `0` for a valid, supported layout, `1` for
problems or a manifest this binary may not mutate, and `4` outside a repository
with `.agents/`. Nothing in this family creates a layout: `agents init` with
`--template`, `--stores`, or `--archive` creates a v2 one, and adopting an
existing v1 repository is `agents layout migrate`'s job, not init's.

`agents layout validate --json` emits one object:

| Field | Meaning |
|---|---|
| `manifest_path` | `.agents/layout.json`; omitted for the implicit v1 layout |
| `problems` | array, never null; each entry has a `code` and whichever of `path`/`detail` the rule supplies |
| `supported` | whether this binary may mutate the repository; false when the layout has problems (`invalid`, `unknown_schema`) or when the version gate refuses |
| `reason` | why not, when `supported` is false: `invalid`, `unknown_schema`, `below_floor`, `unreleased`, or `migrating` |
| `schema` | `agents.layout/v1` or `agents.layout/v2` |
| `layout_status` | `active` or `migrating` |

### Migration (`agents layout migrate`)

`agents layout migrate` adopts an existing v1 repository into an
`agents.layout/v2` manifest. The dry run is the default — `--dry-run` is an
accepted explicit synonym — and writes nothing. `--apply` performs it and
requires `--backup-tag <name>`, an annotated tag created at HEAD before the
first write, so the rollback point exists even without a branch; a name that
already exists or is not a valid ref is malformed, as is `--backup-tag` with
`--resume` or `--abort`, where it cannot take effect. `--template` supplies the
target store map from a template's defaults and `--stores <role>=<path>`
overrides one role; with no template, `--stores` must name all four roles.

Planning refuses, naming every reason in one report, when the router is diverged
or missing, a source store is missing or a symlink, a target path already
exists, `docs/` holds anything but the four stores and the declared archive, or
this binary is below the target's `min_mut_ver_floor`. It also requires a clean
working tree, no merge, rebase, cherry-pick, revert, am, or bisect in progress,
and a branch that is not `master` or `main`.

A `migrating` manifest is never re-planned: `--resume --apply` continues the
journal it froze, and `--abort --apply` deletes a journal this migration created
while nothing can have moved (phase `planned`, every move `pending`), leaving the
backup tag as the record. Every other invocation against a `migrating` manifest
refuses and names the phase and the remedy.

| Invocation | Exit |
|---|---|
| dry run, or no apply flag: the plan is printed and nothing is written | `1` |
| `--apply` applied, no blockers and no link candidates | `0` |
| `--apply` applied, but markdown links still point into the moved stores | `1` |
| `--apply`: plan blockers, or `docs_residue` appeared after the moves | `1` |
| `--apply`: missing `--backup-tag`, conflicting flags, or malformed input | `3` |
| `--apply`: a move failed mid-way; the manifest stays `migrating` | `5` |
| `--abort --apply`: manifest deleted, backup tag left | `0` |
| `--abort --apply` refused, or no `migrating` manifest | `1` |
| any: not inside a repository with `.agents/` | `4` |

`--resume --apply` follows the same rows as `--apply` with one exception: link
candidates are reported only by the run that planned them, so a resume that
completes a migration planned with candidates exits `0` where the fresh
`--apply` exited `1`. The candidates are a property of the pre-move source tree:
a fresh `--apply` scans the stores before moving them, and a resume completes a
move list whose source tree no longer exists and is never re-planned.

`agents layout migrate --json` emits exactly one object on every path. For a
plan it is `repo`, `dry_run`, `phase`, `from`, `to`, `router`, `archive`,
`moves`, `link_candidates`, `blockers`, and `counts`; `phase` is `planned` for a
dry run or a fresh `--apply`, and the journal phase a `--resume` continued from
— a resume reports the journal's own plan, never a re-planned one. `dry_run` is
true whenever nothing was applied, including a plan refused for its blockers.
For a refusal that precedes a plan (the preconditions, the `migrating` routing,
a source that is not v1, malformed flags) it is a refusal object carrying
`repo`, `dry_run`, `phase`, and the `error` sentence the human surface prints,
so a machine consumer never has to skip prose.

---

## License

MIT
