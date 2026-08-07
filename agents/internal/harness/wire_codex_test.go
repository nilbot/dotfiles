package harness

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Only Codex's wiring half lands here; its recording half (Describe, and the
// payload shape it is reconciled against) is Task 9. `agents init` wires every
// registered adapter, so Codex has to be able to write its config now.

// Literals, not a.WireConfigPath: the path is an external contract with Codex,
// and a wrong constant is invisible from inside this module.
func TestCodexWireConfigPathIsTheRealHooksFile(t *testing.T) {
	a, ok := Get("codex")
	if !ok {
		t.Fatal("codex must be registered; `agents init` wires every adapter")
	}
	root := t.TempDir()
	got := a.WireConfigPath(root)
	want := filepath.Join(root, ".codex", "hooks.json")
	if got != want {
		t.Errorf("WireConfigPath = %q, want %q", got, want)
	}
}

func TestCodexEventsUseVendorSpellings(t *testing.T) {
	a, _ := Get("codex")
	want := []Event{
		{Semantic: "session-start", Vendor: "SessionStart", Matcher: "startup|resume"},
		{Semantic: "subagent-start", Vendor: "SubagentStart"},
		{Semantic: "subagent-stop", Vendor: "SubagentStop"},
		{Semantic: "stop", Vendor: "Stop"},
	}
	if got := a.Events(); !reflect.DeepEqual(got, want) {
		t.Errorf("Events() = %+v\nwant %+v", got, want)
	}
}

// Codex has no spawn-time sidecar, so it cannot label a subagent. Declaring the
// capability it does not have would put an always-empty description field in
// every Codex record.
func TestCodexDeclaresNoDescriptionCapability(t *testing.T) {
	a, _ := Get("codex")
	if a.Capabilities().Description {
		t.Fatal("Codex supplies no subagent description")
	}
}

func TestCodexWireWritesEveryEventAndTheSkillsLink(t *testing.T) {
	root := t.TempDir()
	a, _ := Get("codex")
	if err := a.Wire(root, "/Users/n/bin/agents"); err != nil {
		t.Fatalf("Wire: %v", err)
	}

	settings := readSettings(t, a.WireConfigPath(root))
	for vendor, semantic := range map[string]string{
		"SessionStart":  "session-start",
		"SubagentStart": "subagent-start",
		"SubagentStop":  "subagent-stop",
		"Stop":          "stop",
	} {
		cmds := commandsFor(t, settings, vendor)
		want := "/Users/n/bin/agents hook " + semantic + " --harness codex"
		if len(cmds) != 1 || cmds[0] != want {
			t.Errorf("%s: commands = %v, want exactly [%q]", vendor, cmds, want)
		}
	}

	// The matcher is what keeps a SessionStart hook from firing on every source
	// Codex reports; an entry that drops it is a different subscription.
	hooks := settings["hooks"].(map[string]any)
	group := hooks["SessionStart"].([]any)[0].(map[string]any)
	if group["matcher"] != "startup|resume" {
		t.Errorf("SessionStart matcher = %v, want startup|resume", group["matcher"])
	}
	// Events without a matcher must not gain an empty one: an empty string is a
	// pattern, not an absence, and it may match nothing at all.
	stop := hooks["Stop"].([]any)[0].(map[string]any)
	if _, present := stop["matcher"]; present {
		t.Errorf("Stop must carry no matcher key, got %v", stop["matcher"])
	}

	link := filepath.Join(root, ".codex", "skills")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf(".codex/skills must be a symlink: %v", err)
	}
	if target != filepath.Join("..", ".agents", "skills") {
		t.Errorf(".codex/skills -> %q", target)
	}
}

// Both adapters write into their own directory. A Codex Wire that pointed at
// .claude/ would silently replace the Claude Code wiring during `agents init`,
// which runs both.
func TestCodexAndClaudeCodeWireToSeparateFiles(t *testing.T) {
	root := t.TempDir()
	cc, _ := Get("claude-code")
	cx, _ := Get("codex")
	if err := cc.Wire(root, "/Users/n/bin/agents"); err != nil {
		t.Fatal(err)
	}
	if err := cx.Wire(root, "/Users/n/bin/agents"); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		a    Adapter
		want string
	}{{cc, "--harness claude-code"}, {cx, "--harness codex"}} {
		cmds := commandsFor(t, readSettings(t, tc.a.WireConfigPath(root)), "Stop")
		if len(cmds) != 1 || cmds[0] != "/Users/n/bin/agents hook stop "+tc.want {
			t.Errorf("%s: Stop commands = %v, want one %q", tc.a.Name(), cmds, tc.want)
		}
	}
}
