// Package handoff manages lane-scoped notes about work in flight.
//
// One file per (lane, session), not one rolling file. A single rolling handoff
// assumes one person, one thread, one machine; it breaks with concurrent agents
// and with a repo where several tickets are in flight under no common story.
// Distinct files also merge cleanly, which markdown cannot do with merge=union.
package handoff

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nilbot/dotfiles/agents/internal/safetext"
)

const (
	StatusReviewed = "reviewed" // written deliberately
	StatusDraft    = "draft"    // auto-written at session end
)

type Entry struct {
	Lane    string    `yaml:"lane"`
	Session string    `yaml:"session"`
	Status  string    `yaml:"status"`
	When    time.Time `yaml:"when"`
	Path    string    `yaml:"-"` // relative to reports/handoff, slash-separated
}

func root(agentsDir string) string { return filepath.Join(agentsDir, "reports", "handoff") }

// CheckSession reports whether a session id can be used as one. Exported so the
// command can refuse a bad --session as malformed input rather than discovering
// it as a failure to record; Write applies the same check regardless, because it
// is exported too and the invariant is the package's, not the CLI's.
func CheckSession(session string) error { return checkComponent("session", session) }

// checkComponent refuses a lane or session that is not exactly one path
// component.
//
// Both reach the filesystem: laneName becomes a directory under
// reports/handoff/ and session becomes half the filename under it. A ".."
// escapes the handoff tree, a "/" invents directories inside it, and a control
// character forges a row of the generated index. lane.Resolve happens to
// slugify what the CLI passes for the lane, but Write is exported and --session
// is never slugified anywhere.
//
// This refuses where trace.harnessDir, facing the same class of input, maps it
// to a safe name instead. Two reasons, both about this field rather than about
// traversal in general:
//
//   - session is the key the whole scheme rests on. One file per (lane, session)
//     keeps concurrent agents from clobbering each other only while distinct
//     sessions stay distinct, and normalising folds "a/b" and "a-b" onto one
//     file -- reintroducing, silently and at exit 0, the failure the design
//     exists to prevent.
//   - harnessDir reads a tracked file in bulk, where failing the run on one bad
//     record throws away every good one. This reads one flag typed by one
//     caller, which has an error channel and can be told what to fix.
func checkComponent(field, value string) error {
	if value == "" {
		return fmt.Errorf("handoff %s is empty", field)
	}
	if r, ok := safetext.ControlRune(value); ok {
		return fmt.Errorf("handoff %s %q contains a control character (%q): it names a file and one row of the generated index, so it has to be a single printable line", field, value, r)
	}
	if strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("handoff %s %q contains a path separator: it is joined into a path under reports/handoff/, so it must be exactly one path component", field, value)
	}
	// Trimming dots catches "." and ".." along with "..." and anything else that
	// filepath.Join could read as a traversal. filepath.Base is not enough on
	// its own, because Base("..") is "..".
	if strings.Trim(value, ".") == "" {
		return fmt.Errorf("handoff %s %q is nothing but dots, which names a directory other than the one being written to", field, value)
	}
	if strings.HasPrefix(value, ".") {
		return fmt.Errorf("handoff %s %q starts with a dot: nothing in the handoff tree is hidden, and git cannot track a path with a %q component", field, value, ".git")
	}
	return nil
}

// Write creates one handoff and regenerates the index in the same operation, so
// the normal path can never produce a stale index.
func Write(agentsDir, laneName, session, status, body string, when time.Time) (string, error) {
	// Absence has one obvious answer and is normalised; a value that says
	// something specific and wrong is refused. The two are not the same case.
	if laneName == "" {
		laneName = "default"
	}
	if session == "" {
		session = "unknown"
	}
	if err := checkComponent("lane", laneName); err != nil {
		return "", err
	}
	if err := checkComponent("session", session); err != nil {
		return "", err
	}
	// Provenance is a claim about how much checking the note has had. An
	// unrecognised value is not evidence that anyone checked it, so it degrades
	// to the weaker claim rather than reaching the index as a word the reader
	// has no way to weigh.
	if status != StatusReviewed {
		status = StatusDraft
	}

	dir := filepath.Join(root(agentsDir), laneName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.md", when.UTC().Format("2006-01-02"), session))

	// Marshalled rather than interpolated. A session id is an opaque token out
	// of a harness, and "session: " + token hands YAML a value it reinterprets.
	// Measured against the interpolated version: ": " makes a mapping, "[" a
	// sequence and "*" an alias, each of which fails the read back and wedges
	// every later index; " #" starts a comment and surrounding spaces are
	// stripped, each of which silently records a session that is not the one in
	// the filename. A bare "true" is not among them, because yaml.v3 coerces a
	// scalar into a string field -- which is the argument for not writing the
	// YAML by hand rather than for guessing at the list of what to quote.
	fm, err := yaml.Marshal(Entry{Lane: laneName, Session: session, Status: status, When: when.UTC()})
	if err != nil {
		return "", err
	}

	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(fm)
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n")

	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		return "", err
	}
	return path, WriteIndex(agentsDir)
}

// List reads every handoff under a .agents/ directory.
//
// It fails on the first unreadable one rather than skipping it: a handoff that
// silently drops out of the index is invisible, and the index would then be
// confidently incomplete.
func List(agentsDir string) ([]Entry, error) {
	base := root(agentsDir)
	matches, err := filepath.Glob(filepath.Join(base, "*", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	var out []Entry
	for _, p := range matches {
		e, err := parse(p)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(base, p)
		if err != nil {
			return nil, err
		}
		e.Path = filepath.ToSlash(rel)
		// The filename is not authored in YAML -- it comes off the filesystem,
		// which will hold a newline in a name -- and it becomes the link text of
		// one row. Quoted in the message so the refusal itself stays on one
		// line.
		if r, ok := safetext.ControlRune(e.Path); ok {
			return nil, fmt.Errorf("%q: a handoff filename contains a control character (%q): it is rendered as a single line of the index -- rename the file", e.Path, r)
		}
		out = append(out, e)
	}
	return out, nil
}

func parse(p string) (Entry, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return Entry{}, err
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Entry{}, fmt.Errorf("%s: no frontmatter", filepath.Base(p))
	}
	rest := text[4:]
	// The FIRST closing delimiter, not the last: a body may hold a horizontal
	// rule, and searching from the end would swallow it into the YAML.
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Entry{}, fmt.Errorf("%s: frontmatter is not closed", filepath.Base(p))
	}
	var e Entry
	if err := yaml.Unmarshal([]byte(rest[:end]), &e); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", filepath.Base(p), err)
	}
	if e.Lane == "" {
		e.Lane = filepath.Base(filepath.Dir(p))
	}
	// Layer 1 of the index's two: every field below is interpolated into one
	// line of INDEX.md -- lane into a "## " heading, the other two into a table
	// row -- so a newline in any of them forges sections and rows that nobody
	// wrote, deterministically enough that a guard which regenerates the index
	// and compares it waves them through.
	for _, f := range []struct{ name, value string }{
		{"lane", e.Lane},
		{"session", e.Session},
		{"status", e.Status},
	} {
		if err := safetext.CheckSingleLine(filepath.Base(p), f.name, f.value); err != nil {
			return Entry{}, err
		}
	}
	return e, nil
}

// Prune bounds growth per lane, keeping the newest `keep` entries in each. It is
// per lane rather than global so a busy lane cannot evict a quiet one.
func Prune(agentsDir string, keep int) ([]string, error) {
	if keep < 1 {
		keep = 1
	}
	entries, err := List(agentsDir)
	if err != nil {
		return nil, err
	}

	byLane := map[string][]Entry{}
	for _, e := range entries {
		byLane[e.Lane] = append(byLane[e.Lane], e)
	}

	var removed []string
	for _, group := range byLane {
		sortNewestFirst(group)
		for _, e := range group[min(keep, len(group)):] {
			p := filepath.Join(root(agentsDir), filepath.FromSlash(e.Path))
			if err := os.Remove(p); err != nil {
				return removed, err
			}
			removed = append(removed, e.Path)
		}
	}
	// Map iteration is unordered and this list is printed to the user and
	// compared by tests.
	sort.Strings(removed)
	return removed, WriteIndex(agentsDir)
}

// sortNewestFirst puts the live end of a lane first, which is what a reader
// wants and what Prune keeps.
//
// The session tiebreak is not decoration. Two agents finishing in the same
// second is the case this whole scheme is designed for, sort.Slice is not
// stable, and the index is byte-compared by the pre-commit guard: without a
// total order the guard can block on a diff with nothing behind it.
func sortNewestFirst(g []Entry) {
	sort.Slice(g, func(i, j int) bool {
		if g[i].When.Equal(g[j].When) {
			return g[i].Session < g[j].Session
		}
		return g[i].When.After(g[j].When)
	})
}

func RenderIndex(entries []Entry) []byte {
	byLane := map[string][]Entry{}
	for _, e := range entries {
		byLane[e.Lane] = append(byLane[e.Lane], e)
	}

	lanes := make([]string, 0, len(byLane))
	for l := range byLane {
		lanes = append(lanes, l)
		sortNewestFirst(byLane[l])
	}
	// Most recently active lane first: the reader wants what is live.
	sort.Slice(lanes, func(i, j int) bool {
		a, b := byLane[lanes[i]][0].When, byLane[lanes[j]][0].When
		if a.Equal(b) {
			return lanes[i] < lanes[j]
		}
		return a.After(b)
	})

	var b bytes.Buffer
	b.WriteString("# Handoff index\n\n")
	b.WriteString("GENERATED by `agents index`. Do not hand-edit — the pre-commit guard\n")
	b.WriteString("regenerates this file and blocks the commit if it differs.\n\n")
	b.WriteString("`reviewed` was written deliberately. `draft` was written automatically at\n")
	b.WriteString("session end and has not been checked by anyone. Weigh them differently.\n")

	for _, l := range lanes {
		fmt.Fprintf(&b, "\n## %s\n\n", l)
		fmt.Fprintln(&b, "| when | status | session | file |")
		fmt.Fprintln(&b, "|---|---|---|---|")
		for _, e := range byLane[l] {
			// Layer 2. Rejection cannot cover these: "]" and "|" are legal in a
			// session id and in a filename, and refusing them would be a policy
			// this tool has no reason to have. Escaping keeps the cell and the
			// link intact without making the index disagree with the file.
			fmt.Fprintf(&b, "| %s | %s | %s | [%s](%s) |\n",
				e.When.UTC().Format("2006-01-02 15:04"),
				safetext.MarkdownCell(e.Status),
				safetext.MarkdownCell(e.Session),
				safetext.MarkdownLinkText(path.Base(e.Path)),
				safetext.MarkdownLinkDest(e.Path))
		}
	}
	return b.Bytes()
}

func WriteIndex(agentsDir string) error {
	entries, err := List(agentsDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root(agentsDir), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root(agentsDir), "INDEX.md"), RenderIndex(entries), 0o644)
}
