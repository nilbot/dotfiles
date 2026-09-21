package harness

import "path/filepath"

func init() { register(codex{}) }

// codex is registered for its wiring half only. `agents init` wires every
// adapter in the registry, so Codex has to be able to generate its config
// before its recording half is reconciled against a live payload.
type codex struct{}

func (codex) Name() string             { return "codex" }
func (codex) HarnessDir() string       { return ".codex" }
func (codex) NeedsSkillsSymlink() bool { return true }

func (codex) WireConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".codex", "hooks.json")
}

func (c codex) StripHooks(settings map[string]any) error {
	_, err := stripHooksJSON(settings)
	return err
}

func (c codex) Wire(repoRoot, binary string) error {
	return wireRepository(repoRoot, c, binary)
}

// TrustSteps are the manual steps left after wiring. Codex loads a project's
// configuration -- including .codex/hooks.json -- only for a directory the user
// has trusted, so the step is real even though this tool no longer writes hook
// entries: nothing under .codex/ is read until the directory is trusted.
func (codex) TrustSteps(repoRoot string) []string {
	return []string{
		"Codex: start a session in " + repoRoot + " once and answer yes to \"Do you trust the contents of this directory?\"; until then Codex reads nothing under .codex/.",
	}
}
