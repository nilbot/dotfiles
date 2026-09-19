package layout

import (
	"os"
	"path/filepath"
)

// WriteManifest writes a manifest atomically: the document is rendered, written
// to a temporary file in the same directory, and renamed over the target. A
// reader therefore never observes a half-written manifest, and a crash leaves
// either the previous document or the complete new one -- never a truncated
// .agents/layout.json, which every mutating command would then refuse.
func WriteManifest(root string, m Manifest) error {
	b, err := MarshalManifest(m)
	if err != nil {
		return err
	}
	path := filepath.Join(root, ManifestRel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".layout-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
