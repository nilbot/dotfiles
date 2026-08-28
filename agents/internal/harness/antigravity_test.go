package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAntigravityAdapterMetadata(t *testing.T) {
	a, ok := Get("antigravity")
	if !ok {
		t.Fatal("antigravity adapter not found in Get()")
	}
	if got := a.Name(); got != "antigravity" {
		t.Errorf("Name() = %q, want %q", got, "antigravity")
	}
	if got := a.HarnessDir(); got != ".agents" {
		t.Errorf("HarnessDir() = %q, want %q", got, ".agents")
	}
	if a.NeedsSkillsSymlink() {
		t.Errorf("NeedsSkillsSymlink() = true, want false")
	}
	if a.Capabilities().Description {
		t.Errorf("Capabilities().Description = true, want false")
	}
	wantEvents := []Event{{Semantic: Stop, Vendor: "Stop"}}
	if got := a.Events(); !reflect.DeepEqual(got, wantEvents) {
		t.Errorf("Events() = %+v, want %+v", got, wantEvents)
	}
	if got := a.Describe(Payload{}, ""); got != "" {
		t.Errorf("Describe() = %q, want empty string", got)
	}
	root := t.TempDir()
	wantConfig := filepath.Join(root, ".agents", "hooks.json")
	if got := a.WireConfigPath(root); got != wantConfig {
		t.Errorf("WireConfigPath() = %q, want %q", got, wantConfig)
	}
	steps := a.TrustSteps(root)
	if len(steps) != 2 {
		t.Fatalf("TrustSteps() returned %d steps, want 2", len(steps))
	}
	if !strings.Contains(steps[0], "Antigravity App") || !strings.Contains(steps[1], "Antigravity CLI") {
		t.Errorf("unexpected TrustSteps(): %v", steps)
	}

	foundInAll := false
	for _, adapter := range All() {
		if adapter.Name() == "antigravity" {
			foundInAll = true
			break
		}
	}
	if !foundInAll {
		t.Error("antigravity not found in All()")
	}

	if !knownHarness("antigravity") {
		t.Error("knownHarness(\"antigravity\") = false, want true")
	}
}

func TestAntigravityWiring(t *testing.T) {
	tmp := t.TempDir()
	a, ok := Get("antigravity")
	if !ok {
		t.Fatal("antigravity adapter not found")
	}
	bin := "/bin/agents"
	if err := a.Wire(tmp, bin); err != nil {
		t.Fatalf("Wire() error = %v", err)
	}

	cfgPath := a.WireConfigPath(tmp)
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	agentsObj, ok := root["agents"].(map[string]any)
	if !ok {
		t.Fatalf("expected top-level 'agents' object, got %v", root)
	}
	stopList, ok := agentsObj["Stop"].([]any)
	if !ok || len(stopList) != 1 {
		t.Fatalf("expected 1 Stop hook in 'agents', got %v", agentsObj["Stop"])
	}
	stopEntry, ok := stopList[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map in Stop entry, got %T", stopList[0])
	}
	if stopEntry["type"] != "command" {
		t.Errorf("Stop entry type = %v, want command", stopEntry["type"])
	}
	wantCmd := HookCommand(bin, "antigravity", Stop)
	if stopEntry["command"] != wantCmd {
		t.Errorf("Stop entry command = %v, want %v", stopEntry["command"], wantCmd)
	}

	// Verify foreign hook preservation
	agentsDir := filepath.Join(tmp, ".agents")
	existingContent := `{
  "custom_root_key": "preserved_val",
  "agents": {
    "Stop": [
      {
        "type": "command",
        "command": "custom-linter --check"
      }
    ],
    "PreToolUse": [
      {
        "matcher": "bash",
        "hooks": [
          {
            "type": "command",
            "command": "foreign-safety-tool"
          }
        ]
      }
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(agentsDir, "hooks.json"), []byte(existingContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.Wire(tmp, bin); err != nil {
		t.Fatalf("Wire() with foreign hooks error = %v", err)
	}

	data, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var mergedRoot map[string]any
	if err := json.Unmarshal(data, &mergedRoot); err != nil {
		t.Fatal(err)
	}
	if mergedRoot["custom_root_key"] != "preserved_val" {
		t.Errorf("custom_root_key not preserved: got %v", mergedRoot["custom_root_key"])
	}
	mergedAgents := mergedRoot["agents"].(map[string]any)
	mergedStop := mergedAgents["Stop"].([]any)
	if len(mergedStop) != 2 {
		t.Fatalf("expected 2 Stop hooks (foreign + ours), got %d: %v", len(mergedStop), mergedStop)
	}
	preTool := mergedAgents["PreToolUse"].([]any)
	if len(preTool) != 1 {
		t.Fatalf("expected 1 PreToolUse matcher group, got %d", len(preTool))
	}
}

func TestAntigravityWiringIdempotency(t *testing.T) {
	tmp := t.TempDir()
	a, ok := Get("antigravity")
	if !ok {
		t.Fatal("antigravity adapter not found")
	}
	bin := "/bin/agents"
	for i := 0; i < 3; i++ {
		if err := a.Wire(tmp, bin); err != nil {
			t.Fatalf("Wire() pass %d error = %v", i+1, err)
		}
	}

	data, err := os.ReadFile(a.WireConfigPath(tmp))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	agentsObj, ok := root["agents"].(map[string]any)
	if !ok {
		t.Fatalf("expected agents map, got %v", root)
	}
	stopList, ok := agentsObj["Stop"].([]any)
	if !ok || len(stopList) != 1 {
		t.Fatalf("expected exactly 1 Stop hook after 3 wires, got %v", agentsObj["Stop"])
	}
}

func TestAntigravityPayloadRedaction(t *testing.T) {
	raw := `{
		"conversationId": "conv-123",
		"transcriptPath": "/tmp/transcript.jsonl",
		"lastUserInput": "SECRET_USER_INPUT_DO_NOT_LEAK",
		"raw_tool_input": "SECRET_TOOL_INPUT"
	}`
	p, err := Decode(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode() err = %v", err)
	}
	if p.ConversationID != "conv-123" {
		t.Errorf("ConversationID = %s, want conv-123", p.ConversationID)
	}
	data, _ := json.Marshal(p)
	if strings.Contains(string(data), "SECRET_USER_INPUT") || strings.Contains(string(data), "SECRET_TOOL_INPUT") {
		t.Errorf("Payload leaked unmapped forbidden field: %s", data)
	}
}

func TestAntigravityAbsoluteBinaryPath(t *testing.T) {
	tmp := t.TempDir()
	a, ok := Get("antigravity")
	if !ok {
		t.Fatal("antigravity adapter not found")
	}
	bin := filepath.Join(tmp, "path with spaces", "agents")
	if err := a.Wire(tmp, bin); err != nil {
		t.Fatalf("Wire() error = %v", err)
	}

	data, err := os.ReadFile(a.WireConfigPath(tmp))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	agentsObj := root["agents"].(map[string]any)
	stopList := agentsObj["Stop"].([]any)
	stopEntry := stopList[0].(map[string]any)
	cmd := stopEntry["command"].(string)
	if !strings.HasPrefix(cmd, "'"+bin+"'") {
		t.Fatalf("expected POSIX-quoted binary path in command, got %q", cmd)
	}
	if !IsOwnedHookCommand(cmd) {
		t.Fatalf("command should be recognized as owned hook command: %q", cmd)
	}
}

func TestAntigravityBuildPayloadNormalization(t *testing.T) {
	a, ok := Get("antigravity")
	if !ok {
		t.Fatal("antigravity adapter not found")
	}

	raw := `{
		"conversationId": "session-agy-456",
		"workspacePaths": ["/Users/nilbot/project"],
		"transcriptPath": "/Users/nilbot/.gemini/antigravity/brain/session-agy-456/transcript.jsonl"
	}`
	p, err := Decode(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode() err = %v", err)
	}

	tr := Build(a, Stop, p)
	if tr.SessionID != "session-agy-456" {
		t.Errorf("SessionID = %q, want session-agy-456", tr.SessionID)
	}
	if tr.Cwd != "/Users/nilbot/project" {
		t.Errorf("Cwd = %q, want /Users/nilbot/project", tr.Cwd)
	}
	if tr.TurnID != "" {
		t.Errorf("TurnID = %q, want empty string", tr.TurnID)
	}
	if tr.Transcript != "/Users/nilbot/.gemini/antigravity/brain/session-agy-456/transcript.jsonl" {
		t.Errorf("Transcript = %q, want resolved transcript path", tr.Transcript)
	}
	if !tr.PointerVerified {
		t.Errorf("PointerVerified = false, want true because session-agy-456 is in path")
	}

	// Verify session_id takes precedence over conversationId if both set
	p2 := Payload{
		SessionID:      "explicit-session",
		ConversationID: "fallback-conversation",
		Cwd:            "/explicit/cwd",
		WorkspacePaths: []string{"/fallback/workspace"},
	}
	tr2 := Build(a, Stop, p2)
	if tr2.SessionID != "explicit-session" {
		t.Errorf("SessionID = %q, want explicit-session", tr2.SessionID)
	}
	if tr2.Cwd != "/explicit/cwd" {
		t.Errorf("Cwd = %q, want /explicit/cwd", tr2.Cwd)
	}
}
