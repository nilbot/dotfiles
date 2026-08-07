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
	if err := writeHooksJSON(c.WireConfigPath(repoRoot), c.Name(), c.Events(), binary); err != nil {
		return err
	}
	return linkSkills(filepath.Join(repoRoot, ".codex", "skills"))
}

func (codex) TrustSteps(repoRoot string) []string {
	return []string{
		"Codex: start a session in " + repoRoot + " once and approve the project when prompted; a repository Codex has not been told to trust does not run its hooks.",
	}
}
