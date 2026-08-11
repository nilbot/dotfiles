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

// cachedFiles lists every cached transcript under cacheRoot, relative to it, so
// a test can assert on the whole of what a run produced rather than only on the
// one path it went looking for.
func cachedFiles(t *testing.T, cacheRoot string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(cacheRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(cacheRoot, p)
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
	cacheRoot := t.TempDir()
	src := t.TempDir()

	here := write(t, filepath.Join(src, "rollout-here.jsonl"), `{"line":1}`+"\n")

	recs := []record.Record{
		{When: time.Now(), Machine: "m1", Harness: "codex", Transcript: here, PointerVerified: true},
		{When: time.Now(), Machine: "m1", Harness: "codex", Transcript: filepath.Join(src, "gone.jsonl"), PointerVerified: true},
		{When: time.Now(), Machine: "m2", Harness: "codex", Transcript: "/elsewhere/rollout.jsonl", PointerVerified: true},
	}

	rep, err := Cache(cacheRoot, "m1", recs)
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

	dst := filepath.Join(cacheRoot, "codex", wantName(here))
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("expected %s: %v\ncache holds: %v", dst, err, cachedFiles(t, cacheRoot))
	}
	if string(b) != `{"line":1}`+"\n" {
		t.Errorf("cached content = %q", b)
	}
	if got := cachedFiles(t, cacheRoot); len(got) != 1 {
		t.Errorf("cache holds %v, want exactly the one reachable transcript", got)
	}
}

// Unreachable and Elsewhere call for different actions, and the only route to a
// transcript another machine holds is that machine's name. A report that says
// "1 elsewhere" without saying where is not actionable.
func TestCacheDetailsNameTheHoldingMachineAndTheMissingPath(t *testing.T) {
	cacheRoot := t.TempDir()
	src := t.TempDir()

	recs := []record.Record{
		{Machine: "m1", Harness: "codex", Transcript: filepath.Join(src, "gone.jsonl"), PointerVerified: true},
		{Machine: "laptop-7f3a", Harness: "codex", Transcript: "/elsewhere/rollout.jsonl", PointerVerified: true},
	}
	rep, err := Cache(cacheRoot, "m1", recs)
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
	cacheRoot := t.TempDir()
	src := write(t, filepath.Join(t.TempDir(), "rollout-x.jsonl"), "{}\n")
	recs := []record.Record{{Machine: "m1", Harness: "codex", Transcript: src, PointerVerified: true}}

	if _, err := Cache(cacheRoot, "m1", recs); err != nil {
		t.Fatal(err)
	}
	rep, err := Cache(cacheRoot, "m1", recs)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 0 || rep.Skipped != 1 {
		t.Fatalf("second run: Copied=%d Skipped=%d, want 0 and 1", rep.Copied, rep.Skipped)
	}
	if got := cachedFiles(t, cacheRoot); len(got) != 1 {
		t.Errorf("cache holds %v after two runs, want one file", got)
	}
}

// A record whose pointer never verified names a path that may belong to a
// different session. Copying it would put unrelated content under a name that
// implies it is related -- but leaving it out of the report entirely is worse
// than not copying it: an unverified pointer is a normal, expected state (see
// package pointer), so a repo full of them printed "copied 0" at exit 0, which
// reads as "nothing to do" while the transcripts sit readable on this disk. It
// is also the only class the report used to drop; every other one is counted.
func TestCacheCountsUnverifiedPointersInsteadOfDroppingThem(t *testing.T) {
	cacheRoot := t.TempDir()
	dir := t.TempDir()
	unverified := write(t, filepath.Join(dir, "rollout-y.jsonl"), "{}\n")
	verified := write(t, filepath.Join(dir, "rollout-z.jsonl"), "{}\n")

	rep, err := Cache(cacheRoot, "m1", []record.Record{
		{Machine: "m1", Harness: "codex", Transcript: unverified, PointerVerified: false},
		{Machine: "m1", Harness: "codex", Transcript: verified, PointerVerified: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 1 {
		t.Fatalf("Copied = %d, want 1: only the verified pointer is copied", rep.Copied)
	}
	if rep.Unverified != 1 {
		t.Errorf("Unverified = %d, want 1 -- an uncounted skip is indistinguishable from an empty repo", rep.Unverified)
	}
	// Counted but unnamed is barely better than silent: the path is the only
	// thing that tells the reader which session to go and re-verify.
	joined := strings.Join(rep.Details, "\n")
	if !strings.Contains(joined, unverified) {
		t.Errorf("Details must name the unverified transcript; got:\n%s", joined)
	}
	if strings.Contains(joined, verified) {
		t.Errorf("Details must not mention the transcript that was copied; got:\n%s", joined)
	}
	// Not copying it is the part that must not change.
	for _, f := range cachedFiles(t, cacheRoot) {
		if strings.Contains(f, wantName(unverified)) {
			t.Errorf("cache holds %s: an unverified pointer must still not be copied", f)
		}
	}
}

// One session writes many records for one transcript, and a stop that confirmed
// the pointer may arrive either side of a subagent-stop that did not. Deduping
// on first sighting alone makes the query's ordering decide whether the
// transcript is copied or merely reported as unverified.
func TestCacheLetsAVerifiedSightingWinOverAnUnverifiedOne(t *testing.T) {
	src := write(t, filepath.Join(t.TempDir(), "rollout-mixed.jsonl"), "{}\n")
	verified := record.Record{Machine: "m1", Harness: "codex", Event: "stop", Transcript: src, PointerVerified: true}
	unverified := record.Record{Machine: "m1", Harness: "codex", Event: "subagent-stop", Transcript: src}

	for _, tc := range []struct {
		name string
		recs []record.Record
	}{
		{"verified first", []record.Record{verified, unverified}},
		{"unverified first", []record.Record{unverified, verified}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cacheRoot := t.TempDir()
			rep, err := Cache(cacheRoot, "m1", tc.recs)
			if err != nil {
				t.Fatal(err)
			}
			if rep.Copied != 1 || rep.Unverified != 0 {
				t.Fatalf("Copied=%d Unverified=%d, want 1 and 0: one record confirmed this pointer, so it is confirmed",
					rep.Copied, rep.Unverified)
			}
			if got := cachedFiles(t, cacheRoot); len(got) != 1 {
				t.Errorf("cache holds %v, want the one transcript", got)
			}
		})
	}
}

// Identical transcript paths across machines are the expected condition, not an
// edge case: both harnesses write $HOME-relative paths that look the same on
// every machine you own, which is the whole reason a record carries a machine.
// Keyed on the path alone the two records collapse into whichever is newer, so
// the same repository answers "on another machine" or "copied 1" depending on
// which session happened to stop last -- and in the first case the file is
// readable right here and never gets copied.
func TestCacheKeepsOnePathApartPerMachineWhicheverOrderTheyArriveIn(t *testing.T) {
	src := write(t, filepath.Join(t.TempDir(), "rollout.jsonl"), "local bytes\n")
	local := record.Record{Machine: "m1", Harness: "codex", Transcript: src, PointerVerified: true}
	remote := record.Record{Machine: "laptop-7f3a", Harness: "codex", Transcript: src, PointerVerified: true}

	for _, tc := range []struct {
		name string
		recs []record.Record
	}{
		{"remote record newer", []record.Record{remote, local}},
		{"local record newer", []record.Record{local, remote}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cacheRoot := t.TempDir()
			rep, err := Cache(cacheRoot, "m1", tc.recs)
			if err != nil {
				t.Fatal(err)
			}
			if rep.Copied != 1 {
				t.Errorf("Copied = %d, want 1: the file is readable on this machine", rep.Copied)
			}
			if rep.Elsewhere != 1 {
				t.Errorf("Elsewhere = %d, want 1: another machine's session is not this one's", rep.Elsewhere)
			}
			if !strings.Contains(strings.Join(rep.Details, "\n"), "laptop-7f3a") {
				t.Errorf("Details must still name the machine holding the other session; got: %v", rep.Details)
			}
			b, err := os.ReadFile(filepath.Join(cacheRoot, "codex", wantName(src)))
			if err != nil {
				t.Fatalf("expected the local transcript in the cache: %v\ncache holds: %v", err, cachedFiles(t, cacheRoot))
			}
			if string(b) != "local bytes\n" {
				t.Errorf("cached content = %q", b)
			}
		})
	}
}

// The harness is a path component, not decoration: a cache that files every
// transcript under one name loses the only cheap way to tell a Codex rollout
// from a Claude Code session file. The empty case is the one a hand-written or
// pre-harness record produces, and it must not collapse the directory level.
func TestCacheFilesEachTranscriptUnderItsHarness(t *testing.T) {
	cacheRoot := t.TempDir()
	src := t.TempDir()

	codex := write(t, filepath.Join(src, "rollout-codex.jsonl"), "codex\n")
	claude := write(t, filepath.Join(src, "session-claude.jsonl"), "claude\n")
	nameless := write(t, filepath.Join(src, "rollout-nameless.jsonl"), "nameless\n")

	rep, err := Cache(cacheRoot, "m1", []record.Record{
		{Machine: "m1", Harness: "codex", Transcript: codex, PointerVerified: true},
		{Machine: "m1", Harness: "claude-code", Transcript: claude, PointerVerified: true},
		{Machine: "m1", Harness: "", Transcript: nameless, PointerVerified: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 3 {
		t.Fatalf("Copied = %d, want 3; cache holds %v", rep.Copied, cachedFiles(t, cacheRoot))
	}

	for _, tc := range []struct{ dir, src, body string }{
		{"codex", codex, "codex\n"},
		{"claude-code", claude, "claude\n"},
		{"unknown", nameless, "nameless\n"},
	} {
		dst := filepath.Join(cacheRoot, tc.dir, wantName(tc.src))
		b, err := os.ReadFile(dst)
		if err != nil {
			t.Errorf("expected %s: %v\ncache holds: %v", dst, err, cachedFiles(t, cacheRoot))
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
	cacheRoot := t.TempDir()
	src := t.TempDir()

	first := write(t, filepath.Join(src, "morning", "rollout.jsonl"), "first session\n")
	second := write(t, filepath.Join(src, "evening", "rollout.jsonl"), "second session\n")

	rep, err := Cache(cacheRoot, "m1", []record.Record{
		{Machine: "m1", Harness: "codex", Transcript: first, PointerVerified: true},
		{Machine: "m1", Harness: "codex", Transcript: second, PointerVerified: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 2 || rep.Skipped != 0 {
		t.Fatalf("Copied=%d Skipped=%d, want 2 and 0; cache holds %v",
			rep.Copied, rep.Skipped, cachedFiles(t, cacheRoot))
	}
	for _, tc := range []struct{ src, body string }{{first, "first session\n"}, {second, "second session\n"}} {
		dst := filepath.Join(cacheRoot, "codex", wantName(tc.src))
		b, err := os.ReadFile(dst)
		if err != nil {
			t.Errorf("expected %s: %v\ncache holds: %v", dst, err, cachedFiles(t, cacheRoot))
			continue
		}
		if string(b) != tc.body {
			t.Errorf("%s content = %q, want %q -- one session's transcript is filed under the other's name", dst, b, tc.body)
		}
	}
	if got := cachedFiles(t, cacheRoot); len(got) != 2 {
		t.Errorf("cache holds %v, want two distinct files", got)
	}
}

// One session produces many records pointing at one transcript -- a stop and
// several subagent-stops all name the same rollout file. Copying it once per
// record is wasted work that also inflates the number the command reports, so
// "copied 4" would describe four transcripts that do not exist.
func TestCacheCopiesOneTranscriptOncePerRun(t *testing.T) {
	cacheRoot := t.TempDir()
	src := write(t, filepath.Join(t.TempDir(), "rollout-dup.jsonl"), "{}\n")

	rep, err := Cache(cacheRoot, "m1", []record.Record{
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
	if got := cachedFiles(t, cacheRoot); len(got) != 1 {
		t.Errorf("cache holds %v, want one file", got)
	}
}

// Harness is the only field of a record that becomes a directory name, and
// records arrive from a tracked JSONL file that anyone with commit access can
// write. Unsanitised, "../../.." turns `git pull` into a write primitive
// pointed anywhere on the filesystem.
//
// The assertion is on where the copy actually landed, not on a walk of some
// enclosing tempdir: a walk only catches an escape shallow enough to stay inside
// the directory being walked, so a harness with one more ".." than the fixture
// passes it without going anywhere near the cache. Here the copy is looked for
// under the cache root and nowhere else, so any harness that resolves outside it
// -- at any depth -- shows up as a run that reported a copy nobody can find. The
// expected location is never computed through harnessDir, so a sanitiser that
// agrees with itself and still escapes cannot pass.
func TestCacheResolvesAnyHarnessToOneComponentInsideTheCache(t *testing.T) {
	for _, tc := range []struct{ name, harness string }{
		// filepath.Base leaves all three of these as a traversal, which is why
		// harnessDir exists and Base alone is not the same function.
		{"parent", ".."},
		{"parent behind a component", "a/../.."},
		{"deeper than any fixture can chase", "../../../../../../../../escaped"},
		// Base maps these to ".", which collapses the harness level out of the
		// path and files every transcript directly in the cache root.
		{"self", "."},
		{"empty", ""},
		{"nothing but dots", "..."},
		// Ordinary names must survive, and a separator inside one is still one
		// directory: the harness level is a component, not a subtree.
		{"plausible", "codex"},
		{"a separator inside a plausible name", "codex/nightly"},
		{"absolute", "/etc/passwd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cacheRoot := filepath.Join(t.TempDir(), "repo", "cache")
			src := write(t, filepath.Join(t.TempDir(), "rollout-hostile.jsonl"), "payload\n")

			rep, err := Cache(cacheRoot, "m1", []record.Record{
				{Machine: "m1", Harness: tc.harness, Transcript: src, PointerVerified: true},
			})
			if err != nil {
				t.Fatal(err)
			}
			if rep.Copied != 1 {
				t.Fatalf("Copied = %d, want 1: a strange harness name is not a reason to lose the transcript", rep.Copied)
			}

			got := cachedFiles(t, cacheRoot)
			if len(got) != 1 {
				t.Fatalf("the cache holds %v; a run that reports a copy nobody can find under %s wrote it somewhere else",
					got, cacheRoot)
			}
			parts := strings.Split(got[0], "/")
			if len(parts) != 2 {
				t.Fatalf("resolved destination %q, want <one harness component>/<file>", got[0])
			}
			if parts[0] == "" || parts[0] == "." || parts[0] == ".." {
				t.Errorf("harness component = %q, which is not a directory name", parts[0])
			}
			if parts[1] != wantName(src) {
				t.Errorf("file = %q, want %q", parts[1], wantName(src))
			}
			b, err := os.ReadFile(filepath.Join(cacheRoot, got[0]))
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != "payload\n" {
				t.Errorf("cached content = %q, want %q", b, "payload\n")
			}
		})
	}
}

// A transcript that cannot be copied is one transcript, not a failed run. The
// first copy error used to return, so the command printed the error and exited
// without the summary: every record after the bad one was abandoned and the
// caller could not tell whether zero or fifty transcripts had been cached.
//
// The symlink row is also the one that closes a hole rather than a report:
// os.Stat follows the last component, so a record naming a symlink materialised
// the target's bytes into the working tree under a name that says it is a
// transcript, at exit 0. Transcript is tracked, attacker-influenceable content,
// exactly as Harness is.
func TestCacheKeepsGoingWhenOneSourceCannotBeCopied(t *testing.T) {
	const secret = "PRIVATE KEY MATERIAL"

	for _, tc := range []struct {
		name  string
		build func(t *testing.T, dir string) string
	}{
		{"unreadable", func(t *testing.T, dir string) string {
			if os.Geteuid() == 0 {
				t.Skip("running as root: mode bits do not deny anything")
			}
			p := write(t, filepath.Join(dir, "rollout-locked.jsonl"), "locked\n")
			if err := os.Chmod(p, 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(p, 0o644) })
			return p
		}},
		{"a directory where a transcript should be", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "rollout-dir.jsonl")
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"a symlink to something private", func(t *testing.T, dir string) string {
			target := write(t, filepath.Join(dir, "id_ed25519"), secret+"\n")
			p := filepath.Join(dir, "rollout-link.jsonl")
			if err := os.Symlink(target, p); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"a name too long to have a destination", func(t *testing.T, dir string) string {
			// The destination is 13 characters longer than the source stem, and
			// 21 with .partial, so a source that fits can still have no
			// destination that does.
			return write(t, filepath.Join(dir, strings.Repeat("n", 246)+".jsonl"), "long\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cacheRoot := t.TempDir()
			dir := t.TempDir()
			bad := tc.build(t, dir)
			// One good transcript each side of the bad one: the second is what
			// says the run carried on rather than stopping at the failure.
			first := write(t, filepath.Join(dir, "rollout-first.jsonl"), "first\n")
			second := write(t, filepath.Join(dir, "rollout-second.jsonl"), "second\n")

			rep, err := Cache(cacheRoot, "m1", []record.Record{
				{Machine: "m1", Harness: "codex", Transcript: first, PointerVerified: true},
				{Machine: "m1", Harness: "codex", Transcript: bad, PointerVerified: true},
				{Machine: "m1", Harness: "codex", Transcript: second, PointerVerified: true},
			})
			// The error return is for what stops every record; one unusable
			// source is not that, and returning it costs the caller the report.
			if err != nil {
				t.Fatalf("Cache returned %v, which suppresses the whole report over one record", err)
			}
			if rep.Copied != 2 {
				t.Errorf("Copied = %d, want 2: the records either side of the bad one are cacheable", rep.Copied)
			}
			if rep.Unreachable != 1 {
				t.Errorf("Unreachable = %d, want 1: the one that could not be copied is still news", rep.Unreachable)
			}
			if joined := strings.Join(rep.Details, "\n"); !strings.Contains(joined, bad) {
				t.Errorf("Details must name the transcript that could not be copied; got:\n%s", joined)
			}

			files := cachedFiles(t, cacheRoot)
			if len(files) != 2 {
				t.Errorf("cache holds %v, want only the two readable transcripts", files)
			}
			for _, f := range files {
				b, err := os.ReadFile(filepath.Join(cacheRoot, f))
				if err != nil {
					t.Fatal(err)
				}
				// Following a symlink writes the target's bytes into the repo
				// under a transcript's name -- the one outcome this tool exists
				// to prevent.
				if strings.Contains(string(b), secret) {
					t.Errorf("%s holds the content of what the source pointed at:\n%s", f, b)
				}
				if strings.HasSuffix(f, ".partial") {
					t.Errorf("a failed copy left %s behind", f)
				}
			}
		})
	}
}

// The copy lands on a temporary name and is renamed into place. A leftover
// temporary from an interrupted earlier run must not bleed into the next one:
// opened without O_TRUNC, the tail of the longer old file survives past the new
// content and the cached transcript ends in bytes from a different session.
func TestCacheOverwritesAStaleTemporaryFile(t *testing.T) {
	cacheRoot := t.TempDir()
	src := write(t, filepath.Join(t.TempDir(), "rollout-stale.jsonl"), "new\n")

	dst := filepath.Join(cacheRoot, "codex", wantName(src))
	write(t, dst+".partial", "stale bytes from an interrupted run\n")

	rep, err := Cache(cacheRoot, "m1", []record.Record{
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
	for _, f := range cachedFiles(t, cacheRoot) {
		if strings.HasSuffix(f, ".partial") {
			t.Errorf("a successful run left %s behind", f)
		}
	}
}

// The cache used to live at <root>/.agents/.trace-cache. Moving it left real
// content stranded: on the machine this was written for, one worktree's copy
// held 58 transcripts totalling 36 MB, and every one of them was the only
// surviving copy of something the harness had already deleted. A move that
// silently skipped them would have destroyed more than the bug did.
func TestMigrateLegacyCacheMovesContentAndClearsTheOldDirectory(t *testing.T) {
	old := t.TempDir()
	newRoot := filepath.Join(t.TempDir(), "new")

	write(t, filepath.Join(old, "codex", "rollout-a-aaaaaaaaaaaa.jsonl"), "A\n")
	write(t, filepath.Join(old, "claude-code", "session-b-bbbbbbbbbbbb.jsonl"), "B\n")
	// Infrastructure the old layout needed and the new one does not.
	write(t, filepath.Join(old, ".gitignore"), "*\n")

	rep, err := MigrateLegacyCache(old, newRoot)
	if err != nil {
		t.Fatalf("MigrateLegacyCache: %v", err)
	}
	if rep.Moved != 2 {
		t.Errorf("Moved = %d, want 2 (the .gitignore is not a transcript)", rep.Moved)
	}

	for path, want := range map[string]string{
		filepath.Join(newRoot, "codex", "rollout-a-aaaaaaaaaaaa.jsonl"):       "A\n",
		filepath.Join(newRoot, "claude-code", "session-b-bbbbbbbbbbbb.jsonl"): "B\n",
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("expected %s: %v", path, err)
			continue
		}
		if string(b) != want {
			t.Errorf("%s = %q, want %q", path, b, want)
		}
	}
	// The harness level must survive the move: it is what keeps two harnesses'
	// identically-named transcripts apart.
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("the old cache directory is still there (%v); a migration that "+
			"leaves it behind invites a second one that finds nothing and says so",
			err)
	}
}

// Same transcript, cached from two worktrees: cacheName hashes the SOURCE path,
// so both copies carry one name and hold identical bytes. The destination wins
// and the run is not an error -- this is the expected shape of consolidating
// several worktrees, not a conflict.
func TestMigrateLegacyCacheKeepsWhatIsAlreadyThere(t *testing.T) {
	old := t.TempDir()
	newRoot := t.TempDir()

	write(t, filepath.Join(old, "codex", "rollout-x-cccccccccccc.jsonl"), "from the worktree\n")
	write(t, filepath.Join(newRoot, "codex", "rollout-x-cccccccccccc.jsonl"), "already here\n")

	rep, err := MigrateLegacyCache(old, newRoot)
	if err != nil {
		t.Fatalf("MigrateLegacyCache: %v", err)
	}
	if rep.Moved != 0 || rep.Skipped != 1 {
		t.Errorf("Moved/Skipped = %d/%d, want 0/1", rep.Moved, rep.Skipped)
	}
	b, err := os.ReadFile(filepath.Join(newRoot, "codex", "rollout-x-cccccccccccc.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "already here\n" {
		t.Errorf("content = %q; a migration must never overwrite a transcript already cached", b)
	}
}

// Nothing to do is not a failure, and must not be reported as one: every
// `agents trace cache` calls this, forever, long after the last old cache is gone.
func TestMigrateLegacyCacheIsSilentWhenThereIsNoOldCache(t *testing.T) {
	rep, err := MigrateLegacyCache(filepath.Join(t.TempDir(), "absent"), t.TempDir())
	if err != nil {
		t.Fatalf("an absent old cache must not be an error: %v", err)
	}
	if rep.Moved != 0 || rep.Skipped != 0 || len(rep.Details) != 0 {
		t.Errorf("report = %+v, want an empty one", rep)
	}
}

// The destructive path, and the only one that can lose a transcript for good.
//
// A file blocks the harness directory the move needs to create, so one transcript
// cannot go anywhere. The old cache must survive intact: the alternative is
// deleting the last copy of something the harness already dropped, which is
// worse than never having migrated.
func TestMigrateLegacyCacheKeepsTheOldDirectoryWhenAMoveFails(t *testing.T) {
	old := t.TempDir()
	newRoot := t.TempDir()

	stuck := write(t, filepath.Join(old, "codex", "rollout-stuck-dddddddddddd.jsonl"), "irreplaceable\n")
	// Not a directory, so MkdirAll below it fails with ENOTDIR.
	write(t, filepath.Join(newRoot, "codex"), "in the way\n")

	rep, err := MigrateLegacyCache(old, newRoot)
	if err != nil {
		t.Fatalf("one stuck file must not fail the whole run: %v", err)
	}
	if rep.Failed != 1 || rep.Moved != 0 {
		t.Errorf("Failed/Moved = %d/%d, want 1/0", rep.Failed, rep.Moved)
	}
	if joined := strings.Join(rep.Details, "\n"); !strings.Contains(joined, stuck) {
		t.Errorf("Details must name what could not be moved; got:\n%s", joined)
	}

	b, err := os.ReadFile(stuck)
	if err != nil {
		t.Fatalf("the transcript that could not be moved was deleted anyway: %v", err)
	}
	if string(b) != "irreplaceable\n" {
		t.Errorf("content = %q, want it untouched", b)
	}
}

// CachedPath must agree with where Cache actually wrote, or every reader looks
// in the wrong place. Asserted against a real copy rather than against a second
// copy of the naming rule, which would only prove the rule equals itself.
func TestCachedPathNamesWhereCacheActuallyWrote(t *testing.T) {
	cacheRoot := t.TempDir()
	src := write(t, filepath.Join(t.TempDir(), "rollout-agree.jsonl"), "content\n")
	rec := record.Record{Machine: "m1", Harness: "codex", Transcript: src, PointerVerified: true}

	if _, err := Cache(cacheRoot, "m1", []record.Record{rec}); err != nil {
		t.Fatal(err)
	}
	got := CachedPath(cacheRoot, rec)
	b, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("CachedPath = %q, which is not what Cache wrote (%v); the cache holds %v",
			got, err, cachedFiles(t, cacheRoot))
	}
	if string(b) != "content\n" {
		t.Errorf("content at CachedPath = %q, want the transcript", b)
	}
}

// The same cleaning Cache applies, so a record spelling its path differently
// resolves to the one copy rather than to a name nothing wrote.
func TestCachedPathCleansThePathTheSameWayCacheDoes(t *testing.T) {
	cacheRoot := t.TempDir()
	dir := t.TempDir()
	src := write(t, filepath.Join(dir, "rollout-clean.jsonl"), "x\n")
	messy := filepath.Join(dir, ".", "sub", "..", "rollout-clean.jsonl")

	if _, err := Cache(cacheRoot, "m1", []record.Record{
		{Machine: "m1", Harness: "codex", Transcript: src, PointerVerified: true},
	}); err != nil {
		t.Fatal(err)
	}
	got := CachedPath(cacheRoot, record.Record{Machine: "m1", Harness: "codex", Transcript: messy})
	if _, err := os.ReadFile(got); err != nil {
		t.Errorf("CachedPath(%q) = %q, which does not resolve to the copy Cache made "+
			"from the same file spelled plainly: %v", messy, got, err)
	}
}

func TestResolvePrefersTheSourceAndFallsBackToTheCache(t *testing.T) {
	cacheRoot := t.TempDir()
	src := write(t, filepath.Join(t.TempDir(), "rollout-live.jsonl"), "live\n")
	rec := record.Record{Machine: "m1", Harness: "codex", Transcript: src, PointerVerified: true}
	if _, err := Cache(cacheRoot, "m1", []record.Record{rec}); err != nil {
		t.Fatal(err)
	}

	// While the harness still has it, the source is the answer: it is the one
	// that is still growing, so a cached copy may be short.
	path, origin, err := Resolve(cacheRoot, rec)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if origin != "source" || path != src {
		t.Errorf("Resolve = (%q, %q), want the live source %q", path, origin, src)
	}

	// The case the whole cache exists for.
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	path, origin, err = Resolve(cacheRoot, rec)
	if err != nil {
		t.Fatalf("Resolve after the harness deleted the source: %v", err)
	}
	if origin != "cache" {
		t.Errorf("origin = %q, want %q", origin, "cache")
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "live\n" {
		t.Errorf("Resolve pointed at %q holding %q (%v), want the cached transcript", path, b, err)
	}
}

// An error a reader can act on names both places that were tried; "not found"
// alone leaves them unable to tell a missing cache from a missing record.
func TestResolveNamesBothPathsWhenNeitherHoldsIt(t *testing.T) {
	cacheRoot := t.TempDir()
	rec := record.Record{
		Machine: "m1", Harness: "codex",
		Transcript: filepath.Join(t.TempDir(), "rollout-vanished.jsonl"), PointerVerified: true,
	}
	_, _, err := Resolve(cacheRoot, rec)
	if err == nil {
		t.Fatal("Resolve must fail when neither the source nor the cache holds it")
	}
	if !strings.Contains(err.Error(), rec.Transcript) {
		t.Errorf("the error must name the source path; got: %v", err)
	}
	if !strings.Contains(err.Error(), CachedPath(cacheRoot, rec)) {
		t.Errorf("the error must name the cache path it looked in; got: %v", err)
	}
}

// Skip-if-present froze a partial copy forever.
//
// Latent until the hook began caching at subagent-stop, and live from that
// moment: if the harness flushes the tail after the hook fires, the copy taken
// there is short, and every later run saw a destination that existed and left
// it alone. The cache would then hold a truncated transcript under a name that
// says it is the whole thing -- worse than holding nothing, because nothing
// prompts a second look.
func TestCacheReplacesACopyWhoseSourceHasGrown(t *testing.T) {
	cacheRoot := t.TempDir()
	src := write(t, filepath.Join(t.TempDir(), "rollout-growing.jsonl"), `{"line":1}`+"\n")
	rec := record.Record{Machine: "m1", Harness: "codex", Transcript: src, PointerVerified: true}

	if _, err := Cache(cacheRoot, "m1", []record.Record{rec}); err != nil {
		t.Fatal(err)
	}
	// The harness appends after the copy was taken.
	f, err := os.OpenFile(src, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"line":2}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	rep, err := Cache(cacheRoot, "m1", []record.Record{rec})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 1 {
		t.Errorf("Copied = %d, want 1: the source grew, so the cached copy is a prefix", rep.Copied)
	}
	b, err := os.ReadFile(CachedPath(cacheRoot, rec))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"line":1}` + "\n" + `{"line":2}` + "\n"; string(b) != want {
		t.Errorf("cached content = %q, want the whole transcript %q", b, want)
	}
}

// A source that did not grow must not be re-read. This runs on a hook the
// harness is blocked on, and the cache holds files of several megabytes.
func TestCacheDoesNotRecopyASourceOfTheSameSize(t *testing.T) {
	cacheRoot := t.TempDir()
	src := write(t, filepath.Join(t.TempDir(), "rollout-settled.jsonl"), "{}\n")
	rec := record.Record{Machine: "m1", Harness: "codex", Transcript: src, PointerVerified: true}

	if _, err := Cache(cacheRoot, "m1", []record.Record{rec}); err != nil {
		t.Fatal(err)
	}
	rep, err := Cache(cacheRoot, "m1", []record.Record{rec})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Copied != 0 || rep.Skipped != 1 {
		t.Errorf("Copied/Skipped = %d/%d, want 0/1", rep.Copied, rep.Skipped)
	}
}

// A source SHORTER than the copy is the dangerous direction: a transcript that
// was replaced or truncated must not overwrite the fuller copy we hold, because
// ours may be the only one that still has the missing part.
func TestCacheKeepsTheFullerCopyWhenTheSourceShrinks(t *testing.T) {
	cacheRoot := t.TempDir()
	src := write(t, filepath.Join(t.TempDir(), "rollout-shrunk.jsonl"), "{\"a\":1}\n{\"b\":2}\n")
	rec := record.Record{Machine: "m1", Harness: "codex", Transcript: src, PointerVerified: true}

	if _, err := Cache(cacheRoot, "m1", []record.Record{rec}); err != nil {
		t.Fatal(err)
	}
	write(t, src, "{}\n") // truncated behind our back

	if _, err := Cache(cacheRoot, "m1", []record.Record{rec}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(CachedPath(cacheRoot, rec))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "{\"a\":1}\n{\"b\":2}\n" {
		t.Errorf("cached content = %q; a shrunken source must not replace the fuller "+
			"copy -- ours may be the only one that still holds what was dropped", b)
	}
}
