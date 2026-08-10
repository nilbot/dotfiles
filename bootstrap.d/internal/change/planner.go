package change

import (
	"io"
	"path/filepath"
	"strings"
)

// Planner records what would happen and touches nothing.
//
// Queries go through it too, overlaid with its own pending changes, so a link
// into a directory the plan just created sees that directory as present. Plan
// output therefore matches what apply would print, instead of re-reporting the
// same parent directory once per link.
type Planner struct {
	reader  Interface
	out     io.Writer
	pending map[string]FileInfo
	links   map[string]string
}

func NewPlanner(reader Interface, out io.Writer) *Planner {
	return &Planner{
		reader:  reader,
		out:     out,
		pending: map[string]FileInfo{},
		links:   map[string]string{},
	}
}

func (p *Planner) Lstat(path string) (FileInfo, error) {
	if info, ok := p.pending[path]; ok {
		return info, nil
	}
	return p.reader.Lstat(path)
}

func (p *Planner) Readlink(path string) (string, error) {
	if dest, ok := p.links[path]; ok {
		return dest, nil
	}
	return p.reader.Readlink(path)
}

func (p *Planner) LookPath(name string) (string, error) { return p.reader.LookPath(name) }
func (p *Planner) ReadFile(path string) ([]byte, error) { return p.reader.ReadFile(path) }

func (p *Planner) Dir(path string) error {
	info, err := p.Lstat(path)
	if err != nil {
		return err
	}
	v, err := dirVerdict(info, path)
	if err != nil || v == verdictSatisfied {
		return err
	}
	p.recordAncestors(path)
	p.pending[path] = FileInfo{Exists: true, IsDir: true}
	report(p.out, "plan  create directory %s", path)
	return nil
}

// recordAncestors marks the whole chain a single Applier.Dir would create,
// because it uses MkdirAll. Without this the plan announces directories apply
// never reports: a later Dir on an ancestor of one already planned would print
// its own line where apply stays silent. Still exactly one line per Dir call,
// which is what Applier prints however many ancestors MkdirAll created.
func (p *Planner) recordAncestors(path string) {
	for dir := filepath.Dir(path); ; {
		info, err := p.Lstat(dir)
		if err != nil || info.Exists {
			return
		}
		p.pending[dir] = FileInfo{Exists: true, IsDir: true}
		parent := filepath.Dir(dir)
		if parent == dir { // reached the filesystem root
			return
		}
		dir = parent
	}
}

func (p *Planner) Link(source, target string) error {
	info, err := p.Lstat(target)
	if err != nil {
		return err
	}
	var current string
	if info.IsLink {
		if current, err = p.Readlink(target); err != nil {
			return err
		}
	}
	v, err := linkVerdict(info, current, source, target)
	if err != nil || v == verdictSatisfied {
		return err
	}
	// Through p.Lstat, not the reader, so a source an earlier step planned into
	// existence is honoured -- the same overlay discipline Seed uses.
	srcInfo, err := p.Lstat(source)
	if err != nil {
		return err
	}
	if err := linkSourceVerdict(srcInfo, source); err != nil {
		return err
	}
	if err := p.Dir(filepath.Dir(target)); err != nil {
		return err
	}
	p.pending[target] = FileInfo{Exists: true, IsLink: true}
	p.links[target] = source
	report(p.out, "plan  link %s -> %s", target, source)
	return nil
}

func (p *Planner) Seed(source, target string) error {
	info, err := p.Lstat(target)
	if err != nil {
		return err
	}
	v, err := seedVerdict(info, target)
	if err != nil || v == verdictSatisfied {
		return err
	}
	// Through p.Lstat, not the reader, so a template an earlier step planned
	// into existence is honoured.
	srcInfo, err := p.Lstat(source)
	if err != nil {
		return err
	}
	if err := seedSourceVerdict(srcInfo, source); err != nil {
		return err
	}
	// seedSourceVerdict names the two cases worth a remediation message.
	// Everything else -- a dangling symlink, a permission error -- is predicted
	// EXACTLY by performing the same read Applier will and discarding the
	// bytes. Reading is not a mutation, so Planner may do it.
	//
	// This ends a regress rather than narrowing it once more: predicting
	// os.ReadFile from Lstat is approximate by construction, and each fix
	// uncovered a narrower case that still diverged.
	//
	// Safe because a seed source is always a tracked repository file -- the
	// manifest's source column is repo-relative and no plan step creates one --
	// so the overlay can never need to satisfy this read.
	if _, err := p.ReadFile(source); err != nil {
		return err
	}
	if err := p.Dir(filepath.Dir(target)); err != nil {
		return err
	}
	p.pending[target] = FileInfo{Exists: true, IsRegular: true}
	report(p.out, "plan  seed %s from %s", target, source)
	return nil
}

func (p *Planner) Run(name string, args ...string) error {
	report(p.out, "plan  run: %s %s", name, strings.Join(args, " "))
	return nil
}

func (p *Planner) Sudo(name string, args ...string) error {
	report(p.out, "plan  run (sudo): %s %s", name, strings.Join(args, " "))
	return nil
}
