// Package memory reads curated memory entries and generates their index.
//
// The index is generated rather than hand-maintained because a hand-maintained
// list of what is in a directory disagrees with the directory eventually, and a
// context document whose whole value is trustworthiness cannot afford that.
package memory

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nilbot/dotfiles/agents/internal/safeio"
	"github.com/nilbot/dotfiles/agents/internal/safetext"
)

// Source points at material that does not travel with the repository.
//
// The discipline that goes with it: a memory entry must never depend on its
// source being present in order to be correct. State the takeaway in the entry;
// the source is corroboration and a route to more detail.
type Source struct {
	Kind    string `yaml:"kind"`    // transcript | harness-memory | other
	Machine string `yaml:"machine"` // which computer holds it
	Harness string `yaml:"harness"` // for kind: harness-memory
	Ref     string `yaml:"ref"`     // agent_id, resolvable through the trace index
	Note    string `yaml:"note"`
}

type Frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Sources     []Source `yaml:"sources"`
	Metadata    struct {
		Type string `yaml:"type"`
	} `yaml:"metadata"`

	Type string `yaml:"-"` // flattened from Metadata.Type for convenience
	Path string `yaml:"-"` // basename, for linking from the index
}

const delim = "---"

// indexName is the single spelling of the generated file this package writes.
// Compared with strings.EqualFold everywhere it is matched, never with ==: on a
// case-insensitive filesystem the name on disk may be any casing of it.
const indexName = "INDEX.md"

// checkSingleLine rejects a control character in a field that is rendered as one
// line of the index. The rule and its wording live in internal/safetext, which
// the handoff index and the trace listing share; `metadata.type` is the same
// hole one level up, since it is interpolated into the `## ` heading, so a
// newline there forges the section itself along with every row filed under it.
func checkSingleLine(base, field, value string) error {
	return safetext.CheckSingleLine(base, field, value)
}

func Parse(path string) (Frontmatter, error) {
	b, err := safeio.ReadRegular(path)
	if err != nil {
		return Frontmatter{}, err
	}
	return parseBytes(path, b)
}

func parseBytes(path string, b []byte) (Frontmatter, error) {
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(text, delim+"\n") {
		return Frontmatter{}, fmt.Errorf("%s: no frontmatter", filepath.Base(path))
	}
	rest := text[len(delim)+1:]
	// The FIRST closing delimiter, not the last: a body may hold a horizontal
	// rule or quote a second frontmatter block, and searching from the end would
	// swallow the body into the YAML.
	end := strings.Index(rest, "\n"+delim)
	if end < 0 {
		return Frontmatter{}, fmt.Errorf("%s: frontmatter is not closed", filepath.Base(path))
	}

	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return Frontmatter{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if fm.Name == "" || fm.Description == "" {
		return Frontmatter{}, fmt.Errorf("%s: name and description are both required", filepath.Base(path))
	}
	if err := checkSingleLine(filepath.Base(path), "name", fm.Name); err != nil {
		return Frontmatter{}, err
	}
	if err := checkSingleLine(filepath.Base(path), "description", fm.Description); err != nil {
		return Frontmatter{}, err
	}
	// Before the fallback below, not after: an entry whose type is hostile has to
	// be refused, never quietly filed somewhere else.
	if err := checkSingleLine(filepath.Base(path), "metadata.type", fm.Metadata.Type); err != nil {
		return Frontmatter{}, err
	}
	fm.Type = fm.Metadata.Type
	if fm.Type == "" {
		fm.Type = "uncategorized"
	}
	fm.Path = filepath.Base(path)
	return fm, nil
}

// List reads every entry in a memory directory.
//
// It fails on the first unparseable entry rather than skipping it: an entry
// that silently drops out of the index is invisible, and the index would then
// be confidently incomplete.
func List(dir string) ([]Frontmatter, error) {
	root, err := safeio.OpenDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}

	var out []Frontmatter
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		// EqualFold, not ==. On a case-insensitive filesystem -- macOS's
		// default -- os.WriteFile("INDEX.md") lands on an existing "index.md"
		// and the directory entry keeps its original casing, so an exact
		// comparison then reads the generated index back as an entry and fails
		// on it forever. The skip has to be about the name, not its spelling.
		if strings.EqualFold(name, indexName) {
			continue
		}
		b, _, err := safeio.ReadRegularAt(root, name)
		if err != nil {
			return nil, err
		}
		fm, err := parseBytes(name, b)
		if err != nil {
			return nil, err
		}
		out = append(out, fm)
	}
	return out, nil
}

// typeOrder fixes the section order so the output is stable regardless of what
// happens to be on disk.
var typeOrder = []string{"user", "project", "feedback", "reference", "uncategorized"}

func RenderIndex(entries []Frontmatter) []byte {
	byType := map[string][]Frontmatter{}
	for _, e := range entries {
		byType[e.Type] = append(byType[e.Type], e)
	}
	var extra []string
	for k := range byType {
		known := false
		for _, t := range typeOrder {
			if t == k {
				known = true
			}
		}
		if !known {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)

	var b bytes.Buffer
	b.WriteString("# Memory index\n\n")
	b.WriteString("GENERATED by `agents index`. Do not hand-edit — the pre-commit guard\n")
	b.WriteString("regenerates this file and blocks the commit if it differs.\n")

	for _, typ := range append(append([]string{}, typeOrder...), extra...) {
		group := byType[typ]
		if len(group) == 0 {
			continue
		}
		sort.Slice(group, func(i, j int) bool { return group[i].Name < group[j].Name })

		fmt.Fprintf(&b, "\n## %s\n\n", typ)
		for _, e := range group {
			fmt.Fprintf(&b, "- [%s](%s) — %s", linkText(e.Name), linkDest(e.Path), e.Description)
			if n := len(e.Sources); n > 0 {
				// Surfaced here because it is the one thing about an entry
				// that a reader on a different machine needs to know before
				// trusting the detail behind it.
				fmt.Fprintf(&b, " _(sources: %d)_", n)
			}
			b.WriteString("\n")
		}
	}
	return b.Bytes()
}

// linkText and linkDest keep a memory entry's name and path from closing the
// markdown link that carries them. Both live in internal/safetext: the handoff
// index links the same way, and an escaper fixed in one index and not the other
// is how the hole reopens.
func linkText(s string) string { return safetext.MarkdownLinkText(s) }

func linkDest(s string) string { return safetext.MarkdownLinkDest(s) }

// checkIndexCollision refuses to write over a curated entry that differs from
// the generated file only in case.
//
// On a case-insensitive filesystem the two names are one file: writing
// "INDEX.md" into a directory holding "index.md" truncates the entry while the
// directory keeps the lowercase name. They cannot coexist, and the tool has no
// business picking which one survives.
//
// The remedy has to cover both files that can be sitting there. If it is a
// curated entry, renaming it is right. But this refusal also fires on the
// wreckage of the bug it exists to prevent -- where an older build already wrote
// the generated index over the entry -- and renaming *that* only produces
// "old-index.md: no frontmatter" on the next run, still wedged. So the message
// names deletion too, for the case where the file is a generated index.
func checkIndexCollision(dir string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range ents {
		if n := e.Name(); n != indexName && strings.EqualFold(n, indexName) {
			return fmt.Errorf("%s: a memory entry cannot be named this, because on a case-insensitive filesystem it is the same file as the generated %s and writing the index would destroy it -- rename %s to something else if it is your entry, or delete it if it holds a generated index (which means an older build already overwrote your entry with one)", n, indexName, n)
		}
	}
	return nil
}

func WriteIndex(dir string) error {
	if err := checkIndexCollision(dir); err != nil {
		return err
	}
	entries, err := List(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, indexName), RenderIndex(entries), 0o644)
}
