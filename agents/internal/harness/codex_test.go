package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The payloads captured from Codex CLI 0.147.0 on 2026-08-07, committed as
// fixtures. Read from disk for the same reason the Claude Code suite does:
// re-capturing on a version bump overwrites these files, and that is what has
// to fail this suite when the vendor contract moves.
const cxFixtureDir = "../../../docs/superpowers/specs/agents/fixtures/2026-08-07-codex-hook-payloads"

const (
	cxSessionStartFixture   = "codex-session-start.json"
	cxSubagentStartFixture  = "codex-subagent-start.json"
	cxSubagentStart2Fixture = "codex-subagent-start-2.json"
	cxSubagentStopFixture   = "codex-subagent-stop.json"
	cxSubagentStop2Fixture  = "codex-subagent-stop-2.json"
	cxStopFixture           = "codex-stop.json"
	cxPreToolFixture        = "codex-pre-tool.json"
)

// Measured ids from the capture. Named once so a mutated fixture fails on the
// value rather than on an unexplained mismatch.
const (
	cxSessionID = "019fdcab-9733-72e3-ba7c-d2e0cc7fb334"
	// The parent's own turn, distinct from either subagent's.
	cxSessionTurnID = "019fdcab-97c0-71b2-9b13-8e222925686b"

	cxAgentID    = "019fdcab-ac94-7502-a322-d01f047c274a"
	cxAgentTurn  = "019fdcab-ad07-7f23-bba4-e3261c09eae7"
	cxAgent2ID   = "019fdcab-d75b-7583-9389-d61137bce0a9"
	cxAgent2Turn = "019fdcab-d7cd-7720-87bf-43234da61ab9"

	cxCwd = "/private/tmp/claude-501/-Users-nilbot-dotfiles/84837ec0-e818-47e1-a08f-7a2f5b264d1e/scratchpad/agentprobe"

	// The session's own rollout file; at SubagentStop this is what
	// transcript_path holds, and picking it would be the interesting bug.
	cxSessionTranscript = "/Users/nilbot/.codex/sessions/2026/08/07/rollout-2026-08-07T15-41-00-019fdcab-9733-72e3-ba7c-d2e0cc7fb334.jsonl"
	cxAgentTranscript   = "/Users/nilbot/.codex/sessions/2026/08/07/rollout-2026-08-07T15-41-06-019fdcab-ac94-7502-a322-d01f047c274a.jsonl"
	cxAgent2Transcript  = "/Users/nilbot/.codex/sessions/2026/08/07/rollout-2026-08-07T15-41-17-019fdcab-d75b-7583-9389-d61137bce0a9.jsonl"
)

// The marker every forbidden field in these fixtures was replaced with.
const cxRedactionMarker = "<REDACTED — see spec 3.2>"

// readCodexFixture returns the raw committed bytes. A missing or unreadable
// file is fatal, never skipped: a golden test that quietly passes when its
// input has gone is worse than no golden test.
func readCodexFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(cxFixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// loadCodexFixture decodes one committed payload through the real decoder.
func loadCodexFixture(t *testing.T, name string) Payload {
	t.Helper()
	p, err := Decode(bytes.NewReader(readCodexFixture(t, name)))
	if err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return p
}

func mustGet(t *testing.T, name string) Adapter {
	t.Helper()
	a, ok := Get(name)
	if !ok {
		t.Fatalf("adapter %q not registered", name)
	}
	return a
}

// SubagentStop hands over both transcripts and it is the parent that sits in
// transcript_path. Picking the wrong one would point every subagent record at
// the session that spawned it, which is worse than not recording.
func TestCodexSubagentStopPicksTheChildTranscript(t *testing.T) {
	p := loadCodexFixture(t, cxSubagentStopFixture)
	tr := Build(mustGet(t, "codex"), SubagentStop, p)

	if tr.AgentID != cxAgentID {
		t.Fatalf("AgentID = %q, want %q", tr.AgentID, cxAgentID)
	}
	if !strings.Contains(filepath.Base(tr.Transcript), cxAgentID) {
		t.Fatalf("Transcript = %q, want the child's (basename contains %s)", tr.Transcript, cxAgentID)
	}
	// Named in full as well, so "some path mentioning the agent" cannot pass for
	// the right file.
	if tr.Transcript != cxAgentTranscript {
		t.Errorf("Transcript = %q, want %q", tr.Transcript, cxAgentTranscript)
	}
	if tr.Transcript == cxSessionTranscript {
		t.Error("Transcript is the parent session's rollout, not the subagent's")
	}
	if !tr.PointerVerified {
		t.Fatal("PointerVerified = false, want true")
	}
	if tr.TurnID != cxAgentTurn {
		t.Errorf("TurnID = %q; each subagent gets its own turn", tr.TurnID)
	}
	if tr.SessionID != cxSessionID {
		t.Errorf("SessionID = %q, want %q", tr.SessionID, cxSessionID)
	}
	if tr.AgentType != "default" {
		t.Errorf("AgentType = %q, want %q", tr.AgentType, "default")
	}
	if tr.Cwd != cxCwd {
		t.Errorf("Cwd = %q, want %q", tr.Cwd, cxCwd)
	}
}

// SubagentStart supplies only transcript_path, and it is already the child's --
// the opposite of SubagentStop. This asymmetry is the reason pointer resolution
// derives instead of consulting a per-event field map.
//
// Claude Code inverts it: its SubagentStart transcript_path is the *parent's*,
// so a Claude Code subagent-start pointer can never verify while a Codex one
// always can. TestClaudeCodeSubagentStartHasNoAgentTranscript is the other half
// of this pair.
func TestCodexSubagentStartAlsoPicksTheChild(t *testing.T) {
	p := loadCodexFixture(t, cxSubagentStartFixture)
	if p.AgentTranscriptPath != "" {
		t.Fatal("fixture changed: SubagentStart used to omit agent_transcript_path")
	}
	tr := Build(mustGet(t, "codex"), SubagentStart, p)

	// Asserted explicitly: strings.Contains(x, "") is true, so an empty AgentID
	// would make the containment check below pass on nothing at all.
	if tr.AgentID != cxAgentID {
		t.Fatalf("AgentID = %q, want %q", tr.AgentID, cxAgentID)
	}
	if !strings.Contains(filepath.Base(tr.Transcript), tr.AgentID) || !tr.PointerVerified {
		t.Fatalf("Transcript = %q for agent %q, verified=%v", tr.Transcript, tr.AgentID, tr.PointerVerified)
	}
	if tr.Transcript != cxAgentTranscript {
		t.Errorf("Transcript = %q, want %q", tr.Transcript, cxAgentTranscript)
	}
	if tr.TurnID != cxAgentTurn {
		t.Errorf("TurnID = %q, want %q", tr.TurnID, cxAgentTurn)
	}
}

// Session events key on session_id, and the transcript they name is the
// session's own rollout.
func TestCodexSessionEventsVerifyOnSessionID(t *testing.T) {
	// An explicit pair rather than deriving the semantic from the filename: a
	// substring test on the name is one typo away from running both rows
	// through the same branch and asserting nothing about the other.
	for _, tc := range []struct {
		fixture  string
		semantic string
		turnID   string
	}{
		{cxSessionStartFixture, SessionStart, ""},
		{cxStopFixture, Stop, cxSessionTurnID},
	} {
		p := loadCodexFixture(t, tc.fixture)
		tr := Build(mustGet(t, "codex"), tc.semantic, p)

		if !tr.PointerVerified {
			t.Errorf("%s: PointerVerified = false", tc.fixture)
		}
		if tr.Transcript != cxSessionTranscript {
			t.Errorf("%s: Transcript = %q, want the session rollout %q", tc.fixture, tr.Transcript, cxSessionTranscript)
		}
		if tr.AgentID != "" {
			t.Errorf("%s: AgentID = %q, want empty", tc.fixture, tr.AgentID)
		}
		if tr.SessionID != cxSessionID {
			t.Errorf("%s: SessionID = %q, want %q", tc.fixture, tr.SessionID, cxSessionID)
		}
		if tr.TurnID != tc.turnID {
			t.Errorf("%s: TurnID = %q, want %q", tc.fixture, tr.TurnID, tc.turnID)
		}
	}
}

// The assertion above that AgentID comes back empty is vacuous against these
// fixtures -- neither carries agent_id at all, so nothing has to be dropped for
// it to hold. This is the same shape with the fields present, which is the only
// version that can catch a Build that copies them through on a session event.
func TestCodexSessionEventDropsAgentFieldsThatAreThere(t *testing.T) {
	tr := Build(mustGet(t, "codex"), Stop, Payload{
		SessionID:      cxSessionID,
		TurnID:         cxSessionTurnID,
		AgentID:        cxAgentID,
		AgentType:      "default",
		TranscriptPath: cxSessionTranscript,
	})
	if tr.AgentID != "" {
		t.Errorf("AgentID = %q, want empty for a session event", tr.AgentID)
	}
	if tr.AgentType != "" {
		t.Errorf("AgentType = %q, want empty for a session event", tr.AgentType)
	}
	if !tr.PointerVerified || tr.Transcript != cxSessionTranscript {
		t.Errorf("session events must verify against session_id: %q verified=%v", tr.Transcript, tr.PointerVerified)
	}
}

// Both captured subagents, start and stop, resolved independently. One subagent
// cannot show that agent_id is what selects the transcript -- with a single
// child there is only one plausible answer. Two can: each event must land on
// its own child's rollout and its own turn, never on the sibling's and never on
// the shared session's.
func TestCodexPairsEachSubagentToItsOwnTranscript(t *testing.T) {
	a := mustGet(t, "codex")
	for _, tc := range []struct {
		fixture    string
		semantic   string
		agentID    string
		turnID     string
		transcript string
	}{
		{cxSubagentStartFixture, SubagentStart, cxAgentID, cxAgentTurn, cxAgentTranscript},
		{cxSubagentStopFixture, SubagentStop, cxAgentID, cxAgentTurn, cxAgentTranscript},
		{cxSubagentStart2Fixture, SubagentStart, cxAgent2ID, cxAgent2Turn, cxAgent2Transcript},
		{cxSubagentStop2Fixture, SubagentStop, cxAgent2ID, cxAgent2Turn, cxAgent2Transcript},
	} {
		tr := Build(a, tc.semantic, loadCodexFixture(t, tc.fixture))
		if tr.AgentID != tc.agentID {
			t.Errorf("%s: AgentID = %q, want %q", tc.fixture, tr.AgentID, tc.agentID)
		}
		if tr.Transcript != tc.transcript {
			t.Errorf("%s: Transcript = %q, want %q", tc.fixture, tr.Transcript, tc.transcript)
		}
		if !tr.PointerVerified {
			t.Errorf("%s: PointerVerified = false", tc.fixture)
		}
		if tr.TurnID != tc.turnID {
			t.Errorf("%s: TurnID = %q, want %q", tc.fixture, tr.TurnID, tc.turnID)
		}
		if tr.SessionID != cxSessionID {
			t.Errorf("%s: SessionID = %q, want the shared session %q", tc.fixture, tr.SessionID, cxSessionID)
		}
	}
}

// When a subagent event carries both paths and neither names the agent, the
// pointer degrades to the subagent's own transcript, never to the parent
// session's. Only the candidate order enforces that, and no captured Codex
// payload can express it: in every one of them agent_transcript_path does name
// the agent, so a reversed order still finds it on the second try.
func TestCodexSubagentDegradesToAgentTranscript(t *testing.T) {
	const renamed = "/Users/nilbot/.codex/sessions/2026/08/07/rollout-archived.jsonl"
	tr := Build(mustGet(t, "codex"), SubagentStop, Payload{
		SessionID:           cxSessionID,
		AgentID:             cxAgentID,
		AgentTranscriptPath: renamed,
		TranscriptPath:      cxSessionTranscript,
	})
	if tr.PointerVerified {
		t.Error("PointerVerified = true, want false when the agent id matches no path")
	}
	if tr.Transcript != renamed {
		t.Errorf("Transcript = %q, want the agent transcript %q", tr.Transcript, renamed)
	}
}

// Codex has no description field anywhere in any payload. Declaring the gap is
// the point: retrieval for Codex records leans on lane, cwd and agent_type.
//
// TestCodexDeclaresNoDescriptionCapability in wire_codex_test.go asserts the
// declaration; this asserts Build honours it.
func TestCodexBuildProducesNoDescription(t *testing.T) {
	a := mustGet(t, "codex")
	if a.Capabilities().Description {
		t.Fatal("Codex supplies no description; the adapter must say so")
	}
	tr := Build(a, SubagentStop, loadCodexFixture(t, cxSubagentStopFixture))
	if tr.Description != "" {
		t.Fatalf("Description = %q, want empty", tr.Description)
	}

	// Directly, too. Build only calls Describe when the capability is declared,
	// so the body is unreachable today and a Describe that returned payload
	// content would be invisible -- until someone flips the capability. The
	// redaction barrier should not rest on one caller's guard.
	if got := a.Describe(loadCodexFixture(t, cxSubagentStopFixture), cxAgentTranscript); got != "" {
		t.Errorf("Describe = %q, want empty: Codex has no label to read", got)
	}
}

// The redaction promise, checked against a payload that really carries the
// forbidden field.
func TestCodexFixtureCarriesForbiddenFieldAndItIsDropped(t *testing.T) {
	raw := readCodexFixture(t, cxSubagentStopFixture)
	if !strings.Contains(string(raw), "last_assistant_message") {
		t.Fatal("fixture no longer exercises the case this test exists for")
	}
	tr := Build(mustGet(t, "codex"), SubagentStop, loadCodexFixture(t, cxSubagentStopFixture))
	joined := tr.Description + tr.Transcript + tr.AgentType + tr.SessionID + tr.TurnID + tr.AgentID + tr.Cwd + tr.Event
	if strings.Contains(joined, "REDACTED") {
		t.Fatalf("payload content leaked into the trace: %q", joined)
	}
}

// Every committed fixture must stay redacted, and all twelve must still be
// here. Re-capturing on a version bump and committing the raw payloads would
// publish message text into a tracked file -- and the PreToolUse captures are
// the reason that is not hypothetical: they carried encrypted task blobs.
func TestCodexFixturesStayRedactedAndComplete(t *testing.T) {
	entries, err := os.ReadDir(cxFixtureDir)
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	// The capture is 12 payloads. A file that disappears takes its golden
	// assertions with it silently; this is what notices.
	if len(names) != 12 {
		t.Errorf("found %d captured payloads, want 12: %v", len(names), names)
	}

	for _, name := range names {
		var raw map[string]any
		if err := json.Unmarshal(readCodexFixture(t, name), &raw); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, forbidden := range []string{"last_assistant_message", "tool_input", "tool_response"} {
			v, ok := raw[forbidden]
			if !ok {
				continue // the event does not carry it; nothing to redact
			}
			if s, _ := v.(string); s != cxRedactionMarker {
				t.Errorf("%s: %s = %#v, want the redaction marker", name, forbidden, v)
			}
		}
	}
}

// All the measured shapes, decoded through the real decoder, field by field.
// This is what makes editing a committed fixture fatal rather than quietly
// re-baselining what the adapter is tested against.
func TestCodexFixturesDecodeToMeasuredShape(t *testing.T) {
	t.Run("SessionStart", func(t *testing.T) {
		p := loadCodexFixture(t, cxSessionStartFixture)
		if p.HookEventName != "SessionStart" {
			t.Errorf("HookEventName = %q", p.HookEventName)
		}
		if p.Source != "startup" {
			t.Errorf("Source = %q, want %q -- the matcher startup|resume is why", p.Source, "startup")
		}
		if p.SessionID != cxSessionID || p.Cwd != cxCwd {
			t.Errorf("session fields wrong: %+v", p)
		}
		if p.TranscriptPath != cxSessionTranscript {
			t.Errorf("TranscriptPath = %q, want %q", p.TranscriptPath, cxSessionTranscript)
		}
		// Measured: SessionStart carries no turn identifier and no agent fields.
		if p.TurnID != "" || p.PromptID != "" {
			t.Errorf("SessionStart carried a turn identifier: turn_id=%q prompt_id=%q", p.TurnID, p.PromptID)
		}
		if p.AgentID != "" || p.AgentType != "" || p.AgentTranscriptPath != "" {
			t.Errorf("SessionStart carried agent fields: %+v", p)
		}
	})

	t.Run("Stop", func(t *testing.T) {
		p := loadCodexFixture(t, cxStopFixture)
		if p.HookEventName != "Stop" {
			t.Errorf("HookEventName = %q", p.HookEventName)
		}
		if p.SessionID != cxSessionID || p.Cwd != cxCwd {
			t.Errorf("session fields wrong: %+v", p)
		}
		// Codex sends turn_id, not prompt_id. Build reads whichever is present,
		// so this is the half of that contract Codex is responsible for.
		if p.TurnID != cxSessionTurnID || p.PromptID != "" {
			t.Errorf("turn identifier wrong: turn_id=%q prompt_id=%q", p.TurnID, p.PromptID)
		}
		if p.TranscriptPath != cxSessionTranscript {
			t.Errorf("TranscriptPath = %q, want %q", p.TranscriptPath, cxSessionTranscript)
		}
		if p.AgentID != "" || p.AgentType != "" || p.AgentTranscriptPath != "" {
			t.Errorf("Stop carried agent fields: %+v", p)
		}
	})

	t.Run("SubagentStart", func(t *testing.T) {
		for _, tc := range []struct {
			fixture    string
			agentID    string
			turnID     string
			transcript string
		}{
			{cxSubagentStartFixture, cxAgentID, cxAgentTurn, cxAgentTranscript},
			{cxSubagentStart2Fixture, cxAgent2ID, cxAgent2Turn, cxAgent2Transcript},
		} {
			p := loadCodexFixture(t, tc.fixture)
			if p.HookEventName != "SubagentStart" {
				t.Errorf("%s: HookEventName = %q", tc.fixture, p.HookEventName)
			}
			if p.AgentID != tc.agentID || p.AgentType != "default" {
				t.Errorf("%s: agent fields wrong: %+v", tc.fixture, p)
			}
			if p.TurnID != tc.turnID || p.PromptID != "" {
				t.Errorf("%s: turn identifier wrong: turn_id=%q prompt_id=%q", tc.fixture, p.TurnID, p.PromptID)
			}
			// The asymmetry: at Start there is no agent_transcript_path and
			// transcript_path is already the child's.
			if p.AgentTranscriptPath != "" {
				t.Errorf("%s: AgentTranscriptPath = %q, want absent at Start", tc.fixture, p.AgentTranscriptPath)
			}
			if p.TranscriptPath != tc.transcript {
				t.Errorf("%s: TranscriptPath = %q, want the child's %q", tc.fixture, p.TranscriptPath, tc.transcript)
			}
			if p.SessionID != cxSessionID || p.Cwd != cxCwd {
				t.Errorf("%s: session fields wrong: %+v", tc.fixture, p)
			}
		}
	})

	t.Run("SubagentStop", func(t *testing.T) {
		for _, tc := range []struct {
			fixture    string
			agentID    string
			turnID     string
			transcript string
		}{
			{cxSubagentStopFixture, cxAgentID, cxAgentTurn, cxAgentTranscript},
			{cxSubagentStop2Fixture, cxAgent2ID, cxAgent2Turn, cxAgent2Transcript},
		} {
			p := loadCodexFixture(t, tc.fixture)
			if p.HookEventName != "SubagentStop" {
				t.Errorf("%s: HookEventName = %q", tc.fixture, p.HookEventName)
			}
			if p.AgentID != tc.agentID || p.AgentType != "default" {
				t.Errorf("%s: agent fields wrong: %+v", tc.fixture, p)
			}
			if p.TurnID != tc.turnID || p.PromptID != "" {
				t.Errorf("%s: turn identifier wrong: turn_id=%q prompt_id=%q", tc.fixture, p.TurnID, p.PromptID)
			}
			// And here it is the other way round: agent_transcript_path is the
			// child's, transcript_path is the parent's.
			if p.AgentTranscriptPath != tc.transcript {
				t.Errorf("%s: AgentTranscriptPath = %q, want the child's %q", tc.fixture, p.AgentTranscriptPath, tc.transcript)
			}
			if p.TranscriptPath != cxSessionTranscript {
				t.Errorf("%s: TranscriptPath = %q, want the parent's %q", tc.fixture, p.TranscriptPath, cxSessionTranscript)
			}
		}
	})

	t.Run("PreToolUse", func(t *testing.T) {
		// Not an event this adapter wires. It is decoded here because it is the
		// payload that carried the encrypted task blob: the guarantee is that
		// even this one has nowhere to put it.
		raw := readCodexFixture(t, cxPreToolFixture)
		if !strings.Contains(string(raw), "tool_input") {
			t.Fatal("fixture no longer carries tool_input; this test exists for it")
		}
		p := loadCodexFixture(t, cxPreToolFixture)
		if p.HookEventName != "PreToolUse" {
			t.Errorf("HookEventName = %q", p.HookEventName)
		}
		if strings.Contains(fmtPayload(p), "REDACTED") {
			t.Errorf("tool_input reached the decoded payload: %s", fmtPayload(p))
		}
		for _, ev := range mustGet(t, "codex").Events() {
			if ev.Vendor == "PreToolUse" {
				t.Error("PreToolUse must not be wired: it is the event that carries tool arguments")
			}
		}
	})
}

// The generated file, compared whole against the schema that was verified to
// fire on 2026-08-07. Checking commands one key at a time cannot see a wrong
// nesting level, an extra wrapper, or a matcher that grew where none belongs --
// and Codex reports none of those, it just never runs the hook.
func TestCodexWireMatchesTheVerifiedSchema(t *testing.T) {
	root := t.TempDir()
	a := mustGet(t, "codex")
	if err := a.Wire(root, "/Users/n/bin/agents"); err != nil {
		t.Fatalf("Wire: %v", err)
	}

	if got, want := a.WireConfigPath(root), filepath.Join(root, ".codex", "hooks.json"); got != want {
		t.Fatalf("config path = %q, want %q", got, want)
	}

	// Byte-for-byte the shape of the probe hooks.json that Codex 0.147.0 was
	// observed loading and firing, minus its PreToolUse subscription and its
	// description key, which we do not generate.
	const verified = `{
	  "hooks": {
	    "SessionStart": [
	      { "matcher": "startup|resume",
	        "hooks": [{ "type": "command", "command": "/Users/n/bin/agents hook session-start --harness codex" }] }
	    ],
	    "SubagentStart": [
	      { "hooks": [{ "type": "command", "command": "/Users/n/bin/agents hook subagent-start --harness codex" }] }
	    ],
	    "SubagentStop": [
	      { "hooks": [{ "type": "command", "command": "/Users/n/bin/agents hook subagent-stop --harness codex" }] }
	    ],
	    "Stop": [
	      { "hooks": [{ "type": "command", "command": "/Users/n/bin/agents hook stop --harness codex" }] }
	    ]
	  }
	}`
	var want map[string]any
	if err := json.Unmarshal([]byte(verified), &want); err != nil {
		t.Fatalf("the expected schema is not JSON: %v", err)
	}
	got := readSettings(t, a.WireConfigPath(root))
	if !reflect.DeepEqual(got, want) {
		gb, _ := json.MarshalIndent(got, "", "  ")
		wb, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("generated hooks.json does not match the verified schema\ngot:\n%s\nwant:\n%s", gb, wb)
	}

	for _, ev := range []string{"SessionStart", "SubagentStart", "SubagentStop", "Stop"} {
		cmds := commandsFor(t, got, ev)
		if len(cmds) != 1 || !strings.Contains(cmds[0], "--harness codex") {
			t.Errorf("%s: commands = %v", ev, cmds)
		}
	}
	if _, err := os.Readlink(filepath.Join(root, ".codex", "skills")); err != nil {
		t.Errorf(".codex/skills must be a symlink: %v", err)
	}
}

// Codex gates on two things, not one. Project trust is what lets a project-local
// hooks.json load at all -- measured, from the CLI's own prompt: "Trusting the
// directory allows project-local config, hooks, and exec policies to load."
// Hook trust is separate and hash-recorded, which is why 0.147.0 ships
// --dangerously-bypass-hook-trust ("Run enabled hooks without requiring
// persisted hook trust for this invocation"). Naming only the first gate leaves
// a wired repo silently recording nothing.
func TestCodexTrustStepsNameBothGates(t *testing.T) {
	repoRoot := "/repo/root"
	steps := mustGet(t, "codex").TrustSteps(repoRoot)
	if len(steps) < 2 {
		t.Fatalf("TrustSteps = %v, want both the directory gate and the hook gate", steps)
	}
	joined := strings.Join(steps, "\n")
	if !strings.Contains(joined, repoRoot) {
		t.Errorf("TrustSteps must name the repo: %v", steps)
	}

	// Find the directory gate (should be the step mentioning the repo root).
	var dirGateIdx int
	for i, s := range steps {
		if strings.Contains(s, repoRoot) {
			dirGateIdx = i
			break
		}
	}

	// Require hook trust to be mentioned in a different step from the directory
	// gate. The directory gate contains both "hook" (hooks.json) and "trust"
	// ("trust the contents"), so checking only within a single step is
	// insufficient. Mutation proof: replacing step 1 with unrelated text must
	// fail this assertion, not just the others.
	var hookGate bool
	for i, s := range steps {
		if i != dirGateIdx && strings.Contains(s, "hook") && strings.Contains(s, "trust") {
			hookGate = true
			break
		}
	}
	if !hookGate {
		t.Errorf("hook trust must be in a separate step from the directory gate: %v", steps)
	}

	// Hash-recorded means it comes back after any edit to the command. A user
	// told only "do this once" will not know why recording stopped after the
	// next `agents wire`.
	if !strings.Contains(joined, "agents wire") {
		t.Errorf("no step says hook trust recurs when the wiring changes: %v", steps)
	}

	// Both gates are things to do; this is the way to check they took. `/hooks`
	// exists in 0.147.0 -- measured, and its own command list calls it "view and
	// manage lifecycle hooks" -- and its Installed/Active columns are the only
	// place the installed-but-inert state is visible from inside a session. That
	// state is precisely a cleared directory gate with an uncleared hook gate,
	// which otherwise looks identical to a working setup.
	//
	// Matched with its backticks: the bare token "/hooks" is a substring of
	// ".codex/hooks.json" in the directory-gate step, so it passes with the
	// command named nowhere. Deleting the step below has to fail this.
	if !strings.Contains(joined, "`/hooks`") {
		t.Errorf("no step names `/hooks` as the way to check the wired hooks are active: %v", steps)
	}
}
