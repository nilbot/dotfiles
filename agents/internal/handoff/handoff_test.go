package handoff

import (
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
