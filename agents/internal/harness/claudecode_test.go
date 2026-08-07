package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The payloads captured from Claude Code 2.1.224 on 2026-08-07, committed as
// fixtures. The tests below read them from disk rather than carrying a pasted
// copy, so that re-capturing on a version bump and overwriting these files is
// what fails this suite when the vendor contract moves. An inline copy could
// not do that: it would silently keep asserting the old shape.
const ccFixtureDir = "../../../docs/superpowers/specs/agents/fixtures/2026-08-07-claude-code-hook-payloads"

const (
	ccSessionStartFixture  = "cc-session-start.json"
	ccSubagentStartFixture = "cc-subagent-start.json"
	ccSubagentStopFixture  = "cc-subagent-stop.json"
	ccStopFixture          = "cc-stop.json"
)

// Measured ids, shared by the capture's four payloads. Named once so a mutated
// fixture fails loudly on the value rather than on an unexplained mismatch.
const (
	ccSessionID = "1bd3be63-bb4a-4de5-a9a5-6aefd94b64c1"
	ccPromptID  = "eb11ab17-1eef-4d99-b1bb-5d5bd70bb913"
	ccAgentID   = "a4b36fbbd89734e03"
	ccCwd       = "/private/tmp/ccprobe"
)

// readFixture returns the raw committed bytes.
//
// A missing or unreadable file is fatal rather than skipped: a golden test that
// quietly passes when its input has gone is worse than no golden test, which is
// the exact failure this file was rewritten to close.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ccFixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// loadFixture decodes one committed payload through the real decoder.
func loadFixture(t *testing.T, name string) Payload {
	t.Helper()
	p, err := Decode(bytes.NewReader(readFixture(t, name)))
	if err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return p
}

// The committed fixtures are redacted, so none of them can exercise the leak
// path -- their last_assistant_message is already the redaction marker, and
// TestClaudeCodeFixturesStayRedacted asserts it stays that way. This inline
// payload is the measured SubagentStop shape with a canary in that field, which
// is the only way left to assert that forbidden content cannot reach a Trace.
const ccLeakCanaryPayload = `{
  "agent_id": "` + ccAgentID + `",
  "agent_transcript_path": "PLACEHOLDER",
  "agent_type": "Explore",
  "cwd": "` + ccCwd + `",
  "hook_event_name": "SubagentStop",
  "last_assistant_message": "SECRET-LEAK",
  "prompt_id": "` + ccPromptID + `",
  "session_id": "` + ccSessionID + `",
  "tool_input": {"message": "gAAAAABSECRET"},
  "tool_response": {"stdout": "SECRET-LEAK"}
}`

func TestClaudeCodeBuildsVerifiedSubagentTrace(t *testing.T) {
	// A transcript on disk with the sidecar the harness writes at spawn time.
	dir := t.TempDir()
	sub := filepath.Join(dir, "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(sub, "agent-"+ccAgentID+".jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The measured sidecar shape: agentType, description, toolUseId, spawnDepth.
	sidecar := filepath.Join(sub, "agent-"+ccAgentID+".meta.json")
	meta, _ := json.Marshal(map[string]any{
		"agentType":   "Explore",
		"description": "Find the retry window",
		"toolUseId":   "toolu_015tEyu7yWhHTd9umyQnANYw",
		"spawnDepth":  1,
	})
	if err := os.WriteFile(sidecar, meta, 0o644); err != nil {
		t.Fatal(err)
	}

	// The one substitution the fixture needs: the captured transcript path
	// points at a machine-bound file that is not here. Rewriting the decoded
	// field rather than the JSON text keeps the committed bytes authoritative
	// for every other field.
	p := loadFixture(t, ccSubagentStopFixture)
	if p.AgentTranscriptPath == "" {
		t.Fatal("fixture lost agent_transcript_path; SubagentStop must carry one")
	}
	p.AgentTranscriptPath = transcript

	a, _ := Get("claude-code")
	tr := Build(a, SubagentStop, p)

	if tr.Transcript != transcript {
		t.Errorf("Transcript = %q, want %q", tr.Transcript, transcript)
	}
	if !tr.PointerVerified {
		t.Error("PointerVerified = false, want true")
	}
	if tr.AgentID != ccAgentID || tr.AgentType != "Explore" {
		t.Errorf("agent fields wrong: %+v", tr)
	}
	// The pass-through half of Build. Cwd in particular is what the record's
	// provenance is anchored to, so it has to survive the trip.
	if tr.Event != SubagentStop {
		t.Errorf("Event = %q, want %q", tr.Event, SubagentStop)
	}
	if tr.SessionID != ccSessionID {
		t.Errorf("SessionID = %q, want it carried from the payload", tr.SessionID)
	}
	// Claude Code has no turn_id; prompt_id is the per-turn identifier, and the
	// record's turn_id must be populated from it rather than left empty.
	if tr.TurnID != ccPromptID {
		t.Errorf("TurnID = %q, want it carried from prompt_id", tr.TurnID)
	}
	if tr.Cwd != ccCwd {
		t.Errorf("Cwd = %q, want %q", tr.Cwd, ccCwd)
	}
	if tr.Description != "Find the retry window" {
		t.Errorf("Description = %q, want it read from the sidecar", tr.Description)
	}
}

// The leak assertion the redacted fixtures cannot make. Nothing forbidden in
// the payload may surface in any Trace field, including via the sidecar read.
func TestClaudeCodeForbiddenContentCannotReachTrace(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "agent-"+ccAgentID+".jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := Decode(strings.NewReader(strings.Replace(ccLeakCanaryPayload, "PLACEHOLDER", transcript, 1)))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	a, _ := Get("claude-code")
	tr := Build(a, SubagentStop, p)

	joined := strings.Join([]string{
		tr.Event, tr.SessionID, tr.TurnID, tr.AgentID, tr.AgentType,
		tr.Description, tr.Transcript, tr.Cwd,
	}, "|")
	for _, bad := range []string{"SECRET-LEAK", "gAAAAABSECRET"} {
		if strings.Contains(joined, bad) {
			t.Errorf("forbidden content %q reached the trace: %s", bad, joined)
		}
	}
}

// Every committed fixture must stay redacted. Re-capturing on a version bump
// and committing the raw payloads would publish message text into a tracked
// file, which is the hazard the Codex capture found live. This is the guard on
// the fixtures themselves, not on the code that reads them.
func TestClaudeCodeFixturesStayRedacted(t *testing.T) {
	const marker = "<REDACTED — see spec 3.2>"
	for _, name := range []string{
		ccSessionStartFixture, ccSubagentStartFixture, ccSubagentStopFixture, ccStopFixture,
	} {
		var raw map[string]any
		if err := json.Unmarshal(readFixture(t, name), &raw); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, forbidden := range []string{"last_assistant_message", "tool_input", "tool_response"} {
			v, ok := raw[forbidden]
			if !ok {
				continue // the event does not carry it; nothing to redact
			}
			if s, _ := v.(string); s != marker {
				t.Errorf("%s: %s = %#v, want the redaction marker", name, forbidden, v)
			}
		}
	}
}

// All four measured shapes, decoded through the real decoder. SessionStart and
// Stop were previously documentation-only: nothing decoded them, so a change to
// how either is read had no test to fail.
func TestClaudeCodeFixturesDecodeToMeasuredShape(t *testing.T) {
	t.Run("SessionStart", func(t *testing.T) {
		p := loadFixture(t, ccSessionStartFixture)
		if p.HookEventName != "SessionStart" {
			t.Errorf("HookEventName = %q", p.HookEventName)
		}
		if p.Source != "startup" {
			t.Errorf("Source = %q, want %q", p.Source, "startup")
		}
		if p.SessionID != ccSessionID || p.Cwd != ccCwd {
			t.Errorf("session fields wrong: %+v", p)
		}
		if !strings.HasSuffix(p.TranscriptPath, ccSessionID+".jsonl") {
			t.Errorf("TranscriptPath = %q, want it to name the session", p.TranscriptPath)
		}
		// Measured: SessionStart carries no turn identifier under either name,
		// which is why the record's turn_id is omitempty.
		if p.TurnID != "" || p.PromptID != "" {
			t.Errorf("SessionStart carried a turn identifier: turn_id=%q prompt_id=%q", p.TurnID, p.PromptID)
		}
		if p.AgentID != "" || p.AgentTranscriptPath != "" {
			t.Errorf("SessionStart carried agent fields: %+v", p)
		}
	})

	t.Run("SubagentStart", func(t *testing.T) {
		p := loadFixture(t, ccSubagentStartFixture)
		if p.HookEventName != "SubagentStart" {
			t.Errorf("HookEventName = %q", p.HookEventName)
		}
		if p.AgentID != ccAgentID || p.AgentType != "Explore" {
			t.Errorf("agent fields wrong: %+v", p)
		}
		if p.PromptID != ccPromptID || p.TurnID != "" {
			t.Errorf("turn identifier wrong: turn_id=%q prompt_id=%q", p.TurnID, p.PromptID)
		}
	})

	t.Run("SubagentStop", func(t *testing.T) {
		p := loadFixture(t, ccSubagentStopFixture)
		if p.HookEventName != "SubagentStop" {
			t.Errorf("HookEventName = %q", p.HookEventName)
		}
		if p.AgentID != ccAgentID || p.AgentType != "Explore" {
			t.Errorf("agent fields wrong: %+v", p)
		}
		if p.PromptID != ccPromptID || p.TurnID != "" {
			t.Errorf("turn identifier wrong: turn_id=%q prompt_id=%q", p.TurnID, p.PromptID)
		}
		// The asymmetry that matters: at Stop the child's path is present and
		// transcript_path is the parent's.
		if !strings.Contains(p.AgentTranscriptPath, "agent-"+ccAgentID) {
			t.Errorf("AgentTranscriptPath = %q, want it to name the agent", p.AgentTranscriptPath)
		}
		if !strings.HasSuffix(p.TranscriptPath, ccSessionID+".jsonl") {
			t.Errorf("TranscriptPath = %q, want the parent session's", p.TranscriptPath)
		}
	})

	t.Run("Stop", func(t *testing.T) {
		p := loadFixture(t, ccStopFixture)
		if p.HookEventName != "Stop" {
			t.Errorf("HookEventName = %q", p.HookEventName)
		}
		if p.SessionID != ccSessionID || p.Cwd != ccCwd {
			t.Errorf("session fields wrong: %+v", p)
		}
		if p.PromptID != ccPromptID || p.TurnID != "" {
			t.Errorf("turn identifier wrong: turn_id=%q prompt_id=%q", p.TurnID, p.PromptID)
		}
		// Stop is a session event: it carries no agent fields at all.
		if p.AgentID != "" || p.AgentType != "" || p.AgentTranscriptPath != "" {
			t.Errorf("Stop carried agent fields: %+v", p)
		}

		// And Build must key it on the session, not on an agent.
		a, _ := Get("claude-code")
		tr := Build(a, Stop, p)
		if !tr.PointerVerified {
			t.Error("PointerVerified = false, want true: the session id is in its own transcript path")
		}
		if tr.TurnID != ccPromptID {
			t.Errorf("TurnID = %q, want it carried from prompt_id", tr.TurnID)
		}
	})
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
	p := loadFixture(t, ccSubagentStartFixture)
	if p.AgentTranscriptPath != "" {
		t.Fatalf("AgentTranscriptPath = %q, want empty: SubagentStart does not carry one", p.AgentTranscriptPath)
	}

	a, _ := Get("claude-code")
	tr := Build(a, SubagentStart, p)

	if tr.PointerVerified {
		t.Error("PointerVerified = true, want false: the parent transcript is not the subagent's")
	}
	// The pointer degrades to the parent session's transcript. That is the
	// honest result, but a consumer that ignores PointerVerified would mistake
	// it for the subagent's -- so pin which file it actually names.
	if tr.Transcript != p.TranscriptPath {
		t.Errorf("Transcript = %q, want the parent session transcript %q", tr.Transcript, p.TranscriptPath)
	}
	if tr.AgentID != ccAgentID {
		t.Errorf("AgentID = %q, want it carried", tr.AgentID)
	}
	if tr.TurnID != ccPromptID {
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
