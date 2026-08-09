// Package handoff manages lane-scoped notes about work in flight.
//
// One file per (lane, session), not one rolling file. A single rolling handoff
// assumes one person, one thread, one machine; it breaks with concurrent agents
// and with a repo where several tickets are in flight under no common story.
// Distinct files also merge cleanly, which markdown cannot do with merge=union.
package handoff

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
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

// openDir opens one repository-controlled directory component without trusting
// the path spelling after the check. Lstat rejects a symlink that is already
// present; SameFile proves OpenRoot obtained the directory that was checked if
// the component changed between those operations. The returned Root is a
// directory handle, so later renames cannot be redirected by changing a parent
// pathname.
func openDirMode(parent *os.Root, parentPath, name string, create bool) (*os.Root, error) {
	p := filepath.Join(parentPath, name)
	for {
		fi, err := parent.Lstat(name)
		if os.IsNotExist(err) && create {
			if err := parent.Mkdir(name, 0o755); err != nil && !os.IsExist(err) {
				return nil, err
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s is a symlink: handoff writes only use real directories below .agents so repository content cannot redirect them", p)
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("%s is not a directory: move it aside so handoffs can be written there", p)
		}

		child, err := parent.OpenRoot(name)
		if err != nil {
			return nil, err
		}
		opened, err := child.Stat(".")
		if err != nil {
			child.Close()
			return nil, err
		}
		if !os.SameFile(fi, opened) {
			child.Close()
			return nil, fmt.Errorf("%s changed while it was being opened: retry after ensuring it is a real directory, not a symlink", p)
		}
		return child, nil
	}
}

func openDir(parent *os.Root, parentPath, name string) (*os.Root, error) {
	return openDirMode(parent, parentPath, name, true)
}

func openExistingDir(parent *os.Root, parentPath, name string) (*os.Root, error) {
	return openDirMode(parent, parentPath, name, false)
}

// openHandoffRoot deliberately opens agentsDir itself as the trust anchor and
// starts symlink refusal below it. `agents init --local` may keep a shared
// .agents directory elsewhere and link it into a worktree; that local setup is
// legitimate. reports/handoff and everything beneath it are repository content
// and therefore are not trusted to redirect a write.
func openHandoffRoot(agentsDir string) (*os.Root, error) {
	return openHandoffRootMode(agentsDir, true)
}

func openExistingHandoffRoot(agentsDir string) (*os.Root, error) {
	return openHandoffRootMode(agentsDir, false)
}

func openHandoffRootMode(agentsDir string, create bool) (*os.Root, error) {
	agentsRoot, err := os.OpenRoot(agentsDir)
	if err != nil {
		return nil, err
	}
	reports, err := openDirMode(agentsRoot, agentsDir, "reports", create)
	agentsRoot.Close()
	if err != nil {
		return nil, err
	}
	handoffRoot, err := openDirMode(reports, filepath.Join(agentsDir, "reports"), "handoff", create)
	reports.Close()
	return handoffRoot, err
}

// atomicWrite permits an absent leaf or an intentional rewrite of a regular
// file and refuses every other existing filesystem object. A rewrite inherits
// the existing permission bits instead of broadening them to the new-file
// default. The final operation is a handle-relative rename: if the destination
// changes after Lstat, rename replaces that object rather than following it,
// leaving any symlink or hardlink target untouched.
func atomicWrite(dir *os.Root, dirPath, name string, data []byte, perm os.FileMode) error {
	dst := filepath.Join(dirPath, name)
	preservePerm := false
	if fi, err := dir.Lstat(name); err == nil {
		if !fi.Mode().IsRegular() {
			if fi.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%s is a symlink, not a regular file: refusing to replace it; remove the link and retry", dst)
			}
			return fmt.Errorf("%s is not a regular file (mode %s): refusing to replace it; move it aside and retry", dst, fi.Mode())
		}
		perm = fi.Mode().Perm()
		preservePerm = true
	} else if !os.IsNotExist(err) {
		return err
	}

	tmpName := ".handoff-write-" + rand.Text()
	f, err := dir.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		dir.Remove(tmpName)
	}()
	if preservePerm {
		// OpenFile's creation mode is subject to the process umask. Chmod through
		// the still-private temporary's handle so a rewrite preserves the old
		// rwx bits exactly without acting on a repository-controlled pathname.
		if err := f.Chmod(perm); err != nil {
			return err
		}
	}
	if _, err := io.Copy(f, bytes.NewReader(data)); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := dir.Rename(tmpName, name); err != nil {
		return err
	}
	return nil
}

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

// IndexError reports that the handoff itself reached disk and only the index
// refresh afterwards failed. The two are different outcomes and must not share
// an exit code.
//
// WriteIndex re-parses every handoff in the tree, so one hand-broken or
// merge-conflicted file anywhere makes it fail. Handoffs are explicitly designed
// to be merged across branches, which makes a conflicted file a steady state
// rather than an exotic one -- and reporting that as "wanted to record and could
// not" tells a session-end hook a handoff was lost while it is sitting in the
// tree. The path is returned alongside this error, and the caller must print it.
type IndexError struct{ Err error }

func (e *IndexError) Error() string {
	return "the handoff was written but the index could not be regenerated: " + e.Err.Error()
}

func (e *IndexError) Unwrap() error { return e.Err }

// checkCaseCollision refuses a name that an existing entry of dir differs from
// only in case.
//
// internal/memory.checkIndexCollision exists for the same reason and the
// reasoning carries over: on a case-insensitive filesystem -- APFS's default,
// i.e. the platform this is developed on -- two such names are one file.
// Measured before this guard: writing session "abc" over an existing "ABC"
// destroyed the first agent's note at exit 0 and left an index row whose link
// text and session cell disagreed. That is the failure one file per (lane,
// session) exists to prevent, reached through the filesystem instead of through
// a slugifier.
//
// It refuses on a case-sensitive filesystem too, where the two names really are
// two files. The handoff tree is shared -- committed, merged across branches,
// cloned onto other machines -- so a pair of names that cannot coexist on APFS
// is a hazard wherever it was authored, and normalising one onto the other would
// reintroduce the clobber this refuses.
func checkCaseCollision(dir, name, what string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range ents {
		if n := e.Name(); n != name && strings.EqualFold(n, name) {
			return fmt.Errorf("handoff %s %q collides with the existing %q, which differs from it only in case: on a case-insensitive filesystem the two are one %s, so recording this one would destroy that one -- pick a spelling that differs by more than case", what, name, n, what)
		}
	}
	return nil
}

// Write creates one handoff and regenerates the index in the same operation, so
// the normal path can never produce a stale index.
//
// A non-nil error with a non-empty path is an *IndexError and means the handoff
// is on disk; see that type.
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
	status = canonicalStatus(status)

	// Checked before MkdirAll: on a case-insensitive filesystem MkdirAll("Lane")
	// silently succeeds onto an existing "lane" and there is nothing left to
	// notice. The lane variant is milder than the session one -- it renders two
	// "## " sections whose rows all link into one directory rather than losing a
	// file -- but the same scan covers it, so it is refused here too.
	if err := checkCaseCollision(root(agentsDir), laneName, "lane"); err != nil {
		return "", err
	}
	dir := filepath.Join(root(agentsDir), laneName)
	handoffRoot, err := openHandoffRoot(agentsDir)
	if err != nil {
		return "", err
	}
	defer handoffRoot.Close()
	laneRoot, err := openDir(handoffRoot, root(agentsDir), laneName)
	if err != nil {
		return "", err
	}
	defer laneRoot.Close()
	name := fmt.Sprintf("%s-%s.md", when.UTC().Format("2006-01-02"), session)
	if err := checkCaseCollision(dir, name, "file"); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)

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

	if err := atomicWrite(laneRoot, dir, name, b.Bytes(), 0o644); err != nil {
		return "", err
	}
	if err := WriteIndex(agentsDir); err != nil {
		return path, &IndexError{Err: err}
	}
	return path, nil
}

// List reads every handoff under a .agents/ directory.
//
// It fails on the first unreadable one rather than skipping it: a handoff that
// silently drops out of the index is invisible, and the index would then be
// confidently incomplete.
func List(agentsDir string) ([]Entry, error) {
	base := root(agentsDir)
	handoffRoot, err := openExistingHandoffRoot(agentsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer handoffRoot.Close()

	rootDir, err := handoffRoot.Open(".")
	if err != nil {
		return nil, err
	}
	lanes, err := rootDir.ReadDir(-1)
	rootDir.Close()
	if err != nil {
		return nil, err
	}
	sort.Slice(lanes, func(i, j int) bool { return lanes[i].Name() < lanes[j].Name() })

	var out []Entry
	for _, laneEntry := range lanes {
		laneName := laneEntry.Name()
		if err := checkReadPath(laneName, "lane name"); err != nil {
			return nil, err
		}
		laneInfo, err := handoffRoot.Lstat(laneName)
		if err != nil {
			return nil, err
		}
		if laneInfo.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s is a symlink: handoff reads only use real directories below .agents so repository content cannot redirect them", filepath.Join(base, laneName))
		}
		if !laneInfo.IsDir() {
			continue
		}
		laneRoot, err := openExistingDir(handoffRoot, base, laneName)
		if err != nil {
			return nil, err
		}
		laneDir, err := laneRoot.Open(".")
		if err != nil {
			laneRoot.Close()
			return nil, err
		}
		files, err := laneDir.ReadDir(-1)
		laneDir.Close()
		if err != nil {
			laneRoot.Close()
			return nil, err
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
		for _, fileEntry := range files {
			name := fileEntry.Name()
			if !strings.HasSuffix(name, ".md") {
				continue
			}
			rel := path.Join(laneName, name)
			if err := checkReadPath(rel, "filename"); err != nil {
				laneRoot.Close()
				return nil, err
			}
			fi, err := laneRoot.Lstat(name)
			if err != nil {
				laneRoot.Close()
				return nil, err
			}
			if !fi.Mode().IsRegular() {
				laneRoot.Close()
				return nil, fmt.Errorf("%s is not a regular handoff file (mode %s): move it aside and retry", rel, fi.Mode())
			}
			f, err := laneRoot.Open(name)
			if err != nil {
				laneRoot.Close()
				return nil, err
			}
			opened, statErr := f.Stat()
			after, lstatErr := laneRoot.Lstat(name)
			if statErr != nil || lstatErr != nil || !after.Mode().IsRegular() || !os.SameFile(fi, after) || !os.SameFile(after, opened) {
				f.Close()
				laneRoot.Close()
				if statErr != nil {
					return nil, statErr
				}
				if lstatErr != nil {
					return nil, lstatErr
				}
				return nil, fmt.Errorf("%s changed while it was being opened: retry after ensuring it is a regular file, not a symlink", rel)
			}
			b, readErr := io.ReadAll(f)
			closeErr := f.Close()
			if readErr != nil {
				laneRoot.Close()
				return nil, readErr
			}
			if closeErr != nil {
				laneRoot.Close()
				return nil, closeErr
			}
			e, err := parse(b, rel)
			if err != nil {
				laneRoot.Close()
				return nil, err
			}
			e.Path = rel
			out = append(out, e)
		}
		laneRoot.Close()
	}
	return out, nil
}

// checkReadPath rejects repository-controlled directory-entry names before any
// filesystem or parse error can interpolate them. Quoting the whole relative
// spelling keeps the refusal on one diagnostic line while still identifying
// the lane or leaf that must be renamed.
func checkReadPath(displayPath, what string) error {
	if r, ok := safetext.ControlRune(displayPath); ok {
		return fmt.Errorf("%q: a handoff %s contains a control character (%q): it is rendered as a single line of the index -- rename it", displayPath, what, r)
	}
	return nil
}

func parse(b []byte, displayPath string) (Entry, error) {
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Entry{}, fmt.Errorf("%s: no frontmatter", displayPath)
	}
	rest := text[4:]
	// The FIRST closing delimiter, not the last: a body may hold a horizontal
	// rule, and searching from the end would swallow it into the YAML.
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Entry{}, fmt.Errorf("%s: frontmatter is not closed", displayPath)
	}
	var e Entry
	if err := yaml.Unmarshal([]byte(rest[:end]), &e); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", displayPath, err)
	}
	if e.Lane == "" {
		e.Lane = path.Base(path.Dir(displayPath))
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
		if err := safetext.CheckSingleLine(displayPath, f.name, f.value); err != nil {
			return Entry{}, err
		}
	}
	if e.Status != StatusReviewed && e.Status != StatusDraft {
		return Entry{}, fmt.Errorf("%s: handoff status %q is not recognised: status must be exactly %q or %q", displayPath, e.Status, StatusReviewed, StatusDraft)
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

	// A removal that fails partway leaves the ones before it gone. Returning
	// there would leave INDEX.md listing files that are no longer on disk, and
	// the index is the durable record -- the error message is not. Within a lane
	// the removal order is fixed by sortNewestFirst, so this is reachable
	// deterministically, not only by an unlucky map iteration.
	var removed []string
	var rmErr error
	handoffRoot, err := openHandoffRoot(agentsDir)
	if err != nil {
		return nil, err
	}
	defer handoffRoot.Close()
removals:
	for _, group := range byLane {
		sortNewestFirst(group)
		for _, e := range group[min(keep, len(group)):] {
			laneName, name, ok := strings.Cut(e.Path, "/")
			if !ok {
				rmErr = fmt.Errorf("%s: handoff path has no lane component", e.Path)
				break removals
			}
			laneRoot, openErr := openExistingDir(handoffRoot, root(agentsDir), laneName)
			if openErr != nil {
				rmErr = openErr
				break removals
			}
			fi, lstatErr := laneRoot.Lstat(name)
			if lstatErr == nil && !fi.Mode().IsRegular() {
				lstatErr = fmt.Errorf("%s is not a regular handoff file (mode %s): refusing to prune it", e.Path, fi.Mode())
			}
			if lstatErr == nil {
				lstatErr = laneRoot.Remove(name)
			}
			laneRoot.Close()
			if lstatErr != nil {
				rmErr = lstatErr
				break removals
			}
			removed = append(removed, e.Path)
		}
	}
	// Map iteration is unordered and this list is printed to the user and
	// compared by tests.
	sort.Strings(removed)
	err = WriteIndex(agentsDir)
	// The removal failure is the more useful of the two: it names the file that
	// would not go, and the index has just been brought back in line regardless.
	if rmErr != nil {
		return removed, rmErr
	}
	return removed, err
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

func canonicalStatus(status string) string {
	if status == StatusReviewed {
		return StatusReviewed
	}
	return StatusDraft
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
				safetext.MarkdownCell(canonicalStatus(e.Status)),
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
	handoffRoot, err := openHandoffRoot(agentsDir)
	if err != nil {
		return err
	}
	defer handoffRoot.Close()
	return atomicWrite(handoffRoot, root(agentsDir), "INDEX.md", RenderIndex(entries), 0o644)
}
