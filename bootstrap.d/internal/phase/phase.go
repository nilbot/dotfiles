// Package phase holds the six provisioning phases.
//
// It imports NO package capable of I/O -- not os, not os/exec. Everything it
// does to the machine goes through change.Interface, which is what makes
// "a phase cannot mutate outside dry-run control" a property of the import
// graph rather than of a lexical scan. An architecture test enforces this.
package phase

import (
	"fmt"
	"io"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
)

// Machine is the part of change.Interface a PHASE is allowed to reach, and it
// deliberately omits Copy, Rename, RemoveAll and WriteFile.
//
// Those four exist for internal/migrate, which is the only code in this design
// that destroys anything. Leaving them on the interface a phase holds would
// reopen, one layer up, the hole check.Machine closed below: the architecture
// test constrains imports, not method calls, so nothing would catch a future
// phase reaching for RemoveAll, and §5's "apply refuses, it never clobbers"
// would rest on every phase happening to only converge.
//
// Nothing is given up. Converging a machine is Dir, Link and Seed, each of
// which refuses rather than overwrite -- and no phase, present or planned, has
// asked for any of the four.
//
// change.Interface satisfies this implicitly, so nothing at a call site changes.
type Machine interface {
	Lstat(path string) (change.FileInfo, error)
	Readlink(path string) (string, error)
	LookPath(name string) (string, error)
	ReadFile(path string) ([]byte, error)

	Dir(path string) error
	Link(source, target string) error
	Seed(source, target string) error
	Run(name string, args ...string) error
	Sudo(name string, args ...string) error
}

type Context struct {
	Change   Machine
	Root     string
	Home     string
	Platform string
	Profile  string

	// Shell is $SHELL, supplied by main exactly as Home is. Nothing in this
	// package can reach the environment on its own, and the verify phase needs
	// the login shell to report on it.
	Shell string

	Out io.Writer
}

func (c Context) logf(format string, args ...any) {
	fmt.Fprintf(c.Out, format+"\n", args...)
}

type Phase struct {
	Name string
	Run  func(Context) error
}

func All() []Phase {
	return []Phase{
		{"preflight", Preflight},
		{"packages", Packages},
		{"config", Config},
		{"fish", Fish},
		{"devtools", Devtools},
		{"verify", Verify},
	}
}

// dotfilesPhases is the narrow profile: no sudo, no network, no package
// manager, no login-shell change. That is what makes it safe in a container.
var dotfilesPhases = map[string]bool{"preflight": true, "config": true, "verify": true}

func For(profile string) ([]Phase, error) {
	switch profile {
	case "workstation":
		return All(), nil
	case "dotfiles":
		var out []Phase
		for _, p := range All() {
			if dotfilesPhases[p.Name] {
				out = append(out, p)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown profile %q; expected workstation or dotfiles", profile)
}
