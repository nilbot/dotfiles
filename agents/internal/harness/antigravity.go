package harness

import (
	"encoding/json"
	"path/filepath"
)

func init() {
	register(antigravityAdapter{})
}

type antigravityAdapter struct{}

func (a antigravityAdapter) Name() string { return "antigravity" }
func (a antigravityAdapter) HarnessDir() string { return ".agents" }
func (a antigravityAdapter) NeedsSkillsSymlink() bool { return false }
func (a antigravityAdapter) Capabilities() Capabilities { return Capabilities{Description: false} }
func (a antigravityAdapter) Events() []Event {
	return []Event{{Semantic: Stop, Vendor: "Stop"}}
}
func (a antigravityAdapter) Describe(p Payload, transcript string) string { return "" }
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
func (a antigravityAdapter) Render(settings map[string]any, binary string) ([]byte, error) {
	return renderNamedGroupsJSON(settings, a.Name(), a.Events(), binary)
}

func renderNamedGroupsJSON(settings map[string]any, harnessName string, events []Event, binary string) ([]byte, error) {
	root := make(map[string]any)
	for k, v := range settings {
		root[k] = v
	}

	var agentsGroup map[string]any
	if existing, ok := root["agents"].(map[string]any); ok {
		agentsGroup = stripOursNamedGroups(existing)
	} else {
		agentsGroup = make(map[string]any)
	}

	for _, ev := range events {
		cmd := HookCommand(binary, harnessName, ev.Semantic)
		entry := map[string]any{
			"type":    "command",
			"command": cmd,
		}
		if ev.Vendor == "PreToolUse" || ev.Vendor == "PostToolUse" {
			matcherGroup := map[string]any{
				"matcher": ev.Matcher,
				"hooks":   []any{entry},
			}
			var existingGroups []any
			if eg, ok := agentsGroup[ev.Vendor].([]any); ok {
				existingGroups = eg
			}
			agentsGroup[ev.Vendor] = append(existingGroups, matcherGroup)
		} else {
			var existingEntries []any
			if ee, ok := agentsGroup[ev.Vendor].([]any); ok {
				existingEntries = ee
			}
			agentsGroup[ev.Vendor] = append(existingEntries, entry)
		}
	}

	root["agents"] = agentsGroup
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
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
