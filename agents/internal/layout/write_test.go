package layout

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// WriteManifest must be the exact inverse of Resolve: the bytes are
// MarshalManifest's, the parent directory is created when .agents/ is absent, an
// existing document is replaced rather than appended to, no temporary file
// survives, and what lands on disk resolves back to the same layout with no
// problems. A half-written manifest is the one failure every mutating command
// turns into a refusal, so the write is checked here rather than trusted.
//
// The mode is checked too: CreateTemp's 0600 would otherwise reach the manifest
// through the rename, making a fresh document owner-only and downgrading an
// existing 0644 one on every rewrite.
func TestWriteManifestRoundTripsThroughResolve(t *testing.T) {
	root := t.TempDir()
	stores := map[string]string{
		RoleDesign: "context/design", RolePlans: "context/plans",
		RoleJournal: "context/journal", RoleQNA: "context/qna",
	}
	want := mk(stores)
	manifest := filepath.Join(root, ManifestRel)

	if err := WriteManifest(root, want.Manifest); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := MarshalManifest(want.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, rendered) {
		t.Fatalf("written bytes are not MarshalManifest's:\n%s", data)
	}
	if got := Resolve(root); len(got.Problems) > 0 || got.Schema != SchemaV2 {
		t.Fatalf("written manifest does not resolve cleanly: %+v", got)
	}
	if mode := manifestMode(t, manifest); mode != 0o644 {
		t.Fatalf("fresh manifest mode = %v, want 0644", mode)
	}

	// A second write replaces the first: no duplicate document, no stale tail,
	// and no mode downgrade of the existing 0644 document.
	replaced := want
	replaced.Stores[RoleQNA] = "context/answers"
	if err := WriteManifest(root, replaced.Manifest); err != nil {
		t.Fatalf("second WriteManifest: %v", err)
	}
	if got := Resolve(root); got.Stores[RoleQNA] != "context/answers" {
		t.Fatalf("second write did not replace the document: %+v", got.Stores)
	}
	if mode := manifestMode(t, manifest); mode != 0o644 {
		t.Fatalf("rewritten manifest mode = %v, want 0644", mode)
	}
	temps, err := filepath.Glob(filepath.Join(root, ".agents", ".layout-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temps) > 0 {
		t.Fatalf("temporary manifest files survived: %s", strings.Join(temps, ", "))
	}
}

func manifestMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
