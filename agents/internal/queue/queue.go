// Package queue holds drafts that have not been reviewed.
//
// It lives in the machine-local store, never in .agents/. A draft is model
// output that nobody has read yet, and spec 1 rejected aiming unreviewed model
// output at a tracked directory because it makes the secret guard load-bearing
// rather than defence-in-depth. Untracked plus explicit promotion is that same
// idea done safely: the tracked tree only ever receives what a human chose.
//
// The queue is also what lets review be asynchronous. A draft written on Monday
// survives being ignored until Thursday without holding anything open, and an
// ignored queue costs nothing because nothing tracked refers to it.
package queue

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nilbot/dotfiles/agents/internal/safetext"
)

const (
	KindHandoff = "handoff"
	KindMemory  = "memory"
)

// Draft is one unreviewed note.
//
// Kind decides which tracked file it becomes at promotion, and therefore which
// fields have to be present. A memory entry is repo-wide curated knowledge with
// a generated index built from its frontmatter; a handoff is lane-scoped and
// carries its own.
type Draft struct {
	ID string `yaml:"-"` // "<lane>/<file>", assigned by Write

	Kind    string    `yaml:"kind"`
	Lane    string    `yaml:"lane"`
	Session string    `yaml:"session"`
	When    time.Time `yaml:"when"`
	Subject string    `yaml:"subject,omitempty"`

	// Memory-only, and mandatory for that kind. See Validate.
	Name        string `yaml:"name,omitempty"`
	Description string `yaml:"description,omitempty"`
	Type        string `yaml:"type,omitempty"`

	Body string `yaml:"-"`
}

// Validate is the promotion contract, enforced at the point a draft is written
// rather than at the point it is promoted.
//
// A memory entry needs name, description and metadata.type because INDEX.md is
// generated from exactly those and `agents guard --staged` regenerates and
// compares byte-for-byte. Synthesising them at promotion would put a guessed
// slug and description into the tracked tree at the one moment nobody is
// reading carefully, and the malformed entry would surface later as a blocked
// commit far from its cause.
func Validate(d Draft) error {
	switch d.Kind {
	case KindHandoff, KindMemory:
	case "":
		return fmt.Errorf("draft kind is empty; want %q or %q", KindHandoff, KindMemory)
	default:
		return fmt.Errorf("draft kind %q is neither %q nor %q", d.Kind, KindHandoff, KindMemory)
	}
	if err := checkComponent("lane", d.Lane); err != nil {
		return err
	}
	if err := checkComponent("session", d.Session); err != nil {
		return err
	}
	if strings.TrimSpace(d.Body) == "" {
		return fmt.Errorf("draft body is empty; a note that says nothing is not worth reviewing")
	}
	if d.Kind != KindMemory {
		return nil
	}
	for _, f := range []struct{ field, value string }{
		{"name", d.Name},
		{"description", d.Description},
		{"type", d.Type},
	} {
		if strings.TrimSpace(f.value) == "" {
			return fmt.Errorf("a memory draft needs %s: INDEX.md is generated from the frontmatter and the pre-commit guard compares it byte-for-byte, so promotion refuses rather than guessing one", f.field)
		}
		if err := safetext.CheckSingleLine("memory draft", f.field, f.value); err != nil {
			return err
		}
	}
	// The slug becomes a filename as well as an index row.
	return checkComponent("name", d.Name)
}

// checkComponent mirrors internal/handoff's rule, for the same reason: these
// values are joined into paths, and a draft that cannot become a handoff should
// be refused when it is written rather than when it is promoted.
func checkComponent(field, value string) error {
	if value == "" {
		return fmt.Errorf("draft %s is empty", field)
	}
	if r, ok := safetext.ControlRune(value); ok {
		return fmt.Errorf("draft %s %q contains a control character (%q): it names a file, so it has to be a single printable line", field, value, r)
	}
	if strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("draft %s %q contains a path separator: it is joined into a path, so it must be exactly one path component", field, value)
	}
	if strings.Trim(value, ".") == "" {
		return fmt.Errorf("draft %s %q is nothing but dots, which names a directory other than the one being written to", field, value)
	}
	if strings.HasPrefix(value, ".") {
		return fmt.Errorf("draft %s %q starts with a dot", field, value)
	}
	return nil
}

func root(storeDir string) string { return filepath.Join(storeDir, "queue") }

// Write files a draft and returns it with ID set.
//
// The filename carries a sequence number so one session drafting twice in a day
// does not overwrite itself -- which the handoff writer does not need, because
// one (lane, session) has one handoff, but a session may reach several separate
// conclusions.
func Write(storeDir string, d Draft) (Draft, error) {
	if err := Validate(d); err != nil {
		return Draft{}, err
	}
	if d.When.IsZero() {
		d.When = time.Now().UTC()
	}
	dir := filepath.Join(root(storeDir), d.Lane)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Draft{}, err
	}

	stem := fmt.Sprintf("%s-%s", d.When.UTC().Format("2006-01-02"), d.Session)
	name := ""
	for n := 1; n < 1000; n++ {
		candidate := stem + "-" + strconv.Itoa(n) + ".md"
		if _, err := os.Lstat(filepath.Join(dir, candidate)); os.IsNotExist(err) {
			name = candidate
			break
		} else if err != nil {
			return Draft{}, err
		}
	}
	if name == "" {
		return Draft{}, fmt.Errorf("session %q already has 999 drafts for %s; review some before writing more", d.Session, d.When.UTC().Format("2006-01-02"))
	}

	// Marshalled rather than interpolated, for the reason internal/handoff
	// documents at length: a session id is an opaque harness token, and
	// "session: " + token hands YAML a value it reinterprets.
	fm, err := yaml.Marshal(d)
	if err != nil {
		return Draft{}, err
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(fm)
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(d.Body, "\n"))
	b.WriteString("\n")

	if err := os.WriteFile(filepath.Join(dir, name), b.Bytes(), 0o644); err != nil {
		return Draft{}, err
	}
	d.ID = d.Lane + "/" + name
	return d, nil
}

// Get reads one draft by the ID Write assigned.
func Get(storeDir, id string) (Draft, error) {
	lane, name, err := splitID(id)
	if err != nil {
		return Draft{}, err
	}
	return parse(filepath.Join(root(storeDir), lane, name), id)
}

// List reads every pending draft, oldest first.
//
// Oldest first because the queue is a backlog: the note most at risk of going
// stale is the one to look at first.
func List(storeDir string) ([]Draft, error) {
	base := root(storeDir)
	lanes, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Draft
	for _, l := range lanes {
		if !l.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(base, l.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			id := l.Name() + "/" + f.Name()
			d, err := parse(filepath.Join(base, l.Name(), f.Name()), id)
			if err != nil {
				// Loud rather than skipped: a draft that silently drops out of
				// the listing is one nobody will ever review.
				return nil, err
			}
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].When.Before(out[j].When) })
	return out, nil
}

// Remove deletes one draft. Binning leaves no trace anywhere tracked, which is
// the point: a rejected draft is not a decision the repository needs to carry.
func Remove(storeDir, id string) error {
	lane, name, err := splitID(id)
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(root(storeDir), lane, name))
}

// Path is where a draft lives, for an editor to open.
func Path(storeDir, id string) (string, error) {
	lane, name, err := splitID(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(root(storeDir), lane, name), nil
}

func splitID(id string) (lane, name string, err error) {
	lane, name, ok := strings.Cut(id, "/")
	if !ok {
		return "", "", fmt.Errorf("draft id %q is not <lane>/<file>", id)
	}
	if err := checkComponent("lane", lane); err != nil {
		return "", "", err
	}
	if err := checkComponent("file", name); err != nil {
		return "", "", err
	}
	return lane, name, nil
}

func parse(path, id string) (Draft, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Draft{}, err
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Draft{}, fmt.Errorf("%s: no frontmatter", id)
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return Draft{}, fmt.Errorf("%s: frontmatter is not closed", id)
	}
	var d Draft
	if err := yaml.Unmarshal([]byte(rest[:end+1]), &d); err != nil {
		return Draft{}, fmt.Errorf("%s: %w", id, err)
	}
	d.Body = strings.TrimLeft(rest[end+len("\n---\n"):], "\n")
	d.ID = id
	return d, nil
}
