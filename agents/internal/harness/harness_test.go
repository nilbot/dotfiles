package harness

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/record"
)

// The decoder is the boundary where untrusted JSON becomes Go. Fields the
// record must never carry have to die here, not later.
func TestDecodeDiscardsForbiddenFields(t *testing.T) {
	raw := `{
	  "hook_event_name": "SubagentStop",
	  "session_id": "sess-1",
	  "agent_id": "agent-1",
	  "last_assistant_message": "SECRET-LEAK",
	  "tool_input": {"message": "gAAAAABSECRET"},
	  "tool_response": {"stdout": "SECRET-LEAK"}
	}`

	p, err := Decode(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if p.SessionID != "sess-1" || p.AgentID != "agent-1" {
		t.Fatalf("Decode dropped wanted fields: %+v", p)
	}

	// Nothing in the decoded value may reference the forbidden content.
	rendered := fmtPayload(p)
	for _, bad := range []string{"SECRET-LEAK", "gAAAAABSECRET"} {
		if strings.Contains(rendered, bad) {
			t.Fatalf("decoded payload retained %q: %s", bad, rendered)
		}
	}
	_ = record.ForbiddenFields
}

func fmtPayload(p Payload) string {
	return strings.Join([]string{
		p.HookEventName, p.SessionID, p.TurnID, p.AgentID, p.AgentType,
		p.Cwd, p.TranscriptPath, p.AgentTranscriptPath, p.Source,
	}, "|")
}

// fmtPayload only renders fields it already knows about, so it cannot notice a
// newly added destination field for a forbidden key. This does: the guarantee
// is that Payload has nowhere to put one, and that is a property of the struct
// tags, not of any one decoded value.
func TestPayloadHasNoDestinationForForbiddenFields(t *testing.T) {
	rt := reflect.TypeOf(Payload{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		for _, bad := range record.ForbiddenFields {
			if name == bad {
				t.Errorf("Payload.%s decodes %q; forbidden keys must have no destination field", f.Name, bad)
			}
		}
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := Decode(strings.NewReader("not json")); err == nil {
		t.Fatal("want an error for malformed input")
	}
}

func TestGetIsCaseInsensitiveAndFailsClosed(t *testing.T) {
	if _, ok := Get("claude-code"); !ok {
		t.Fatal("claude-code must be registered")
	}
	if _, ok := Get("Claude-Code"); !ok {
		t.Fatal("Get must be case-insensitive")
	}
	if _, ok := Get("gemini"); ok {
		t.Fatal("unregistered harness must not resolve")
	}
}

func TestAllReturnsOnlyRegisteredAdapters(t *testing.T) {
	var names []string
	for _, a := range All() {
		names = append(names, a.Name())
		if _, ok := Get(a.Name()); !ok {
			t.Errorf("All returned %q, which Get does not resolve", a.Name())
		}
	}
	if len(names) == 0 || names[0] != "claude-code" {
		t.Fatalf("All() = %v, want claude-code first", names)
	}
}
