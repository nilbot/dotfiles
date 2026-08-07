package machine

import (
	"os"
	"regexp"
	"testing"
)

var idPattern = regexp.MustCompile(`^[a-z0-9-]+-[0-9a-f]{4}$`)

func TestIDIsGeneratedThenStable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	first, err := ID()
	if err != nil {
		t.Fatalf("ID() error: %v", err)
	}
	if !idPattern.MatchString(first) {
		t.Fatalf("ID() = %q, want <slug>-<4 hex>", first)
	}

	second, err := ID()
	if err != nil {
		t.Fatalf("second ID() error: %v", err)
	}
	if second != first {
		t.Fatalf("ID() not stable: %q then %q", first, second)
	}
}

func TestIDHonoursExistingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if err := os.MkdirAll(dir+"/agents", 0o755); err != nil {
		t.Fatal(err)
	}
	// Trailing newline and surrounding space must be tolerated: this file is
	// meant to be editable by hand.
	if err := os.WriteFile(dir+"/agents/machine-id", []byte("  m1-mbp-a7f3\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ID()
	if err != nil {
		t.Fatalf("ID() error: %v", err)
	}
	if got != "m1-mbp-a7f3" {
		t.Fatalf("ID() = %q, want m1-mbp-a7f3", got)
	}
}

func TestIDFailsWhenHomeAndXDGStateHomeUnavailable(t *testing.T) {
	// Unset both $HOME and XDG_STATE_HOME to simulate an environment where
	// stable state cannot be located (e.g., some daemon or CI contexts).
	t.Setenv("HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	_, err := ID()
	if err == nil {
		t.Fatalf("ID() error = nil, want non-nil error when $HOME and XDG_STATE_HOME are unset")
	}
}

func TestIDSurvivesReadError(t *testing.T) {
	// Create a machine-id file that is unreadable (no read permission).
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if err := os.MkdirAll(dir+"/agents", 0o755); err != nil {
		t.Fatal(err)
	}

	// Write an existing machine-id file with known content.
	idFile := dir + "/agents/machine-id"
	if err := os.WriteFile(idFile, []byte("existing-id-a1b2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Make it unreadable to simulate a read error (but keep it for future inspection).
	if err := os.Chmod(idFile, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(idFile, 0o600) // Restore for cleanup

	// ID() should return an error instead of minting a new identity.
	_, err := ID()
	if err == nil {
		t.Fatalf("ID() error = nil, want non-nil error when machine-id is unreadable")
	}

	// Restore read permission and verify the original file is still there untouched.
	os.Chmod(idFile, 0o600)
	content, err := os.ReadFile(idFile)
	if err != nil {
		t.Fatalf("failed to read machine-id after test: %v", err)
	}
	if string(content) != "existing-id-a1b2\n" {
		t.Fatalf("ID() overwrote file, got %q, want existing-id-a1b2\\n", string(content))
	}
}
