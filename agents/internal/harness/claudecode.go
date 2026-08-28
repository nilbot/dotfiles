package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func init() { register(claudeCode{}) }

type claudeCode struct{}

func (claudeCode) Name() string { return "claude-code" }
func (claudeCode) HarnessDir() string { return ".claude" }
func (claudeCode) NeedsSkillsSymlink() bool { return true }

func (claudeCode) Capabilities() Capabilities {
	// Claude Code writes an agent-<id>.meta.json sidecar at spawn time, so a
	// human label is available. Codex has no equivalent.
	return Capabilities{Description: true}
}

func (claudeCode) Events() []Event {
	return []Event{
		{Semantic: SessionStart, Vendor: "SessionStart"},
		{Semantic: SubagentStart, Vendor: "SubagentStart"},
		{Semantic: SubagentStop, Vendor: "SubagentStop"},
		{Semantic: Stop, Vendor: "Stop"},
	}
}

// Describe reads the spawn-time sidecar that sits beside the transcript.
// Best effort by design: a missing or unreadable sidecar costs a label, and a
// record without a label is still a usable pointer.
func (claudeCode) Describe(p Payload, transcript string) string {
	if transcript == "" || !strings.HasSuffix(transcript, ".jsonl") {
		return ""
	}
	sidecar := strings.TrimSuffix(transcript, ".jsonl") + ".meta.json"
	b, err := os.ReadFile(sidecar)
	if err != nil {
		return ""
	}
	var meta struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return ""
	}
	return meta.Description
}

func (claudeCode) WireConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".claude", "settings.json")
}

func (c claudeCode) Render(settings map[string]any, binary string) ([]byte, error) {
	return renderHooksJSON(settings, c.Name(), c.Events(), binary)
}

func (c claudeCode) Wire(repoRoot, binary string) error {
	// Neither harness discovers .agents/skills on its own: Claude Code reads
	// .claude/skills, Codex reads .codex/skills. One directory, two names.
	return wireRepository(repoRoot, c, binary)
}

func (claudeCode) TrustSteps(repoRoot string) []string {
	return []string{
		"Claude Code: open a session in " + repoRoot + " and accept the project-trust prompt once.",
		"Claude Code: hooks are re-read when the settings file changes; if they do not fire, open /hooks once in an interactive session.",
	}
}
