package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Measured, not reconstructed: this is the real SubagentStop payload from
// Claude Code 2.1.224, captured 2026-08-07 and committed verbatim (bar the
// redaction) at
// docs/superpowers/specs/agents/fixtures/2026-08-07-claude-code-hook-payloads/cc-subagent-stop.json.
//
// Two deliberate substitutions: agent_transcript_path is PLACEHOLDER so the
// test can point it at a real temp file, and last_assistant_message carries a
// SECRET-LEAK canary in place of the redaction marker so the leak assertion
// below has something to catch.
//
// Note what is NOT here: there is no turn_id. Claude Code names the per-turn
// identifier prompt_id. The earlier reconstruction asserted turn_id and was
// wrong about it.
const ccSubagentStopPayload = `{
  "agent_id": "a4b36fbbd89734e03",
  "agent_transcript_path": "PLACEHOLDER",
  "agent_type": "Explore",
  "background_tasks": [],
  "cwd": "/private/tmp/ccprobe",
  "effort": {
    "level": "xhigh"
  },
  "hook_event_name": "SubagentStop",
  "last_assistant_message": "SECRET-LEAK",
  "permission_mode": "default",
  "prompt_id": "eb11ab17-1eef-4d99-b1bb-5d5bd70bb913",
  "session_crons": [],
  "session_id": "1bd3be63-bb4a-4de5-a9a5-6aefd94b64c1",
  "stop_hook_active": false,
  "transcript_path": "/Users/nilbot/.claude/projects/-private-tmp-ccprobe/1bd3be63-bb4a-4de5-a9a5-6aefd94b64c1.jsonl"
}`

// The real SubagentStart payload from the same capture. It is a different
// shape from SubagentStop in one way that matters: no agent_transcript_path.
const ccSubagentStartPayload = `{
  "agent_id": "a4b36fbbd89734e03",
  "agent_type": "Explore",
  "cwd": "/private/tmp/ccprobe",
  "hook_event_name": "SubagentStart",
  "prompt_id": "eb11ab17-1eef-4d99-b1bb-5d5bd70bb913",
  "session_id": "1bd3be63-bb4a-4de5-a9a5-6aefd94b64c1",
  "transcript_path": "/Users/nilbot/.claude/projects/-private-tmp-ccprobe/1bd3be63-bb4a-4de5-a9a5-6aefd94b64c1.jsonl"
}`

func TestClaudeCodeBuildsVerifiedSubagentTrace(t *testing.T) {
	// A transcript on disk with the sidecar the harness writes at spawn time.
	dir := t.TempDir()
	sub := filepath.Join(dir, "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(sub, "agent-a4b36fbbd89734e03.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The measured sidecar shape: agentType, description, toolUseId, spawnDepth.
	sidecar := filepath.Join(sub, "agent-a4b36fbbd89734e03.meta.json")
	meta, _ := json.Marshal(map[string]any{
		"agentType":   "Explore",
		"description": "Find the retry window",
		"toolUseId":   "toolu_015tEyu7yWhHTd9umyQnANYw",
		"spawnDepth":  1,
	})
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
	if tr.AgentID != "a4b36fbbd89734e03" || tr.AgentType != "Explore" {
		t.Errorf("agent fields wrong: %+v", tr)
	}
	// The pass-through half of Build. Cwd in particular is what the record's
	// provenance is anchored to, so it has to survive the trip.
	if tr.Event != SubagentStop {
		t.Errorf("Event = %q, want %q", tr.Event, SubagentStop)
	}
	if tr.SessionID != "1bd3be63-bb4a-4de5-a9a5-6aefd94b64c1" {
		t.Errorf("SessionID = %q, want it carried from the payload", tr.SessionID)
	}
	// Claude Code has no turn_id; prompt_id is the per-turn identifier, and the
	// record's turn_id must be populated from it rather than left empty.
	if tr.TurnID != "eb11ab17-1eef-4d99-b1bb-5d5bd70bb913" {
		t.Errorf("TurnID = %q, want it carried from prompt_id", tr.TurnID)
	}
	if tr.Cwd != "/private/tmp/ccprobe" {
		t.Errorf("Cwd = %q, want %q", tr.Cwd, "/private/tmp/ccprobe")
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
	// The agent fields are populated on purpose: a session event that copied
	// them through would be indistinguishable from a subagent event in the
	// record, so the guard in Build has to drop them even when they are there.
	tr := Build(a, SessionStart, Payload{
		SessionID:      "019fdcab-9733",
		AgentID:        "leaked-agent",
		AgentType:      "leaked-type",
		TranscriptPath: "/Users/n/.claude/projects/p/019fdcab-9733.jsonl",
	})
	if !tr.PointerVerified {
		t.Fatal("session events must verify against session_id")
	}
	if tr.AgentID != "" {
		t.Fatalf("AgentID = %q, want empty for a session event", tr.AgentID)
	}
	if tr.AgentType != "" {
		t.Fatalf("AgentType = %q, want empty for a session event", tr.AgentType)
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

// SubagentStart was measured on 2026-08-07: Claude Code does emit it, so the
// adapter is right to wire it. If a future version stops emitting it, this test
// is not what will notice -- a re-capture is -- but the declaration is asserted
// here so removing it is a deliberate act.
func TestClaudeCodeDeclaresSubagentStart(t *testing.T) {
	a, _ := Get("claude-code")
	var vendor string
	for _, ev := range a.Events() {
		if ev.Semantic == SubagentStart {
			vendor = ev.Vendor
		}
	}
	if vendor != "SubagentStart" {
		t.Fatalf("SubagentStart vendor = %q, want %q (measured: the event exists)", vendor, "SubagentStart")
	}
}

// The measured SubagentStart payload carries no agent_transcript_path -- the
// only path in it is the parent session's. The pointer therefore cannot verify
// against agent_id, and the record must say so rather than claim a verified
// pointer to a transcript that is not the subagent's.
func TestClaudeCodeSubagentStartHasNoAgentTranscript(t *testing.T) {
	p, err := Decode(strings.NewReader(ccSubagentStartPayload))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if p.AgentTranscriptPath != "" {
		t.Fatalf("AgentTranscriptPath = %q, want empty: SubagentStart does not carry one", p.AgentTranscriptPath)
	}

	a, _ := Get("claude-code")
	tr := Build(a, SubagentStart, p)

	if tr.PointerVerified {
		t.Error("PointerVerified = true, want false: the parent transcript is not the subagent's")
	}
	if tr.AgentID != "a4b36fbbd89734e03" {
		t.Errorf("AgentID = %q, want it carried", tr.AgentID)
	}
	if tr.TurnID != "eb11ab17-1eef-4d99-b1bb-5d5bd70bb913" {
		t.Errorf("TurnID = %q, want it carried from prompt_id", tr.TurnID)
	}
}

// turn_id wins over prompt_id when a payload somehow carries both, so a harness
// growing the second name later does not change which value is recorded.
func TestTurnIDPrefersTurnIDOverPromptID(t *testing.T) {
	a, _ := Get("claude-code")
	tr := Build(a, Stop, Payload{TurnID: "turn-wins", PromptID: "prompt-loses"})
	if tr.TurnID != "turn-wins" {
		t.Fatalf("TurnID = %q, want %q", tr.TurnID, "turn-wins")
	}
}

func TestClaudeCodeDeclaresDescriptionCapability(t *testing.T) {
	a, _ := Get("claude-code")
	if !a.Capabilities().Description {
		t.Fatal("Claude Code supplies descriptions via the spawn-time sidecar")
	}
}
