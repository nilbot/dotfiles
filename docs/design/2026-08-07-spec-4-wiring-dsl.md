# Spec 4 — the wiring DSL

**Date:** 2026-08-07 (deferred) / **2026-08-22 (designed — triggers fired)**
**Status:** designed, not implemented.
**Depends on:** [spec 1](2026-08-07-agents-repo-context-design.md) §5 (harness
adapters), §6 (exit codes), §3.2 (the redaction guarantee).
**Reads against:** [knowledge is documentation](2026-08-19-knowledge-is-documentation.md)
— which decides that most of what we deliver is not wiring at all.

> **History.** This document was "deferred, not rejected" from 2026-08-07 to
> 2026-08-22, gated on three named triggers. Two have fired. The 2026-08-12
> contingent requirement from spec 7 lapsed with spec 7's capture half; it is
> preserved in the archive, not restated here. The original text's core
> argument — that a DSL forces the *semantic event* to be named separately from
> each vendor's encoding of it — survives intact and is now the design.

---

## 1. Why now

Two of the three build triggers are true.

**Trigger 1 — a third target landed.** The original wording was "a new harness,
or `agy` gaining workspace-local hook support." Both, at once.
[Antigravity is not out of scope](../qna/is-antigravity-really-out-of-scope.md)
records the re-test: workspace-local `<workspace>/.agents/hooks.json` has loaded
since `agy` 1.1.1, and the Antigravity **app** ships the same hook machinery as
the CLI (measured 2026-08-22 on 1.1.18 — see
[the app shares the harness](../qna/does-the-antigravity-app-share-the-cli-harness.md)).

**Trigger 3 — capability requirements need to be declarative.** The original
example was, verbatim: *"this hook requires the subagent-transcript-pointer
capability; skip it on harnesses that lack it."* That is now the central
requirement, and §3 shows why it is not sufficient.

Trigger 2 — per-repo wiring divergence — remains false. Every repo still wants
identical wiring. **The DSL varies by harness, not by repository**, and nothing
below introduces a per-repo knob.

One engineering fact the original could not know: the "two targets whose schemas
are ~90% identical" premise is dead. Claude Code and Codex both nest under a
`hooks` object. Antigravity keys on the **hook name at top level**, with no
wrapper — a config written in the Claude dialect loads nothing, which cost a
probe. Three targets, two structurally different renderers, and a third vendor is
one release away from a fourth.

## 2. What it is

A declarative source of truth for hook wiring: one tracked file describing which
**semantic moments** this tool wants to act on and **what each needs to work**,
compiled per harness into `.claude/settings.json`, `<repo>/.codex/hooks.json`,
`<workspace>/.agents/hooks.json`, and whatever comes next.

It covers **wiring only**. Skills, subagent definitions and rules are
markdown-with-frontmatter, already near-common across all three vendors, and get
*placed* — never generated. See §7.

## 3. The correction that reshapes the design

The 2026-08-07 model was one-sided: *the tool has needs; harnesses have
capabilities; emit where capability ⊇ need.* Measured 2026-08-22 against this
repository's own trace records, that is wrong.

```
records by harness:                claude-code 304    codex 14
verified pointers still on disk:   claude-code 165    codex 12
verified pointers now gone:        claude-code 110    codex  2
codex subagent-start/subagent-stop records:                  0
```

Claude Code has lost **110 of 275** verified pointers — 40% — including
[25 subagent transcripts deleted inside the session that produced them](../qna/why-are-subagent-transcripts-gone.md).
Codex's two losses are both from one abandoned 2026-08-10 session: loss mode 1
(a whole session file), not mid-session pruning. Codex has produced **no**
subagent records at all in production use, though spec 1 observed both events
firing in a probe — so the wiring is live and the need has never been
demonstrated.

**The transcript cache exists because Claude Code destroys raw material
mid-session. That is a measured property of one harness, not of harnesses.** We
wired `subagent-stop` on Codex for a problem Codex has not been shown to have,
and the one-sided model is what let us do it without noticing.

So the model has two axes, not one:

> Emit a hook where the harness **exhibits the need** *and* **provides the
> capability**. Absence of either is a reason not to wire, and the two absences
> mean different things.

| | provides | does not provide |
|---|---|---|
| **exhibits the need** | wire it | **a real gap** — degrade, and say so |
| **need not shown** | wire nothing; record the hypothesis | not a gap; silence |

The bottom-left cell is where Codex's `subagent-stop` sits today. The top-right
is where Antigravity's missing `SessionStart` sits — *if* the need is ever shown
there.

This axis is not decoration. It is the difference between a tool that ports its
author's harness's problems to every other vendor, and one that asks each vendor
what it actually breaks.

## 4. The finding the vocabulary produced

§5 is the schema. This section is the one thing that only became visible once
needs and capabilities were tabulated separately, stated before the tables so it
is not lost inside them.

**The asymmetry runs both ways.** Antigravity has no `session.begin`, and its
subagent events are unmeasured — but it is the only harness in the fleet
offering `turn.before-model` with `result.inject-context`. That is a per-turn
injection point with a concrete subject, able to stay silent on no match:
precisely the mechanism the 2026-08-19 redesign wanted for the read trigger and
recorded as not having.

The harness we excluded for fifteen releases is the only one that can do the
thing we said was impossible. That is what a comparison table buys, and it is
why §5 is the deliverable and the compiler is an afterthought.

## 5. The schema

Five types. The compiler is small; this vocabulary is the artifact, so each type
is given in full — fields, legal values, and what a value *claims*.

| type | kind | what it is |
|---|---|---|
| `moment` | closed enum | a semantic lifecycle point, named for what happens |
| `capability` | closed enum | something a harness can supply *at* a moment |
| `intent` | record | one thing this tool wants done, and what it needs to work |
| `target` | record | one harness's output disposition and dialects |
| `dialect` | closed enum ×2 | how config is rendered; how payload keys are spelled |

`moment` and `capability` are closed because the compiler must be able to reject
a typo. Adding a value is a deliberate edit with a measurement behind it.

### 5.1 `moment`

> **Provenance.** Claude Code cells: 2.1.224, spec 1 task 8. Codex cells:
> CLI 0.147.0. Antigravity cells: hook *loading* on `agy` 1.1.16 (2026-08-20);
> event and payload *schema* extracted from the binary's embedded docs on
> **1.1.18** (2026-08-22), re-verified unchanged from 1.1.17.
>
> `agy` ships roughly a release a day — 1.1.0 on 2026-08-07, 1.1.18 on
> 2026-08-22. This spec's Antigravity rows are the perishable part, and the last
> time a perishable Antigravity measurement went unstamped it stood wrong for
> fifteen releases. Re-verifying is one command: extract the five
> `### N. \`Event\` Contract` headings and check the count and the names.

Named for what happens, never for a vendor's spelling. **Measured** means this
repository observed it firing; **documented** means a vendor states it exists and
we have not run it; **blank** means nobody has checked, and is not a zero.

| `moment` | Claude Code | Codex | Antigravity |
|---|---|---|---|
| `session.begin` | `SessionStart` *(measured)* | `SessionStart` *(measured)* | **absent** |
| `session.end` | | `SessionEnd` *(documented)* | **absent** |
| `prompt.submit` | | `UserPromptSubmit` *(documented)* | |
| `turn.before-model` | | | `PreInvocation` *(documented)* |
| `turn.after-model` | | | `PostInvocation` *(documented)* |
| `turn.end` | `Stop` *(measured)* | `Stop` *(measured)* | `Stop` *(documented, 1.1.10+)* |
| `subagent.begin` | `SubagentStart` *(measured)* | `SubagentStart` *(measured)* | |
| `subagent.end` | `SubagentStop` *(measured)* | `SubagentStop` *(measured)* | |
| `tool.before` | `PreToolUse` *(documented)* | `PreToolUse` *(measured)* | `PreToolUse` *(documented)* |
| `tool.after` | `PostToolUse` *(documented)* | `PostToolUse` *(documented)* | `PostToolUse` *(documented)* |
| `context.compact` | | `PreCompact`/`PostCompact` *(documented)* | |
| `permission.request` | | `PermissionRequest` *(documented)* | |

Claude Code's row is sparse because spec 1 measured one session and recorded only
what fired. Its vendor documents more events; none are cited here, because this
table's job is to distinguish measured from assumed and a borrowed list would
defeat it.

**`subagent.end` on Antigravity is the highest-value blank in this document.**
Antigravity has subagents (`invoke_subagent`), so `tool.after` with a matcher of
`invoke_subagent` is a candidate for the same moment under a different spelling.
Whether the moment is reachable that way is untested.

### 5.2 `capability`

What a harness supplies at a moment. This is `harness.Capabilities` promoted from
one bool to a vocabulary.

| `capability` | means | Claude Code | Codex | Antigravity |
|---|---|---|---|---|
| `payload.session-id` | a stable conversation identifier | `session_id` | `session_id` | `conversationId` |
| `payload.turn-id` | a per-turn identifier | `prompt_id` | `turn_id` | `stepIdx`/`invocationNum` — *ordinals, not ids* |
| `payload.cwd` | one working directory | `cwd` | `cwd` | `workspacePaths` — **an array** |
| `payload.own-transcript` | the transcript of the agent that fired | ✔ | ✔ | `transcriptPath` *(documented)* |
| `payload.child-transcript` | the *child's* transcript at `subagent.end` | `agent_transcript_path` | `agent_transcript_path` | |
| `payload.agent-id` | pairs `subagent.begin` ↔ `subagent.end` | `agent_id` | `agent_id` | |
| `payload.description` | a human label for a subagent | via spawn-time sidecar | **no** | |
| `payload.artifact-dir` | a directory of run artifacts | **no** | **no** | `artifactDirectoryPath` |
| `result.inject-context` | stdout can add context to the model | at `session.begin` | | `injectSteps` at `turn.before-model`/`turn.after-model` |
| `result.block` | the hook can deny, or force continuation | | | `decision` at `tool.before`, `terminationBehavior`, `Stop` — **bounded**¹ |
| `config.matcher` | handlers can be scoped by a pattern | ✔ | ✔ | ✔ — **required** for tool-scoped events |
| `handler.timeout` | per-handler execution timeout | | | ✔, seconds, default 30 |

¹ A `Stop` hook cannot block indefinitely: `agy` defuses one that always
continues, "after a configurable number of consecutive continuations, the hook
can no longer block and the turn ends normally". Any intent depending on
`result.block` at `turn.end` must be correct when its blocking is ignored.

Three of these rows are load-bearing and none was visible before the vocabulary
was written down.

**`payload.turn-id` is not portable.** Claude Code and Codex supply opaque ids
under different names, which `harness.turnID` already reconciles. Antigravity
supplies `stepIdx`, `invocationNum` and `executionNum` — **ordinals within a
conversation, not identifiers**. A record field typed as an id cannot hold one
honestly, and the adapter must declare the gap rather than coerce a number into
it. This is the `description` problem again, one type deeper.

**`payload.cwd` is not portable either.** `workspacePaths` is plural:
Antigravity models multi-root workspaces, and `Trace.Cwd` is a single string. The
DSL cannot fix that — it can only make the compiler refuse to pretend.

**`config.matcher` is universal but not optional.** Antigravity *requires* a
matcher group for `tool.before`/`tool.after` and matches regex against tool names
derived by lowercasing the step type and stripping `CORTEX_STEP_TYPE_`. Codex's
existing `session-start` entry carries `matcher: "startup|resume"`. So a matcher
is a first-class intent field, not an adapter detail.

### 5.3 `intent`

| field | type | required | meaning |
|---|---|---|---|
| `moment` | `moment` | ✔ | when to run |
| `run` | string | ✔ | the `agents` subcommand; rendered into the target's dialect |
| `requires` | `[capability]` | ✔ (may be empty) | absent capability ⇒ degrade and report |
| `needed-by` | `[target]` | ✔ | harnesses where the need is **demonstrated**. Allowlist |
| `matcher` | string | — | pattern scoping; mandatory where the target requires one |
| `timeout` | int | — | seconds; emitted only where `handler.timeout` exists |
| `why` | string | ✔ | the evidence, and for omitted harnesses, why the need is unshown |

`why` is required because §3's whole point is that `needed-by` encodes a
falsifiable claim. An intent that cannot say why a harness is missing from its
allowlist is the one-sided model sneaking back in.

**The complete intent set.** Three built, two named and deliberately unbuilt.

| intent | `moment` | `requires` | `needed-by` | status |
|---|---|---|---|---|
| `record-session-pointer` | `session.begin` | — | claude-code, codex | live |
| `record-turn-end` | `turn.end` | — | claude-code, codex | live; antigravity eligible |
| `cache-subagent-transcript` | `subagent.end` | `payload.child-transcript` | **claude-code only** | live; §3 corrects it |
| `record-subagent-pointer` | `subagent.begin` | `payload.agent-id` | — | wired today on codex against no demonstrated need |
| `inject-retrieval-prompt` | `turn.before-model` | `result.inject-context` | — | **not proposed** — §9 |

`inject-retrieval-prompt` is listed to fix its shape, not to authorise it. The
read trigger is instruction-delivered today; mechanising it is a separate design
with its own falsifier. What the table records is that the capability now exists
on exactly one harness, so the question is answerable rather than academic.

### 5.4 `target`

| field | type | meaning |
|---|---|---|
| `path` | repo-relative path | where the generated config goes |
| `config-dialect` | `dialect.config` | how hook entries nest |
| `payload-dialect` | `dialect.payload` | how payload keys are spelled |
| `tracked` | bool | whether the generated file is committed |
| `ownership` | `own` \| `merge` | may we replace the file, or must we merge into it |
| `hook-cwd` | `unspecified` \| `config-dir` | the working directory the harness gives the hook process |
| `transcript-root` | `home` \| `workspace` \| `opaque` | where raw material lives, and therefore who may copy it |
| `scope` | `repository` \| `global` | which config this row describes. **This spec wires `repository` only** |
| `trust` | free text | the manual gate; never defeated, only reported |

| | Claude Code | Codex | Antigravity |
|---|---|---|---|
| `path` | `.claude/settings.json` | `.codex/hooks.json` | `.agents/hooks.json` |
| `config-dialect` | `nested-hooks` | `nested-hooks` | `flat-named` |
| `payload-dialect` | `snake` | `snake` | `camel` |
| `tracked` | false | false | **true** — `.agents/` is tracked context |
| `ownership` | `merge` — holds unrelated settings | `own` | `merge` — shares the customization root |
| `hook-cwd` | unspecified | unspecified | `config-dir` |
| `transcript-root` | `home` | `home` | `opaque` |
| `scope` | repository | repository | repository |
| `trust` | project-trust prompt once | hash-based; `/hooks` reviews | folder trust; **app mechanism unknown** |

Three consequences the row-by-row view makes unavoidable.

**`tracked` differs, and it is the first time it has.** `.claude/` and `.codex/`
are git-ignored generated output. Antigravity's config lives in `.agents/`, which
this repository *tracks*. A generated file entering the tracked tree is exactly
what spec 1 §3.2 governs, and the guard's `unsafe-path` rule already watches
staged `.agents/` blobs. Either `.agents/hooks.json` is tracked — and then it must
be deterministic, machine-independent, and free of absolute paths — or it is
ignored, and Antigravity is the one harness whose config is invisible to review.
**Not decided here.** It is the first genuine per-target policy question the DSL
surfaces, and it deserves its own decision rather than a default.

**`transcript-root: opaque` is not a shrug.** Antigravity's hook docs give
`transcriptPath` as `<workspace>/.gemini/antigravity/transcript.jsonl`, but no
workspace on this machine contains a `.gemini` directory, and the real
conversation store is `~/.gemini/antigravity/conversations/<uuid>.db` and `.pb` —
**SQLite and protobuf, not JSONL**. The documented path and the observed store
disagree, and no Antigravity hook has ever run here to settle it. Until one does,
the cell is `opaque`, and `cache-subagent-transcript` cannot name Antigravity for
a second reason beyond §3: `pointer.Resolve` requires a `.jsonl` suffix, and
copying a live SQLite file is a torn read, not a backup.

**`scope` exists because Antigravity has two config locations, and one of them
is shared.** `agy` 1.1.x moved the `/hooks` command's output from
`~/.gemini/antigravity-cli/hooks.json` to `~/.gemini/config/hooks.json`, its own
changelog explaining the fix as *"ensuring hooks remain synchronized between the
TUI and the backend"*. `~/.gemini/config/` is shared across all three products —
`~/.gemini/antigravity/mcp_config.json` is a symlink into it, and it holds the
project registry that names workspaces by `gitFolder` URI. So a *global* hooks
path exists that would apply to the app, the CLI and the IDE at once.

This spec does not wire it, and the `scope` field is how it says so out loud
rather than by omission. Global wiring is a fleet decision with a different blast
radius: one file, every repository, including repositories that never opted in.
Spec 1's placement rule exists to keep this tool's writes inside repositories
that asked for them.

Two facts about that shared directory are worth carrying anyway. It already
contains `skills -> /Users/nilbot/dotfiles/gemini/skills`, so **this repository
already places skills for Antigravity globally** — the placement half of §7 is
not merely portable in principle, it is deployed. And the shared config holds
`globalPermissionGrants` but **no `trustedWorkspaces`**: that key lives only in
the CLI's own settings file, which is why the app's trust mechanism is still the
open question in §9 and not answered by finding the shared directory.

**`hook-cwd: config-dir` breaks a silent assumption.** Antigravity runs the hook
from the directory containing `hooks.json`. Every relative path in a generated
command would resolve differently there. `HookCommand` already emits an absolute
binary path, so nothing is broken today — but the reason it is not broken is an
accident of an unrelated decision, and the DSL should say so rather than rely on
it.

### 5.5 `dialect`

Two independent enums. Conflating them is what cost a probe.

`dialect.config` — how hook entries nest in the generated file:

| value | shape | targets |
|---|---|---|
| `nested-hooks` | `{"hooks": {"<Vendor>": [{"matcher": …, "hooks": [{"type","command"}]}]}}` | claude-code, codex |
| `flat-named` | `{"<Vendor>": [{"matcher": …, "hooks": [{"type","command","timeout"}]}]}` | antigravity |

`dialect.payload` — how the harness spells keys on stdin:

| value | spelling | targets |
|---|---|---|
| `snake` | `session_id`, `transcript_path`, `agent_transcript_path` | claude-code, codex |
| `camel` | `conversationId`, `transcriptPath`, `artifactDirectoryPath` (protojson) | antigravity |

`harness.Payload` decodes `snake` only. A `camel` adapter needs its own decode
path, and the redaction guarantee must hold across both: the guarantee is
structural — `Payload` has no field able to hold a forbidden value — so a second
decoder is a second place that guarantee must be proven, not inherited. That is a
test obligation, and it is the single largest implementation cost in this spec.

### 5.6 What the compiler may not infer

- **A blank cell is not `false`.** Absent capability ⇒ degrade and report;
  unmeasured ⇒ report differently. Two states, never merged into one.
- **A moment present under another name is not automatically the same moment.**
  `tool.after` + `matcher = "invoke_subagent"` may be `subagent.end`. Until
  measured it is a hypothesis, and the DSL records hypotheses in `why`, not in
  `needed-by`.
- **An ordinal is not an id.** `stepIdx` must not populate a `turn_id` field.
- **Vendor documentation is not a measurement.** §5.1 keeps the two apart on
  purpose; the last time they were conflated the answer stood wrong for fifteen
  releases.

### 5.7 Worked example

```toml
# .agents/wiring.toml — tracked, hand-edited, the only source of hook truth.

[intent.cache-subagent-transcript]
moment    = "subagent.end"
run       = "hook subagent-stop"
requires  = ["payload.child-transcript"]
needed-by = ["claude-code"]
why = """
Claude Code prunes subagent transcripts during the producing session; capture is
now-or-never (docs/qna/why-are-subagent-transcripts-gone.md). Codex is absent
deliberately: 0 subagent records, no pruning observed
(docs/qna/which-harnesses-actually-lose-transcripts.md). Antigravity is absent
for two reasons - the need is unshown, and transcript-root is opaque.
"""

[intent.record-turn-end]
moment    = "turn.end"
run       = "hook stop"
requires  = []
needed-by = ["claude-code", "codex"]
why = "A pointer to the session transcript. Antigravity is eligible and unwired."

[target.antigravity]
path            = ".agents/hooks.json"
config-dialect  = "flat-named"
payload-dialect = "camel"
tracked         = true          # OPEN - see 5.4
ownership       = "merge"
hook-cwd        = "config-dir"
transcript-root = "opaque"
trust           = "folder trust; app mechanism unknown"
```

## 6. Compilation and degradation

For each target, for each intent:

1. Skip unless the target is in `needed-by`. Silent: not a gap.
2. Look up `moment` in the harness matrix. If absent → **degrade**, and emit an
   advisory (exit 1) naming the intent, the harness, and the missing moment.
3. Check `requires` against the harness's capabilities. Missing → same.
4. Render `run` into the target's dialect at the vendor spelling.

**Degradation must be loud and must not block.** A missing hook produces no error
on its own — that is the whole reason `doctor` exists — so an intent that cannot
be wired has to be visible somewhere a human looks. Exit 1 (advisory), never 2
(block): an unwireable intent on one harness is not a broken repository.

`agents doctor` gains one check: **every intent in `needed-by` is either wired or
reported as degraded.** Nothing else in the tool learns the vocabulary — the DSL
is read by the compiler and by `doctor`, and by nothing else.

## 7. What it must not do

- **Must not swallow content.** Skills, rules, and subagent definitions are
  placed, not generated. All three vendors read markdown-with-frontmatter from a
  directory; Antigravity's customization root *is* `.agents/`. That convergence
  is free and needs no DSL.
- **Must not introduce per-repo knobs.** Trigger 2 has not fired.
- **Must not encode unmeasured behaviour as a decision.** A blank cell in §4c is
  an open measurement. The compiler must distinguish "this harness lacks the
  moment" from "nobody has checked," and report the second differently.
- **Must not defeat a trust gate.** Unchanged from spec 1: no harness lets a
  freshly wired repo's hooks fire unattended, and that is by design.
- **Must not grow a `[repo.*]` section.** If one is ever proposed, re-read
  trigger 2 first.

## 8. The reason to build it that is not cost-justified

Preserved from the original, because it still will not show up in any
cost/benefit table, and because it is now half-earned.

A DSL forces the semantic event to be named separately from each vendor's
encoding of it. "A subagent finished" is not `SubagentStop`, and it is not
`PostToolUse` matching `invoke_subagent` — those are two spellings of one idea.
Writing the abstraction down turns the differences between vendors into **data
you can compare**, rather than implementation detail rederived each time.

What has changed since 2026-08-07 is that the comparison has started paying. §3
is a finding about *our own wiring* that only became visible once needs and
capabilities were tabulated separately. §4c is a finding about the vendors: the
one harness we excluded for fifteen releases is the only one offering the
mechanism the redesign said it lacked.

Read alongside
[the empty knowledge stores](../qna/do-vendor-knowledge-subsystems-get-used.md),
the shape of the landscape is not what "bridge the divide between vendors"
assumes. All three vendors converged on the same *placement* primitives, so most
compatibility is already free. They diverge on hooks — the surface with the least
demonstrated value in this repository's record. The divide worth bridging is not
vendor-to-vendor. It is **mechanism versus instruction**, and this document is
the small, honest mechanism half.

## 9. Open, and deliberately not decided

- **Does Antigravity destroy subagent trajectories?** The decisive measurement
  for whether `cache-subagent-transcript` ever gains `antigravity`. Its tooling
  says *"logs and artifacts are preserved"* when a subagent tree is killed, while
  a `Failed to prune trajectory` path also exists. Until measured, the cell stays
  blank.
- **Do Antigravity hooks fire inside its subagents?** Unmeasured, and it decides
  whether `turn.before-model` can carry the read trigger where instructions
  demonstrably fail (0 of 31).
- **How does the Antigravity app grant workspace trust?** `~/.gemini/antigravity/`
  has no `settings.json` at all, so the CLI's `trustedWorkspaces` mechanism is
  not the app's. Trust is the gate the CLI probe had to clear.
- **What re-verifies the Antigravity rows, and when?** `agy` released 18 patches
  in 15 days. §5.1 carries a version stamp and a one-command re-check, but
  nothing *runs* it. Spec 5's gate is the obvious host; whether a CI job should
  assert a vendor binary's embedded schema is genuinely arguable — it fails on
  the vendor's schedule, not ours — and is not decided here.
- **Should Codex's `subagent-stop` wiring be removed?** §3 says the need is
  undemonstrated. It is also nearly free and already trusted. Left in place
  pending either a Codex pruning observation or a decision to enforce
  `needed-by` retroactively.
- **Whether `turn.before-model` gets an intent at all.** Nothing here proposes
  one. The redesign's read trigger is delivered by instruction today; mechanising
  it is a separate design with its own falsifier, and this document only records
  that the capability now exists.
