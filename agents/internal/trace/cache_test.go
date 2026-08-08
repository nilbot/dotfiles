package trace

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/record"
)

// wantName recomputes the documented cache filename from scratch, out of the
// standard library, so it agrees with cache.go only if cache.go still follows
// the rule. It is the layout a person navigates, so it is a contract.
func wantName(src string) string {
	clean := filepath.Clean(src)
	sum := sha256.Sum256([]byte(clean))
	tag := hex.EncodeToString(sum[:])[:12]
	base := filepath.Base(clean)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext) + "-" + tag + ext
}

func write(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// cachedFiles lists every regular file under agentsDir, relative to it, so a
// test can assert on the whole of what a run produced rather than only on the
// one path it went looking for.
func cachedFiles(t *testing.T, agentsDir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(agentsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(agentsDir, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCacheCopiesOnlyReachableLocalTranscripts(t *testing.T) {
	agentsDir := t.TempDir()
	src := t.TempDir()

	here := write(t, filepath.Join(src, "rollout-here.jsonl"), `{"line":1}`+"\n")

	recs := []record.Record{
		{When: time.Now(), Machine: "m1", Harness: "codex", Transcript: here, PointerVerified: true},
		{When: time.Now(), Machine: "m1", Harness: "codex", Transcript: filepath.Join(src, "gone.jsonl"), PointerVerified: true},
		{When: time.Now(), Machine: "m2", Harness: "codex", Transcript: "/elsewhere/rollout.jsonl", PointerVerified: true},
	}

	rep, err := Cache(agentsDir, "m1", recs)
	if err != nil {
		t.Fatalf("Cache: %v", err)
	}
	if rep.Copied != 1 {
		t.Errorf("Copied = %d, want 1", rep.Copied)
	}
	if rep.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", rep.Skipped)
	}
	if rep.Unreachable != 1 {
		t.Errorf("Unreachable = %d, want 1 (this machine, file gone)", rep.Unreachable)
	}
	if rep.Elsewhere != 1 {
		t.Errorf("Elsewhere = %d, want 1 (another machine holds it)", rep.Elsewhere)
	}

	dst := filepath.Join(agentsDir, ".trace-cache", "codex", wantName(here))
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("expected %s: %v\ncache holds: %v", dst, err, cachedFiles(t, agentsDir))
	}
	if string(b) != `{"line":1}`+"\n" {
		t.Errorf("cached content = %q", b)
	}
	if got := cachedFiles(t, agentsDir); len(got) != 1 {
		t.Errorf("cache holds %v, want exactly the one reachable transcript", got)
	}
}

// Unreachable and Elsewhere call for different actions, and the only route to a
// transcript another machine holds is that machine's name. A report that says
// "1 elsewhere" without saying where is not actionable.
func TestCacheDetailsNameTheHoldingMachineAndTheMissingPath(t *testing.T) {
	agentsDir := t.TempDir()
	src := t.TempDir()

	recs := []record.Record{
		{Machine: "m1", Harness: "codex", Transcript: filepath.Join(src, "gone.jsonl"), PointerVerified: true},
		{Machine: "laptop-7f3a", Harness: "codex", Transcript: "/elsewhere/rollout.jsonl", PointerVerified: true},
	}
	rep, err := Cache(agentsDir, "m1", recs)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rep.Details, "\n")
	if len(rep.Details) != 2 {
		t.Fatalf("Details = %v, want one line per record that is not in the cache", rep.Details)
	}
	// The machine id is the whole point: it is the only thing that says where
	// to go and get it.
	if !strings.Contains(joined, "laptop-7f3a") {
		t.Errorf("Details must name the machine holding the transcript; got:\n%s", joined)
	}
	if !strings.Contains(joined, "/elsewhere/rollout.jsonl") {
		t.Errorf("Details must name the transcript held elsewhere; got:\n%s", joined)
	}
	if !strings.Contains(joined, filepath.Join(src, "gone.jsonl")) {
		t.Errorf("Details must name the unreachable transcript; got:\n%s", joined)
	}
}

func TestCacheIsIdempotent(t *testing.T) {
	agentsDir := t.TempDir()
	src := write(t, filepath.Join(t.TempDir(), "rollout-x.jsonl"), "{}\n")
	recs := []record.Record{{Machine: "m1", Harness: "codex", Transcript: src, PointerVerified: true}}

	if _, err := Cache(agentsDir, "m1", recs); err != nil {
		t.Fatal(err)
	}
	rep, err := Cache(agentsDir, "m1", recs)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 0 || rep.Skipped != 1 {
		t.Fatalf("second run: Copied=%d Skipped=%d, want 0 and 1", rep.Copied, rep.Skipped)
	}
	if got := cachedFiles(t, agentsDir); len(got) != 1 {
		t.Errorf("cache holds %v after two runs, want one file", got)
	}
}

// A record whose pointer never verified names a path that may belong to a
// different session. Copying it would put unrelated content under a name that
// implies it is related.
func TestCacheSkipsUnverifiedPointers(t *testing.T) {
	agentsDir := t.TempDir()
	src := write(t, filepath.Join(t.TempDir(), "rollout-y.jsonl"), "{}\n")
	rep, err := Cache(agentsDir, "m1", []record.Record{
		{Machine: "m1", Harness: "codex", Transcript: src, PointerVerified: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 0 {
		t.Fatalf("Copied = %d, want 0 for an unverified pointer", rep.Copied)
	}
	if got := cachedFiles(t, agentsDir); len(got) != 0 {
		t.Errorf("cache holds %v, want nothing", got)
	}
}

// The harness is a path component, not decoration: a cache that files every
// transcript under one name loses the only cheap way to tell a Codex rollout
// from a Claude Code session file. The empty case is the one a hand-written or
// pre-harness record produces, and it must not collapse the directory level.
func TestCacheFilesEachTranscriptUnderItsHarness(t *testing.T) {
	agentsDir := t.TempDir()
	src := t.TempDir()

	codex := write(t, filepath.Join(src, "rollout-codex.jsonl"), "codex\n")
	claude := write(t, filepath.Join(src, "session-claude.jsonl"), "claude\n")
	nameless := write(t, filepath.Join(src, "rollout-nameless.jsonl"), "nameless\n")

	rep, err := Cache(agentsDir, "m1", []record.Record{
		{Machine: "m1", Harness: "codex", Transcript: codex, PointerVerified: true},
		{Machine: "m1", Harness: "claude-code", Transcript: claude, PointerVerified: true},
		{Machine: "m1", Harness: "", Transcript: nameless, PointerVerified: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 3 {
		t.Fatalf("Copied = %d, want 3; cache holds %v", rep.Copied, cachedFiles(t, agentsDir))
	}

	for _, tc := range []struct{ dir, src, body string }{
		{"codex", codex, "codex\n"},
		{"claude-code", claude, "claude\n"},
		{"unknown", nameless, "nameless\n"},
	} {
		dst := filepath.Join(agentsDir, ".trace-cache", tc.dir, wantName(tc.src))
		b, err := os.ReadFile(dst)
		if err != nil {
			t.Errorf("expected %s: %v\ncache holds: %v", dst, err, cachedFiles(t, agentsDir))
			continue
		}
		if string(b) != tc.body {
			t.Errorf("%s content = %q, want %q", dst, b, tc.body)
		}
	}
}

// Both harnesses name transcripts by session, but nothing in a record
// guarantees it: two sessions in two directories can arrive with one basename.
// Keyed on the basename alone the second copy either overwrites the first or is
// counted as already cached -- and "already cached" is the worse of the two,
// because the run then reports success over a transcript that is not there and
// a file whose name implies the other session's content.
//
// The destination is therefore keyed on the whole source path, so two sources
// can never claim one name.
func TestCacheKeepsTwoTranscriptsWithOneBasenameApart(t *testing.T) {
	agentsDir := t.TempDir()
	src := t.TempDir()

	first := write(t, filepath.Join(src, "morning", "rollout.jsonl"), "first session\n")
	second := write(t, filepath.Join(src, "evening", "rollout.jsonl"), "second session\n")

	rep, err := Cache(agentsDir, "m1", []record.Record{
		{Machine: "m1", Harness: "codex", Transcript: first, PointerVerified: true},
		{Machine: "m1", Harness: "codex", Transcript: second, PointerVerified: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 2 || rep.Skipped != 0 {
		t.Fatalf("Copied=%d Skipped=%d, want 2 and 0; cache holds %v",
			rep.Copied, rep.Skipped, cachedFiles(t, agentsDir))
	}
	for _, tc := range []struct{ src, body string }{{first, "first session\n"}, {second, "second session\n"}} {
		dst := filepath.Join(agentsDir, ".trace-cache", "codex", wantName(tc.src))
		b, err := os.ReadFile(dst)
		if err != nil {
			t.Errorf("expected %s: %v\ncache holds: %v", dst, err, cachedFiles(t, agentsDir))
			continue
		}
		if string(b) != tc.body {
			t.Errorf("%s content = %q, want %q -- one session's transcript is filed under the other's name", dst, b, tc.body)
		}
	}
	if got := cachedFiles(t, agentsDir); len(got) != 2 {
		t.Errorf("cache holds %v, want two distinct files", got)
	}
}

// One session produces many records pointing at one transcript -- a stop and
// several subagent-stops all name the same rollout file. Copying it once per
// record is wasted work that also inflates the number the command reports, so
// "copied 4" would describe four transcripts that do not exist.
func TestCacheCopiesOneTranscriptOncePerRun(t *testing.T) {
	agentsDir := t.TempDir()
	src := write(t, filepath.Join(t.TempDir(), "rollout-dup.jsonl"), "{}\n")

	rep, err := Cache(agentsDir, "m1", []record.Record{
		{Machine: "m1", Harness: "codex", Event: "subagent-stop", Transcript: src, PointerVerified: true},
		{Machine: "m1", Harness: "codex", Event: "stop", Transcript: src, PointerVerified: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Skipped means "was already in the cache before this run". Counting the
	// run's own second sighting as Skipped would report a file as pre-existing
	// that this very run created.
	if rep.Copied != 1 || rep.Skipped != 0 {
		t.Fatalf("Copied=%d Skipped=%d, want 1 and 0 for one transcript named twice", rep.Copied, rep.Skipped)
	}
	if got := cachedFiles(t, agentsDir); len(got) != 1 {
		t.Errorf("cache holds %v, want one file", got)
	}
}

// Harness is the only field of a record that becomes a directory name, and
// records arrive from a tracked JSONL file that anyone with commit access can
// write. Unsanitised, "../../.." turns `git pull` into a write primitive
// pointed anywhere on the filesystem.
func TestCacheKeepsAHostileHarnessInsideTheCacheDirectory(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, "repo", ".agents")
	src := write(t, filepath.Join(t.TempDir(), "rollout-hostile.jsonl"), "payload\n")

	rep, err := Cache(agentsDir, "m1", []record.Record{
		{Machine: "m1", Harness: "../../../escaped", Transcript: src, PointerVerified: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 1 {
		t.Errorf("Copied = %d, want 1: a strange harness name is not a reason to lose the transcript", rep.Copied)
	}

	cacheRoot := filepath.Join(agentsDir, ".trace-cache")
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasPrefix(p, cacheRoot+string(filepath.Separator)) {
			t.Errorf("wrote %s, which is outside %s", p, cacheRoot)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The copy lands on a temporary name and is renamed into place. A leftover
// temporary from an interrupted earlier run must not bleed into the next one:
// opened without O_TRUNC, the tail of the longer old file survives past the new
// content and the cached transcript ends in bytes from a different session.
func TestCacheOverwritesAStaleTemporaryFile(t *testing.T) {
	agentsDir := t.TempDir()
	src := write(t, filepath.Join(t.TempDir(), "rollout-stale.jsonl"), "new\n")

	dst := filepath.Join(agentsDir, ".trace-cache", "codex", wantName(src))
	write(t, dst+".partial", "stale bytes from an interrupted run\n")

	rep, err := Cache(agentsDir, "m1", []record.Record{
		{Machine: "m1", Harness: "codex", Transcript: src, PointerVerified: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 1 {
		t.Fatalf("Copied = %d, want 1", rep.Copied)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("expected %s: %v", dst, err)
	}
	if string(b) != "new\n" {
		t.Errorf("cached content = %q, want %q -- stale bytes survived into the transcript", b, "new\n")
	}
	// Nothing half-written may remain under a name a later run would trust or a
	// person would mistake for a transcript.
	for _, f := range cachedFiles(t, agentsDir) {
		if strings.HasSuffix(f, ".partial") {
			t.Errorf("a successful run left %s behind", f)
		}
	}
}
