package safeio

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadRegularRejectsSpecialLeavesWithoutFollowingOrBlocking(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte("observed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadRegular(regular); err != nil || string(got) != "observed\n" {
		t.Fatalf("ReadRegular(regular) = %q, %v", got, err)
	}

	for _, tc := range []struct {
		name  string
		build func(string) error
	}{
		{"symlink", func(path string) error { return os.Symlink(regular, path) }},
		{"directory", func(path string) error { return os.Mkdir(path, 0o700) }},
		{"fifo", func(path string) error { return syscall.Mkfifo(path, 0o600) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			if err := tc.build(path); err != nil {
				t.Fatal(err)
			}
			if got, err := ReadRegular(path); err == nil {
				t.Fatalf("ReadRegular(%s) = %q, nil; want prompt rejection", tc.name, got)
			}
		})
	}

	shortDir, err := os.MkdirTemp("/tmp", "asi-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(shortDir) })
	socket := filepath.Join(shortDir, "socket")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if got, err := ReadRegular(socket); err == nil {
		t.Fatalf("ReadRegular(socket) = %q, nil; want prompt rejection", got)
	}
}
