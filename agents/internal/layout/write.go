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
//
// The result is 0644 whatever it replaces. CreateTemp creates 0600 and the
// rename carries that mode onto the manifest, so without the explicit chmod a
// fresh manifest would land unreadable to anyone but its owner and a rewrite
// would silently downgrade an existing 0644 document. Every other file the tool
// scaffolds is 0644.
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
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
