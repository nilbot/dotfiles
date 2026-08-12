# `agents` — repo-tracked agent context, wired from dotfiles

**Date:** 2026-08-07
**Status:** implemented
**Scope:** spec 1 of 3 (see [Roadmap](#roadmap-specs-2-to-4) for what is deliberately deferred)

> **Amended 2026-08-12 by [spec 7](2026-08-12-spec-7-capture-and-review.md).**
> Five days of measured use found the tracked trace index to be a reference to
> tier-3 material filed in tier 2: 48% of records were unreachable on the machine
> that wrote them, and the curated store they were meant to feed held zero
> handoffs. Spec 7 amends **§1** (the tracked tier holds conclusions; everything
> upstream is machine-local — the trace index and cache move out of the tracked
> tree), **§3.6** and **§3.7** (the index is machine-local; `merge=union` on
> traces retires), **§4** (a new `agents handoff draft` verb writes to an
> untracked queue, `handoff write --draft` retires, and the auto-draft caller §4
> specified but never built now exists), and **§6** (adds
> `agents review` and `agents handoff draft`; demotes `agents save` to an escape
> hatch).
>
> **§2's scaffolded `CLAUDE.md` gains a capture paragraph**, which under spec 7's
> shipped phases *is* the capture mechanism — `doctor` checks for it the way it
> already checks for the doctor instruction.
>
> **§6's exit-code rule stands for now.** Spec 7 considered a blocking `Stop` gate
> that would have been a second deliberate exception alongside `agents guard`
> (guard blocks on *failure*; the gate would block on *success*). That gate was
> demoted to a contingency (spec 7 §3c) and is **not built**, so "`agents guard`
> is the sole deliberate exception" remains true. If §3c is ever built, this rule
> is what it amends.

---

## Origin

This design came out of reviewing a work workspace (referred to below as
**`agents_clone`**) that had solved a similar problem for a multi-project setup.
It is a single git repository whose only tracked content is a `.agents/`
directory plus a short `CLAUDE.md` — it tracks *agent context*, not code, and
sits as the parent directory of several cloned project repos.

Several of its ideas are adopted here; several are deliberately not. Where this
document says "`agents_clone` does X," it is describing prior art that informed a
decision, not a dependency. Nothing in this design requires access to it.

## Terminology

Defined once, used throughout.

| Term | Meaning |
|---|---|
| **harness** | A coding-agent runtime: Claude Code, Codex CLI, Antigravity. Each has its own config format, hook events, and on-disk layout. |
| **hook** | A command a harness runs at a lifecycle moment (session start, subagent finished, before a tool call). Configured per harness; the harness pipes a JSON payload on stdin. |
| **wiring** | The generated harness config that points hook events at this tool. Produced by `agents wire`. |
| **transcript** | A harness's own full log of a session or subagent, written under its private directory. Large, machine-local, never tracked. |
| **trace record** | One tracked JSON line saying *where* a transcript lives and what produced it. A pointer, never the content. |
| **pointer** | A tracked reference to something untracked and machine-local, carrying enough provenance to know whether it is reachable from here and who holds it if not. |
| **provenance** | The `machine` + `harness` fields that make a pointer meaningful on a different computer. |
| **lane** | A unit of work-in-progress that handoffs and traces are grouped by. Defaults to the git branch. |
| **fleet** | The set of repos on this machine that have been `agents init`-ed. |

---

## Problem

Agent work leaves its durable record in machine-bound harness directories —
`~/.claude/projects/…`, `~/.codex/sessions/…`. Those directories do not travel to
another machine, are not shared between harnesses, and vanish when the machine
does. The knowledge a repo accumulates — why a thing is built this way, what a
subagent actually ran, where a half-finished ticket stands — ends up somewhere the
repo cannot see.

Separately, the same hook gets hand-maintained in several places. Today a single
"prefer codebase-memory-mcp" reminder exists in three formats across
`~/.claude/settings.json` (plus a script under `~/.claude/hooks/`),
`~/.gemini/settings.json`, and `~/.gemini/antigravity-cli/settings.json` — already
drifted in wording, matcher, and coverage, and none of them tracked in dotfiles.

## Goal

One command populates and maintains a tracked `.agents/` directory in any repo,
new or existing. The repo becomes the durable home for agent records; dotfiles
stays the single source for the machinery that produces them.

## Non-goals

- **Retro-fitting history.** Records that were never captured are not
  reconstructed. Synthesising them would misrepresent coverage.
- **Antigravity (`agy`) and Gemini CLI support.** Out of scope — `agy` 1.1.0 does
  not read workspace-local hook config (see [Measured facts](#measured-facts-2026-08-07)).
- Everything listed under [Roadmap](#roadmap-specs-2-to-4).

---

## 1. The placement rule

Every later decision derives from this. Anything that does not clearly fit one
tier is a design smell.

| Tier | Location | Holds | Travels? |
|---|---|---|---|
| Global | `~/.claude/`, `~/.codex/` ← dotfiles | **How I work.** Skills, style, commit rules, model prefs | with dotfiles |
| Per-repo | `.agents/` (tracked) | **What this codebase is.** Memory, traces, specs, plans, handoffs | with the repo |
| Machine-bound | harness dirs | **Raw material.** Never authoritative | never |

The third tier is not ignored — it is *pointed at*. Concretely, there are exactly
two kinds of pointer in this design, and both are defined below:

1. **Trace record → transcript** (§3). A tracked JSONL line carrying `machine`,
   `harness`, and the absolute transcript path.
2. **Memory entry → machine-bound source** (§3.4). A `sources:` list in a memory
   file's frontmatter, naming material that lives outside the repo.

Following a pointer has three defined operations:

| Operation | Command | Behaviour |
|---|---|---|
| **Check** | `agents doctor` | Report which pointers are unreachable from this machine, which are gone but cached, and which machine holds the rest |
| **Materialize** | `agents trace cache`, and the `subagent-stop` hook | Copy reachable transcripts into the cache in the git **common** directory |
| **Read** | `agents trace show <id>` | Resolve one record to content — the harness's copy if it is still there, otherwise ours |
| **Distil** | `agents distill` (**spec 3**) | On the machine that owns the material, read it and draft curated memory entries |

Materialize was originally a `.trace-cache/` inside the working tree, kept out of
commits by an ignore rule. It is now `<git-common-dir>/agents/trace-cache`: one
cache per repository rather than one per worktree, outliving any of them, and
unstageable structurally because git does not track its own directory. It is also
no longer only manual — the `subagent-stop` hook caches the child transcript as it
records the pointer, because subagent transcripts are deleted part-way through the
session that made them, on a schedule nothing can anticipate.

Spec 1 delivers Check, Materialize and Read. Distil is deferred, but the pointer format
that makes it possible is specified here, because records written without
provenance can never be repaired retroactively.

## 2. Layout

```
<repo>/
├── CLAUDE.md                    thin trigger — points at .agents/, carries the wiring check
├── AGENTS.md                    symlink → CLAUDE.md
├── .agents/
│   ├── memory/                  curated only; frontmatter + [[wikilinks]]
│   │   └── INDEX.md             GENERATED from frontmatter — never hand-edited
│   ├── reports/
│   │   ├── handoff/
│   │   │   ├── INDEX.md         GENERATED — lane, last touched, session, reviewed|draft
│   │   │   └── <lane>/<date>-<session>.md
│   │   ├── specs/ plans/ analysis/
│   │   └── traces/YYYY-MM-DD.jsonl    append-only pointer index
│   └── skills/                  repo-specific procedures, SKILL.md shape
├── .claude/settings.json        GENERATED, git-ignored
├── .claude/skills → ../.agents/skills      symlink, git-ignored
├── .codex/hooks.json            GENERATED, git-ignored
└── .codex/skills → ../.agents/skills       symlink, git-ignored
```

**Why `CLAUDE.md` stays short.** It is the only file every harness loads
automatically, so it costs context in *every* session — including ones that never
touch `.agents/`. It is the trigger, not the payload.

**Why `AGENTS.md` is a symlink, not a copy.** `agents_clone` documented a symlink
and shipped two byte-identical files, which can silently diverge. One source, two
names.

**Why the harness configs are generated and git-ignored.** They embed absolute
paths that do not survive a change of machine or `$HOME`, and Claude Code's
`settings.json` holds unrelated settings (theme, plugins, effort level) that the
generator must merge into rather than own.

**Why `.agents/skills/` is symlinked into both harnesses.** Neither harness
discovers `.agents/skills/` on its own: Claude Code reads `.claude/skills/`, Codex
reads `.codex/skills/`. `agents wire` creates both as symlinks to
`../.agents/skills`, so a repo-specific procedure is written once and loaded by
both. Same move as the `AGENTS.md` symlink, and it mirrors the existing
`~/.claude/skills` → `~/dotfiles/claude/skills` link already used at the global tier.

## 3. The record

### 3.1 Schema

One JSON object per line in `.agents/reports/traces/YYYY-MM-DD.jsonl`:

```json
{"when":"2026-08-07T15:41:14Z","harness":"codex","machine":"m1-mbp",
 "event":"subagent_stop","lane":"sq-123-payments","cwd":"payments/api",
 "session_id":"019fdcab-9733-…","turn_id":"019fdcab-ad07-…",
 "agent_id":"019fdcab-ac94-…","agent_type":"default","description":"",
 "transcript":"/Users/nilbot/.codex/sessions/2026/08/07/rollout-….jsonl",
 "pointer_verified":true}
```

`turn_id` is the record's name for the per-turn identifier under either harness:
Codex sends `turn_id`, Claude Code sends `prompt_id` and has no `turn_id` at all
(see [Measured facts](#measured-facts-2026-08-07)). The decoder accepts both and
prefers `turn_id`. It is omitted when neither is present, as on `SessionStart`.

### 3.2 What is never written

`last_assistant_message`, `tool_input`, `tool_response`. All three appear in the
payloads and can quote command output containing credentials. Only pointer and
label fields are ever serialized.

This is enforced *structurally*: the record type has no field capable of carrying
them, and the payload decoder names only the fields it wants, so unknown fields
are discarded before they reach any writer. A test asserts this rather than
grepping output.

### 3.3 Why provenance is mandatory

Both harnesses write `$HOME`-relative paths. `/Users/nilbot/.codex/sessions/…`
looks identical on every machine you own. A record lacking `machine` is therefore
not merely incomplete elsewhere — it is **misleading**, resolving either to nothing
or to a *different* session that happens to occupy the same path.

This cannot be added later: records written without it are unrepairable, because
nothing else in the line identifies where it came from. Hence v1.

### 3.4 Memory provenance

A curated memory entry may legitimately depend on material that does not travel.
Its frontmatter carries an optional `sources:` list:

```yaml
---
name: payments-retry-semantics
description: Why the retry window is 90s and not the documented 30s
metadata:
  type: reference
sources:
  - kind: transcript
    machine: m1-mbp
    ref: 019fdcab-ac94-7502-a322-d01f047c274a   # agent_id; resolve via the trace index
  - kind: harness-memory
    machine: m1-mbp
    harness: codex
    note: "full derivation in Codex auto-memory; distil before relying on the numbers"
---
```

`agents doctor` reports entries whose sources are unreachable from here.
This list is what `agents distill` (spec 3) consumes.

The discipline that goes with it, inherited from `agents_clone`'s handling of
git-ignored reference clones: **a memory entry must never depend on a source being
present in order to be correct.** State the takeaway in the entry itself; the
source is corroboration and a route to more detail, not the carrier of the fact.

### 3.5 Pointer resolution: derive and verify

Do **not** hardcode a per-event field map. The measured invariant is stronger and
holds across both harnesses:

> The child's transcript filename always embeds `agent_id`.

- Codex — `agent_id=019fdcab-ac94…` → `rollout-2026-08-07T15-41-06-019fdcab-ac94-….jsonl`
- Claude Code — `agent_id=a4e4a1bc424b2047f` → `…/subagents/agent-a4e4a1bc424b2047f.jsonl`

The adapter collects the candidate paths from the payload, selects the one whose
basename contains `agent_id`, and sets `pointer_verified: true`. If none match, it
records the best candidate with `pointer_verified: false` rather than dropping the
record.

This matters because Codex's own per-event semantics are already inconsistent (see
[Measured facts](#measured-facts-2026-08-07)) and it is a fast-moving target. A
lookup table encodes today's inconsistency; deriving and verifying survives it.

### 3.6 Retrieval

`lane` and `cwd` exist to make retrieval mechanical. `cwd` is repo-relative and,
in a multi-module repo, identifies the module. `lane` is the same key handoffs use
(§4), so *"what was I doing on this ticket"* answers across both stores at once.

```bash
agents trace ls --lane sq-123-payments --module payments/api --since 3d --machine m1-mbp
```

Daily JSONL files are **storage**; the CLI is the index. A derived index file would
drift out of sync with storage; a query command cannot.

**Honest limit:** mechanical filters (`lane`, `cwd`, `machine`, `harness`, time)
collapse the candidate set, but choosing among survivors is semantic and falls back
to free-text matching on `description` — which Codex does not even populate. No
mechanical solution is claimed for relevance ranking.

### 3.7 Git handling

`.gitattributes`, written into every initialized repo:

```
.agents/reports/traces/*.jsonl merge=union
.agents/** linguist-generated=true
```

`merge=union` is required, not cosmetic: two branches appending on the same day
otherwise produce conflict markers that are not valid JSON, and a line-oriented
reader silently drops those lines. `linguist-generated` collapses `.agents/` in
GitHub diffs.

## 4. Handoffs are lane-scoped

A single rolling `handoff-latest.md` (what `agents_clone` does) assumes one person,
one thread, one machine. It breaks with concurrent agents, and with a repo of
modules where several tickets are in flight under no common story.

**Lane resolution**, in precedence order: explicit `--lane` › git branch
(slugified) › worktree name › `default`. Branch is the strongest default because it
already exists, already tracks the ticket, and requires no explanation from the user.

**One file per (lane, session)**: `handoff/<lane>/<date>-<session>.md`. Two agents
on the same branch never touch the same file, so they cannot clobber each other and
git merges cleanly — a property no single-file scheme has, since markdown cannot
`merge=union`. `agents handoff prune --keep N` bounds growth; the generated
`INDEX.md` keeps it navigable.

**Provenance, two levels.** A handoff written deliberately via the skill is marked
`reviewed`. One auto-drafted at `Stop`/`SessionEnd` is marked `draft`, with its
timestamp and session id. The SessionStart reader is told to weigh them
differently. An unreviewed file that reads as authoritative is the same failure
mode as a drifted context document; this reduces it, it does not eliminate it.

**SessionStart reports lanes relevant to the current branch and cwd first**, then
others by age — not "a handoff exists".

### 4.1 Lane health

Branch-as-lane degrades when one branch absorbs unrelated work — at which point
both handoff scoping and trace retrieval lose their edge, silently. Since the data
needed to notice is already in the records, `agents doctor` reports it:

A lane is flagged when, within a rolling window, it accumulates traces spanning
more than *N* distinct top-level `cwd` values, or more than *M* days, or more than
*K* sessions. Thresholds are configurable with defensible defaults, tuned once
there is real data.

**Advisory only. It never creates or switches branches.** Automating git surgery
on the basis of a heuristic is a much worse failure than a stale lane. The report
names the distinct modules seen, so splitting is a small manual step if wanted.

## 5. Harness adapters

A Go interface per harness declaring: config path, emit format, event-name mapping,
payload field mapping, and capability set. Two implementations: Claude Code and Codex.

**Harness identity is passed explicitly** (`agents hook subagent-stop --harness
codex`), never inferred from the environment. This is measured, not defensive:
Codex sets no environment variables of its own for hooks, while a Codex hook
launched from a Claude Code session inherits ~14 `CLAUDE_CODE_*` / `ANTHROPIC_*`
variables. Env detection fails in both directions — false positives from
inheritance, and no true positive available.

**Capability differences are declared, not assumed.** `description` is the clearest
case: Claude Code supplies it via the `agent-<id>.meta.json` sidecar written at
spawn time; Codex's payload has no equivalent. Codex records therefore carry an
empty description, and retrieval for them leans harder on `lane` / `cwd` /
`agent_type`. The adapter declares the gap rather than the record format pretending
both harnesses are equal.

## 6. Binary surface

```
agents init [--local]                 create .agents/, triggers, wiring, fleet entry
agents wire                           regenerate harness configs (merge, never overwrite)
agents doctor                         wiring live? trusted? what is unreachable? lane health?
agents index                          regenerate memory/INDEX.md and handoff/INDEX.md
agents save                           scoped commit of .agents/ paths only
agents handoff [write|prune]          lane-scoped handoff management
agents trace ls|show|cache            query records; read one back; copy reachable ones
agents trace cache prune --lane <n>   remove that lane's cached copies (dry run without --yes)
agents ls | update [--all]            fleet (update is --dry-run by default)
agents hook <event> --harness <name>  harness hook entrypoints
agents githook                        git hook entrypoint (multicall, see §8)
agents guard --staged                 pre-commit: secret scan, generated-file check, mixed-commit warning
```

`agents init --local` produces a git-ignored `.agents/` for repos where committing
agent artifacts is not acceptable. Same layout either way.

**Shared exit codes**, identical across every subcommand. This is the main
practical reason the tool is one Go binary rather than a pile of scripts:

| Code | Meaning |
|---|---|
| 0 | ok |
| 1 | advisory finding |
| 2 | block — the only code that stops work |
| 3 | malformed input |
| 4 | not applicable / skip |
| 5 | could not record |

**Fail-open vs fail-closed is declared per hook, in code, with the reason.**
Recording hooks exit 0 on every path: a failed record must never disrupt a
dispatch. `agents guard` is the sole deliberate exception.

## 7. Guards, layered by authority

| Layer | Runs on | Authority |
|---|---|---|
| `pre-commit` → `agents guard --staged` | every commit, any actor | **authoritative** |
| `PreToolUse` (narrow matcher) | that harness's tool calls | advisory only |
| `commit-msg` → footer strip | every commit | mechanical |

The secret gate belongs at pre-commit because **commit is where "tracked" is
actually decided**. A `PreToolUse` guard sees only that harness's own tool calls —
it misses subagents running under another harness, the hooks' own writes, and
anything done by hand. It is early warning and must never be described as the
guarantee.

`guard --staged` performs three checks:

1. **Secret scan** of staged `.agents/` content. Blocks (exit 2).
2. **Generated-file check** — regenerate `INDEX.md` files in memory and compare
   byte-for-byte against what is staged. Blocks if they differ. See
   [Drift](#drift-generate-or-guard) for why this exists and why it is not the
   drift guard that was rejected.
3. **Mixed-commit warning** — a commit touching both `.agents/` and code paths.
   Advisory (exit 1). This is the mechanism behind `agents save`, so the habit is
   not required for correctness.

## 8. Git hooks become Go-backed

### 8.1 What exists today, and what is wrong with it

The current dotfiles setup independently invented the same hybrid split this design
uses: `git/templates/hooks/*` are installed per-repo by `init.templatedir` and glob
`~/dotfiles/git/hooks/*.<hook-type>`, so each repo gets a stable shim while the real
logic stays in dotfiles and stays current. The pattern is sound. Three things are wrong:

- **Two divergent dispatchers.** `commit-msg` is a later rewrite with `nullglob`,
  `"$@"` argument passing, and file/executable checks. `run-hooks.sh` — used by the
  `pre-commit`, `post-merge`, `post-checkout` symlinks — is the 2021 original with
  none of them: it passes no arguments (so `recent.post-checkout` loses git's three
  positional args), references `$COLOR_*` variables that are never defined, and
  without `nullglob` will hard-fail the moment a dispatcher name has no matching hook.
- **`init.templatedir` only fires at `git init` / `git clone`.** Every repo that
  already existed never got the hooks. `claude/update-repo-hooks.sh` exists purely
  to backfill them by hand.
- **Dead code:** `claude/commit-msg` (GNU `sed -i`, broken on macOS, referenced by
  nothing), `claude/check-commits.sh` (points at `~/.git-templates/`, a path that has
  never existed), `claude/setup-protection.sh` (re-sets a value `gitconfig.symlink`
  already sets).

### 8.2 The replacement

**Global `core.hooksPath`, pointed at a directory of symlinks to the Go binary.**

```
~/dotfiles/git/hooks.d/
├── pre-commit    → agents-githook   (symlink)
├── commit-msg    → agents-githook
├── post-merge    → agents-githook
└── post-checkout → agents-githook
```

with `core.hooksPath = ~/dotfiles/git/hooks.d` in the global gitconfig. The binary
dispatches on `basename(os.Args[0])` — the busybox multicall pattern.

**This eliminates per-repo installation entirely.** Every repo on the machine, new
or pre-existing, gets the hooks with no `git init` and no backfill script. The
problem `update-repo-hooks.sh` was written to solve stops existing, and
`init.templatedir` can be retired.

**Verified, not assumed** (2026-08-07): a dispatcher symlinked under three hook
names fired correctly as each, and received git's arguments
(`commit-msg` got `.git/COMMIT_EDITMSG`).

### 8.3 The catch, and the mitigation

`core.hooksPath` **shadows** `.git/hooks/`. Measured: a repo with its own
`.git/hooks/pre-commit` ran it on a normal commit and **silently skipped it** when
`core.hooksPath` was set. Left unhandled this would break every repo with its own
hooks.

So the dispatcher chains, in order:

1. The repo's own `.git/hooks/<name>`, if present and executable.
2. Any `~/dotfiles/git/hooks/*.<name>` extras — preserving today's
   extension-glob convention so personal hooks stay easy to add.
3. Built-in behaviour (`agents guard --staged` on pre-commit, footer strip on
   commit-msg).

Any non-zero exit stops the chain and propagates. Arguments are forwarded verbatim
to every stage — the bug `run-hooks.sh` has today.

**Known limitation:** a repo that sets `core.hooksPath` *locally* (husky and
similar) overrides the global value, and these hooks do not run there at all. Local
config beating global is correct git behaviour and should not be fought.
`agents doctor` detects and reports it.

### 8.4 Also in scope

**Still to do:**

- Add `git/gitattributes` and link it to `~/.gitattributes`.
  `core.attributesfile = ~/.gitattributes` currently points at a file that does not
  exist, and this design depends on gitattributes semantics.

**Already done (2026-08-07, commit `37f00a0`)** — recorded so it is not attempted
twice. Both were the same defect: a `$HOME` path the system *writes to* was aimed at
content this repo *publishes*.

- `~/.gitconfig` was a symlink to `git/gitconfig.symlink`, so every
  `git config --global …` wrote into tracked public content — which is how identity
  (`user.name`, `user.email`) and 1Password's `gpg.format = ssh` came to be
  committed there. It is now a machine-local regular file that only `[include]`s the
  shared config; global writes land after the include, so they stay local *and*
  correctly override. Identity now lives solely in `~/etc/extras.secret/gitconfig`.
- `ln -sfn $(CURDIR)/claude $(HOME)/.claude` was stale: `~/.claude` is a
  harness-owned directory (`plugins/`, `projects/`, `sessions/`, `settings.json`)
  and only `skills/` comes from dotfiles, so the link created a stray
  `~/.claude/claude` *inside* it. Now only `claude/skills` is linked.

The second one is worth carrying as a general caution for this design: **`~/.claude`
and `~/.codex` are harness-owned and must never be symlinked wholesale from
dotfiles.** Link individual subdirectories only. §2's per-repo wiring follows the
same rule.

## 9. Bootstrap and trust

**This section originally claimed no harness lets a freshly cloned repo's hooks fire
unattended. Measurement says half of that is wrong — and the surviving half is the
load-bearing one.** Both corrections are recorded under
[Measured facts](#measured-facts-2026-08-07):

- *Project* trust is **not** a gate for non-interactive mode. `claude -p` fired
  project-local hooks on the very first run in a fresh repo with no trust prompt at
  all, and `codex exec` silently writes `trust_level = "trusted"` for its own workspace.
- *Hook* trust **is** a gate for Codex, and a hard, silent one. This was confirmed by
  the positive control on 2026-08-07 (see
  [the hook-trust control](#codex-hook-trust-confirmed-by-positive-control-2026-08-07)):
  untrusted, no hook dispatches and nothing is printed at any log level; after
  accepting the TUI's hook-review prompt, the same wiring fires and writes records.
  `codex exec` cannot create that trust — only the interactive TUI's hook-review
  flow can.
- `agy` gates on a `trustedWorkspaces` list, untested in practice because 1.1.0 reads no
  workspace-local hooks at all.

So the manual step is real for Codex and absent for Claude Code in non-interactive mode
(`claude -p`); interactive project trust was not retested. The wiring must not assume either way.

**Trust is a human gate by design, and this tool does not defeat it.** Codex ships
`--dangerously-bypass-hook-trust`; wiring it in is an explicit non-goal. So yes —
the final step is manual. The design's job is to make it *one obvious step* rather
than a silent failure. Concretely:

| Actor | Action |
|---|---|
| `agents init` | After writing wiring, prints the exact remaining trust steps for each harness it wired, and exits 1 (advisory) so the state is visible rather than assumed |
| `agents doctor` | Re-checks and prints the remediation for anything unmet — the command to run, or the UI step (`/hooks` in Codex) with what to look for |
| `CLAUDE.md` | Instructs the agent to run `agents doctor` early and surface the result, because a hook cannot install itself and a missing hook fails silently |
| you | Perform the trust step once per repo per harness |

`agents doctor` answers four questions:

1. Is the generated wiring present?
2. Is the `agents` binary present and on `PATH`?
3. Is the project trusted by this harness? (Note: not a gate for either harness's non-interactive mode.)
4. Is the hook's current hash trusted? (Codex re-flags a hook after any edit, so
   this recurs whenever the wiring changes — confirmed by control, see below.)

**Q4 is only partly answerable by reading `~/.codex/config.toml`.** It gains a
`trusted_hash` entry once the TUI's hook-review prompt is accepted; before any grant
on a machine, the key is absent entirely, which is why an earlier search found no
store. Each position may also carry an `enabled` boolean, so `doctor` can report an
explicitly disabled current hook. Codex does not persist whether `/hooks` itself was
opened or reviewed, and `doctor` cannot independently reproduce Codex's private hash
comparison. Complete current trust entries with no explicit disablement are therefore
reported as `ok` with that limitation in the detail; absent, incomplete, or explicitly
disabled entries remain warnings. Granting trust and changing activation are still
TUI-only, so `doctor` observes but never establishes either state.

Q3 is worth keeping only as a diagnostic, not as a gate: project trust gates neither
harness in non-interactive mode.

## 10. Fleet registry

**Presence of `.agents/` in a repo is the truth.** The registry is a *cache* of
known repo paths so `agents update --all` and cross-repo trace search work without
scanning the disk.

**The registry is machine-local and is never tracked**, at
`~/.local/state/agents/registry.json` (XDG state). It lives outside dotfiles entirely.

This is a hard requirement, not a preference: **the dotfiles repo is public on
GitHub.** A registry enumerating every repo path on the machine would publish
project names, client or employer names, and directory structure. It is also
genuinely machine-specific — the same dotfiles clone on two machines should have
two different registries — so tracking it would be wrong even if the repo were
private.

`agents ls` reconciles cache against reality and reports drift in both directions:
registered-but-missing, and present-but-unregistered. Drift here is a **normal
state to report, not an error to block** — a repo can be moved, archived, or
deleted at any time, and none of those are mistakes.

## 11. Testing

- Per-adapter golden-file tests: given a payload, assert the emitted config and the
  emitted record byte for byte.
- A **redaction test** asserting forbidden fields cannot serialize — structural,
  not a string search on output.
- Pointer-resolution tests, including the `pointer_verified: false` path.
- Index-generation tests proving generated output matches disk state.
- Git-hook chain tests: ordering, argument forwarding, non-zero propagation,
  repo-local hook still running under `core.hooksPath`.
- Guard tests: secret detection, generated-file mismatch, mixed-commit detection,
  exit codes.
- **Fixtures are the real payloads captured on 2026-08-07** (see
  [Measured facts](#measured-facts-2026-08-07)), not hand-written guesses.

---

## Drift: generate, or guard?

Worth stating explicitly, because the answer differs by case and the reasoning is
not obvious.

`agents_clone` runs a Go + CommonMark-parser check that blocks `git commit` when
its context document's hand-written directory tree and memory index drift from
what is on disk. That guard exists because the same list is maintained by hand in
three places.

**Generation removes the need for that particular guard** — `INDEX.md` derived from
frontmatter cannot disagree with its source. But generation only helps *if it
actually runs*. A committed `INDEX.md` that nobody regenerated is exactly the
original problem with extra steps.

So the design uses both, at different layers:

- **Correct by construction:** every command that writes memory or a handoff
  regenerates the relevant `INDEX.md` in the same operation. The normal path never
  produces drift.
- **Backstop:** `agents guard --staged` regenerates and compares byte-for-byte,
  blocking on mismatch. This catches hand-edits and out-of-band writes.

The distinction from the rejected guard is the *nature of the check*. Comparing
regenerated output byte-for-byte is exact and trivial. Parsing prose to infer
whether a hand-written description still matches reality is neither — that check
was unreliable, and an unreliable guard protecting a file whose whole value is
trustworthiness is worse than no guard.

**The fleet registry deliberately gets no such guard.** A generated index and a
cache are different things: the index has one correct value derivable from tracked
source, so a mismatch is a defect. The cache describes a mutable world it does not
control, so a mismatch is news, not a bug. `agents ls` reports; nothing blocks.

---

## Roadmap: specs 2 to 5

Recorded here so the sequencing survives a fresh session with no conversation
history. Numbers are stable catalog identifiers, not implementation priority;
dependencies and readiness decide what starts next. Each graduates to a full design
and implementation plan when started.

### Spec 2 — dotfiles hygiene

Independent of the agents work; no shared code. Deferred only to keep spec 1
reviewable.

- Remove `zsh/` (1.6 MB, 335 files, mostly vendored oh-my-zsh themes and plugins).
  The login shell is already fish. Nothing in the agents design touches zsh.
- Rationalize `softlinks.sh` and the `Makefile` link targets.
- Remove the dead `claude/` scripts left over after §8.
- Decide the fate of `git/hooks/go.pre-commit`, which currently runs
  `go build -n && go test && go fmt && go vet` on every commit in any repo with
  `.go` files at the root. Under §8's chain it keeps working; whether it *should*
  is a separate question.

**Depends on spec 1** only in that §8 must land first, or the hook cleanup has to
be done twice.

### Spec 3 — `agents distill`

Turns machine-bound material into curated, tracked knowledge — the second half of
the pointer story in §1.

- Per-harness readers: Codex's `~/.codex/memories/` (a git repo containing a large
  `MEMORY.md`, `raw_memories.md`, and rollout summaries) and Claude Code's
  auto-memory directory.
- A transcript reader that mines the files the trace index points at. The known
  hazard, from `agents_clone`: never dedupe by first match — that hides a first
  attempt that failed and a later corrected retry. Reconstruct chronologically
  with results, then classify.
- An interactive draft-and-review flow producing `.agents/memory/` entries with
  correct `sources:` frontmatter.
- Runs **on the machine that owns the material** — that is the whole point.

**Depends on spec 1** for the pointer format and the `sources:` schema, which is
why both are specified now even though nothing consumes them yet.

### Spec 4 (candidate) — the wiring DSL

Deferred, not rejected. A declarative wiring source compiled per harness was the
right instinct; it is not yet worth its cost against two targets whose schemas are
~90% identical.

**Build it when any of these becomes true:**

1. A third target lands (a new harness, or `agy` gaining workspace-local hooks).
2. Per-repo wiring divergence appears — a repo needing hooks the others should not have.
3. Capability requirements need to be declarative, e.g. *"this hook needs
   subagent-transcript-pointer; skip it on harnesses that lack the capability"* —
   at which point wiring stops being configuration and becomes a small language
   with a type system.

There is also a reason to build it that is not cost-justified by today's output,
and is worth naming rather than losing: **a DSL forces the semantic event to be
named separately from each vendor's encoding.** "A subagent finished" is not
`SubagentStop`, nor `PostToolUse` matching `invoke_subagent` — those are two
spellings of one idea. Writing the abstraction down is how the differences between
vendors become visible as data rather than as scattered adapter code, and that
comparison is where insight about the vendors themselves would come from. That is
a research payoff, not an engineering one, and it should be undertaken knowingly
rather than smuggled in as premature abstraction.

### Specs 5 and 6 — verification, then releases

The implemented local loop builds the current checkout directly to
`~/bin/agents`; it provides neither pull-request verification nor a way to
install and upgrade a published binary.

These were one scope note until 2026-08-11, when they were split because only the
first needed no open decisions. **[Spec 5](2026-08-11-spec-5-verification-gate.md)
is the verification gate** — the first automated build, test, `vet` and `gofmt`
gate this repository has ever run, plus the invariants it enforces: generated
indexes, the exit-code table, help coverage, and documentation derived from a
command registry. It also owns the doctor check that the stamped checkout root
still exists (§8), which is a defect in this spec's surface.

**[Spec 6](2026-08-11-spec-6-releases-and-distribution.md) is releases and
distribution** — platform support, versioning, checksums and provenance, artifact
or bottle production, safe activation, and how an installed binary reports drift
from the checked-out source. Its hardest open problem belongs to this spec's
surface too: a binary built by CI belongs to no checkout, so it has no root to
stamp.

---

## Measured facts (2026-08-07)

Everything below was observed on this machine, not read from documentation.
Documentation was wrong or stale in three of these cases.

### Codex CLI 0.147.0

- `codex features list` → `hooks  stable  true`. Enabled by default, no flag needed.
  Third-party guides claiming `codex_hooks = true` is required are stale.
- Project-local `<repo>/.codex/hooks.json` **loads and fires** — confirmed by a live
  run, not inferred from documentation. The capture run cleared the hook-trust gate
  (inference — see the 2026-08-07 re-check below);
  a later re-check without any bypass flag found the same wiring inert. See
  [the live-session re-check](#codex-cli-01470--live-session-re-check-task-10) below.
- Events observed firing: `SessionStart`, `PreToolUse`, `SubagentStart`,
  `SubagentStop`, `Stop`. The binary also implements `PostToolUse`,
  `PermissionRequest`, `PreCompact`, `PostCompact`, `SessionEnd`, `UserPromptSubmit`.
- `SubagentStop` payload keys: `session_id`, `turn_id`, `agent_id`, `agent_type`,
  `transcript_path`, `agent_transcript_path`, `cwd`, `model`, `permission_mode`,
  `stop_hook_active`, `hook_event_name`, `last_assistant_message`.
- **Per-event asymmetry.** At `SubagentStart`, `transcript_path` is the *child's*
  and `agent_transcript_path` is **absent**. At `SubagentStop`, `transcript_path` is
  the *parent's* and `agent_transcript_path` is the child's. Verified across two
  independent subagents in one session. This is why §3.5 derives rather than hardcodes.
- `agent_id` pairs Start↔Stop. Each subagent gets its own `turn_id`, distinct from
  the parent's.
- No `description` field in any payload.
- Transcripts: `~/.codex/sessions/YYYY/MM/DD/rollout-<ISO>-<uuid>.jsonl` —
  date-partitioned, unlike Claude Code's per-project/per-session directories.
- Codex sets **no** `CODEX_*` / `HOOK_*` / `AGENT_*` environment variables for hooks.
- Hook trust is hash-based. `/hooks` reviews and trusts;
  `--dangerously-bypass-hook-trust` overrides for a single invocation.
- `notify` in `config.toml` is the **legacy** path, superseded by the `Stop` hook.
- `~/.codex/config.toml` is co-owned by the ChatGPT desktop app, which writes
  `[desktop]` and `[projects]` sections into it. Prefer the dedicated `hooks.json`
  over an inline `[hooks]` block there.

### Codex CLI 0.147.0 — live-session re-check (task 10)

Task 9 golden-tested the adapter against captured payloads. Task 10 asked a different
question: does a live `codex exec` session, in a fresh repo wired only by `agents init`,
actually write records? **It does not** — and the mechanism is worth pinning down,
because it is not the one §9 assumed.

Procedure: fresh `git init` repo at `/private/tmp/codexprobe2`, `agents init` (exit 1,
advisory; `.codex/hooks.json` written), then `codex exec 'Use a subagent to count the
files in this directory, then stop.'` with **no trust- or permission-bypass flag**.

- The session ran to completion (exit 0) and did delegate — stderr showed `collab: Wait`.
  `.agents/reports/traces/` stayed empty. No hook ran.
- **The directory gate does not apply to `codex exec`.** The repo was absent from
  `~/.codex/config.toml` before the run; afterwards the file had gained
  `[projects."/private/tmp/codexprobe2"]` with `trust_level = "trusted"`. No prompt was
  shown and stdin was `/dev/null`. `codex exec` self-trusts its workspace.
- **`.codex/hooks.json` is read regardless.** Replacing it with invalid JSON produced
  `warning: failed to parse hooks config /private/tmp/codexprobe2/.codex/hooks.json: key
  must be a string at line 1 column 3` on stderr. The file is loaded and parsed; the hook
  is dropped later.
- **The hook-trust gate is silent.** With valid wiring and `RUST_LOG=codex_hooks=trace`,
  not one `codex_hooks::engine::command_runner` span appeared. Nothing on stderr mentioned
  hooks, nothing prompted, exit was 0. A user gets no signal at all. `codex doctor` does
  not mention hooks either.
- **Not the binary's fault.** Piping a captured `SessionStart` payload (`cwd` rewritten to
  the probe repo) straight into `agents hook session-start --harness codex` wrote a correct
  record with `pointer_verified: true`. The hook works; Codex never calls it.
- **Nothing on this machine has persisted hook trust.** No `[hooks]` section in
  `~/.codex/config.toml`, no file under `~/.codex/` containing `trusted_hash` (search covered `*.toml`, `*.json`, `*.jsonl`), no matching
  table in `state_5.sqlite`. A control `codex exec` in the *original fixture-capture repo*
  — still present, still `trust_level = "trusted"`, whose hooks demonstrably fired earlier
  the same day — also produced **zero** new dumps.
- **Only the TUI can clear it.** The review UI is `tui/src/startup_hooks_review.rs`
  ("Hooks need review" / "Trust all and continue" / "Continue without trusting (hooks
  won't run)"). `codex exec` has no equivalent; its only escape is
  `--dangerously-bypass-hook-trust`, whose help text — run hooks "without requiring
  *persisted* hook trust" — is itself the confirmation that trust is persisted state that
  `exec` cannot create.
- *Inference, not measurement:* because the fixture-capture repo carries no persisted hook
  trust, the 2026-08-07 capture run must have used `--dangerously-bypass-hook-trust`. The
  capture command was not recorded, so this is reconstruction from surviving state.

**No payload divergence was observed** — nothing contradicted the payload keys, event
names, transcript paths, or the per-event `transcript_path` asymmetry recorded above. But
no live payloads were captured either, so that is *absence of contradiction*, not
confirmation. Re-capturing fixtures against 0.147.x is flagged, not done.

### Codex hook trust, confirmed by positive control (2026-08-07)

The re-check above established only the negative half — untrusted wiring does not fire —
and left the mechanism as inference, because an agent must not click through a security
consent prompt on a user's behalf. The repo owner supplied the positive half by hand.

Setup was `agents init` in a throwaway repo, then the TUI's hook-review prompt accepted
with "Trust all and continue" **before** any `codex exec` run.

- **Trusted wiring fires.** `codex exec` printed `hook: SessionStart` /
  `hook: SessionStart Completed` … `hook: Stop` / `hook: Stop Completed`, and wrote a
  trace file. Identical wiring, identical command, opposite result from the untrusted
  run — so persisted hook trust is the variable, observed rather than inferred.
- **Trust is per-hook and keyed by the command string.** Editing one hook's command
  (adding `--lane probe` to `session-start` only) and re-running left `Stop` firing while
  `SessionStart` went **silently** inert — no warning, no non-zero exit. Record tallies
  before/after confirm it: `session-start` stayed at 1 while `stop` incremented to 2.
  This is the behaviour `TrustSteps` promises users, now measured.
- **The store is `~/.codex/config.toml`.** After the grant, `trusted_hash` appears there.
  Before any grant on a machine the key is absent entirely, which is why the earlier
  search found nothing — the store is created on first trust, not shipped empty. So
  `agents doctor` **can** read hook-trust state; it just cannot create it.
- **`.codex/hooks.json` is read regardless of hook trust.** Replacing it with invalid
  JSON produced `warning: failed to parse hooks config <abs path>` even while hooks were
  inert — parsing and dispatch are separate stages.

Correction to the bullet above: "nothing on this machine has persisted hook trust" was
true when written and false immediately after the first TUI grant. The absence of a
readable store is a property of a machine that has never granted trust, not of Codex.

### Codex `/hooks` confirmed in the TUI (2026-08-07)

Task 9 could not find a `/hooks` literal in the binary and concluded the command was
"neither confirmed nor refuted", removing it from the printed guidance. **That
conclusion was wrong.** Driven under a PTY (`tmux new-session` → `send-keys` →
`capture-pane`) in an already-trusted directory, so no consent prompt was encountered:

- **`/hooks` exists**, described in Codex's own command list as *"view and manage
  lifecycle hooks"*. The strings analysis failed because slash commands are rendered
  from names stored without the prefix, and the bare `hooks` token sits in a serde
  struct-name region (`configRequirements/read.hooks`) rather than the command cluster.
  This is the same failure shape as the hook-trust inference: absence of evidence in
  one search read as evidence of absence.
- **The view is a ready-made diagnostic.** It tabulates every event with **Installed**
  and **Active** counts — precisely the distinction `agents doctor` needs, since
  installed-but-not-active is exactly the silent-failure state. All eleven events are
  listed, matching the event list recorded above: `PreToolUse`, `PermissionRequest`,
  `PostToolUse`, `PreCompact`, `PostCompact`, `SessionStart`, `SessionEnd`,
  `UserPromptSubmit`, `SubagentStart`, `SubagentStop`, `Stop`.
- **Per-hook hash trust is visible in the prompt.** Re-opening with wiring whose
  `session-start` command had been edited (and its three siblings untouched) showed
  *"Hooks need review — **1 hook** is new or changed"*, not four. The prompt offers
  "Review hooks" / "Trust all and continue" / "Continue without trusting (hooks won't
  run)".
- **The interactive TUI does gate on directory trust**, unlike `codex exec`. Opening in
  a fresh untrusted directory presented *"Do you trust the contents of this
  directory?"* before anything else. So `TrustSteps`' first step is accurate for the
  TUI path and wrong only for `exec` — both halves now observed.

Neither consent prompt was answered; both sessions were terminated instead, and
`~/.codex/config.toml` was verified byte-identical afterwards.

### Environment contamination

A Codex hook launched from a Claude Code session inherits `CLAUDE_CODE_ENTRYPOINT`,
`CLAUDE_CODE_SESSION_ID`, `CLAUDE_CODE_HOST_SESSION_ID`, `CLAUDE_PID`,
`ANTHROPIC_BASE_URL` and ~9 more, **even after six were explicitly unset**.

The remaining variables were not scrubbed exhaustively, deliberately: the
conclusion does not depend on it. Codex publishes no positive identity signal of
its own, so env-based harness detection has no true positive to find regardless of
how clean the inherited environment is. A fully scrubbed run would have produced a
tidier demonstration of a conclusion already established from the other direction.

One design consequence worth carrying: the binary should not pass its inherited
environment unfiltered to anything it spawns.

### Git hooks

- `core.hooksPath` + a multicall dispatcher symlinked under several hook names
  works; the binary can dispatch on `basename(argv[0])` and receives git's
  arguments correctly.
- `core.hooksPath` **shadows** `.git/hooks/`: a repo-local `pre-commit` ran on a
  normal commit and was silently skipped once `core.hooksPath` was set. Chaining
  (§8.3) is mandatory.

### Antigravity `agy` 1.1.0 — out of scope, recorded for re-test

- Does **not** read `<workspace>/.agents/hooks.json`. With the workspace present in
  `trustedWorkspaces`, its hook manager logged
  `loaded 0 named hooks from 0 hooks.json file(s)`.
- Binary strings indicate hooks load from plugin bundles
  (`plugins/<name>/hooks.json`) and a global config path.
- The published documentation describes `.agents/hooks.json` as a workspace
  customization directory. That is either IDE-only or newer than the 1.1.0 CLI.
- **Re-test trigger:** any `agy` upgrade.

### Claude Code 2.1.224

Measured in task 8 by wiring a dump script to `<repo>/.claude/settings.json` in a
throwaway repo and running one headless `claude -p` session that dispatched one
`Explore` subagent. No permission- or trust-bypass flag was used; the hooks fired
on the first run. Fixtures:
[`fixtures/2026-08-07-claude-code-hook-payloads/`](fixtures/2026-08-07-claude-code-hook-payloads/).

- Project-local `<repo>/.claude/settings.json` **loads and fires**, including for a
  non-interactive `claude -p` session in a repo with no prior trust record.
- Events observed firing: `SessionStart` (`source: startup`), `SubagentStart`,
  `SubagentStop`, `Stop`. **`SubagentStart` exists** — the plan had listed it
  without evidence, and the evidence now supports it.
- Full payload key sets, exactly as captured:
  - `SessionStart`: `cwd`, `hook_event_name`, `session_id`, `source`, `transcript_path`.
  - `SubagentStart`: `agent_id`, `agent_type`, `cwd`, `hook_event_name`, `prompt_id`,
    `session_id`, `transcript_path`.
  - `SubagentStop`: the `SubagentStart` set plus `agent_transcript_path`,
    `background_tasks`, `effort`, `last_assistant_message`, `permission_mode`,
    `session_crons`, `stop_hook_active`.
  - `Stop`: the `SubagentStop` set minus the three `agent_*` keys.
- **There is no `turn_id`.** Claude Code names the per-turn identifier `prompt_id`;
  `turn_id` is Codex's spelling only. The adapter test had been reconstructed with
  `turn_id` and was wrong. `harness.Payload` now decodes both names and prefers
  `turn_id`, so one record field is populated under either harness.
- **Per-event asymmetry, opposite to Codex's.** At `SubagentStart` there is no
  `agent_transcript_path` at all and `transcript_path` is the *parent's*; at
  `SubagentStop` `agent_transcript_path` is the child's and `transcript_path` is the
  parent's. Codex instead sends the *child's* path in `transcript_path` at
  `SubagentStart`. Consequently a Claude Code `subagent_start` record can only ever
  carry `pointer_verified: false` — the subagent's transcript does not exist under a
  name the payload discloses yet. This is expected, not a defect, and is a second
  independent reason §3.5 derives rather than hardcodes.
- `description` is **not** in any payload. It comes from the `agent-<id>.meta.json`
  sidecar the harness writes at spawn time, beside the transcript. Measured sidecar
  shape: `{"agentType","description","toolUseId","spawnDepth"}`. The adapter's
  sidecar read is confirmed as the only available source.
- `SessionStart` does **not** fire for subagents — confirmed: one session with one
  subagent produced exactly one `SessionStart`. Subagents inherit `CLAUDE.md` but
  do not act on it — 0 of 31 observed subagents followed an inherited bootstrap
  directive. **This is why recording must be a hook and never an instruction.**
- Transcripts: `~/.claude/projects/<slug>/<session-uuid>/subagents/agent-<id>.jsonl`,
  confirmed.

---

## Rejected alternatives

**A tracked per-repo binary.** `agents_clone` commits per-platform Go binaries into
the repo and needs reproducible-build flags plus a drift test to prove they still
match source. Under hybrid coupling the binary lives in dotfiles and is invoked by
path, so none of that apparatus is needed.

**A prose-parsing doc-drift guard.** See [Drift](#drift-generate-or-guard) — the
intention is kept, the mechanism replaced by generation plus an exact
regenerate-and-compare check.

**Redirecting harness auto-memory into `.agents/memory/`.** Technically possible via
`autoMemoryDirectory`, though that key is ignored from checked-in project settings
and would have to go in git-ignored `.claude/settings.local.json` per repo. Rejected
because it aims a firehose of unreviewed model output at a git-tracked directory,
making the secret guard load-bearing rather than defence-in-depth. `.agents/memory/`
stays curated; §1's third tier covers the gap by reporting what is unreachable, and
spec 3 closes it deliberately.

**Orphan branch or git notes for agent artifacts.** Zero diff pollution, but agents
could not simply read a file to recall prior context, which is most of the value.

**Keeping `init.templatedir` for hooks.** Superseded by `core.hooksPath` (§8.2),
which covers pre-existing repos for free.

---

## Risks

| Risk | Mitigation |
|---|---|
| Auto-drafted handoffs go stale and read as authoritative | `draft` vs `reviewed` provenance; reader weighs by age. Reduced, not eliminated. |
| `agents init` cannot make hooks live in Codex | `doctor` reports hook trust state with remediation (depends on trust store being readable); `CLAUDE.md` carries the check |
| `core.hooksPath` shadows repo-local hooks | Dispatcher chains to `.git/hooks/<name>` first; `doctor` reports local overrides |
| `update --all` touching many repos at once | `--dry-run` is the default |
| Codex contract churn | Every payload assumption is a golden-file test; `pointer_verified` degrades instead of breaking |
| `git add -A` sweeping trace records into a code commit | `guard` warns on mixed commits; `agents save` for the common path |
| Secrets reaching the tracked tree | Structural redaction in the record type; `guard --staged` at the commit boundary |
| Registry leaking repo paths into a public dotfiles repo | Registry lives in `~/.local/state/`, never in dotfiles (§10) |

## Open questions

- **Machine identity.** Hostname is the obvious key but changes. A stable id
  generated once into machine-local state is more durable but needs bootstrapping,
  and must not be the hostname if hostnames repeat across machines. Decide during
  implementation; the record field is `machine` either way.
- **Secret detection for `guard`.** ~~Open~~ **Settled 2026-08-08:** delegated to
  [gitleaks](https://github.com/gitleaks/gitleaks), invoked as a subprocess over each
  staged `.agents/` blob (`gitleaks stdin --redact`), not hand-rolled. A bespoke
  pattern list is a maintenance treadmill against an adversary that changes
  quarterly, and the tuning burden this question anticipated is itself the argument
  against owning one. Importing gitleaks as a Go library was rejected: 14 direct
  dependencies into a module that has one. trufflehog was rejected: it verifies
  candidates by calling the issuing service, so a pre-commit gate would send
  candidate credentials off-machine. One domain rule -- the Fernet-style
  `gAAAAAB…` task blob observed in real Codex payloads -- ships as `[extend]`
  configuration rather than code. Missing scanner blocks the commit, but only when
  `.agents/` content is actually staged.
- **Lane-health thresholds** (§4.1). Defaults need real data before they mean anything.
