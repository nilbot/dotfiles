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

// The handoff index is regenerated in the same operation that writes a handoff,
// so this command matters only for hand-edits and out-of-band writes -- which is
// exactly the case where a stale index is most believable. The location is the
// discriminating part: handoff.WriteIndex takes the .agents/ directory and finds
// reports/handoff under it itself, so passing it the handoff root instead
// produces a perfectly good index at reports/handoff/reports/handoff/INDEX.md
// that nothing reads, at exit 0.
//
// Kills: leaving handoff.WriteIndex out of runIndex, and passing it any path
// other than .agents/.
func TestIndexRegeneratesTheHandoffIndexInPlace(t *testing.T) {
	root := newRepo(t)
	dir := filepath.Join(root, ".agents", "reports", "handoff", "lane-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-08-10-s1.md"),
		[]byte("---\nlane: lane-a\nsession: s1\nstatus: reviewed\nwhen: 2026-08-10T09:00:00Z\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	if code := runIndex(nil, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, out.String())
	}
	b, err := os.ReadFile(filepath.Join(root, ".agents", "reports", "handoff", "INDEX.md"))
	if err != nil {
		t.Fatalf("index was not written to .agents/reports/handoff/: %v", err)
	}
	for _, want := range []string{"lane-a", "s1", "reviewed"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("index does not carry %q:\n%s", want, b)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "reports", "handoff", "reports")); err == nil {
		t.Error("the index was written through a doubled path")
	}
}

// Raw handoffs can be hand-edited or merge-produced, so status validation has
// to sit in the parser reached by the real index command. Otherwise an invented
// provenance word is rendered at exit 0 and looks like a supported trust level.
//
// Kills: validating only handoff.Write, or reporting a parse rejection as a
// successful index regeneration.
func TestIndexRefusesAnUnrecognisedHandoffStatus(t *testing.T) {
	root := newRepo(t)
	dir := filepath.Join(root, ".agents", "reports", "handoff", "lane-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "2026-08-10-s1.md"
	if err := os.WriteFile(filepath.Join(dir, name),
		[]byte("---\nlane: lane-a\nsession: s1\nstatus: approved\nwhen: 2026-08-10T09:00:00Z\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	if code := runIndex(nil, &out); code != exitcode.NoRecord {
		t.Fatalf("exit = %d, want NoRecord (%d); output:\n%s", code, exitcode.NoRecord, out.String())
	}
	for _, want := range []string{name, "status"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the refusal must name %q; output:\n%s", want, out.String())
		}
	}
	if _, err := os.Lstat(filepath.Join(root, ".agents", "reports", "handoff", "INDEX.md")); err == nil {
		t.Fatal("the command wrote an index containing unsupported provenance")
	}
}

// The command prints List's error as a diagnostic. A repository-controlled
// newline in the relative path must stay escaped there even when malformed
// status would otherwise make parse interpolate that path first.
//
// Kills: checking a handoff path only after parse succeeds.
func TestIndexDiagnosticDoesNotPrintRawControlCharactersFromHandoffPaths(t *testing.T) {
	for _, tc := range []struct {
		label, lane, name, rawPath string
	}{
		{
			label:   "lane name",
			lane:    "lane-a\nFORGED-OUTPUT",
			name:    "2026-08-10-s1.md",
			rawPath: "lane-a\nFORGED-OUTPUT/2026-08-10-s1.md",
		},
		{
			label:   "leaf name",
			lane:    "lane-a",
			name:    "2026-08-10-s1\nFORGED-OUTPUT.md",
			rawPath: "lane-a/2026-08-10-s1\nFORGED-OUTPUT.md",
		},
	} {
		t.Run(tc.label, func(t *testing.T) {
			root := newRepo(t)
			dir := filepath.Join(root, ".agents", "reports", "handoff", tc.lane)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, tc.name),
				[]byte("---\nlane: lane-a\nsession: s1\nstatus: approved\nwhen: 2026-08-10T09:00:00Z\n---\n\nbody\n"), 0o644); err != nil {
				t.Skipf("this filesystem will not hold the hostile path fixture: %v", err)
			}
			t.Chdir(root)

			var out bytes.Buffer
			if code := runIndex(nil, &out); code != exitcode.NoRecord {
				t.Fatalf("exit = %d, want NoRecord (%d); output:\n%s", code, exitcode.NoRecord, out.String())
			}
			if strings.Contains(out.String(), tc.rawPath) {
				t.Fatalf("diagnostic contains the raw path and can forge output: %q", out.String())
			}
			for _, want := range []string{"FORGED-OUTPUT", `\n`, "control character"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("the safely quoted diagnostic must contain %q; got: %q", want, out.String())
				}
			}
			if got := strings.Count(strings.TrimSuffix(out.String(), "\n"), "\n"); got != 1 {
				t.Errorf("command emitted %d internal newlines, want only the separator between its two diagnostics: %q", got, out.String())
			}
		})
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
