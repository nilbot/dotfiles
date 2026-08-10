package handoff

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// caseInsensitiveFS reports whether dir aliases names that differ only in case,
// by asking the filesystem rather than by guessing from runtime.GOOS -- a
// case-sensitive volume on macOS and a case-insensitive one on Linux both exist.
// Same probe as internal/memory's, for the same hazard.
func caseInsensitiveFS(t *testing.T, dir string) bool {
	t.Helper()
	p := filepath.Join(dir, "casefold-probe")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p)
	_, err := os.Stat(filepath.Join(dir, "CASEFOLD-PROBE"))
	return err == nil
}

// makeUndeletable pins one file so os.Remove on it fails while its neighbours in
// the same writable directory still go. Mode bits cannot do this -- on POSIX
// unlink is a permission on the directory, not on the file -- so it uses BSD's
// user-immutable flag and reports false where there is no chflags to run.
func makeUndeletable(t *testing.T, p string) bool {
	t.Helper()
	if err := exec.Command("chflags", "uchg", p).Run(); err != nil {
		return false
	}
	// Registered after t.TempDir's own cleanup and therefore run before it: an
	// immutable file left behind stops the temp directory being removed.
	t.Cleanup(func() { _ = exec.Command("chflags", "nouchg", p).Run() })
	return true
}

// naivePath is where an implementation that interpolates its arguments straight
// into a path would put the file. It is built here, without calling anything in
// this package, so a sanitiser that agrees with itself and still escapes cannot
// pass the tests that use it.
func naivePath(agentsDir, laneName, session string, when time.Time) string {
	return filepath.Clean(filepath.Join(agentsDir, "reports", "handoff", laneName,
		when.UTC().Format("2006-01-02")+"-"+session+".md"))
}

// writeRaw plants a handoff file that Write would never have produced. Every
// hostile-content test needs one: Write refuses the input at the boundary, so
// the only way to exercise the reading side is to put the bytes on disk
// directly -- which is exactly what a hand-edit or a merge does.
func writeRaw(t *testing.T, agentsDir, laneName, name, body string) string {
	t.Helper()
	dir := filepath.Join(agentsDir, "reports", "handoff", laneName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// unescapedIndexes finds every c in s that is not preceded by an odd number of
// backslashes -- i.e. every one markdown would act on.
func unescapedIndexes(s string, c byte) []int {
	var out []int
	for i := 0; i < len(s); i++ {
		if s[i] != c {
			continue
		}
		n := 0
		for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
			n++
		}
		if n%2 == 0 {
			out = append(out, i)
		}
	}
	return out
}

// rowFor returns the one table row of the index that mentions want.
func rowFor(t *testing.T, idx, want string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(idx, "\n") {
		if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "|---") &&
			!strings.HasPrefix(line, "| when ") && strings.Contains(line, want) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one row mentioning %q, got %d:\n%s", want, len(found), idx)
	}
	return found[0]
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			n++
		}
		return nil
	})
	return n
}

func TestWritePlacesFilePerLaneAndSession(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)

	p, err := Write(dir, "sq-123-payments", "019fdcab", "reviewed", "Left off at the retry test.", when)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := filepath.Join(dir, "reports", "handoff", "sq-123-payments", "2026-08-10-019fdcab.md")
	if p != want {
		t.Fatalf("path = %q, want %q", p, want)
	}

	b, _ := os.ReadFile(p)
	// Every field carries a distinct value, and "when" is here rather than only
	// in the path: the date in the filename is one day's resolution, so a Write
	// that dropped the timestamp from the frontmatter would still land the file
	// in the right place and lose the ordering key.
	for _, want := range []string{
		"lane: sq-123-payments", "status: reviewed", "session: 019fdcab",
		"when: 2026-08-10T09:00:00Z", "Left off at the retry test.",
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("file missing %q:\n%s", want, b)
		}
	}
}

// New notes and deliberate rewrites of the same regular handoff are both
// supported. This is the positive control for symlink refusal: the boundary is
// about what kind of filesystem object would be replaced, not a blanket
// no-overwrite policy.
func TestWriteCreatesAndRewritesARegularHandoff(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)

	first, err := Write(dir, "lane-a", "s1", StatusReviewed, "first body", when)
	if err != nil {
		t.Fatalf("new Write: %v", err)
	}
	second, err := Write(dir, "lane-a", "s1", StatusReviewed, "replacement body", when)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if second != first {
		t.Fatalf("rewrite path = %q, want existing regular handoff %q", second, first)
	}
	b, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "first body") || !strings.Contains(string(b), "replacement body") {
		t.Errorf("regular handoff was not intentionally replaced:\n%s", b)
	}
}

// Atomic replacement is only valid for an absent leaf or an existing regular
// file. A FIFO is repository-controlled filesystem input too; replacing it
// would silently change the kind of object at a tracked destination.
//
// Kills: accepting every existing non-symlink leaf in atomicWrite.
func TestWriteRefusesANonRegularHandoffLeaf(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	laneDir := filepath.Join(dir, "reports", "handoff", "lane-a")
	if err := os.MkdirAll(laneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(laneDir, "2026-08-10-s1.md")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Write(dir, "lane-a", "s1", StatusReviewed, "body", when)
	if err == nil {
		t.Fatal("Write replaced a FIFO handoff destination")
	}
	for _, want := range []string{"2026-08-10-s1.md", "not a regular file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q; got: %v", want, err)
		}
	}
	fi, statErr := os.Lstat(p)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if fi.Mode()&os.ModeNamedPipe == 0 {
		t.Errorf("the FIFO was replaced: mode %v", fi.Mode())
	}
}

// INDEX.md reaches the same atomic primitive as a handoff leaf and therefore
// has the same absent-or-regular contract.
//
// Kills: hardening only the requested handoff leaf while allowing a FIFO index
// destination to be replaced.
func TestWriteIndexRefusesANonRegularLeaf(t *testing.T) {
	dir := t.TempDir()
	writeRaw(t, dir, "lane-a", "2026-08-10-s1.md",
		"---\nlane: lane-a\nsession: s1\nstatus: reviewed\nwhen: 2026-08-10T09:00:00Z\n---\n\nbody\n")
	index := filepath.Join(dir, "reports", "handoff", "INDEX.md")
	if err := syscall.Mkfifo(index, 0o600); err != nil {
		t.Fatal(err)
	}

	err := WriteIndex(dir)
	if err == nil {
		t.Fatal("WriteIndex replaced a FIFO destination")
	}
	for _, want := range []string{"INDEX.md", "not a regular file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q; got: %v", want, err)
		}
	}
	fi, statErr := os.Lstat(index)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if fi.Mode()&os.ModeNamedPipe == 0 {
		t.Errorf("the FIFO was replaced: mode %v", fi.Mode())
	}
}

// Atomic rename must not broaden an existing file's permissions. The default
// is only for a new leaf; an intentional rewrite inherits the regular leaf's
// permission bits.
//
// Kills: always creating the replacement temporary with 0644.
func TestRegularRewritesPreserveExistingPermissions(t *testing.T) {
	t.Run("handoff", func(t *testing.T) {
		dir := t.TempDir()
		when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
		p, err := Write(dir, "lane-a", "s1", StatusReviewed, "first", when)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Write(dir, "lane-a", "s1", StatusReviewed, "replacement", when); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != 0o600 {
			t.Errorf("handoff mode = %04o, want 0600", got)
		}
	})

	t.Run("index", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := Write(dir, "lane-a", "s1", StatusReviewed, "body", time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		index := filepath.Join(dir, "reports", "handoff", "INDEX.md")
		if err := os.Chmod(index, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := WriteIndex(dir); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(index)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != 0o600 {
			t.Errorf("index mode = %04o, want 0600", got)
		}
	})
}

// Reading the old mode must not turn the rewrite into an in-place write. A
// tracked hardlink may share an inode with a file outside .agents; atomic rename
// must replace only the handoff directory entry and leave the shared inode's
// bytes untouched.
func TestRegularRewriteDoesNotWriteThroughAHardlink(t *testing.T) {
	dir := t.TempDir()
	laneDir := filepath.Join(dir, "reports", "handoff", "lane-a")
	if err := os.MkdirAll(laneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "shared-inode.md")
	wantExternal := []byte("external bytes must survive\n")
	if err := os.WriteFile(external, wantExternal, 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(laneDir, "2026-08-10-s1.md")
	if err := os.Link(external, destination); err != nil {
		t.Fatal(err)
	}

	p, err := Write(dir, "lane-a", "s1", StatusReviewed, "replacement body", time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	gotExternal, err := os.ReadFile(external)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotExternal, wantExternal) {
		t.Errorf("hardlink target changed:\n got %q\nwant %q", gotExternal, wantExternal)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "replacement body") {
		t.Errorf("handoff directory entry was not replaced:\n%s", b)
	}
	externalInfo, err := os.Stat(external)
	if err != nil {
		t.Fatal(err)
	}
	destinationInfo, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(externalInfo, destinationInfo) {
		t.Error("handoff still shares the external inode after rewrite")
	}
}

// INDEX.md is another repository-controlled destination written as a side
// effect of Write. Protecting only the requested handoff leaf would still let a
// tracked index symlink redirect that side effect to an arbitrary file.
//
// Kills: making only the handoff note atomic while WriteIndex still uses
// os.WriteFile on its final path.
func TestWriteDoesNotFollowASymlinkedIndex(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "reports", "handoff", "INDEX.md")
	if err := os.MkdirAll(filepath.Dir(index), 0o755); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "outside.txt")
	want := []byte("outside index target\n")
	if err := os.WriteFile(external, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, index); err != nil {
		t.Fatal(err)
	}

	p, err := Write(dir, "lane-a", "s1", StatusReviewed, "body", time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC))
	if p == "" {
		t.Fatalf("the handoff itself should reach disk before index refresh fails: %v", err)
	}
	var stale *IndexError
	if !errors.As(err, &stale) {
		t.Fatalf("err = %v (%T), want IndexError", err, err)
	}
	for _, s := range []string{"INDEX.md", "symlink"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("the refusal must name %q; got: %v", s, err)
		}
	}
	got, readErr := os.ReadFile(external)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("external target changed:\n got %q\nwant %q", got, want)
	}
}

// Two agents on one branch must not be able to clobber each other. Distinct
// sessions means distinct files, which is the entire reason for this scheme.
func TestConcurrentSessionsGetSeparateFiles(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	a, _ := Write(dir, "same-lane", "sess-a", "draft", "A", when)
	b, _ := Write(dir, "same-lane", "sess-b", "draft", "B", when)
	if a == b {
		t.Fatal("two sessions collided on one path")
	}
	for _, p := range []string{a, b} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s missing: %v", p, err)
		}
	}
}

// Every field distinct and non-zero, because the four frontmatter keys are
// otherwise interchangeable to a test that only counts entries.
//
// Kills: yaml tags swapped between Session and Status, a parse that drops
// `when` (leaving the zero time, which then orders everything wrongly), and a
// Path that is absolute or a bare basename instead of relative to
// reports/handoff -- the last one being what Prune joins back onto the root.
func TestListReturnsEveryFieldTheEntryWasWrittenWith(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 8, 10, 9, 30, 0, 0, time.UTC)
	if _, err := Write(dir, "lane-x", "sess-y", "reviewed", "body-z", when); err != nil {
		t.Fatal(err)
	}

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Lane != "lane-x" {
		t.Errorf("Lane = %q, want %q", e.Lane, "lane-x")
	}
	if e.Session != "sess-y" {
		t.Errorf("Session = %q, want %q", e.Session, "sess-y")
	}
	if e.Status != StatusReviewed {
		t.Errorf("Status = %q, want %q", e.Status, StatusReviewed)
	}
	if !e.When.Equal(when) {
		t.Errorf("When = %v, want %v", e.When, when)
	}
	if e.Path != "lane-x/2026-08-10-sess-y.md" {
		t.Errorf("Path = %q, want it relative to reports/handoff", e.Path)
	}
}

// Status is provenance, not an open-ended workflow state. Raw files arrive via
// hand edits and merges, bypassing Write's normalization, so the parser itself
// must reject both an absent claim and an invented stronger-sounding one.
//
// Kills: validating only Write input, accepting every single-line status, or
// treating a missing status as the zero-value equivalent of draft.
func TestListRefusesMissingOrUnrecognisedStatus(t *testing.T) {
	for _, tc := range []struct {
		label  string
		status string
	}{
		{"missing", ""},
		{"unrecognised", "status: approved\n"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			dir := t.TempDir()
			name := "2026-08-10-s1.md"
			writeRaw(t, dir, "lane-a", name,
				"---\nlane: lane-a\nsession: s1\n"+tc.status+"when: 2026-08-10T09:00:00Z\n---\n\nbody\n")

			_, err := List(dir)
			if err == nil {
				t.Fatal("List accepted a handoff without canonical provenance")
			}
			for _, want := range []string{filepath.ToSlash(filepath.Join("lane-a", name)), "status"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal must name %q; got: %v", want, err)
				}
			}
			if strings.Contains(err.Error(), dir) {
				t.Errorf("the refusal leaked the unrelated absolute temp path %q: %v", dir, err)
			}
			if err := WriteIndex(dir); err == nil {
				t.Fatal("WriteIndex rendered a handoff without canonical provenance")
			}
		})
	}
}

// Repository-controlled directory entries can contain control characters even
// though Write refuses them. Their spelling must be checked before parse gets
// a chance to interpolate the path into an otherwise unrelated status error.
//
// Kills: validating e.Path only after parse returns successfully.
func TestListSafelyQuotesControlCharactersInFilesystemPathsBeforeParsing(t *testing.T) {
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
			dir := t.TempDir()
			writeRaw(t, dir, tc.lane, tc.name,
				"---\nlane: lane-a\nsession: s1\nstatus: approved\nwhen: 2026-08-10T09:00:00Z\n---\n\nbody\n")

			_, err := List(dir)
			if err == nil {
				t.Fatal("List accepted a repository-controlled path containing a control character")
			}
			if strings.Contains(err.Error(), tc.rawPath) {
				t.Fatalf("error contains the raw path and can forge another diagnostic line: %q", err)
			}
			for _, want := range []string{"FORGED-OUTPUT", `\n`, "control character"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the safely quoted refusal must contain %q; got: %q", want, err)
				}
			}
			if strings.ContainsAny(err.Error(), "\n\r\t") {
				t.Errorf("returned error contains a raw line-breaking control character: %q", err)
			}
		})
	}
}

// Both and only both canonical provenance values survive the same raw parsing
// route used for hand edits and merges, then appear in the generated index.
func TestListAndRenderAcceptBothCanonicalStatuses(t *testing.T) {
	dir := t.TempDir()
	for i, status := range []string{StatusReviewed, StatusDraft} {
		name := fmt.Sprintf("2026-08-%02d-s%d.md", 10+i, i)
		writeRaw(t, dir, "lane-a", name, fmt.Sprintf(
			"---\nlane: lane-a\nsession: s%d\nstatus: %s\nwhen: 2026-08-%02dT09:00:00Z\n---\n\nbody\n",
			i, status, 10+i))
	}

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	idx := string(RenderIndex(entries))
	for _, status := range []string{StatusReviewed, StatusDraft} {
		if strings.Count(idx, "| "+status+" |") != 1 {
			t.Errorf("want exactly one %q provenance row:\n%s", status, idx)
		}
	}
}

// RenderIndex is exported and Task 16 calls it directly with Entry values, so
// parser validation cannot be its only provenance boundary. An unsupported
// value carries no evidence of review and must degrade to draft rather than be
// presented as a stronger-sounding status.
//
// Kills: interpolating Entry.Status directly in the generated row.
func TestRenderIndexDegradesAnUnrecognisedDirectStatusToDraft(t *testing.T) {
	idx := string(RenderIndex([]Entry{{
		Lane: "lane-a", Session: "s1", Status: "approved",
		When: time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC),
		Path: "lane-a/2026-08-10-s1.md",
	}}))
	if strings.Contains(idx, "| approved |") {
		t.Fatalf("RenderIndex presented unsupported provenance:\n%s", idx)
	}
	if !strings.Contains(idx, "| draft |") {
		t.Errorf("unsupported direct status must degrade to the weaker claim:\n%s", idx)
	}
}

// Provenance is a claim about how much checking a note has had, and only two
// values mean anything. An unrecognised third one is not evidence that anyone
// checked it, so it has to degrade to the weaker claim rather than reach the
// index as a word the reader has no way to weigh.
//
// Kills: writing status through as given. The "Reviewed" case is the adjacent
// non-match -- it kills a comparison written with strings.EqualFold, which
// would promote a value nothing in this tool ever writes.
func TestWriteDegradesAnUnrecognisedStatusToDraft(t *testing.T) {
	for _, tc := range []struct{ given, want string }{
		{"reviewed", StatusReviewed},
		{"draft", StatusDraft},
		{"urgent", StatusDraft},
		{"Reviewed", StatusDraft},
		{"", StatusDraft},
	} {
		t.Run("status="+tc.given, func(t *testing.T) {
			dir := t.TempDir()
			p, err := Write(dir, "lane-a", "s1", tc.given, "x", time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(p)
			if !strings.Contains(string(b), "status: "+tc.want+"\n") {
				t.Errorf("status %q was recorded as something other than %q:\n%s", tc.given, tc.want, b)
			}
			entries, err := List(dir)
			if err != nil {
				t.Fatal(err)
			}
			if entries[0].Status != tc.want {
				t.Errorf("List reports status %q, want %q", entries[0].Status, tc.want)
			}
		})
	}
}

func TestListAndRenderIndex(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	Write(dir, "lane-a", "s1", "reviewed", "x", base)
	Write(dir, "lane-a", "s2", "draft", "x", base.Add(24*time.Hour))
	Write(dir, "lane-b", "s3", "draft", "x", base.Add(48*time.Hour))

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	idx := string(RenderIndex(entries))
	if !strings.Contains(idx, "GENERATED") {
		t.Error("the index must say it is generated")
	}
	// Newest lane first: the reader wants what is live, not what is oldest.
	if strings.Index(idx, "lane-b") > strings.Index(idx, "lane-a") {
		t.Error("lanes must be ordered by most recent activity")
	}
	if !strings.Contains(idx, "draft") || !strings.Contains(idx, "reviewed") {
		t.Error("the index must carry provenance so a reader can weigh entries")
	}

	if string(RenderIndex(entries)) != idx {
		t.Error("RenderIndex must be deterministic")
	}
}

// Ordering between lanes, which TestListAndRenderIndex above cannot
// discriminate: its newest lane, lane-b, is also the alphabetically last one, so
// reverse-alphabetical-by-name and ranked-by-oldest-entry both agree with the
// right answer there and pass it.
//
// Here every lane name sorts against its recency and every lane holds two
// entries, so the four candidate rules give four different answers:
//
//	newest entry first (correct) -> zeta, alpha, mid
//	ascending by name            -> alpha, mid, zeta
//	descending by name           -> zeta, mid, alpha
//	ranked by oldest entry       -> alpha, mid, zeta
//
// The entries are handed over in two orders, neither of them the answer, so a
// comparator that returns a constant cannot pass by leaving the input alone.
//
// Kills: lanes[i] < lanes[j] and lanes[i] > lanes[j] (name only, time ignored
// entirely), and ranking a lane by byLane[l][len-1] -- its oldest entry --
// instead of byLane[l][0].
func TestRenderIndexOrdersLanesByTheirNewestEntry(t *testing.T) {
	base := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	at := func(lane, session string, d time.Duration) Entry {
		return Entry{Lane: lane, Session: session, Status: StatusDraft, When: base.Add(d),
			Path: lane + "/2026-08-10-" + session + ".md"}
	}
	// zeta is newest overall and also holds the oldest entry of all; alpha's
	// oldest is the newest "oldest"; mid sits between them either way.
	entries := []Entry{
		at("mid", "m-old", 30*time.Minute),
		at("alpha", "a-new", 5*time.Hour),
		at("zeta", "z-old", 0),
		at("mid", "m-new", 1*time.Hour),
		at("alpha", "a-old", 4*time.Hour),
		at("zeta", "z-new", 10*time.Hour),
	}
	reversed := make([]Entry, len(entries))
	for i, e := range entries {
		reversed[len(entries)-1-i] = e
	}

	for _, tc := range []struct {
		label string
		in    []Entry
	}{
		{"interleaved", entries},
		{"interleaved backwards", reversed},
	} {
		t.Run(tc.label, func(t *testing.T) {
			idx := string(RenderIndex(tc.in))
			z := strings.Index(idx, "## zeta")
			a := strings.Index(idx, "## alpha")
			m := strings.Index(idx, "## mid")
			if z < 0 || a < 0 || m < 0 {
				t.Fatalf("a lane heading is missing:\n%s", idx)
			}
			if !(z < a && a < m) {
				t.Errorf("lanes must be ranked by their newest entry (zeta, alpha, mid); got zeta=%d alpha=%d mid=%d in:\n%s", z, a, m, idx)
			}
		})
	}
}

// Ordering within a lane, which the lane-ordering assertion above does not
// reach. The fixture puts every entry on one date so the order List returns
// them in -- by path, and the path leads with the date -- is the exact reverse
// of the order the index must print them in. The input is also handed to
// RenderIndex in both directions.
//
// Kills: no within-lane sort at all (which prints List's order), an ascending
// sort (When.Before), and a sort that happens to agree with the input because
// the fixture never disagreed with it.
func TestRenderIndexOrdersALaneNewestFirst(t *testing.T) {
	base := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	// Alphabetical by session is oldest-last; chronological is oldest-first.
	newest := Entry{Lane: "l", Session: "a", Status: StatusDraft, When: base.Add(2 * time.Hour), Path: "l/2026-08-10-a.md"}
	middle := Entry{Lane: "l", Session: "b", Status: StatusDraft, When: base.Add(1 * time.Hour), Path: "l/2026-08-10-b.md"}
	oldest := Entry{Lane: "l", Session: "c", Status: StatusDraft, When: base, Path: "l/2026-08-10-c.md"}

	for _, tc := range []struct {
		label string
		in    []Entry
	}{
		{"path order", []Entry{newest, middle, oldest}},
		{"reverse path order", []Entry{oldest, middle, newest}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			idx := string(RenderIndex(tc.in))
			a, b, c := strings.Index(idx, "-a.md"), strings.Index(idx, "-b.md"), strings.Index(idx, "-c.md")
			if a < 0 || b < 0 || c < 0 {
				t.Fatalf("an entry is missing from the index:\n%s", idx)
			}
			if !(a < b && b < c) {
				t.Errorf("entries must be newest first within a lane; got a=%d b=%d c=%d in:\n%s", a, b, c, idx)
			}
		})
	}
}

// Two agents finishing in the same second is the case this whole scheme is
// designed for, and sort.Slice is not stable, so When alone is not a total
// order. The index is byte-compared by the pre-commit guard: an order that
// depends on the order entries arrived in makes the guard block on a diff with
// nothing behind it.
//
// Kills: a comparator that returns only When.After, which leaves the input
// order untouched for a tie.
func TestRenderIndexIsTotallyOrderedWhenTwoEntriesShareAnInstant(t *testing.T) {
	when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	a := Entry{Lane: "l", Session: "sess-a", Status: StatusDraft, When: when, Path: "l/2026-08-10-sess-a.md"}
	b := Entry{Lane: "l", Session: "sess-b", Status: StatusDraft, When: when, Path: "l/2026-08-10-sess-b.md"}

	forward := string(RenderIndex([]Entry{a, b}))
	backward := string(RenderIndex([]Entry{b, a}))
	if forward != backward {
		t.Errorf("the index depends on the order entries arrived in:\n%s\n---\n%s", forward, backward)
	}
	if i, j := strings.Index(forward, "sess-a"), strings.Index(forward, "sess-b"); i > j {
		t.Errorf("a tie must break on session so the order is total; got:\n%s", forward)
	}
}

// Counting survivors cannot tell "keeps the newest" from "keeps the oldest", so
// this asserts which entries are left. The fixture gives every entry the same
// date and orders the sessions against the clock, so the order List returns
// (by path, i.e. by session here) is the reverse of the order Prune must judge
// by.
//
// Kills: keeping the oldest `keep`, keeping the first or last `keep` in list
// order, and pruning globally instead of per lane (which would evict the quiet
// lane's only entry).
func TestPruneKeepsNewestPerLane(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	for i, s := range []string{"e", "d", "c", "b", "a"} {
		if _, err := Write(dir, "busy", s, "draft", "x", base.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Write(dir, "quiet", "s1", "draft", "x", base); err != nil {
		t.Fatal(err)
	}

	removed, err := Prune(dir, 2)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	wantRemoved := []string{"busy/2026-08-01-c.md", "busy/2026-08-01-d.md", "busy/2026-08-01-e.md"}
	if strings.Join(removed, ",") != strings.Join(wantRemoved, ",") {
		t.Errorf("removed %v, want %v", removed, wantRemoved)
	}

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := map[string][]string{}
	for _, e := range entries {
		got[e.Lane] = append(got[e.Lane], e.Session)
	}
	if strings.Join(got["busy"], ",") != "a,b" {
		t.Errorf("busy kept %v, want the two newest (a,b)", got["busy"])
	}
	if strings.Join(got["quiet"], ",") != "s1" {
		t.Errorf("quiet kept %v, want s1 (prune is per lane)", got["quiet"])
	}
	for _, p := range wantRemoved {
		if _, err := os.Stat(filepath.Join(dir, "reports", "handoff", filepath.FromSlash(p))); err == nil {
			t.Errorf("%s is still on disk but was reported as removed", p)
		}
	}
}

// Prune removes files one at a time and INDEX.md is the durable record of what
// is left, so a partial failure must not leave the index naming files that are
// gone. The coin flip is only *between* lanes: within one lane sortNewestFirst
// fixes the order, so one lane, five entries and keep=2 makes the second removal
// target deterministic.
//
// Kills: `return removed, err` from inside the loop, which skips WriteIndex and
// leaves the index listing the entry that was already deleted.
func TestPruneLeavesTheIndexMatchingWhatItActuallyRemoved(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	// Newest first is a, b, c, d, e; keep=2 keeps a and b and removes c, then d,
	// then e.
	for i, s := range []string{"e", "d", "c", "b", "a"} {
		if _, err := Write(dir, "busy", s, "draft", "x", base.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	blocked := filepath.Join(dir, "reports", "handoff", "busy", "2026-08-01-d.md")
	if !makeUndeletable(t, blocked) {
		t.Skip("no chflags here: cannot pin one file against os.Remove while its neighbours stay removable")
	}

	removed, err := Prune(dir, 2)
	if err == nil {
		t.Skipf("fixture did not land: %q was pinned but removal still succeeded", blocked)
	}
	if strings.Join(removed, ",") != "busy/2026-08-01-c.md" {
		t.Fatalf("removed = %v, want just busy/2026-08-01-c.md (the removal before the blocked one)", removed)
	}

	idx, readErr := os.ReadFile(filepath.Join(dir, "reports", "handoff", "INDEX.md"))
	if readErr != nil {
		t.Fatalf("Prune must leave an index behind even when a removal failed: %v", readErr)
	}
	if strings.Contains(string(idx), "2026-08-01-c.md") {
		t.Errorf("the index still lists 2026-08-01-c.md, which Prune deleted; error was %v:\n%s", err, idx)
	}
	for _, want := range []string{"2026-08-01-d.md", "2026-08-01-e.md", "2026-08-01-a.md", "2026-08-01-b.md"} {
		if !strings.Contains(string(idx), want) {
			t.Errorf("the index dropped %s, which is still on disk:\n%s", want, idx)
		}
	}
}

func TestWriteRegeneratesTheIndex(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, "lane-a", "s1", "draft", "x", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "reports", "handoff", "INDEX.md"))
	if err != nil {
		t.Fatalf("Write must regenerate the index in the same operation: %v", err)
	}
	if !strings.Contains(string(b), "lane-a") {
		t.Errorf("index does not list the entry just written:\n%s", b)
	}
}

// WriteIndex re-parses every handoff in the tree, so one hand-broken or
// merge-conflicted file anywhere makes it fail -- and handoffs are explicitly
// designed to be merged across branches, which makes that a steady state. The
// handoff that was just written is on disk regardless, and the caller has to be
// able to tell that from "wanted to record and could not": exit 5 sends a
// session-end hook looking for a handoff that is sitting in the tree.
//
// Kills: `return path, WriteIndex(agentsDir)`, which reports the other file's
// failure as this write's; and swallowing the index error, which would leave a
// stale index with nothing said about it.
func TestWriteReportsThePathWhenOnlyTheIndexRefreshFails(t *testing.T) {
	dir := t.TempDir()
	writeRaw(t, dir, "lane-a", "2026-08-09-bad.md", "<<<<<<< HEAD\nnot a handoff\n")

	p, err := Write(dir, "lane-a", "s1", "reviewed", "body", time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC))
	if p == "" {
		t.Fatalf("Write must report the path of a handoff that reached disk; err = %v", err)
	}
	b, readErr := os.ReadFile(p)
	if readErr != nil {
		t.Fatalf("Write reported %q, which is not on disk: %v", p, readErr)
	}
	if !strings.Contains(string(b), "body") {
		t.Errorf("the handoff at %q is not the one that was written:\n%s", p, b)
	}

	var stale *IndexError
	if !errors.As(err, &stale) {
		t.Fatalf("err = %v (%T), want an *IndexError so the caller can tell a stale index from a lost handoff", err, err)
	}
	// The cause has to survive, or nobody can find the file to fix.
	if !strings.Contains(err.Error(), "2026-08-09-bad.md") {
		t.Errorf("the advisory must name the file that would not parse; got: %v", err)
	}
}

// A1. Both arguments reach the filesystem: laneName becomes a directory under
// reports/handoff/ and session becomes half the filename under it. lane.Resolve
// happens to slugify what the CLI passes for the lane, but Write is exported and
// --session is never slugified anywhere.
//
// The error must name the field, not just fail: an implementation that
// interpolates the value and lets the filesystem refuse it also returns an
// error, and for the deep-escape cases that error is the only difference between
// "refused" and "wrote outside the repository but the sandbox happened to say
// no". The expected location is computed without this package, so a sanitiser
// that agrees with itself and still escapes cannot pass.
//
// Kills: no validation; filepath.Base alone (Base("..") is ".."); a check for
// ".." by substring (which lets "sub/dir" through); and validating the lane but
// not the session.
func TestWriteRefusesALaneOrSessionThatIsNotOnePathComponent(t *testing.T) {
	for _, tc := range []struct{ label, value string }{
		{"parent", ".."},
		{"parent behind a component", "a/../.."},
		{"deeper than any fixture can chase", "../../../../../../../../escaped"},
		{"a subdirectory", "sub/dir"},
		{"the current directory", "."},
		{"nothing but dots", "..."},
		{"a backslash separator", `a\b`},
		{"a newline that forges an index row", "x\n| 2026-01-01 00:00 | reviewed | ok | [a](a) |"},
		{"a NUL", "a\x00b"},
		{"git's own directory name", ".git"},
	} {
		for _, field := range []string{"lane", "session"} {
			t.Run(field+"/"+tc.label, func(t *testing.T) {
				dir := t.TempDir()
				when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
				laneName, session := "lane-fixed", "sess-fixed"
				if field == "lane" {
					laneName = tc.value
				} else {
					session = tc.value
				}

				// Sampled before and after rather than just after: the deepest
				// escape cleans to a path that does not depend on the tempdir
				// at all, so a run that did escape leaves something behind that
				// would otherwise fail every later run for the wrong reason.
				target := naivePath(dir, laneName, session, when)
				before, _ := os.Stat(target)

				p, err := Write(dir, laneName, session, "reviewed", "body", when)
				if err == nil {
					t.Fatalf("Write accepted %s = %q and wrote %q", field, tc.value, p)
				}
				if !strings.Contains(err.Error(), field) {
					t.Errorf("the refusal must name the field it is about; got: %v", err)
				}
				if p != "" {
					t.Errorf("a refused write must not report a path; got %q", p)
				}
				switch after, err := os.Stat(target); {
				case err == nil && before == nil:
					t.Errorf("a file appeared at %q", target)
				case err == nil && !after.ModTime().Equal(before.ModTime()):
					t.Errorf("the file at %q was rewritten", target)
				}
				if n := countFiles(t, dir); n != 0 {
					t.Errorf("a refused write left %d file(s) behind under %s", n, dir)
				}
			})
		}
	}
}

// A1, the reason it refuses instead of normalising. One file per (lane, session)
// stops concurrent agents clobbering each other only while distinct sessions
// stay distinct. Slugifying folds "a/b" onto "a-b" and hands the second agent
// the first agent's file -- reintroducing, silently and at exit 0, the exact
// failure the scheme exists to prevent.
//
// Kills: sanitising by mapping unsafe characters to "-", the way
// trace.harnessDir does for a field with no such uniqueness requirement.
func TestWriteRefusesRatherThanFoldingTwoSessionsOntoOneFile(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)

	first, err := Write(dir, "lane-fixed", "a-b", "reviewed", "the first agent's note", when)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := Write(dir, "lane-fixed", "a/b", "reviewed", "the second agent's note", when); err == nil {
		t.Errorf("Write accepted a session with a separator and put it at %q", p)
	}

	b, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "the first agent's note") {
		t.Errorf("the first session's handoff was overwritten:\n%s", b)
	}
	if n := countFiles(t, filepath.Join(dir, "reports", "handoff", "lane-fixed")); n != 1 {
		t.Errorf("lane holds %d files, want 1", n)
	}
}

// A1 again, reached through the filesystem instead of through a slugifier. On a
// case-insensitive filesystem -- APFS's default -- "2026-08-10-ABC.md" and
// "2026-08-10-abc.md" are one file. Measured before the guard: the second Write
// returned nil, the lane held one file carrying the SECOND agent's note, and
// List reported session "abc" at path "lane/2026-08-10-ABC.md" -- the first
// agent's handoff destroyed at exit 0, and an index row whose link text
// disagrees with its own session cell.
//
// The refusal is asserted unconditionally because it does not depend on the
// filesystem: the tree is committed and merged across machines, so a pair of
// names that cannot coexist on APFS is a hazard wherever it was authored. The
// probe gates only the half that needs it -- that the two names really do
// resolve to one file here, which is what makes the refusal a rescue rather
// than a nicety.
//
// Kills: no collision check at all, and one written with == instead of
// strings.EqualFold so it never fires.
func TestWriteRefusesASessionThatCollidesWithAnExistingHandoffByCase(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	laneDir := filepath.Join(dir, "reports", "handoff", "lane")

	first, err := Write(dir, "lane", "ABC", "reviewed", "the FIRST agent's note", when)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Write(dir, "lane", "abc", "reviewed", "the SECOND agent's note", when)
	if err == nil {
		t.Fatalf("Write accepted a session that differs from an existing one only in case and put it at %q", p)
	}
	if p != "" {
		t.Errorf("a refused write must not report a path; got %q", p)
	}
	// Both spellings, or the author cannot see what it collided with.
	for _, want := range []string{"2026-08-10-ABC.md", "2026-08-10-abc.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q so both spellings are visible; got: %v", want, err)
		}
	}

	b, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("the first session's handoff is gone: %v", err)
	}
	if !strings.Contains(string(b), "the FIRST agent's note") {
		t.Errorf("the first session's handoff was overwritten:\n%s", b)
	}
	if n := countFiles(t, laneDir); n != 1 {
		t.Errorf("lane holds %d files, want 1", n)
	}

	if !caseInsensitiveFS(t, dir) {
		t.Log("case-sensitive filesystem: the two names are two files here, so the refusal is the conservative half only")
		return
	}
	// The half that needs the probe: the lowercase name resolves onto the
	// uppercase file, so an unguarded Write would have truncated it.
	b, err = os.ReadFile(filepath.Join(laneDir, "2026-08-10-abc.md"))
	if err != nil {
		t.Fatalf("the probe says this filesystem folds case but the two names are not one file: %v", err)
	}
	if !strings.Contains(string(b), "the FIRST agent's note") {
		t.Errorf("the two names are one file and it no longer holds the first agent's note:\n%s", b)
	}
}

// The lane variant, which the same directory scan covers. It is milder than the
// session one -- nothing is lost, but the index grows two "## " sections whose
// rows all link into one directory -- and it is refused for the same reason.
//
// lane.Resolve lowercases, so this cannot arrive through the CLI's branch
// resolution; it arrives through --lane, through a direct Write, or through a
// directory somebody made by hand.
//
// Kills: checking only the filename and not the lane directory.
func TestWriteRefusesALaneThatCollidesWithAnExistingLaneByCase(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)

	if _, err := Write(dir, "Lane", "s1", "reviewed", "x", when); err != nil {
		t.Fatal(err)
	}
	p, err := Write(dir, "lane", "s2", "reviewed", "x", when)
	if err == nil {
		t.Fatalf("Write accepted a lane that differs from an existing one only in case and put it at %q", p)
	}
	for _, want := range []string{"Lane", "lane"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q so both spellings are visible; got: %v", want, err)
		}
	}
	ents, err := os.ReadDir(filepath.Join(dir, "reports", "handoff"))
	if err != nil {
		t.Fatal(err)
	}
	var lanes []string
	for _, e := range ents {
		if e.IsDir() {
			lanes = append(lanes, e.Name())
		}
	}
	if strings.Join(lanes, ",") != "Lane" {
		t.Errorf("lane directories = %v, want just [Lane]", lanes)
	}
}

// A3, layer 1. The index row is `| when | status | session | [file](path) |`, so
// a newline in any of the three fields it interpolates forges a row -- and the
// row regenerates byte for byte, so a guard that regenerates the index and
// compares it waves the forgery through. Write refuses these at the boundary;
// this is the other way in, a hand-edit or a merge.
//
// Rejecting rather than flattening is the decision, and it is memory's: a
// flattened value would make the generated index quietly disagree with the file
// it came from.
//
// Kills: parsing the frontmatter without checking it, and checking only some of
// the three fields.
func TestListRefusesFrontmatterThatWouldForgeIndexRows(t *testing.T) {
	forged := "| 2026-01-01 00:00 | reviewed | security-approved | [x](../../../bin/curl-pipe-sh) |"
	for _, tc := range []struct{ label, field, body string }{
		{
			"a block scalar in session forges a row",
			"session",
			"---\nlane: lane-a\nsession: |\n  s1\n  " + forged + "\nstatus: draft\nwhen: 2026-08-10T09:00:00Z\n---\n\nbody\n",
		},
		{
			"a quoted newline in status forges a row",
			"status",
			"---\nlane: lane-a\nsession: s1\nstatus: \"draft |\\n" + forged + "\"\nwhen: 2026-08-10T09:00:00Z\n---\n\nbody\n",
		},
		{
			"a newline in lane forges a section and everything filed under it",
			"lane",
			"---\nlane: \"lane-a\\n\\n## trusted-lane\\n\\n| when | status | session | file |\\n|---|---|---|---|\\n" + forged + "\"\nsession: s1\nstatus: draft\nwhen: 2026-08-10T09:00:00Z\n---\n\nbody\n",
		},
		{
			// A lone CR survives the CRLF normalisation and a terminal renders
			// it by returning to the start of the line.
			"a bare carriage return in session",
			"session",
			"---\nlane: lane-a\nsession: \"s1\\rforged\"\nstatus: draft\nwhen: 2026-08-10T09:00:00Z\n---\n\nbody\n",
		},
	} {
		t.Run(tc.label, func(t *testing.T) {
			dir := t.TempDir()
			writeRaw(t, dir, "lane-a", "2026-08-10-s1.md", tc.body)

			_, err := List(dir)
			if err == nil {
				t.Fatal("List must refuse the directory rather than index a forged row")
			}
			for _, want := range []string{"2026-08-10-s1.md", tc.field} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal must mention %q; got: %v", want, err)
				}
			}
			if err := WriteIndex(dir); err == nil {
				t.Error("WriteIndex must fail too, not write an index holding the forgery")
			}
		})
	}
}

// A3, layer 1 again, by the route the frontmatter check does not cover: the
// filename. It is not authored in YAML, it comes off the filesystem, and it goes
// into the index as the link text.
//
// Kills: checking only the frontmatter fields.
func TestListRefusesAFilenameThatWouldForgeIndexRows(t *testing.T) {
	dir := t.TempDir()
	name := "2026-08-10-s1.md](x) |\n| 2026-01-01 00:00 | reviewed | forged | [y.md"
	body := "---\nlane: lane-a\nsession: s1\nstatus: draft\nwhen: 2026-08-10T09:00:00Z\n---\n\nbody\n"

	target := filepath.Join(dir, "reports", "handoff", "lane-a")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, name), []byte(body), 0o644); err != nil {
		t.Skipf("this filesystem will not hold a filename with a newline: %v", err)
	}

	if _, err := List(dir); err == nil {
		t.Fatal("List must refuse a handoff filename that cannot be rendered on one line")
	}
}

// A3, layer 2. Rejection does not cover these: "]" and "|" are legal in a
// filename and in a session id, and refusing them would be a policy this tool
// has no reason to have. They have to survive into the index without ending the
// cell or the link early.
//
// The assertions are about the shape of the row rather than the exact escape
// spelling, so they hold for backslash escapes, percent-encoding or anything
// else that keeps the row intact -- except flattening, which the last assertion
// rules out: the index must not quietly disagree with the file it points at.
//
// Kills: `| %s | %s | %s | [%s](%s) |` with the values interpolated raw.
func TestRenderIndexKeepsTheRowIntactWhateverTheSessionIsCalled(t *testing.T) {
	for _, session := range []string{"a|b", "a]b", "a b(c)", "a#b"} {
		t.Run("session="+session, func(t *testing.T) {
			dir := t.TempDir()
			when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
			if _, err := Write(dir, "lane-a", session, "reviewed", "x", when); err != nil {
				t.Fatalf("Write refused a legal filename component: %v", err)
			}
			entries, err := List(dir)
			if err != nil {
				t.Fatalf("List: %v", err)
			}

			row := rowFor(t, string(RenderIndex(entries)), "reviewed")
			pipes := unescapedIndexes(row, '|')
			if len(pipes) != 5 {
				t.Fatalf("a four-column row needs 5 live %q, got %d in %q", "|", len(pipes), row)
			}
			assertLinkCellResolves(t, dir, strings.TrimSpace(row[pipes[3]+1:pipes[4]]))

			// Escaped, not flattened: unescaping the cell must give back what
			// the file says the session is, or the index disagrees with it.
			cell := strings.TrimSpace(row[pipes[2]+1 : pipes[3]])
			if unescapeCell(cell) != session {
				t.Errorf("the session cell %q does not unescape to %q", cell, session)
			}
		})
	}
}

func unescapeCell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\|`, "|"), `\\`, `\`)
}

// assertLinkCellResolves checks the shape of a "[text](dest)" cell without
// caring how it was escaped, and then checks that the destination still names
// the file -- which no assertion about escaping alone can promise.
func assertLinkCellResolves(t *testing.T, agentsDir, cell string) {
	t.Helper()
	open := unescapedIndexes(cell, '[')
	if len(open) == 0 || open[0] != 0 {
		t.Fatalf("the link text must open the cell: %q", cell)
	}
	shut := unescapedIndexes(cell, ']')
	if len(shut) == 0 || shut[0]+1 >= len(cell) || cell[shut[0]+1] != '(' {
		t.Fatalf("the first live %q must be the one that closes the link text: %q", "]", cell)
	}
	rest := cell[shut[0]+2:]
	end := strings.IndexByte(rest, ')')
	if end < 0 {
		t.Fatalf("the link destination is never closed: %q", cell)
	}
	dest := rest[:end]
	// "#" is in the set because it does not end the destination, it splits it:
	// the link points at everything before it and calls the rest a fragment, so
	// it resolves to a file that does not exist while still looking like a link.
	if strings.ContainsAny(dest, " ()|#") {
		t.Fatalf("destination %q ends early, splits, or ends the cell; cell: %q", dest, cell)
	}
	decoded, err := url.PathUnescape(dest)
	if err != nil {
		t.Fatalf("destination %q is not decodable: %v", dest, err)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "reports", "handoff", filepath.FromSlash(decoded))); err != nil {
		t.Errorf("the index links to %q, which is not the file: %v", decoded, err)
	}
}

// A3, layer 2, at the ordering that gets it wrong. Write refuses a backslash in
// a session, so this value can only arrive the way the layer-1 tests arrive: by
// hand-edit or merge. It is the case that separates a one-pass escaper from two
// passes -- escape "|" alone and "a\|b" becomes "a\\|b", an escaped backslash
// followed by a live "|" that opens a column no header describes.
//
// Kills: a MarkdownCell that escapes "|" without escaping "\" first, and one
// that escapes "\" in a second pass after the pipes.
func TestRenderIndexEscapesAPipeHidingBehindABackslash(t *testing.T) {
	dir := t.TempDir()
	writeRaw(t, dir, "lane-a", "2026-08-10-s1.md",
		"---\nlane: lane-a\nsession: \"a\\\\|b\"\nstatus: draft\nwhen: 2026-08-10T09:00:00Z\n---\n\nbody\n")

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if entries[0].Session != `a\|b` {
		t.Fatalf("fixture did not land: session = %q", entries[0].Session)
	}

	row := rowFor(t, string(RenderIndex(entries)), "draft")
	pipes := unescapedIndexes(row, '|')
	if len(pipes) != 5 {
		t.Fatalf("a four-column row needs 5 live %q, got %d in %q", "|", len(pipes), row)
	}
	if cell := strings.TrimSpace(row[pipes[2]+1 : pipes[3]]); unescapeCell(cell) != `a\|b` {
		t.Errorf("the session cell %q does not unescape to %q", cell, `a\|b`)
	}
}

// The frontmatter is YAML, and a session id is an opaque token from a harness.
// Interpolating it into "session: %s" hands YAML a value it reinterprets.
// Measured against the interpolated version: "a: b", "[a, b]" and "*anchor" all
// fail the read back, which wedges every later index; "a #c" and "  spaced  "
// are the dangerous ones, because they do not fail -- they record a session
// that is not the one in the filename, at exit 0. "true" and "0755" round-trip
// either way, since yaml.v3 coerces a scalar into a string field; they are here
// as the adjacent non-matches, so the fixture cannot be satisfied by quoting
// everything that merely looks scalar-ish.
//
// Kills: building the frontmatter with fmt.Fprintf instead of yaml.Marshal.
func TestWriteRoundTripsASessionYAMLWouldOtherwiseReinterpret(t *testing.T) {
	for _, session := range []string{"a: b", "true", "a #c", "0755", "[a, b]", "*anchor", "  spaced  "} {
		t.Run("session="+session, func(t *testing.T) {
			dir := t.TempDir()
			when := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
			if _, err := Write(dir, "lane-a", session, "reviewed", "x", when); err != nil {
				t.Fatalf("Write: %v", err)
			}
			entries, err := List(dir)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("got %d entries, want 1", len(entries))
			}
			if entries[0].Session != session {
				t.Errorf("session round-tripped as %q, want %q", entries[0].Session, session)
			}
		})
	}
}
