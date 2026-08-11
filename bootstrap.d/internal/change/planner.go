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

// NewPlanner mirrors NewApplier's signature, root included, so the two are
// constructed identically at every call site and neither can be built without
// naming the checkout it serves.
//
// The root is deliberately not kept. Seed writes nothing here, so there is
// nothing to substitute into -- and that is NOT a plan/apply asymmetry: see
// Planner.Seed, where a future reader is most likely to try to "fix" it.
func NewPlanner(reader Interface, out io.Writer, root string) *Planner {
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
	// Through p, not p.reader, so the walk sees directories earlier steps
	// planned into existence -- the same overlay discipline Link and Seed use.
	if err := ancestorConflict(p, path); err != nil {
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
	// The bytes are read and DISCARDED, so no root is substituted into them.
	// That is deliberate and is not a plan/apply asymmetry: substitution is a
	// pure rewrite that cannot fail, so there is no outcome here for a plan to
	// get wrong. Substituting anyway would be work whose result is thrown away,
	// and it would need a root this type has no reason to hold.
	if err := p.Dir(filepath.Dir(target)); err != nil {
		return err
	}
	p.pending[target] = FileInfo{Exists: true, IsRegular: true}
	report(p.out, "plan  seed %s from %s", target, source)
	return nil
}

// The four migration operations. No verb plans a migration today -- `migrate`
// always carries an Applier -- so these record rather than predict anything
// clever. They exist because the alternative is a second interface and a type
// assertion at the one call site that needs them, and an assertion is a runtime
// failure where this is a compile-time one.
//
// Each consults the same verdict its Applier counterpart does. That is the rule
// five defects in this design came from breaking, and it holds exactly for
// Rename, RemoveAll and WriteFile.
//
// Copy is a declared exception, the second in this package alongside Run/Sudo
// below. copyVerdict is shared, but Applier.Copy then walks the tree through
// copyTree, which refuses a socket, fifo or device node (applier.go) -- and
// Planner.Copy has no counterpart for that, because deciding it means reading
// every node under the source rather than the two this method Lstats. So for a
// source containing one, the plan reports a copy the apply would refuse.
//
// Unreachable today: no verb plans a migration, `migrate` always carries an
// Applier, and the one migration that copies walks a fish configuration
// directory. Stated rather than silently tolerated because the whole point of
// the rule is that a divergence is written down where the next reader looks --
// closing it means hoisting the node-type test into a shared verdict that both
// consult over the same walk.

func (p *Planner) Copy(source, target string) error {
	srcInfo, err := p.Lstat(source)
	if err != nil {
		return err
	}
	dstInfo, err := p.Lstat(target)
	if err != nil {
		return err
	}
	if err := copyVerdict(srcInfo, dstInfo, source, target); err != nil {
		return err
	}
	// The copy has the source's shape, so a later step reading the target sees
	// a directory where a directory was copied.
	p.pending[target] = srcInfo
	if dest, ok := p.links[source]; ok {
		p.links[target] = dest
	}
	report(p.out, "plan  copy %s -> %s", source, target)
	return nil
}

func (p *Planner) Rename(source, target string) error {
	srcInfo, err := p.Lstat(source)
	if err != nil {
		return err
	}
	dstInfo, err := p.Lstat(target)
	if err != nil {
		return err
	}
	if err := renameVerdict(srcInfo, dstInfo, source, target); err != nil {
		return err
	}
	p.pending[target] = srcInfo
	p.pending[source] = FileInfo{}
	if dest, ok := p.links[source]; ok {
		p.links[target] = dest
		delete(p.links, source)
	}
	report(p.out, "plan  rename %s -> %s", source, target)
	return nil
}

func (p *Planner) RemoveAll(path string) error {
	info, err := p.Lstat(path)
	if err != nil {
		return err
	}
	if removeVerdict(info) == verdictSatisfied {
		return nil
	}
	p.pending[path] = FileInfo{}
	delete(p.links, path)
	report(p.out, "plan  remove %s", path)
	return nil
}

// The bytes are DISCARDED, exactly as Seed discards what it reads: writing is
// the mutation, and there is nothing here a plan could get wrong by not
// performing it.
func (p *Planner) WriteFile(path string, _ []byte) error {
	info, err := p.Lstat(path)
	if err != nil {
		return err
	}
	if err := writeVerdict(info, path); err != nil {
		return err
	}
	p.pending[path] = FileInfo{Exists: true, IsRegular: true}
	report(p.out, "plan  rewrite %s", path)
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
