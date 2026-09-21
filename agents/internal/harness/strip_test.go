package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// This file replaces the wiring tests written when `wire` rendered hook
// entries. That renderer is gone: the tool no longer records harness lifecycle
// events, so `wire` now has one job -- remove the entries an earlier version
// left behind -- and the assertions have to match that job rather than the old
// one. A test that still asserted "one entry per event" would be asserting the
// presence of the very thing this change exists to delete.

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

func writeJSON(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// jsonValue parses a fixture literal into the same Go types a read of the
// config produces, so an assertion can compare a whole shape at once. Parsed
// values rather than bytes is the right comparison: the strip rewrites the file
// through its own marshaller, and what the preservation rule protects is the
// meaning, not the spelling.
func jsonValue(t *testing.T, body string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("fixture %s is not JSON: %v", body, err)
	}
	return v
}

// eventValue returns one event's value under a top-level container exactly as
// it stands in the written config, so a test can assert on the whole shape
// rather than on the commands it happens to contain.
func eventValue(t *testing.T, settings map[string]any, container, event string) any {
	t.Helper()
	m, ok := settings[container].(map[string]any)
	if !ok {
		t.Fatalf("%s in the written config = %#v, want an object holding the events", container, settings[container])
	}
	return m[event]
}

// wireRepoFixture is a repository with the .agents/ skills directory `wire`
// validates, so a call reaches the config write rather than failing early.
func wireRepoFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// The whole point of this change: a config carrying entries an earlier version
// wrote is left with none of them, and the file goes with them.
func TestWireRemovesRetiredEntriesAndTheFileThatOnlyHeldThem(t *testing.T) {
	root := wireRepoFixture(t)
	a, ok := Get("claude-code")
	if !ok {
		t.Fatal("claude-code adapter is not registered")
	}
	path := a.WireConfigPath(root)
	writeJSON(t, path, `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"/opt/homebrew/bin/agents hook session-start --harness claude-code"}]}],"Stop":[{"hooks":[{"type":"command","command":"/opt/homebrew/bin/agents hook stop --harness claude-code"}]}]}}`)

	if _, err := a.Wire(root); err != nil {
		t.Fatalf("wire: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("a config that held nothing but retired entries should be removed, not left as {}: %v", err)
	}
}

// The safety property that outlived the feature: a hook this tool did not write
// is never touched. An audit hook silently dropped from someone's config would
// be a far worse outcome than a stale entry.
func TestWireKeepsForeignHooksAndSettings(t *testing.T) {
	root := wireRepoFixture(t)
	a, _ := Get("claude-code")
	path := a.WireConfigPath(root)
	writeJSON(t, path, `{"hooks":{"Notification":[{"hooks":[{"type":"command","command":"/my/own/notify.sh"}]}],"Stop":[{"hooks":[{"type":"command","command":"/opt/homebrew/bin/agents hook stop --harness claude-code"}]}]},"permissions":{"allow":["Bash(ls:*)"]}}`)

	if _, err := a.Wire(root); err != nil {
		t.Fatalf("wire: %v", err)
	}
	settings := readSettings(t, path)
	if got := commandsFor(t, settings, "Notification"); len(got) != 1 || got[0] != "/my/own/notify.sh" {
		t.Errorf("foreign hook = %v, want the untouched notify.sh", got)
	}
	if got := commandsFor(t, settings, "Stop"); len(got) != 0 {
		t.Errorf("Stop still holds %v; the retired entry should be gone", got)
	}
	if _, ok := settings["permissions"]; !ok {
		t.Error("a setting unrelated to hooks was dropped")
	}
}

// A group that mixes a foreign hook with ours keeps the foreign one, because
// stripOurs works at the level of individual hooks rather than whole groups.
func TestWireKeepsAForeignHookSharingOurGroup(t *testing.T) {
	root := wireRepoFixture(t)
	a, _ := Get("claude-code")
	path := a.WireConfigPath(root)
	writeJSON(t, path, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/my/own/audit.sh"},{"type":"command","command":"/opt/homebrew/bin/agents hook stop --harness claude-code"}]}]}}`)

	if _, err := a.Wire(root); err != nil {
		t.Fatalf("wire: %v", err)
	}
	settings := readSettings(t, path)
	got := commandsFor(t, settings, "Stop")
	if len(got) != 1 || got[0] != "/my/own/audit.sh" {
		t.Errorf("Stop = %v, want only the foreign audit hook", got)
	}
}

// A group is a foreign object; only the individual hooks inside it were ever
// this tool's. Rebuilding the group around its "hooks" list would silently
// delete the keys that decide how the harness runs those hooks -- a "matcher"
// scoping an audit hook to one tool, say -- and it would do that even to a
// group holding nothing of ours, where the strip has no entry to remove and so
// no reason to write anything at all. Preserving the keys is what keeps this a
// removal rather than a normalisation of a config the tool does not own.
func TestWireKeepsTheOtherKeysOfAGroupItRewrites(t *testing.T) {
	cases := []struct {
		name     string
		group    string
		expected string
	}{
		{
			name:     "group holding only foreign hooks",
			group:    `{"matcher":"Bash","extra":1,"hooks":[{"type":"command","command":"/my/own/audit.sh"}]}`,
			expected: `{"matcher":"Bash","extra":1,"hooks":[{"type":"command","command":"/my/own/audit.sh"}]}`,
		},
		{
			name:     "group sharing a hook list with ours",
			group:    `{"matcher":"Bash","extra":1,"hooks":[{"type":"command","command":"/my/own/audit.sh"},{"type":"command","command":"/opt/homebrew/bin/agents hook stop --harness claude-code"}]}`,
			expected: `{"matcher":"Bash","extra":1,"hooks":[{"type":"command","command":"/my/own/audit.sh"}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := wireRepoFixture(t)
			a, _ := Get("claude-code")
			path := a.WireConfigPath(root)
			writeJSON(t, path, `{"hooks":{"Stop":[`+tc.group+`]}}`)

			if _, err := a.Wire(root); err != nil {
				t.Fatalf("wire: %v", err)
			}
			groups, _ := eventValue(t, readSettings(t, path), "hooks", "Stop").([]any)
			if len(groups) != 1 {
				t.Fatalf("Stop = %#v, want the one group back", groups)
			}
			want := jsonValue(t, tc.expected)
			if got := groups[0]; !reflect.DeepEqual(got, want) {
				t.Errorf("group = %#v, want %#v: a key this tool did not write must survive the strip", got, want)
			}
		})
	}
}

// A group is recognised by its "hooks" list and nothing else, and a list may
// hold values this tool has no interpretation for: a string, a number, a group
// that names no hook list at all. A value it cannot read is precisely the one
// it must not act on -- discarding it is a deletion with no entry behind it to
// justify the deletion, the same mistake as removing a foreign hook -- and
// rewriting it into a shape the tool does understand would be normalising
// somebody else's config on their behalf.
func TestWireKeepsGroupsWhoseShapeItDoesNotRecognise(t *testing.T) {
	const foreign = `{"hooks":[{"type":"command","command":"/my/own/notify.sh"}]}`
	cases := []struct {
		name     string
		groups   string
		expected string
	}{
		{
			name:     "string group",
			groups:   `["a plain string",` + foreign + `]`,
			expected: `["a plain string",` + foreign + `]`,
		},
		{
			name:     "number group",
			groups:   `[42,` + foreign + `]`,
			expected: `[42,` + foreign + `]`,
		},
		{
			name:     "group with no hook list",
			groups:   `[{"matcher":"Bash"},` + foreign + `]`,
			expected: `[{"matcher":"Bash"},` + foreign + `]`,
		},
		{
			name:     "group whose hook list is not a list",
			groups:   `[{"hooks":"not a list","extra":true},` + foreign + `]`,
			expected: `[{"hooks":"not a list","extra":true},` + foreign + `]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := wireRepoFixture(t)
			a, _ := Get("claude-code")
			path := a.WireConfigPath(root)
			writeJSON(t, path, `{"hooks":{"Notification":`+tc.groups+`}}`)

			if _, err := a.Wire(root); err != nil {
				t.Fatalf("wire: %v", err)
			}
			got := eventValue(t, readSettings(t, path), "hooks", "Notification")
			want := jsonValue(t, tc.expected)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Notification = %#v, want %#v: a shape this tool cannot read is not a shape it may drop or rewrite", got, want)
			}
		})
	}
}

// Antigravity keys its entries by event name, so a value that is not a list is
// a shape this tool has never written -- a vendor schema from before or after
// it, or a hand edit. The strip exists to clear entries this tool left behind,
// so an event it cannot read has to be carried through untouched: dropping it
// takes the operator's own configuration with it, and when that value was the
// only thing the file held, the empty-config rule then removes the file too.
func TestWireKeepsAntigravityEventValuesThatAreNotLists(t *testing.T) {
	const ours = `{"type":"command","command":"/opt/homebrew/bin/agents hook session-start --harness antigravity"}`
	cases := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "kept alongside an event this tool can read",
			body:     `{"agents":{"Stop":"a value this tool never wrote","SubagentStop":[{"type":"command","command":"/my/own/audit.sh"}],"SessionStart":[` + ours + `]}}`,
			expected: `{"agents":{"Stop":"a value this tool never wrote","SubagentStop":[{"type":"command","command":"/my/own/audit.sh"}]}}`,
		},
		{
			name:     "the only thing the config holds",
			body:     `{"agents":{"Stop":"a value this tool never wrote","SessionStart":[` + ours + `]}}`,
			expected: `{"agents":{"Stop":"a value this tool never wrote"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := wireRepoFixture(t)
			a, ok := Get("antigravity")
			if !ok {
				t.Fatal("antigravity adapter is not registered")
			}
			path := a.WireConfigPath(root)
			writeJSON(t, path, tc.body)

			if _, err := a.Wire(root); err != nil {
				t.Fatalf("wire: %v", err)
			}
			got := readSettings(t, path)
			want := jsonValue(t, tc.expected)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("config = %#v, want %#v: only entries recognised as this tool's own may be removed", got, want)
			}
		})
	}
}

// Running wire twice must be the same as running it once, in both the file's
// content and its existence. An idempotence bug here would either resurrect the
// entries or delete a config on the second pass that the first preserved.
func TestWireIsIdempotent(t *testing.T) {
	root := wireRepoFixture(t)
	a, _ := Get("claude-code")
	path := a.WireConfigPath(root)
	writeJSON(t, path, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/my/own/audit.sh"}]}]}}`)
	// The first run normalizes the file to this tool's formatting; idempotence
	// is that every run AFTER that settles. Comparing raw bytes would fail on
	// the first pass simply because the fixture was written on one line.
	if _, err := a.Wire(root); err != nil {
		t.Fatalf("wire: %v", err)
	}
	settled, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := a.Wire(root); err != nil {
			t.Fatalf("wire %d: %v", i, err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(settled) != string(after) {
		t.Errorf("wire is not idempotent:\n settled %s\n after   %s", settled, after)
	}
	var got, want map[string]any
	if err := json.Unmarshal(settled, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("wire changed the meaning of a foreign-only config: %v -> %v", want, got)
	}
}

// Antigravity nests its entries under a named group rather than per event, so
// its removal is a different shape and gets its own case. The group is deleted
// outright, and a config that ends up empty goes with it.
func TestWireRemovesTheAntigravityGroup(t *testing.T) {
	root := wireRepoFixture(t)
	a, ok := Get("antigravity")
	if !ok {
		t.Fatal("antigravity adapter is not registered")
	}
	path := a.WireConfigPath(root)
	writeJSON(t, path, `{"agents":{"Stop":[{"type":"command","command":"/opt/homebrew/bin/agents hook stop --harness antigravity"}]}}`)

	if _, err := a.Wire(root); err != nil {
		t.Fatalf("wire: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("the antigravity config held nothing else and should be removed: %v", err)
	}
}

// The refusal that protects a repository whose skills directory is real: wiring
// would have to replace it with a symlink, which is somebody's deliberate state.
func TestWireRefusesToReplaceARealSkillsDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	a, _ := Get("claude-code")
	if _, err := a.Wire(root); err == nil {
		t.Error("wire replaced a real .claude/skills directory; it must refuse")
	}
}

// An unparseable config is refused rather than rewritten. It is a file this
// tool did not produce and cannot safely interpret.
func TestWireRefusesUnparseableSettings(t *testing.T) {
	root := wireRepoFixture(t)
	a, _ := Get("claude-code")
	path := a.WireConfigPath(root)
	writeJSON(t, path, "{not json")
	if _, err := a.Wire(root); err == nil {
		t.Error("wire accepted an unparseable config; it must refuse")
	}
}

// ParseHookCommand is what decides which entries belong to this tool, so the
// narrow predicate is the difference between cleaning up and deleting someone
// else's hook. It recognises the two spellings this tool has written and
// nothing else.
func TestParseHookCommandIsNarrow(t *testing.T) {
	ours := []string{
		"/opt/homebrew/bin/agents hook stop --harness claude-code",
		"/opt/homebrew/bin/agents hook session-start --harness claude-code",
	}
	for _, cmd := range ours {
		if _, _, _, ok := ParseHookCommand(cmd); !ok {
			t.Errorf("ParseHookCommand(%q) = false; an entry this tool wrote must be recognised so wire can remove it", cmd)
		}
	}
	foreign := []string{
		"/my/own/notify.sh",
		"agents hook stop --harness claude-code",                   // not an absolute path
		"/opt/homebrew/bin/otheragent hook stop --harness codex",   // a different binary
		"/opt/homebrew/bin/agents hook stop --harness other-agent", // an unknown harness
		"/opt/homebrew/bin/agents hook invent --harness claude-code",
	}
	for _, cmd := range foreign {
		if _, _, _, ok := ParseHookCommand(cmd); ok {
			t.Errorf("ParseHookCommand(%q) = true; a hook this tool did not write must never be treated as ours", cmd)
		}
	}
}

// The wide predicate is for reporting only. It must not become the deletion
// predicate: it matches a shape this tool generates without requiring its own
// binary name, which is what lets doctor notice an entry `wire` cannot remove.
func TestResemblesHookCommandIsWiderThanItDeletes(t *testing.T) {
	// `agents-test-bin` and `agents-standalone` are names this tool is built
	// under, so they are owned on purpose -- the binary has to recognise entries
	// it wrote before it was installed under its final name.
	for _, owned := range []string{"agents", "agents-test-bin", "agents-standalone"} {
		cmd := "/usr/local/bin/" + owned + " hook stop --harness claude-code"
		if _, _, _, ok := ParseHookCommand(cmd); !ok {
			t.Errorf("ParseHookCommand(%q) = false; every name this tool is built under must be owned", cmd)
		}
	}

	// The wide predicate accepts a generated SHAPE without owning the binary.
	// That asymmetry is deliberate: what may be reported is broader than what
	// may be deleted, so doctor can flag an entry wire would refuse to touch.
	foreignBinary := "/usr/local/bin/some-other-tool hook stop --harness claude-code"
	if !ResemblesHookCommand(foreignBinary) {
		t.Error("ResemblesHookCommand must recognise the generated shape, whatever the binary is called")
	}
	if _, _, _, ok := ParseHookCommand(foreignBinary); ok {
		t.Error("ParseHookCommand claimed a binary this tool does not own; that is the deletion predicate and must stay narrow")
	}
}

func TestRegistryIsStable(t *testing.T) {
	all := All()
	if len(all) != 3 {
		t.Fatalf("All() returned %d adapters, want 3 (claude-code, codex, antigravity)", len(all))
	}
	for _, name := range []string{"claude-code", "codex", "antigravity"} {
		if _, ok := Get(name); !ok {
			t.Errorf("Get(%q) found nothing", name)
		}
	}
}

// A config that holds no keys is left exactly as it was, including one whose
// only content is an EMPTY hook container.
//
// Measured before this test existed: `{"hooks":{"Stop":[]}}` was deleted with a
// nil error, although it holds nothing this tool ever wrote -- an empty list is
// not an entry. The justification the removal rule carried ("an empty result
// means the file was carrying nothing but our now-retired entries") is simply
// false for that input, and the whole-file deletion is a new consequence of the
// strip: the renderer this replaced always re-appended an entry per event, so it
// never emptied a file.
func TestWireLeavesAPlaceholderConfigItNeverWrote(t *testing.T) {
	for _, tc := range []struct {
		harness string
		body    string
	}{
		{"claude-code", `{"hooks":{"Stop":[]}}`},
		{"codex", `{"hooks":{"Stop":[]}}`},
		{"antigravity", `{"agents":{"Stop":[]}}`},
	} {
		t.Run(tc.harness, func(t *testing.T) {
			root := wireRepoFixture(t)
			a, ok := Get(tc.harness)
			if !ok {
				t.Fatalf("%s adapter is not registered", tc.harness)
			}
			path := a.WireConfigPath(root)
			writeJSON(t, path, tc.body)
			if _, err := a.Wire(root); err != nil {
				t.Fatalf("wire: %v", err)
			}
			if _, err := os.Lstat(path); err != nil {
				t.Errorf("a config holding no entry of ours was removed: %v", err)
			}
		})
	}
}
