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
	p.pending[path] = FileInfo{Exists: true, IsDir: true}
	report(p.out, "plan  create directory %s", path)
	return nil
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
	// Applier reads the source, so Planner must check it exists. Otherwise a
	// plan reports success where apply fails -- the exact divergence this
	// package exists to prevent.
	srcInfo, err := p.Lstat(source)
	if err != nil {
		return err
	}
	if !srcInfo.Exists {
		return refuse(source, "seed template is missing",
			"restore it, or correct the manifest row that names it")
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
