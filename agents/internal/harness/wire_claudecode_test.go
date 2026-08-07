package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("settings is not JSON: %v\n%s", err, b)
	}
	return m
}

// assertHookTypes checks the "type" discriminator on every generated hook.
// commandsFor reads only "command", and nothing in this module reads "type" at
// all -- so a wrong value here is invisible from the inside and total from the
// outside: both harnesses refuse to run the hook and nothing is ever recorded.
func assertHookTypes(t *testing.T, settings map[string]any, event string) {
	t.Helper()
	hooks, _ := settings["hooks"].(map[string]any)
	groups, _ := hooks[event].([]any)
	var seen int
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		inner, _ := gm["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			cmd, _ := hm["command"].(string)
			if !strings.Contains(cmd, " --harness ") {
				continue // a foreign hook; its type is not ours to police
			}
			seen++
			if hm["type"] != "command" {
				t.Errorf("%s: hook type = %v, want the literal %q", event, hm["type"], "command")
			}
		}
	}
	if seen == 0 {
		t.Errorf("%s: no generated hook found to check the type of", event)
	}
}

func commandsFor(t *testing.T, settings map[string]any, event string) []string {
	t.Helper()
	hooks, _ := settings["hooks"].(map[string]any)
	groups, _ := hooks[event].([]any)
	var out []string
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		inner, _ := gm["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if c, ok := hm["command"].(string); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

// The config path and the vendor event spellings are an external contract with
// Claude Code: nothing in this module can detect that they are wrong, only
// Claude Code can, by silently never firing. So they are asserted against
// literals here. Every other test in this file locates the file through
// a.WireConfigPath, which would follow a wrong constant just as happily.
func TestClaudeCodeWireConfigPathIsTheRealSettingsFile(t *testing.T) {
	a, _ := Get("claude-code")
	root := t.TempDir()
	got := a.WireConfigPath(root)
	want := filepath.Join(root, ".claude", "settings.json")
	if got != want {
		t.Errorf("WireConfigPath = %q, want %q", got, want)
	}
}

func TestClaudeCodeEventsUseVendorSpellings(t *testing.T) {
	a, _ := Get("claude-code")
	want := []Event{
		{Semantic: "session-start", Vendor: "SessionStart"},
		{Semantic: "subagent-start", Vendor: "SubagentStart"},
		{Semantic: "subagent-stop", Vendor: "SubagentStop"},
		{Semantic: "stop", Vendor: "Stop"},
	}
	if got := a.Events(); !reflect.DeepEqual(got, want) {
		t.Errorf("Events() = %+v\nwant %+v", got, want)
	}
}

func TestClaudeCodeWireWritesEveryEvent(t *testing.T) {
	root := t.TempDir()
	a, _ := Get("claude-code")

	if err := a.Wire(root, "/Users/n/bin/agents"); err != nil {
		t.Fatalf("Wire: %v", err)
	}

	settings := readSettings(t, a.WireConfigPath(root))
	for _, ev := range []string{"SessionStart", "SubagentStart", "SubagentStop", "Stop"} {
		cmds := commandsFor(t, settings, ev)
		if len(cmds) != 1 {
			t.Fatalf("%s: got %d commands, want 1: %v", ev, len(cmds), cmds)
		}
		if !strings.HasPrefix(cmds[0], "/Users/n/bin/agents hook ") {
			t.Errorf("%s: command = %q", ev, cmds[0])
		}
		if !strings.Contains(cmds[0], "--harness claude-code") {
			t.Errorf("%s: command must name the harness explicitly: %q", ev, cmds[0])
		}
	}

	// The skills symlink is how a repo-specific procedure written once is
	// loaded by both harnesses.
	link := filepath.Join(root, ".claude", "skills")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf(".claude/skills must be a symlink: %v", err)
	}
	if target != filepath.Join("..", ".agents", "skills") {
		t.Errorf(".claude/skills -> %q", target)
	}
}

// Each generated command must carry the semantic event it fires for. Without
// this, every vendor event could render the same command and the record would
// label every firing identically -- which the assertions above cannot see,
// since they only check the prefix and the harness flag.
func TestClaudeCodeWireMapsEachVendorEventToItsSemanticName(t *testing.T) {
	root := t.TempDir()
	a, _ := Get("claude-code")
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
		want := "/Users/n/bin/agents hook " + semantic + " --harness claude-code"
		if len(cmds) != 1 || cmds[0] != want {
			t.Errorf("%s: commands = %v, want exactly [%q]", vendor, cmds, want)
		}
		assertHookTypes(t, settings, vendor)
	}
}

func TestClaudeCodeWireMergesAndDoesNotDuplicate(t *testing.T) {
	root := t.TempDir()
	a, _ := Get("claude-code")
	path := a.WireConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	existing := `{
	  "effortLevel": "high",
	  "permissions": {"allow": ["Bash(git *)"]},
	  "hooks": {
	    "PreToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"my-audit.sh"}]}],
	    "SubagentStop": [{"hooks":[{"type":"command","command":"my-notify.sh"}]}]
	  }
	}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.Wire(root, "/Users/n/bin/agents"); err != nil {
		t.Fatalf("Wire: %v", err)
	}
	if err := a.Wire(root, "/Users/n/bin/agents"); err != nil {
		t.Fatalf("second Wire: %v", err)
	}

	settings := readSettings(t, path)
	if settings["effortLevel"] != "high" {
		t.Error("unrelated settings must survive wiring")
	}
	if _, ok := settings["permissions"]; !ok {
		t.Error("permissions must survive wiring")
	}
	if got := commandsFor(t, settings, "PreToolUse"); len(got) != 1 || got[0] != "my-audit.sh" {
		t.Errorf("foreign hooks on other events must survive: %v", got)
	}

	cmds := commandsFor(t, settings, "SubagentStop")
	var mine, ours int
	for _, c := range cmds {
		if c == "my-notify.sh" {
			mine++
		}
		if strings.Contains(c, "--harness claude-code") {
			ours++
		}
	}
	if mine != 1 {
		t.Errorf("a foreign hook on an event we own must survive: %v", cmds)
	}
	if ours != 1 {
		t.Errorf("re-wiring must replace, not duplicate: %v", cmds)
	}
}

// A group that mixes a foreign hook with one of ours must lose only ours. The
// merge test above puts the foreign hook in a group of its own, so dropping
// whole groups instead of individual hooks survives it.
func TestClaudeCodeWireKeepsAForeignHookSharingOurGroup(t *testing.T) {
	root := t.TempDir()
	a, _ := Get("claude-code")
	path := a.WireConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.Wire(root, "/Users/n/bin/agents"); err != nil {
		t.Fatalf("Wire: %v", err)
	}

	// Slide a foreign hook into the group we just generated, the way a user
	// editing settings.json by hand would.
	settings := readSettings(t, path)
	hooks := settings["hooks"].(map[string]any)
	group := hooks["Stop"].([]any)[0].(map[string]any)
	group["hooks"] = append(group["hooks"].([]any),
		map[string]any{"type": "command", "command": "my-notify.sh"})
	b, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.Wire(root, "/Users/n/bin/agents"); err != nil {
		t.Fatalf("re-Wire: %v", err)
	}

	cmds := commandsFor(t, readSettings(t, path), "Stop")
	var mine, ours int
	for _, c := range cmds {
		if c == "my-notify.sh" {
			mine++
		}
		if strings.Contains(c, "--harness claude-code") {
			ours++
		}
	}
	if mine != 1 {
		t.Errorf("a foreign hook sharing a group with ours must survive: %v", cmds)
	}
	if ours != 1 {
		t.Errorf("re-wiring must replace, not duplicate: %v", cmds)
	}
}

// A binary that moved must still be recognised as ours and replaced, or every
// `make agents` to a new location would leave a dead hook behind.
func TestClaudeCodeWireReplacesAMovedBinary(t *testing.T) {
	root := t.TempDir()
	a, _ := Get("claude-code")
	if err := a.Wire(root, "/old/path/agents"); err != nil {
		t.Fatal(err)
	}
	if err := a.Wire(root, "/new/path/agents"); err != nil {
		t.Fatal(err)
	}

	cmds := commandsFor(t, readSettings(t, a.WireConfigPath(root)), "Stop")
	if len(cmds) != 1 || !strings.HasPrefix(cmds[0], "/new/path/agents") {
		t.Fatalf("commands = %v, want exactly the new path", cmds)
	}
}

// Wiring must never eat a real directory: somebody's .claude/skills/ content is
// not ours to replace with a link.
func TestWireRefusesToReplaceARealSkillsDirectory(t *testing.T) {
	root := t.TempDir()
	a, _ := Get("claude-code")
	real := filepath.Join(root, ".claude", "skills")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(real, "mine.md")
	if err := os.WriteFile(keep, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.Wire(root, "/Users/n/bin/agents"); err == nil {
		t.Fatal("Wire must fail rather than replace a real skills directory")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("existing skills content was destroyed: %v", err)
	}
}

// A settings.json that is not valid JSON must stop wiring loudly. Parsing it as
// an empty object instead would silently discard every unrelated setting in it.
func TestWireRefusesToClobberUnparseableSettings(t *testing.T) {
	root := t.TempDir()
	a, _ := Get("claude-code")
	path := a.WireConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.Wire(root, "/Users/n/bin/agents"); err == nil {
		t.Fatal("Wire must refuse to overwrite a settings file it cannot parse")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "{ not json" {
		t.Errorf("the unparseable file was rewritten anyway: %s", b)
	}
}
