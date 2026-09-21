package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/harness"
)

// `agents wire` removes what this tool left in harness configs and writes
// nothing of its own. The command used to be tested as "regenerates configs";
// that expectation is the thing the change removed, so the test asserts the
// new contract instead of being deleted with the renderer.
func TestWireRemovesRetiredEntriesAndWritesNothingNew(t *testing.T) {
	root := newRepoWithAgents(t)
	t.Chdir(root)

	// Stand in for what an earlier version left behind, in each harness's own
	// shape, plus a foreign hook that must survive all of it.
	stale := map[string]string{
		".claude/settings.json": `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/opt/homebrew/bin/agents hook stop --harness claude-code"}]}],"Notification":[{"hooks":[{"type":"command","command":"/my/own/notify.sh"}]}]}}`,
		".codex/hooks.json":     `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/opt/homebrew/bin/agents hook stop --harness codex"}]}]}}`,
		".agents/hooks.json":    `{"agents":{"Stop":[{"type":"command","command":"/opt/homebrew/bin/agents hook stop --harness antigravity"}]}}`,
	}
	for rel, body := range stale {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	if code := runWire(nil, &out); code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, out.String())
	}

	// A config holding only our entry is gone. One holding a foreign hook keeps
	// that hook and loses ours.
	if _, err := os.Stat(filepath.Join(root, ".codex/hooks.json")); !os.IsNotExist(err) {
		t.Errorf("a config holding only a retired entry should be removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agents/hooks.json")); !os.IsNotExist(err) {
		t.Errorf("the antigravity config held only a retired group and should be removed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, ".claude/settings.json"))
	if err != nil {
		t.Fatalf("the config with a foreign hook must survive: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(b, &settings); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("agents hook")) {
		t.Errorf("a retired entry survived:\n%s", b)
	}
	if !bytes.Contains(b, []byte("/my/own/notify.sh")) {
		t.Errorf("a foreign hook was dropped:\n%s", b)
	}

	// wire still does not scaffold. The fixture already has the router from
	// scaffold.Create, so the assertion is on a file only init writes.
	if _, err := os.Stat(filepath.Join(root, "docs")); err != nil {
		t.Errorf("the fixture should be scaffolded already: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills")); err != nil {
		t.Errorf("wire must still install the skills symlink: %v", err)
	}
}

// Wiring a repository with nothing stale must not create config files. This is
// the behaviour that replaced "regenerate": a fresh repository has no generated
// config, and that is the correct state rather than a gap.
func TestWireCreatesNoConfigWhenThereIsNothingToRemove(t *testing.T) {
	root := newRepoWithAgents(t)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runWire(nil, &out); code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, out.String())
	}
	for _, a := range harness.All() {
		if _, err := os.Stat(a.WireConfigPath(root)); err == nil {
			t.Errorf("wire created %s with nothing to write", a.WireConfigPath(root))
		}
	}
}

func TestWireOutsideRepoSkips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	nested := filepath.Join(dir, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	var out bytes.Buffer
	if code := runWire(nil, &out); code != 4 {
		t.Fatalf("exit = %d, want 4 (skip) outside a repo", code)
	}
	if _, err := os.Stat(filepath.Join(nested, ".claude")); err == nil {
		t.Error("wire wrote a config outside a repository")
	}
}

// A wiring failure must not be reported as success. A repository whose skills
// symlink could not be installed is not the state the caller asked for.
func TestWireReportsFailureAsNoRecord(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)

	// A real .claude/skills directory is content linkSkills refuses to replace.
	if err := os.MkdirAll(filepath.Join(root, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runWire(nil, &out); code != 5 {
		t.Fatalf("exit = %d, want 5 (no-record); output:\n%s", code, out.String())
	}
}
