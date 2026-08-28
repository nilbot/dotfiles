# Design: Antigravity Multi-Harness Onboarding and Dual-Layer Instruction Topology

**Date:** 2026-08-28  
**Status:** Designed, pending implementation  
**Applies to:** `agents` CLI, `dotfiles` harness subsystem, `autogo-mlx`, and collaborative repositories  
**Depends on:** [Spec 1](2026-08-07-agents-repo-context-design.md) (harness adapters, exit codes), [Spec 4](2026-08-07-spec-4-wiring-dsl.md) (wiring DSL, `named-groups` dialect, need vs capability), [Knowledge is Documentation](2026-08-19-knowledge-is-documentation.md)  
**Reads against:** [`docs/qna/why-didnt-antigravity-apply-my-rules.md`](../qna/why-didnt-antigravity-apply-my-rules.md), [`docs/qna/what-does-opening-a-repo-in-antigravity-run.md`](../qna/what-does-opening-a-repo-in-antigravity-run.md), [`docs/qna/how-does-antigravity-expose-subagents.md`](../qna/how-does-antigravity-expose-subagents.md)

---

## 1. Executive Summary & Problem Formulation

The `agents` CLI in `dotfiles` provides automated, reproducible configuration and diagnostics for AI coding harnesses across repositories. Previously, `agents` generated hook configurations for **Claude Code** (`.claude/settings.json`) and **Codex** (`.codex/hooks.json`), while treating Antigravity as unverified.

With `agy` 1.1.22 and the Antigravity Desktop App, the operational mechanics of Antigravity are empirically established:
1. **Dialect Divergence**: Antigravity keys `.agents/hooks.json` on a top-level hook name (`"agents"`). Tool-scoped events (`PreToolUse`, `PostToolUse`) take matcher groups with nested `hooks` arrays, while lifecycle events (`Stop`, `PreInvocation`, `PostInvocation`) take flat handler arrays directly.
2. **Skills Placement Convergence**: Antigravity's customization root is `.agents/`, natively auto-loading `.agents/skills/<skill-name>/SKILL.md`. Claude Code and Codex require explicit symlinks (`.claude/skills` and `.codex/skills`) pointing to `../.agents/skills`.
3. **Execution on Open**: Opening a repository in the Antigravity Desktop App automatically registers a project manifest and executes `.agents/hooks.json` via `sh -c` before any user prompt is typed. Consequently, `.agents/hooks.json` must be machine-local and excluded from git tracking.
4. **Dual-Layer Instructions**: Root `AGENTS.md` (symlinked by `CLAUDE.md`) serves as the fleet-wide meta-harness context (`docs/` index, `agents doctor` diagnostic directive), while `.agents/AGENTS.md` serves as the repository-specific domain rulebook (DeepMind docstrings, tensor shape annotations, LaTeX math, execution safety).

This specification formalizes the **Antigravity harness integration**, the **dual-layer instruction architecture**, and the **sandboxed verification protocol** required to onboard repositories without degrading existing domain workflows.

---

## 2. The Dual-Layer Instruction Architecture

### 2.1 Separation of Concerns

AI coding assistants operate across two distinct categories of context:
- **Meta-Harness Context**: How the repository organizes documentation (`docs/qna/`, `docs/design/`, `docs/journal/`), machine wiring rules, diagnostic instructions (`Run agents doctor early...`), and global capture policies.
- **Domain-Specific Guidelines**: Project-specific engineering standards (e.g. Google DeepMind commenting style, tensor shape annotations `# [B, H, W, C]`, math rationale, automated Q&A logging triggers, safety constraints).

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ 1. Root AGENTS.md (symlinked by CLAUDE.md)                                  │
│    Scope: Repository-Wide Meta-Harness Context                              │
│    Readers: Claude Code, Codex, Antigravity                                 │
│    Role: Governs documentation structure, diagnostic checks, wiring rules,   │
│          and delegates domain-specific guidelines to .agents/AGENTS.md.     │
└──────────────────────────────────────┬──────────────────────────────────────┘
                                       │ references / delegates domain rules
┌──────────────────────────────────────▼──────────────────────────────────────┐
│ 2. Customization Root .agents/AGENTS.md (or .agents/rules/*.md)             │
│    Scope: Domain Engineering & Workflow Rules                               │
│    Readers: Antigravity (native), Claude/Codex (by pointer in root)         │
│    Role: Specific code formatting, math rationale, Q&A capture triggers,     │
│          and safety constraints ([WIP] execution gates).                   │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Harness Ingestion Semantics & Asymmetry

The two layers reach harnesses through different delivery mechanisms:

| Layer | Antigravity | Claude Code / Codex |
|---|---|---|
| Root `AGENTS.md` | **Native** (loads repo root) | **Native** (`AGENTS.md` / `CLAUDE.md` symlink) |
| `.agents/AGENTS.md` | **Native** (loads customization root) | **By pointer** (referenced in root `AGENTS.md`) |

**Delivery Asymmetry Note**: Antigravity ingests both files natively into its context window. Claude Code and Codex ingest root `AGENTS.md` natively and receive a pointer directing them to `.agents/AGENTS.md`. In accordance with the 2026-08-19 redesign finding that instruction-delivered pointers carry compliance limits, this pointer is the available cross-harness bridge until those vendors support modular customization roots.

### 2.3 Canonical Preamble (`DefaultAgentsMD`)

When `agents init` initializes a repository lacking a root instruction file, it writes the canonical preamble:

```markdown
# Agent context

Durable context for this repo lives in `docs/`. Read it before assuming;
it is the record, and this file is only the pointer to it.

- `docs/qna/` — answers indexed by the question you would ask again
- `docs/journal/` — dated record of what happened
- `docs/design/` — the design still in force

## Repository Architecture & Guidelines
- Domain engineering guidelines, commenting standards, and safety constraints 
  are defined in `.agents/AGENTS.md`.
- Repo-specific procedures and skills are located in `.agents/skills/`.

## Machine Wiring
`.agents/` holds machine wiring and local skills. A hook cannot install itself 
and a missing hook fails silently, so an empty or stale `.agents/` means the setup 
is broken rather than that there is nothing to say — report it rather than 
working around it.

Run `agents doctor` early and report any warnings before relying on this context.

Recording is covered by the global instruction and the `recording-what-you-learn` 
skill; it is not repo-specific and is not restated here.
```

**Non-Destructive Invariant**: `scaffold.Create` **never rewrites an existing instruction file**. If `.agents/AGENTS.md` already exists (as in `autogo-mlx`), it is preserved byte-for-byte.

---

## 3. Core Harness Engine & Adapter Interface

### 3.1 Interface Refactoring (`agents/internal/harness/harness.go`)

To decouple directory navigation, skill symlinking, and dialect rendering, `Adapter` declares:

```go
type Adapter interface {
	Name() string
	HarnessDir() string
	NeedsSkillsSymlink() bool
	Capabilities() Capabilities
	Events() []Event
	Describe(p Payload, transcript string) string
	WireConfigPath(repoRoot string) string
	Wire(repoRoot, binary string) error
	Render(settings map[string]any, binary string) ([]byte, error)
	TrustSteps(repoRoot string) []string
}
```

| Adapter | `HarnessDir()` | `NeedsSkillsSymlink()` | `WireConfigPath(repoRoot)` | `Render()` Dispatch |
|---|---|---|---|---|
| `claudeAdapter` | `".claude"` | `true` | `<repoRoot>/.claude/settings.json` | `renderHooksJSON` |
| `codexAdapter` | `".codex"` | `true` | `<repoRoot>/.codex/hooks.json` | `renderHooksJSON` |
| `antigravityAdapter` | `".agents"` | `false` | `<repoRoot>/.agents/hooks.json` | `renderNamedGroupsJSON` |

### 3.2 Shared `wireRepository` Execution

`wireRepository` uses `a.HarnessDir()`, gates skill symlinking on `a.NeedsSkillsSymlink()`, and delegates configuration generation to `a.Render(settings, binary)`:

```go
func wireRepository(repoRoot string, a Adapter, binary string) error {
	harnessDir := a.HarnessDir()
	configName := filepath.Base(a.WireConfigPath(repoRoot))

	dir, dirPath, err := openHarnessDir(repoRoot, harnessDir)
	if err != nil {
		return err
	}
	defer dir.Close()
	lock, err := acquireWireLock(dir, dirPath)
	if err != nil {
		return err
	}
	defer releaseWireLock(lock)

	var skills skillsSnapshot
	if a.NeedsSkillsSymlink() {
		var err error
		skills, err = preflightSkills(dir, dirPath)
		if err != nil {
			return err
		}
		if !skills.exists {
			if err := dir.Symlink(filepath.Join("..", ".agents", "skills"), "skills"); err != nil {
				return err
			}
			skills, err = preflightSkills(dir, dirPath)
			if err != nil {
				return err
			}
		}
	}

	settings, snapshot, err := readHooksJSON(dir, dirPath, configName)
	if err != nil {
		return err
	}
	out, err := a.Render(settings, binary)
	if err != nil {
		return err
	}

	validate := func() error {
		if a.NeedsSkillsSymlink() {
			return validateSkills(dir, dirPath, skills)
		}
		return nil
	}
	return atomicWriteHooks(dir, dirPath, configName, out, snapshot, validate)
}
```

### 3.3 Antigravity Dialect Engine (`agents/internal/harness/antigravity.go`)

1. **`renderNamedGroupsJSON`**:
   Renders the `"agents"` top-level object:
   ```json
   {
     "agents": {
       "Stop": [
         {
           "type": "command",
           "command": "'/path/to/agents' hook stop --harness antigravity"
         }
       ]
     }
   }
   ```
2. **`stripOursNamedGroups`**:
   - Recognizes owned commands via `IsOwnedHookCommand(cmd)`.
   - Iterates over flat slices for lifecycle events (`Stop`, `PreInvocation`, `PostInvocation`).
   - Iterates over `group["hooks"]` for matcher tool events (`PreToolUse`, `PostToolUse`).
   - Preserves all foreign hooks (`"lint-checker"`) and foreign event keys.
3. **`TrustSteps`**:
   ```go
   func (a antigravityAdapter) TrustSteps(repoRoot string) []string {
       return []string{
           "Antigravity App: open the workspace folder (hooks execute automatically).",
           "Antigravity CLI: add repository root to trustedWorkspaces in ~/.gemini/antigravity-cli/settings.json.",
       }
   }
   ```
4. **Registration Sites**:
   Registration requires updates at three distinct sites:
   - `func init() { register(antigravityAdapter{}) }` in `antigravity.go`.
   - Adding `"antigravity"` to the ordered slice in `All()` (`harness.go:119`).
   - Adding `"antigravity"` to `knownHarness` (`harness.go:244`).

### 3.4 Payload Normalization & Redaction

`Payload` decodes both snake_case and camelCase protojson fields:

```go
type Payload struct {
	HookEventName       string   `json:"hook_event_name"`
	SessionID           string   `json:"session_id"`
	ConversationID      string   `json:"conversationId"`
	TurnID              string   `json:"turn_id"`
	PromptID            string   `json:"prompt_id"`
	AgentID             string   `json:"agent_id"`
	AgentType           string   `json:"agent_type"`
	Cwd                 string   `json:"cwd"`
	WorkspacePaths      []string `json:"workspacePaths"`
	TranscriptPath      string   `json:"transcript_path"`
	TranscriptPathCamel string   `json:"transcriptPath"`
	AgentTranscriptPath string   `json:"agent_transcript_path"`
	Source              string   `json:"source"`
}
```

In `Build` (`harness.go:150`):
- `SessionID`: `p.SessionID` if non-empty, else `p.ConversationID`.
- `TurnID`: `turnID(p)`. For Antigravity, `p.TurnID` and `p.PromptID` are empty, so `Trace.TurnID` remains empty. Ordinals (`stepIdx`/`invocationNum`) must not populate this field.
- `Cwd`: `p.Cwd` if non-empty, else `p.WorkspacePaths[0]` if non-empty. Note: `WorkspacePaths` is an array; taking `[0]` models single-root execution and knowingly collapses multi-root workspaces.
- `TranscriptPath`: resolved via `pointer.Resolve([]string{p.AgentTranscriptPath, p.TranscriptPath, p.TranscriptPathCamel}, key)`.

**Redaction Guarantee**: Unmapped fields (`lastUserInput`, `raw_tool_input`, assistant messages) are discarded during JSON decoding, verified by unit test `TestAntigravityPayloadRedaction`.

### 3.5 Rationale for Wiring `Stop` (`record-turn-end`)

In Spec 4 §3's two-axis model, wiring an intent requires demonstrating both capability and need. For Antigravity:
- **Capability**: Transcripts are written in JSONL format to `~/.gemini/antigravity/brain/<conversation-id>/.system_generated/logs/transcript.jsonl` and survive across 15+ months without mid-session deletion.
- **Need**: Recording a session-transcript pointer on `Stop` provides uniform provenance across all coding harnesses. Unlike Claude Code, Antigravity does not prune subagent transcripts mid-session, so `cache-subagent-transcript` remains unwired (`needed-by = ["claude-code"]`).

---

## 4. Scaffolding, Exclusions & Diagnostics

### 4.1 Exclusion Purity (`scaffold.go`)
`excludeLines` appends machine-local generated paths strictly to `.git/info/exclude`:
```go
var excludeLines = []string{
	"/.claude/settings.json",
	"/.claude/.agents-wire.lock",
	"/.claude/skills",
	"/.codex/hooks.json",
	"/.codex/.agents-wire.lock",
	"/.codex/skills",
	"/.agents/hooks.json",
	"/.agents/.agents-wire.lock",
}
```
**Rule**: `agents` never writes to or alters repository-tracked `.gitignore`.

### 4.2 Diagnostic Engine (`doctor.go`)
- `checkWiring` implements dual-dialect traversal:
  - `nested-hooks` for Claude Code and Codex.
  - `named-groups` for Antigravity (validating `"agents"` group, flat vs matcher shapes, and emitting canonical `path:event:group:hook` keys).
- `checkAntigravityTrust` reports the accurate trust state:
  - Desktop App: records that no trust gate exists (hooks execute on folder open).
  - CLI: checks `trustedWorkspaces` in `~/.gemini/antigravity-cli/settings.json`.

---

## 5. What This Design Must Not Do

- **Must not swallow content**: Existing instruction files (`.agents/AGENTS.md`) and skill definitions must never be overwritten or deleted.
- **Must not touch `.gitignore`**: Machine-local exclusions belong strictly in `.git/info/exclude`.
- **Must not assume an App trust gate exists**: The Antigravity Desktop App executes `.agents/hooks.json` on folder open; diagnostics must not report an empty manifest as a verified trust gate.
- **Must not populate `turn_id` with ordinals**: `stepIdx` and `invocationNum` are counters, not identifiers.

---

## 6. Open, and Deliberately Not Decided

- **Reliability of pointer delivery for Claude Code and Codex**: Whether Claude Code and Codex consistently follow the `.agents/AGENTS.md` pointer in root `AGENTS.md` remains unmeasured.
- **Disambiguation of `subagent.end` vs `turn.end` in Antigravity**: Antigravity hook payloads do not include `parentConversationId`. Disambiguating root from child sessions without payload support remains open.
- **Whether `turn.before-model` gets an intent**: The capability exists in Antigravity (`PreInvocation` + `injectSteps`); mechanizing the read trigger is left for a future specification.

---

## 7. Version Provenance

- **Harness Version**: `agy 1.1.22` (2026-08-28), re-verified against Antigravity Desktop App language server binary.
- **Re-check Command**:
  ```bash
  strings $(which agy) | grep -E "### [0-9]+\. \`[A-Za-z0-9_]+\` Contract"
  ```
  Asserts that the 5 documented contracts (`PreToolUse`, `PostToolUse`, `PreInvocation`, `PostInvocation`, `Stop`) remain unchanged.

---

## 8. Verification & Migration Protocol

```mermaid
sequenceDiagram
    autonumber
    participant Dev as Test Runner
    participant Unit as Unit Tests
    participant Sandbox1 as autogo-mlx Sandbox
    participant Sandbox2 as cowork Sandbox
    participant Live as Live autogo-mlx
    participant Fleet as Registered Fleet

    Dev->>Unit: Run go test ./agents/...
    Unit-->>Dev: 100% Pass (3x Idempotency, Redaction, Absolute Paths)

    Dev->>Sandbox1: git clone --no-hardlinks --recurse-submodules
    Dev->>Sandbox1: cd sandbox && XDG_STATE_HOME=$(mktemp -d) agents init
    Sandbox1-->>Dev: Verified diff, exclusions, byte-identical AGENTS.md

    Dev->>Sandbox2: Init fresh repo & XDG_STATE_HOME=$(mktemp -d) agents init
    Sandbox2-->>Dev: Verified 3-harness scaffolding; all three wiring checks OK

    Dev->>Live: Assert binary hash & execute live agents init
    Live-->>Dev: All 3 harnesses active; domain rules intact

    Dev->>Fleet: agents init in each registered repo (scaffold + wire)
    Fleet-->>Dev: Exclusions added, all rewired; wiring:antigravity green fleet-wide
```

**Two hazards apply to every phase below, and both have produced a false result
before.**

*Working directory.* `agents init` takes no path argument: it calls
`os.Getwd()` and hands the result to `repo.Discover` (`cmd_init.go:24`). A build
step that leaves the shell in `dotfiles/agents` will onboard **`dotfiles`**,
while every assertion is read against a sandbox nothing touched. Each phase
below therefore builds and runs in separate commands, and names the directory it
runs in.

*What `doctor` returns.* `checkRecording` reports Warn — *"this harness has never
recorded here"* — for any harness with no trace yet (`doctor.go:408`), and Warn
maps to `exitcode.Advisory` (`cmd_doctor.go:110`). A freshly wired sandbox
**cannot** exit `OK`, because no harness has run in it. Assert the named checks;
never assert `doctor`'s exit code in a sandbox.

### Phase 1: Unit & Regression Testing
Run `go test ./agents/... -v` to verify:
1. `TestAntigravityWiring`: correct generation of `.agents/hooks.json`.
2. `TestAntigravityWiringIdempotency`: 3 consecutive wire runs produce a stable, single-entry hook config.
3. `TestAntigravityPayloadRedaction`: unmapped protojson fields are discarded.
4. `TestAntigravityAbsoluteBinaryPath`: hook commands emit POSIX-quoted absolute paths.

### Phase 2: Synthetic `autogo-mlx` Sandbox Verification
1. **Build the test binary** (this leaves the shell in `dotfiles/agents`; the
   next step moves out of it before running anything):
   ```bash
   go build -o /tmp/agents-test-bin .
   ```
2. **Clone Sandbox with Isolation**:
   ```bash
   rm -rf /tmp/sandbox-autogo-mlx
   git clone --no-hardlinks --recurse-submodules /Users/nilbot/playground/autogo-mlx /tmp/sandbox-autogo-mlx
   ```
3. **Capture Git Baseline**:
   ```bash
   BASELINE=$(git -C /tmp/sandbox-autogo-mlx status --porcelain)
   ```
   A recursed submodule can report dirty or detached state on a clean clone, so
   this baseline is the comparison point — not the empty string.
4. **Run Onboarding with Isolated Registry**, from inside the sandbox:
   ```bash
   cd /tmp/sandbox-autogo-mlx
   XDG_STATE_HOME=$(mktemp -d) /tmp/agents-test-bin init
   ```
   Confirm before asserting anything: `git -C /Users/nilbot/dotfiles status
   --porcelain` is unchanged. If `dotfiles` moved, the run went to the wrong
   repository and the phase must be redone.
5. **Assertions** (run from `/tmp/sandbox-autogo-mlx`):
   - `git diff --name-only` contains strictly `.gitattributes`.
   - `git diff .gitattributes` contains strictly `+.agents/** linguist-generated=true`.
   - New untracked files relative to `$BASELINE` are strictly a subset of `{AGENTS.md, CLAUDE.md, .agents/skills/.gitkeep}`.
   - `git check-ignore -v` confirms exclusion of `.claude/settings.json`, `.codex/hooks.json`, `.agents/hooks.json`, and all `.agents-wire.lock` paths.
   - `git diff --stat` confirms zero other tracked files modified; `.agents/AGENTS.md` is byte-identical.
   - `.agents/skills` is a real directory with original skills intact.
   - `.claude/skills` and `.codex/skills` symlink to `../.agents/skills`.
   - `XDG_STATE_HOME=$(mktemp -d) /tmp/agents-test-bin doctor` reports `wiring:claude-code`, `wiring:codex`, and `wiring:antigravity` as `OK`. Its exit code is `Advisory`, and that is the correct result here — see the hazard note above.

### Phase 3: Synthetic `cowork-enterprise` Fresh Repo Verification
1. Initialize a clean git repository at `/tmp/sandbox-cowork-enterprise`.
2. Run the onboarding from inside it:
   ```bash
   cd /tmp/sandbox-cowork-enterprise
   XDG_STATE_HOME=$(mktemp -d) /tmp/agents-test-bin init
   ```
3. Verify root `AGENTS.md`, the `CLAUDE.md` symlink, `.agents/skills/`, and
   three-harness wiring.
4. `XDG_STATE_HOME=$(mktemp -d) /tmp/agents-test-bin doctor` reports all three
   `wiring:*` checks `OK` and all three `recording:*` checks `Warn` with
   *"this harness has never recorded here"*. Both halves are asserted: the Warns
   are the expected state of a repository no harness has run in, and asserting
   them keeps a genuinely broken wiring check from hiding inside a blanket
   "advisory, therefore fine."

### Phase 4: Live Execution on `autogo-mlx`
1. Assert that `which agents` resolves to the newly installed binary and that
   its hash matches the one Phases 2 and 3 exercised. `go install` writes to
   `GOBIN`/`GOPATH/bin`; if that is not what `PATH` resolves, the live phase
   verifies a different binary than the one under test.
2. Run `agents init` in `/Users/nilbot/playground/autogo-mlx`.
3. Verify `git status` and `agents doctor`.

### Phase 5: Fleet Migration
Registering `antigravity` in `All()` changes every repository already onboarded,
not only the ones touched above. `checkWiring` returns **Fail** — *"generated
hook config is unavailable"* (`doctor.go:233`) — for a harness with no config on
disk, so `wiring:antigravity` goes red in every registered repository the moment
the new binary is installed, and stays red until each is rewired. This phase is
not optional cleanup; it is the second half of shipping the change.

**`agents update --all --apply` is not sufficient on its own.** It calls
`wireAll`, which loops `harness.All()` calling `a.Wire` and nothing else
(`cmd_init.go:81`); it never runs `scaffold.Create`. The two new exclusion lines
in §4.1 are written by `scaffold.Create`, so on a repository onboarded before
this change `update --apply` would write `.agents/hooks.json` into a `.agents/`
that git tracks, with no matching entry in `.git/info/exclude`. The file §1.3
requires to be machine-local would then sit untracked and visible, one `git add
-A` away from being committed and distributed. The migration therefore runs
`init`, not `update`.

1. **Enumerate the fleet and count what is affected**:
   ```bash
   agents ls
   agents update --all
   ```
   `update --all` without `--apply` is a dry run and exits `Advisory` by design;
   it is used here only for the count and the missing/unknown lines.
2. **Re-run `init` in each registered repository.** `scaffold.Create` is
   idempotent — `appendMissingLines` adds only absent lines, and
   `writeIfAbsent`/`linkIfAbsent` never overwrite an existing instruction file —
   so this adds the two exclusions and rewires all three harnesses without
   touching anything already correct:
   ```bash
   agents ls   # then, in each listed repository root:
   agents init
   ```
3. **Assert the exclusion landed before trusting the wiring.** In each migrated
   repository:
   ```bash
   git check-ignore -v .agents/hooks.json .agents/.agents-wire.lock
   git status --porcelain .agents/
   ```
   The first must resolve both paths to `.git/info/exclude`; the second must
   print nothing. A repository where the wiring check is green but
   `.agents/hooks.json` shows as untracked is the failure this step exists to
   catch, and it is the one that can reach a remote.
4. Spot-check `agents doctor` in one repository that was **not** part of
   Phases 2-4: `wiring:antigravity` `OK`, `recording:antigravity` `Warn`.

**What this distributes.** Migration writes `.agents/hooks.json` into every
registered repository, and §1.3 establishes that the Antigravity Desktop App
executes that file on folder open with no trust prompt. Once step 3 passes the
file is machine-local by §4.1, so nothing reaches a remote and no collaborator
inherits it — but the blast radius of this phase is every repository in the
local fleet, and it should be run deliberately rather than as a side effect of
an upgrade.
