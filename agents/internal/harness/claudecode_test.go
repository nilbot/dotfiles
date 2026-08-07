package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Constructed from the payload shape recorded in spec "Measured facts >
// Claude Code". Task 8 captures a live payload and reconciles this against it.
const ccSubagentStopPayload = `{
  "hook_event_name": "SubagentStop",
  "session_id": "019fdcab-9733-72e3-ba7c-d2e0cc7fb334",
  "agent_id": "a4e4a1bc424b2047f",
  "agent_type": "Explore",
  "cwd": "/Users/n/work/myrepo",
  "agent_transcript_path": "PLACEHOLDER",
  "last_assistant_message": "SECRET-LEAK"
}`

func TestClaudeCodeBuildsVerifiedSubagentTrace(t *testing.T) {
	// A transcript on disk with the sidecar the harness writes at spawn time.
	dir := t.TempDir()
	sub := filepath.Join(dir, "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(sub, "agent-a4e4a1bc424b2047f.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(sub, "agent-a4e4a1bc424b2047f.meta.json")
	meta, _ := json.Marshal(map[string]string{"description": "Find the retry window"})
	if err := os.WriteFile(sidecar, meta, 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := Decode(strings.NewReader(strings.Replace(ccSubagentStopPayload, "PLACEHOLDER", transcript, 1)))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	a, _ := Get("claude-code")
	tr := Build(a, SubagentStop, p)

	if tr.Transcript != transcript {
		t.Errorf("Transcript = %q, want %q", tr.Transcript, transcript)
	}
	if !tr.PointerVerified {
		t.Error("PointerVerified = false, want true")
	}
	if tr.AgentID != "a4e4a1bc424b2047f" || tr.AgentType != "Explore" {
		t.Errorf("agent fields wrong: %+v", tr)
	}
	if tr.Description != "Find the retry window" {
		t.Errorf("Description = %q, want it read from the sidecar", tr.Description)
	}
	if strings.Contains(tr.Description+tr.Transcript, "SECRET-LEAK") {
		t.Error("forbidden content reached the trace")
	}
}

func TestClaudeCodeDescriptionEmptyWithoutSidecar(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "agent-deadbeef.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a, _ := Get("claude-code")
	tr := Build(a, SubagentStop, Payload{AgentID: "deadbeef", AgentTranscriptPath: transcript})

	if tr.Description != "" {
		t.Fatalf("Description = %q, want empty when there is no sidecar", tr.Description)
	}
	if !tr.PointerVerified {
		t.Fatal("a missing sidecar must not invalidate the pointer")
	}
}

func TestClaudeCodeSessionEventsKeyOnSessionID(t *testing.T) {
	a, _ := Get("claude-code")
	tr := Build(a, SessionStart, Payload{
		SessionID:      "019fdcab-9733",
		TranscriptPath: "/Users/n/.claude/projects/p/019fdcab-9733.jsonl",
	})
	if !tr.PointerVerified {
		t.Fatal("session events must verify against session_id")
	}
	if tr.AgentID != "" {
		t.Fatalf("AgentID = %q, want empty for a session event", tr.AgentID)
	}
}

// When a subagent event carries both paths and neither contains the agent id,
// the pointer degrades to the subagent's own transcript, never to the parent
// session's. Only the candidate order enforces that.
func TestClaudeCodeSubagentDegradesToAgentTranscript(t *testing.T) {
	a, _ := Get("claude-code")
	tr := Build(a, SubagentStop, Payload{
		SessionID:           "019fdcab-9733",
		AgentID:             "an-id-in-neither-path",
		AgentTranscriptPath: "/Users/n/.claude/projects/p/subagents/agent-renamed.jsonl",
		TranscriptPath:      "/Users/n/.claude/projects/p/019fdcab-9733.jsonl",
	})
	if tr.PointerVerified {
		t.Error("PointerVerified = true, want false when the agent id matches no path")
	}
	if tr.Transcript != "/Users/n/.claude/projects/p/subagents/agent-renamed.jsonl" {
		t.Errorf("Transcript = %q, want the agent transcript", tr.Transcript)
	}
}

func TestClaudeCodeDeclaresDescriptionCapability(t *testing.T) {
	a, _ := Get("claude-code")
	if !a.Capabilities().Description {
		t.Fatal("Claude Code supplies descriptions via the spawn-time sidecar")
	}
}
