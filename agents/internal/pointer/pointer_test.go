package pointer

import "testing"

const (
	codexParent = "/Users/n/.codex/sessions/2026/08/07/rollout-2026-08-07T15-41-00-019fdcab-9733-72e3-ba7c-d2e0cc7fb334.jsonl"
	codexChild  = "/Users/n/.codex/sessions/2026/08/07/rollout-2026-08-07T15-41-06-019fdcab-ac94-7502-a322-d01f047c274a.jsonl"
	ccChild     = "/Users/n/.claude/projects/-Users-n-work/019f-sess/subagents/agent-a4e4a1bc424b2047f.jsonl"
	ccSession   = "/Users/n/.claude/projects/-Users-n-work/019fdcab-9733-72e3-ba7c-d2e0cc7fb334.jsonl"
)

func TestPicksChildAtCodexSubagentStop(t *testing.T) {
	// SubagentStop hands over both paths, parent first in field order.
	got, verified := Resolve([]string{codexParent, codexChild}, "019fdcab-ac94-7502-a322-d01f047c274a")
	if got != codexChild || !verified {
		t.Fatalf("Resolve = (%q, %v), want child and verified", got, verified)
	}
}

func TestPicksChildAtCodexSubagentStart(t *testing.T) {
	// SubagentStart supplies only one path, and it is already the child's.
	got, verified := Resolve([]string{codexChild}, "019fdcab-ac94-7502-a322-d01f047c274a")
	if got != codexChild || !verified {
		t.Fatalf("Resolve = (%q, %v), want child and verified", got, verified)
	}
}

func TestClaudeCodeSubagentBasename(t *testing.T) {
	got, verified := Resolve([]string{ccChild}, "a4e4a1bc424b2047f")
	if got != ccChild || !verified {
		t.Fatalf("Resolve = (%q, %v), want child and verified", got, verified)
	}
}

// Claude Code puts the session id in a directory component, not the basename.
// The invariant is "the key appears in the path", so this must still verify.
func TestClaudeCodeSessionIDInPathComponent(t *testing.T) {
	got, verified := Resolve([]string{ccSession}, "019fdcab-9733-72e3-ba7c-d2e0cc7fb334")
	if got != ccSession || !verified {
		t.Fatalf("Resolve = (%q, %v), want session path and verified", got, verified)
	}
}

// A basename match beats a path-component match, so a parent whose directory
// happens to contain the child's id cannot win.
func TestBasenameBeatsPathComponent(t *testing.T) {
	decoy := "/Users/n/.claude/projects/p/a4e4a1bc424b2047f/session.jsonl"
	got, _ := Resolve([]string{decoy, ccChild}, "a4e4a1bc424b2047f")
	if got != ccChild {
		t.Fatalf("Resolve = %q, want basename match %q", got, ccChild)
	}
}

// Degrade, never drop. An unverified pointer is still a lead; a missing record
// is nothing. When no key matches, returns the first candidate unverified.
func TestUnverifiedWhenNoCandidateMatches(t *testing.T) {
	// Pass two candidates, neither matching the key. Should return the first.
	got, verified := Resolve([]string{codexParent, codexChild}, "some-other-id")
	if got != codexParent {
		t.Fatalf("Resolve = %q, want first candidate %q", got, codexParent)
	}
	if verified {
		t.Fatal("verified = true, want false")
	}
}

func TestEmptyKeyIsNeverVerified(t *testing.T) {
	// Empty key returns first candidate but never verified. Pass two to verify ordering.
	got, verified := Resolve([]string{codexParent, codexChild}, "")
	if got != codexParent {
		t.Fatalf("Resolve = %q, want first candidate %q", got, codexParent)
	}
	if verified {
		t.Fatal("an empty key cannot verify anything")
	}
}

func TestNoCandidates(t *testing.T) {
	got, verified := Resolve([]string{"", "  "}, "id")
	if got != "" || verified {
		t.Fatalf("Resolve = (%q, %v), want empty and unverified", got, verified)
	}
}

// When both candidates match by basename, the first wins. Both Codex constants
// share the prefix 019fdcab in their basenames, creating a realistic tie.
func TestFirstWinsWhenBothMatchByBasename(t *testing.T) {
	got, verified := Resolve([]string{codexParent, codexChild}, "019fdcab")
	if got != codexParent {
		t.Fatalf("Resolve = %q, want first candidate %q when both match basename", got, codexParent)
	}
	if !verified {
		t.Fatal("verified = false, want true")
	}
}

// When both candidates match by path-component only (key in directory, not
// basename), the first wins. Both paths contain the key but not in their
// filenames, only in the directory structure.
func TestFirstWinsWhenBothMatchByPathComponent(t *testing.T) {
	first := "/Users/n/.claude/projects/a1b2c3d4-5678/session-file.jsonl"
	second := "/Users/n/.claude/projects/other/a1b2c3d4-5678/session.jsonl"
	got, verified := Resolve([]string{first, second}, "a1b2c3d4-5678")
	if got != first {
		t.Fatalf("Resolve = %q, want first candidate %q when both match path-component", got, first)
	}
	if !verified {
		t.Fatal("verified = false, want true")
	}
}
