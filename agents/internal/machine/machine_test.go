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
