package harness

import "path/filepath"

func init() { register(codex{}) }

// codex is registered for its wiring half only. `agents init` wires every
// adapter in the registry, so Codex has to be able to generate its config
// before its recording half is reconciled against a live payload.
type codex struct{}

func (codex) Name() string { return "codex" }

func (codex) Capabilities() Capabilities {
	// Codex writes no spawn-time sidecar, so there is nowhere to read a human
	// label from. Claiming otherwise would put an always-empty description in
	// every Codex record.
	return Capabilities{Description: false}
}

func (codex) Events() []Event {
	return []Event{
		// startup|resume, not every source: a session that is only being
		// re-rendered is not a new session to record.
		{Semantic: SessionStart, Vendor: "SessionStart", Matcher: "startup|resume"},
		{Semantic: SubagentStart, Vendor: "SubagentStart"},
		{Semantic: SubagentStop, Vendor: "SubagentStop"},
		{Semantic: Stop, Vendor: "Stop"},
	}
}

// Describe is always empty; see Capabilities. Build never calls it.
func (codex) Describe(p Payload, transcript string) string { return "" }

func (codex) WireConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".codex", "hooks.json")
}

func (c codex) Wire(repoRoot, binary string) error {
	return wireRepository(repoRoot, ".codex", "hooks.json", c.Name(), c.Events(), binary)
}

// TrustSteps names both of Codex's gates, because they are separate and
// clearing only the first leaves a wired repo recording nothing.
//
// Measured against Codex CLI 0.147.0 on this machine:
//
//   - `codex features list` reports `hooks stable true`. No feature flag is
//     needed; guides that say otherwise are stale.
//   - The directory gate is the CLI's own prompt: "Do you trust the contents of
//     this directory? ... Trusting the directory allows project-local config,
//     hooks, and exec policies to load." Untrusted, `.codex/hooks.json` is not
//     loaded at all.
//   - The hook gate is separate and persisted. `codex --help` documents
//     `--dangerously-bypass-hook-trust` as "Run enabled hooks without requiring
//     persisted hook trust for this invocation", and the review prompt offers
//     "Review hooks" / "Trust all and continue" / "Continue without trusting
//     (hooks won't run)".
//   - It is keyed by hash: the hook browser distinguishes "New hook - review
//     required", "Trusted", and "Modified since last trusted - review
//     required", and the stored key is `trusted_hash`. So it recurs whenever a
//     generated command changes.
//   - `/hooks` exists, and its own entry in the command list describes it as
//     "view and manage lifecycle hooks". An earlier binary-strings search
//     concluded otherwise and was wrong: slash commands are stored without the
//     `/` prefix, and the bare `hooks` token sits in a serde struct-name region
//     (`configRequirements/read.hooks`) rather than in the command cluster.
//     The view is a table of all eleven lifecycle events -- PreToolUse,
//     PermissionRequest, PostToolUse, PreCompact, PostCompact, SessionStart,
//     SessionEnd, UserPromptSubmit, SubagentStart, SubagentStop, Stop -- each
//     with an Installed and an Active count. That pair is the only way to see
//     the installed-but-inert state from inside a session, which is exactly the
//     shape a cleared directory gate and an uncleared hook gate produce.
func (codex) TrustSteps(repoRoot string) []string {
	return []string{
		"Codex: start a session in " + repoRoot + " once and answer yes to \"Do you trust the contents of this directory?\"; until the directory is trusted, .codex/hooks.json is not loaded at all.",
		"Codex: in that session, accept the hook-review prompt (\"Trust all and continue\"; \"Continue without trusting\" leaves the hooks inert). Hook trust is recorded per hook by hash, so it comes back after any `agents wire` that changes a command.",
		"Codex: check with `/hooks`, which tables every lifecycle event with an Installed and an Active count. Installed but not Active on SessionStart, SubagentStart, SubagentStop or Stop is the silent-failure state -- the config loaded and nothing will run.",
	}
}
