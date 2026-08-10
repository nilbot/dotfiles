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

type Context struct {
	Change   change.Interface
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
