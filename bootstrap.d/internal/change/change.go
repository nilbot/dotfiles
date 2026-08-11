// Package change is the only route by which the rest of bootstrap touches the
// machine -- reads included. internal/phase imports this package and nothing
// capable of I/O, which makes "a phase cannot mutate outside dry-run control"
// a property of the import graph rather than of a lexical scan.
package change

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
)

// RootToken is what a seeded template writes where the checkout's location
// belongs; Seed replaces it with the root as it writes.
//
// Spec §7 already claims a seeded stub "necessarily names the clone location,
// and that is correct: it is machine-local and seeded once, so it is the right
// place for the one fact that varies per machine." A template copied
// byte-for-byte cannot carry a per-machine fact, and substitution is what closes
// that gap -- the checkout can be anywhere, because root() resolves it from the
// executable rather than assuming ~/dotfiles.
//
// Both consumers name a file that is passed over IN SILENCE when it is not
// there: git ignores an include it cannot open, fish ignores a source it cannot
// read. So a template hardcoding one machine's path does not fail loudly
// elsewhere; every shared setting simply stops applying.
const RootToken = "@DOTFILES_ROOT@"

// substituteRoot is the whole of the transformation Seed applies as it writes.
// It is a pure rewrite of bytes that cannot fail, which is why Planner -- which
// writes nothing -- has no outcome here to mispredict.
func substituteRoot(data []byte, root string) []byte {
	return bytes.ReplaceAll(data, []byte(RootToken), []byte(root))
}

// FileInfo is the deliberately small view phases get of a path. It answers the
// three questions the placement rule asks -- is it a real directory, a symlink,
// or a regular file -- and nothing else.
type FileInfo struct {
	Exists    bool
	IsDir     bool // a real directory; a symlink to one has IsLink instead
	IsLink    bool
	IsRegular bool
}

// Interface is everything the rest of bootstrap may do to a machine.
//
// The last four exist only for internal/migrate, and they are the only
// operations here that can destroy something. That is not a hole in §5's
// "refuse, never clobber": §5 constrains APPLY, and the phases converge a
// machine using Dir, Link and Seed, each of which refuses rather than
// overwrite. Migrations are the quarantined code that knows about the past --
// declared, named, and run by their own verb. They are also the reason the
// capability cannot be kept out of the type system entirely: a phase could
// already destroy anything it liked through Run, so withholding RemoveAll would
// buy an appearance of safety rather than safety.
type Interface interface {
	Lstat(path string) (FileInfo, error)
	Readlink(path string) (string, error)
	LookPath(name string) (string, error)
	ReadFile(path string) ([]byte, error)

	Dir(path string) error
	Link(source, target string) error
	Seed(source, target string) error
	Run(name string, args ...string) error
	Sudo(name string, args ...string) error

	Copy(source, target string) error
	Rename(source, target string) error
	RemoveAll(path string) error
	WriteFile(path string, data []byte) error
}

// Refusal is "refuse, never clobber": the operation was not performed and
// nothing was changed. Remediation is required -- a refusal that does not tell
// you what to do is a dead end.
type Refusal struct {
	Path        string
	Problem     string
	Remediation string
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("refusing: %s: %s; %s", r.Path, r.Problem, r.Remediation)
}

func refuse(path, problem, remediation string) *Refusal {
	return &Refusal{Path: path, Problem: problem, Remediation: remediation}
}

const moveAside = "move it aside deliberately, then retry"

// classify decides what an existing target means for each kind, so Applier and
// Planner cannot disagree about what counts as a conflict.
type verdict int

const (
	verdictProceed   verdict = iota // nothing there; do the work
	verdictSatisfied                // already exactly right; no-op
)

func linkVerdict(info FileInfo, current, want, target string) (verdict, error) {
	switch {
	case info.IsLink && current == want:
		return verdictSatisfied, nil
	case info.IsLink:
		return 0, refuse(target, fmt.Sprintf("points to %q, not %q", current, want), moveAside)
	case info.Exists:
		return 0, refuse(target, "exists and is not a symlink", moveAside)
	}
	return verdictProceed, nil
}

// linkSourceVerdict refuses a link whose source is absent. Without it a
// manifest typo -- or a row referring to a file a later commit will create --
// produces a dangling symlink silently, which is worse than a refusal: the
// machine ends up in a broken state that nothing reports. This masked half of
// a real plan-ordering defect in Task 4.
//
// Unlike a seed, the source is not read, so existence is the whole test.
func linkSourceVerdict(info FileInfo, source string) error {
	if !info.Exists {
		return refuse(source, "link source does not exist",
			"restore it, or correct the manifest row that names it")
	}
	return nil
}

func seedVerdict(info FileInfo, target string) (verdict, error) {
	switch {
	case info.IsLink:
		return 0, refuse(target,
			"must be a machine-local regular file but is a symlink",
			"run './bootstrap migrate', or "+moveAside)
	case info.IsRegular:
		return verdictSatisfied, nil
	case info.Exists:
		return 0, refuse(target, "exists and is not a regular file", moveAside)
	}
	return verdictProceed, nil
}

// seedSourceVerdict decides whether a seed's SOURCE is usable. Applier reads it
// with os.ReadFile and Planner cannot, so both consult this rather than each
// deciding separately -- otherwise plan reports success for a source apply
// cannot read, and the two produce different error types for one condition.
//
// The test is Exists && !IsDir, deliberately not IsRegular: Lstat reports a
// symlinked template as IsLink, and os.ReadFile follows symlinks happily, so an
// IsRegular test would diverge in the opposite direction.
func seedSourceVerdict(info FileInfo, source string) error {
	if !info.Exists {
		return refuse(source, "seed template is missing",
			"restore it, or correct the manifest row that names it")
	}
	if info.IsDir {
		return refuse(source, "seed template is a directory, not a file",
			"correct the manifest row that names it")
	}
	return nil
}

// ancestorConflict refuses when the nearest existing ancestor of path is not a
// real directory, so it cannot contain what is about to be created. Both
// implementations consult it, because without it the two disagree -- measured
// on darwin:
//
//	regular file ancestor       both fail, but with a raw *fs.PathError
//	                            (ENOTDIR) that names neither the ancestor nor
//	                            any remediation
//	dangling symlink ancestor   Applier's MkdirAll fails EEXIST; Planner plans
//	                            the directory and returns nil
//
// A symlink to a real directory is refused too, which MkdirAll would have
// followed. That is deliberate and consistent: IsDir means a REAL directory
// everywhere in this package, and dirVerdict already refuses a symlinked target
// with the same remediation. Telling them apart would need a following Stat,
// which Interface does not offer -- and writing into a symlinked tree is the
// "wrong tree" failure this design exists to prevent.
//
// The walk stops at the first ancestor that exists. It cannot run past the
// filesystem root: filepath.Dir("/") is "/", which terminates the loop.
func ancestorConflict(m Interface, path string) error {
	for dir := filepath.Dir(path); ; {
		info, err := m.Lstat(dir)
		if err != nil {
			return err
		}
		if info.Exists {
			if !info.IsDir {
				return refuse(dir,
					"exists and is not a directory, so it cannot contain "+path,
					moveAside)
			}
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir { // reached the filesystem root
			return nil
		}
		dir = parent
	}
}

func dirVerdict(info FileInfo, target string) (verdict, error) {
	switch {
	case info.IsDir:
		return verdictSatisfied, nil
	case info.Exists:
		return 0, refuse(target, "exists and is not a real directory",
			"run './bootstrap migrate', or "+moveAside)
	}
	return verdictProceed, nil
}

// copyVerdict and renameVerdict are one rule stated twice, because the two
// operations differ only in what happens to the source. Both refuse a target
// that is already there: a migration moving data that is not in git must never
// merge into something it did not create, and "the destination already exists"
// is either a half-finished earlier run or a path this migration has no claim
// to. Both are worth stopping for rather than writing through.
func copyVerdict(srcInfo, dstInfo FileInfo, source, target string) error {
	if !srcInfo.Exists {
		return refuse(source, "copy source does not exist",
			"restore it, or correct the migration that names it")
	}
	if dstInfo.Exists {
		return refuse(target, "copy target already exists", moveAside)
	}
	return nil
}

func renameVerdict(srcInfo, dstInfo FileInfo, source, target string) error {
	if !srcInfo.Exists {
		return refuse(source, "rename source does not exist",
			"restore it, or correct the migration that names it")
	}
	if dstInfo.Exists {
		return refuse(target, "rename target already exists", moveAside)
	}
	return nil
}

// removeVerdict makes an absent path a silent no-op rather than a reported
// removal. os.RemoveAll returns nil either way, so without this an Applier
// announces "removed" for something that was never there -- and a Planner,
// which cannot tell the difference, would announce it too.
func removeVerdict(info FileInfo) verdict {
	if !info.Exists {
		return verdictSatisfied
	}
	return verdictProceed
}

// writeVerdict guards the one clobbering write in this package. It exists for
// the gitconfig migration, which rewrites a machine-local file in place; a
// symlink there means the pre-37f00a0 layout, where the write would land in the
// checkout and be published. That is the exact fault §1's rule was written
// about, so it is refused rather than performed.
func writeVerdict(info FileInfo, target string) error {
	if info.Exists && !info.IsRegular {
		return refuse(target, "must be a machine-local regular file to be rewritten",
			"run './bootstrap migrate', or "+moveAside)
	}
	return nil
}

func report(out io.Writer, format string, args ...any) {
	fmt.Fprintf(out, format+"\n", args...)
}
