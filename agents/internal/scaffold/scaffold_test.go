package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateBuildsLayout(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Create(root, false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, rel := range []string{
		".agents/memory",
		".agents/reports/handoff",
		".agents/reports/specs",
		".agents/reports/plans",
		".agents/reports/analysis",
		".agents/reports/traces",
		".agents/skills",
	} {
		if fi, err := os.Stat(filepath.Join(root, rel)); err != nil || !fi.IsDir() {
			t.Errorf("missing directory %s", rel)
		}
	}

	// AGENTS.md is a symlink, not a copy: two byte-identical files silently
	// diverge, which is what the prior art actually did.
	fi, err := os.Lstat(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("AGENTS.md must be a symlink to CLAUDE.md")
	}
	target, _ := os.Readlink(filepath.Join(root, "AGENTS.md"))
	if target != "CLAUDE.md" {
		t.Fatalf("AGENTS.md -> %q, want CLAUDE.md", target)
	}

	attrs, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatalf(".gitattributes: %v", err)
	}
	if !strings.Contains(string(attrs), "merge=union") {
		t.Error("traces need merge=union or a concurrent append produces invalid JSON")
	}
	if !strings.Contains(string(attrs), "linguist-generated=true") {
		t.Error(".agents/** should collapse in diffs")
	}
}

func TestCreatePreservesExistingFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("*.png binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Create(root, false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	claude, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if string(claude) != "# mine\n" {
		t.Error("an existing CLAUDE.md must not be overwritten")
	}
	attrs, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if !strings.Contains(string(attrs), "*.png binary") {
		t.Error("existing gitattributes lines must survive")
	}
	if !strings.Contains(string(attrs), "merge=union") {
		t.Error("our lines must still be appended")
	}

	// Idempotent: a second Create must not duplicate the appended lines.
	if err := Create(root, false); err != nil {
		t.Fatalf("second Create: %v", err)
	}
	attrs2, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if strings.Count(string(attrs2), "merge=union") != 1 {
		t.Errorf("gitattributes duplicated on re-run:\n%s", attrs2)
	}
}

func TestCreateLocalExcludesAgentsDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, true); err != nil {
		t.Fatalf("Create: %v", err)
	}
	exclude, err := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("exclude: %v", err)
	}
	if !strings.Contains(string(exclude), "/.agents/") {
		t.Errorf("--local must exclude .agents/:\n%s", exclude)
	}
	// The substring check above is satisfied by the always-written
	// /.agents/.trace-cache/ entry, so on its own it passes even when --local
	// adds nothing at all. The exclude entry has to be the bare directory.
	if !hasLine(string(exclude), "/.agents/") {
		t.Errorf("--local must exclude the whole directory, not just a subpath of it:\n%s", exclude)
	}
}

// Without --local, .agents/ is tracked. An exclude entry for it would make the
// scaffolded directory invisible to git in the mode whose entire point is that
// it is committed.
func TestCreateWithoutLocalTracksAgentsDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, false); err != nil {
		t.Fatalf("Create: %v", err)
	}
	exclude, err := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	if err != nil {
		// Otherwise a missing file reads as empty and this assertion is vacuous.
		t.Fatalf("exclude: %v", err)
	}
	if hasLine(string(exclude), "/.agents/") {
		t.Errorf("without --local, .agents/ must stay tracked:\n%s", exclude)
	}
}

// hasLine reports whether want is present as a whole line, which strings.Contains
// cannot distinguish from being a prefix of some longer entry.
func hasLine(content, want string) bool {
	for _, l := range strings.Split(content, "\n") {
		if strings.TrimSpace(l) == want {
			return true
		}
	}
	return false
}

func TestCreateAlwaysExcludesGeneratedHarnessConfigs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, false); err != nil {
		t.Fatalf("Create: %v", err)
	}
	exclude, _ := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	for _, want := range []string{"/.claude/settings.json", "/.codex/hooks.json", "/.agents/.trace-cache/"} {
		if !strings.Contains(string(exclude), want) {
			t.Errorf("exclude missing %q:\n%s", want, exclude)
		}
	}
}
