// Package change is the only route by which the rest of bootstrap touches the
// machine -- reads included. internal/phase imports this package and nothing
// capable of I/O, which makes "a phase cannot mutate outside dry-run control"
// a property of the import graph rather than of a lexical scan.
package change

import (
	"fmt"
	"io"
)

// FileInfo is the deliberately small view phases get of a path. It answers the
// three questions the placement rule asks -- is it a real directory, a symlink,
// or a regular file -- and nothing else.
type FileInfo struct {
	Exists    bool
	IsDir     bool // a real directory; a symlink to one has IsLink instead
	IsLink    bool
	IsRegular bool
}

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

func report(out io.Writer, format string, args ...any) {
	fmt.Fprintf(out, format+"\n", args...)
}
