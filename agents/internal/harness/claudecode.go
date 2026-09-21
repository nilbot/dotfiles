package harness

import (
	"path/filepath"
)

func init() { register(claudeCode{}) }

type claudeCode struct{}

func (claudeCode) Name() string             { return "claude-code" }
func (claudeCode) HarnessDir() string       { return ".claude" }
func (claudeCode) NeedsSkillsSymlink() bool { return true }

func (claudeCode) WireConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".claude", "settings.json")
}

func (c claudeCode) StripHooks(settings map[string]any) error {
	_, err := stripHooksJSON(settings)
	return err
}

func (c claudeCode) Wire(repoRoot, binary string) error {
	// Neither harness discovers .agents/skills on its own: Claude Code reads
	// .claude/skills, Codex reads .codex/skills. One directory, two names.
	return wireRepository(repoRoot, c, binary)
}

// TrustSteps are the manual steps left after wiring: Claude Code asks for a
// project-trust decision once per directory, and it reads nothing in the
// project until that is answered.
func (claudeCode) TrustSteps(repoRoot string) []string {
	return []string{
		"Claude Code: open a session in " + repoRoot + " and accept the project-trust prompt once.",
	}
}
