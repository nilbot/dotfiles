package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const entry = `---
name: payments-retry-semantics
description: Why the retry window is 90s and not the documented 30s
metadata:
  type: reference
sources:
  - kind: transcript
    machine: m1-mbp-a7f3
    ref: 019fdcab-ac94-7502-a322-d01f047c274a
  - kind: harness-memory
    machine: m1-mbp-a7f3
    harness: codex
    note: "full derivation in Codex auto-memory; distil before relying on the numbers"
---

The window is 90s. See [[payments-timeouts]].
`

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseFrontmatterAndSources(t *testing.T) {
	fm, err := Parse(write(t, t.TempDir(), "retry.md", entry))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if fm.Name != "payments-retry-semantics" {
		t.Errorf("Name = %q", fm.Name)
	}
	if fm.Type != "reference" {
		t.Errorf("Type = %q, want reference", fm.Type)
	}
	if len(fm.Sources) != 2 {
		t.Fatalf("got %d sources, want 2", len(fm.Sources))
	}
	if fm.Sources[0].Kind != "transcript" || fm.Sources[0].Ref == "" {
		t.Errorf("source 0 = %+v", fm.Sources[0])
	}
	if fm.Sources[1].Harness != "codex" || fm.Sources[1].Note == "" {
		t.Errorf("source 1 = %+v", fm.Sources[1])
	}
}

func TestParseRejectsMissingFrontmatter(t *testing.T) {
	if _, err := Parse(write(t, t.TempDir(), "bare.md", "just prose\n")); err == nil {
		t.Fatal("want an error: an entry with no frontmatter cannot be indexed")
	}
}

func TestParseRequiresNameAndDescription(t *testing.T) {
	body := "---\nname: only-a-name\n---\n\nbody\n"
	if _, err := Parse(write(t, t.TempDir(), "partial.md", body)); err == nil {
		t.Fatal("want an error when description is missing")
	}
}

func TestRenderIndexIsDeterministicAndGrouped(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "retry.md", entry)
	write(t, dir, "b.md", "---\nname: b-thing\ndescription: B\nmetadata:\n  type: project\n---\n\nx\n")
	write(t, dir, "a.md", "---\nname: a-thing\ndescription: A\nmetadata:\n  type: project\n---\n\nx\n")

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	first := string(RenderIndex(entries))
	second := string(RenderIndex(entries))
	if first != second {
		t.Fatal("RenderIndex must be deterministic; the guard compares it byte for byte")
	}
	if !strings.Contains(first, "GENERATED") {
		t.Error("the index must say it is generated")
	}
	if strings.Index(first, "a-thing") > strings.Index(first, "b-thing") {
		t.Error("entries must be sorted by name within a type")
	}
	if !strings.Contains(first, "sources: 2") {
		t.Error("the index must surface that an entry depends on material outside the repo")
	}
}

// INDEX.md is generated from the entries; it must never be read as one.
func TestListIgnoresTheIndexItself(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.md", "---\nname: a\ndescription: A\nmetadata:\n  type: project\n---\n\nx\n")
	if err := WriteIndex(dir); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}
	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (INDEX.md must be excluded)", len(entries))
	}
}

func TestWriteIndexIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.md", "---\nname: a\ndescription: A\nmetadata:\n  type: project\n---\n\nx\n")
	if err := WriteIndex(dir); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "INDEX.md"))
	if err := WriteIndex(dir); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, "INDEX.md"))
	if string(first) != string(second) {
		t.Fatal("WriteIndex is not idempotent; the pre-commit guard would block on every commit")
	}
}

// A malformed entry must not silently vanish from the index -- that would make
// the index quietly wrong, which is worse than an error.
func TestListReportsUnparseableEntries(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.md", "---\nname: a\ndescription: A\nmetadata:\n  type: project\n---\n\nx\n")
	write(t, dir, "broken.md", "no frontmatter here\n")
	if _, err := List(dir); err == nil {
		t.Fatal("want an error naming the unparseable entry")
	} else if !strings.Contains(err.Error(), "broken.md") {
		t.Fatalf("error must name the file: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The fixtures above illustrate the behaviour but cannot discriminate several
// plausible wrong implementations. Everything below exists to close a specific
// gap; each test names the implementation it kills.
// ---------------------------------------------------------------------------

// doc builds a minimal entry. Type is omitted from the frontmatter when typ is
// empty, which is how the uncategorized fallback is exercised.
func doc(name, description, typ string) string {
	var b strings.Builder
	b.WriteString("---\nname: " + name + "\ndescription: " + description + "\n")
	if typ != "" {
		b.WriteString("metadata:\n  type: " + typ + "\n")
	}
	b.WriteString("---\n\nbody\n")
	return b.String()
}

// index renders a directory the way WriteIndex would, failing the test on any
// error so the callers below can stay about ordering.
func index(t *testing.T, dir string) string {
	t.Helper()
	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return string(RenderIndex(entries))
}

// before asserts that a comes before b in s, naming both when it does not.
func before(t *testing.T, s, a, b string) {
	t.Helper()
	ia, ib := strings.Index(s, a), strings.Index(s, b)
	if ia < 0 {
		t.Fatalf("%q missing from index:\n%s", a, s)
	}
	if ib < 0 {
		t.Fatalf("%q missing from index:\n%s", b, s)
	}
	if ia > ib {
		t.Errorf("%q must come before %q:\n%s", a, b, s)
	}
}

// lineFor returns the single index line that mentions name.
func lineFor(t *testing.T, s, name string) string {
	t.Helper()
	var found []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "["+name+"]") {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one index line for %q, got %d:\n%s", name, len(found), s)
	}
	return found[0]
}

// Deficit 1. typeOrder is user, project, feedback, reference, uncategorized;
// alphabetical order of those same names is feedback, project, reference, user.
// The two agree on project-vs-reference, which is all the brief's fixture uses.
//
// Kills: sorting the sections alphabetically (sort.Strings over the type keys)
// instead of walking the fixed typeOrder.
func TestRenderIndexSectionOrderIsFixedNotAlphabetical(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "u.md", doc("u-entry", "U", "user"))
	write(t, dir, "p.md", doc("p-entry", "P", "project"))
	write(t, dir, "f.md", doc("f-entry", "F", "feedback"))
	write(t, dir, "r.md", doc("r-entry", "R", "reference"))

	got := index(t, dir)
	before(t, got, "## user", "## project")
	before(t, got, "## project", "## feedback")
	before(t, got, "## feedback", "## reference")
}

// Deficit 2. In the brief's fixture every filename agrees with the name it
// holds, and List reads files in filename order, so sorting by either passes.
// Here z.md holds a-thing and a.md holds z-thing, and the descriptions disagree
// with the names too.
//
// Kills: sorting entries by Path (or leaving them in List's filename order), and
// sorting them by Description.
func TestRenderIndexSortsByNameNotFilenameOrDescription(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "z.md", doc("a-thing", "zzz-description", "project"))
	write(t, dir, "a.md", doc("z-thing", "aaa-description", "project"))

	got := index(t, dir)
	before(t, got, "a-thing", "z-thing")
}

// Deficit 3. No fixture in the brief omits metadata.type, so an implementation
// that leaves Type empty -- rendering a "## " section with no name -- passes.
//
// Kills: dropping the "uncategorized" fallback.
func TestParseFallsBackToUncategorized(t *testing.T) {
	dir := t.TempDir()
	fm, err := Parse(write(t, dir, "n.md", doc("no-type", "N", "")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if fm.Type != "uncategorized" {
		t.Fatalf("Type = %q, want uncategorized", fm.Type)
	}
	if got := index(t, dir); !strings.Contains(got, "## uncategorized\n") {
		t.Errorf("an entry with no type must land in a named section:\n%s", got)
	}
}

// Deficit 4. No fixture uses a type outside typeOrder, so the extras path never
// runs. Two unknown types are used so that "sorted" is observable, and the whole
// render is repeated because a map-iteration-order bug is intermittent by
// nature: one render could pass by luck, fifty could not.
//
// Kills: dropping entries whose type is unknown; emitting the unknown sections
// in map-iteration order; emitting them before the known sections.
func TestRenderIndexAppendsUnknownTypesSortedAfterKnownOnes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "p.md", doc("p-entry", "P", "project"))
	write(t, dir, "n.md", doc("n-entry", "N", ""))
	write(t, dir, "w.md", doc("w-entry", "W", "workflow"))
	write(t, dir, "d.md", doc("d-entry", "D", "decision"))

	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	first := string(RenderIndex(entries))
	for i := 0; i < 50; i++ {
		got := string(RenderIndex(entries))
		if got != first {
			t.Fatalf("render %d differs from the first; RenderIndex is not deterministic:\n%s\n---\n%s", i, first, got)
		}
	}

	before(t, first, "## project", "## uncategorized")
	before(t, first, "## uncategorized", "## decision")
	before(t, first, "## decision", "## workflow")
}

// Deficit 5. The brief's only source count is 2, so a constant "sources: 2" --
// or a marker printed whenever any source exists anywhere -- passes.
//
// Kills: printing a constant count; printing the marker for an entry with no
// sources; omitting the marker for a single source.
func TestRenderIndexReportsTheActualSourceCount(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "two.md", entry)
	write(t, dir, "one.md", "---\nname: one-source\ndescription: One\nmetadata:\n  type: project\nsources:\n  - kind: transcript\n    machine: m2-mbp-0001\n    ref: 019fdcab-ac94-7502-a322-000000000001\n---\n\nbody\n")
	// Named without the word "sources" so the assertion below can look for the
	// bare word rather than the exact marker spelling.
	write(t, dir, "none.md", doc("stands-alone", "Needs nothing outside the repo", "project"))

	got := index(t, dir)
	if l := lineFor(t, got, "payments-retry-semantics"); !strings.Contains(l, "sources: 2") {
		t.Errorf("two-source entry line = %q", l)
	}
	if l := lineFor(t, got, "one-source"); !strings.Contains(l, "sources: 1") {
		t.Errorf("one-source entry line = %q", l)
	}
	// An entry that stands on its own must not be labelled as depending on
	// anything: the marker is a warning, and a warning on every line is noise.
	if l := lineFor(t, got, "stands-alone"); strings.Contains(l, "sources") {
		t.Errorf("entry with no sources must carry no marker: %q", l)
	}
}

// Deficit 6. A markdown horizontal rule, or a second frontmatter-looking block,
// is realistic in a body and no fixture in the brief has one. The frontmatter is
// closed by the first delimiter; every later one is body.
//
// Kills: any parser that treats the delimiter count as a validity check --
// strings.Split(text, "\n---") followed by a len(parts) != 2 rejection is the
// usual spelling -- which refuses a perfectly good entry because its body has a
// horizontal rule in it.
//
// Does NOT kill closing on the last delimiter instead of the first
// (strings.LastIndex): yaml.Unmarshal decodes only the first document of a
// stream and silently discards everything after a "---" separator, so the two
// spellings produce identical Frontmatter for any input whose frontmatter is
// closed at all. Verified, not assumed -- see the report.
func TestParseAcceptsAHorizontalRuleInTheBody(t *testing.T) {
	const withRule = `---
name: rule-in-body
description: Every delimiter after the first one is body
metadata:
  type: project
---

Intro paragraph, then a horizontal rule.

---

name: not-the-frontmatter
description: this text lives in the body and must never reach the index
`
	dir := t.TempDir()
	fm, err := Parse(write(t, dir, "rule.md", withRule))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if fm.Name != "rule-in-body" {
		t.Errorf("Name = %q, want rule-in-body", fm.Name)
	}
	if fm.Description != "Every delimiter after the first one is body" {
		t.Errorf("Description = %q", fm.Description)
	}
	if fm.Type != "project" {
		t.Errorf("Type = %q, want project", fm.Type)
	}
	got := index(t, dir)
	for _, leak := range []string{"not-the-frontmatter", "lives in the body"} {
		if strings.Contains(got, leak) {
			t.Errorf("body text %q reached the index:\n%s", leak, got)
		}
	}
}

// The delimiter is a line, not a substring. Prose says "---" mid-sentence often
// enough -- an em dash typed by someone whose keyboard does not have one -- and
// a search that ignores the line start truncates the entry at that point,
// quietly losing the tail of a description and the whole metadata block.
//
// Kills: strings.Index(rest, "---") without the leading newline.
func TestParseClosesOnlyOnALineStartDelimiter(t *testing.T) {
	const dashInValue = `---
name: dash-in-value
description: the retry window --- 90s, not 30s --- is measured
metadata:
  type: reference
---

body
`
	fm, err := Parse(write(t, t.TempDir(), "dash.md", dashInValue))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if fm.Description != "the retry window --- 90s, not 30s --- is measured" {
		t.Errorf("Description = %q; the delimiter search truncated the value", fm.Description)
	}
	if fm.Type != "reference" {
		t.Errorf("Type = %q, want reference; the metadata block was cut off", fm.Type)
	}
}

// The brief covers a missing description but not a missing name. The name is
// the link text and the sort key; without it the index grows a nameless row.
//
// Kills: checking only one of the two required fields.
func TestParseRequiresName(t *testing.T) {
	body := "---\ndescription: only-a-description\nmetadata:\n  type: project\n---\n\nbody\n"
	if _, err := Parse(write(t, t.TempDir(), "noname.md", body)); err == nil {
		t.Fatal("want an error when name is missing")
	}
}

// Frontmatter that is opened and never closed is valid YAML all the way to EOF,
// so an implementation that falls back to "the rest of the file" parses it
// happily and indexes an entry whose body it just ate.
//
// Kills: treating a missing closing delimiter as end-of-file.
func TestParseRejectsUnclosedFrontmatter(t *testing.T) {
	body := "---\nname: unclosed\ndescription: D\nmetadata:\n  type: project\n"
	if _, err := Parse(write(t, t.TempDir(), "unclosed.md", body)); err == nil {
		t.Fatal("want an error: frontmatter that is never closed is not frontmatter")
	}
}

// The brief's fixture for a file with no frontmatter only asserts that some
// error comes back, and the closing-delimiter search alone produces one -- so
// dropping the leading-delimiter check is invisible to it. What changes is the
// message, and the message is the whole output the author gets: "no frontmatter"
// versus "cannot unmarshal !!str into memory.Frontmatter", which sends them
// hunting for a YAML bug in a file that contains no YAML.
//
// Kills: dropping the leading-delimiter check and letting the YAML decoder
// produce the diagnostic instead.
func TestParseSaysWhenThereIsNoFrontmatterAtAll(t *testing.T) {
	_, err := Parse(write(t, t.TempDir(), "bare.md", "just prose\n"))
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "no frontmatter") {
		t.Errorf("error must say the file has none, got: %v", err)
	}
	if !strings.Contains(err.Error(), "bare.md") {
		t.Errorf("error must name the file, got: %v", err)
	}
}

// The banner is the whole reason the file can be trusted: it tells a reader that
// editing it by hand is pointless and tells the pre-commit guard's victim why
// their commit was blocked.
//
// Kills: dropping the "do not hand-edit" half of the banner, and dropping the
// banner from an index that happens to have no entries.
func TestRenderIndexAlwaysCarriesTheGeneratedBanner(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []Frontmatter
	}{
		{"empty", nil},
		{"populated", []Frontmatter{{Name: "a", Description: "A", Type: "project", Path: "a.md"}}},
	} {
		got := string(RenderIndex(tc.entries))
		if !strings.Contains(got, "GENERATED") {
			t.Errorf("%s: index must say it is generated:\n%s", tc.name, got)
		}
		if !strings.Contains(got, "hand-edit") {
			t.Errorf("%s: index must warn against hand-editing:\n%s", tc.name, got)
		}
	}
}

// WriteIndex over a single entry cannot observe a map-iteration-order bug. This
// one has four types and is written repeatedly: the pre-commit guard compares
// the file byte for byte, so a single differing byte blocks every commit.
//
// Kills: any non-determinism in RenderIndex reaching the file.
func TestWriteIndexIsByteStableAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "retry.md", entry)
	write(t, dir, "p.md", doc("p-entry", "P", "project"))
	write(t, dir, "u.md", doc("u-entry", "U", "user"))
	write(t, dir, "n.md", doc("n-entry", "N", ""))
	write(t, dir, "w.md", doc("w-entry", "W", "workflow"))

	if err := WriteIndex(dir); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := WriteIndex(dir); err != nil {
			t.Fatal(err)
		}
		again, err := os.ReadFile(filepath.Join(dir, "INDEX.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(again) != string(first) {
			t.Fatalf("run %d wrote a different index; the guard would block on every commit:\n%s\n---\n%s", i, first, again)
		}
	}
}
