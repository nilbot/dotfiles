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
}

// fmtPayloadFields is the number of fields fmtPayload renders. Kept beside it
// so TestFmtPayloadRendersEveryField can catch the two drifting apart.
const fmtPayloadFields = 10

func fmtPayload(p Payload) string {
	return strings.Join([]string{
		p.HookEventName, p.SessionID, p.TurnID, p.PromptID, p.AgentID, p.AgentType,
		p.Cwd, p.TranscriptPath, p.AgentTranscriptPath, p.Source,
	}, "|")
}

// fmtPayload is hand-written, so adding a field to Payload without adding it
// here silently shrinks the scan above into a weaker test. That already
// happened once, when PromptID was added. This makes it impossible to repeat
// quietly: the reflection test proves no forbidden *destination* exists, and
// this proves the string scan still covers the whole type.
func TestFmtPayloadRendersEveryField(t *testing.T) {
	if n := reflect.TypeOf(Payload{}).NumField(); n != fmtPayloadFields {
		t.Fatalf("Payload has %d fields but fmtPayload renders %d; add the new field to fmtPayload and update the count", n, fmtPayloadFields)
	}
	// And every rendered field must actually be distinguishable, so a copy-paste
	// that renders the same field twice does not pass on count alone.
	if got := strings.Count(fmtPayload(Payload{}), "|"); got != fmtPayloadFields-1 {
		t.Fatalf("fmtPayload produced %d separators, want %d", got, fmtPayloadFields-1)
	}
}

// fmtPayload only renders fields it already knows about, so it cannot notice a
// newly added destination field for a forbidden key. This does: the guarantee
// is that Payload has nowhere to put one, and that is a property of the type,
// not of any one decoded value.
//
// A json tag is not the only way to name a destination. encoding/json falls
// back to case-insensitive matching against the field name when the tag is
// absent, so an untagged Tool_Input field decodes tool_input just as well.
// Compare on the normalised key, which is what the decoder effectively does.
func TestPayloadHasNoDestinationForForbiddenFields(t *testing.T) {
	rt := reflect.TypeOf(Payload{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if key == "" {
			key = f.Name // untagged: encoding/json matches on the field name
		}
		for _, bad := range record.ForbiddenFields {
			if normalizeKey(key) == normalizeKey(bad) {
				t.Errorf("Payload.%s is a destination for %q; forbidden keys must have none", f.Name, bad)
			}
		}
	}
}

// normalizeKey collapses the spellings encoding/json treats as the same key.
func normalizeKey(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "_", ""))
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
