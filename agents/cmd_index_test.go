package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

func writeMemory(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, ".agents", "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The only thing this command does that nothing else checks is pick the
// directory. A wrong path here writes a perfectly good index nobody reads, and
// exits 0 while doing it.
//
// Kills: pointing WriteIndex at .agents/ itself, or at any spelling other than
// .agents/memory/.
func TestIndexRegeneratesTheMemoryIndexInPlace(t *testing.T) {
	root := newRepo(t)
	writeMemory(t, root, "a.md", "---\nname: a-thing\ndescription: A\nmetadata:\n  type: project\n---\n\nbody\n")
	t.Chdir(root)

	var out bytes.Buffer
	if code := runIndex(nil, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, out.String())
	}
	b, err := os.ReadFile(filepath.Join(root, ".agents", "memory", "INDEX.md"))
	if err != nil {
		t.Fatalf("index was not written to .agents/memory/: %v", err)
	}
	if !strings.Contains(string(b), "a-thing") {
		t.Errorf("index does not list the entry:\n%s", b)
	}
}

// An index that quietly omits an entry is worse than no index: it reads as a
// complete list. The command must fail loudly and name the file.
//
// Kills: discarding the error from List and writing an index without the
// unparseable entry.
func TestIndexRefusesToWriteAnIndexThatWouldBeIncomplete(t *testing.T) {
	root := newRepo(t)
	writeMemory(t, root, "a.md", "---\nname: a-thing\ndescription: A\nmetadata:\n  type: project\n---\n\nbody\n")
	writeMemory(t, root, "broken.md", "no frontmatter here\n")
	t.Chdir(root)

	var out bytes.Buffer
	if code := runIndex(nil, &out); code != exitcode.NoRecord {
		t.Fatalf("exit = %d, want NoRecord (%d); output:\n%s", code, exitcode.NoRecord, out.String())
	}
	if !strings.Contains(out.String(), "broken.md") {
		t.Errorf("the refusal must name the file:\n%s", out.String())
	}
	if _, err := os.Lstat(filepath.Join(root, ".agents", "memory", "INDEX.md")); err == nil {
		t.Error("a half-true index must not be written")
	}
}

// A subcommand that is not registered in main.go is unreachable however well it
// works. Skip is only reachable through runIndex; an unregistered command
// returns Malformed.
func TestMainRegistersIndex(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	out := captureStdout(t, func() { code = run([]string{"index"}) })
	if code != exitcode.Skip {
		t.Fatalf("run(index) = %d, want Skip (%d); stdout:\n%s", code, exitcode.Skip, out)
	}
}
