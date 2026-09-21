package harness

import (
	"path/filepath"
)

func init() {
	register(antigravityAdapter{})
}

type antigravityAdapter struct{}

func (a antigravityAdapter) Name() string             { return "antigravity" }
func (a antigravityAdapter) HarnessDir() string       { return ".agents" }
func (a antigravityAdapter) NeedsSkillsSymlink() bool { return false }
func (a antigravityAdapter) WireConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".agents", "hooks.json")
}
func (a antigravityAdapter) TrustSteps(repoRoot string) []string {
	return []string{
		"Antigravity App: open the workspace folder (hooks execute automatically).",
		"Antigravity CLI: add repository root to trustedWorkspaces in ~/.gemini/antigravity-cli/settings.json.",
	}
}
func (a antigravityAdapter) Wire(repoRoot, binary string) error {
	return wireRepository(repoRoot, a, binary)
}
func (a antigravityAdapter) StripHooks(settings map[string]any) error {
	return stripNamedGroups(settings)
}

// stripNamedGroups removes this tool's own group from an Antigravity config.
//
// It is stripped whether or not any event list mentions it, and the group is
// removed outright rather than left as an empty object: Antigravity reads a
// named group, not a key per event, so "no group" and "an empty group" are the
// same statement and the first one does not leave a file claiming to be
// generated.
func stripNamedGroups(settings map[string]any) error {
	existing, ok := settings["agents"].(map[string]any)
	if !ok {
		return nil
	}
	// A group that held no keys is left alone even though stripping leaves
	// nothing, for the same reason `wire` leaves a keyless file alone: an empty
	// container this tool did not write is a placeholder, not litter of ours.
	// Measured before this guard existed, `{"agents":{"Stop":[]}}` -- which holds
	// no entry of ours -- had its whole config file deleted.
	kept := stripOursNamedGroups(existing)
	if len(kept) == 0 {
		delete(settings, "agents")
		return nil
	}
	settings["agents"] = kept
	return nil
}

func stripOursNamedGroups(group map[string]any) map[string]any {
	out := make(map[string]any)
	for evName, val := range group {
		slice, ok := val.([]any)
		if !ok {
			out[evName] = val
			continue
		}
		var kept []any
		for _, item := range slice {
			m, ok := item.(map[string]any)
			if !ok {
				kept = append(kept, item)
				continue
			}
			if innerHooks, hasHooks := m["hooks"].([]any); hasHooks {
				var keptInner []any
				for _, h := range innerHooks {
					hm, ok := h.(map[string]any)
					if ok {
						if cmd, ok := hm["command"].(string); ok && IsOwnedHookCommand(cmd) {
							continue
						}
					}
					keptInner = append(keptInner, h)
				}
				if len(keptInner) > 0 {
					newM := make(map[string]any, len(m))
					for k, v := range m {
						newM[k] = v
					}
					newM["hooks"] = keptInner
					kept = append(kept, newM)
				}
			} else {
				if cmd, ok := m["command"].(string); ok && IsOwnedHookCommand(cmd) {
					continue
				}
				kept = append(kept, item)
			}
		}
		if len(kept) > 0 {
			out[evName] = kept
		}
	}
	return out
}
