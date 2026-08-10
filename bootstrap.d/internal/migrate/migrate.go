// Package migrate holds the declared, idempotent, one-time operations that
// reconcile a machine provisioned by an older layout with the one this
// repository now describes.
//
// Keeping them out of apply is what preserves spec §5 intact: apply never
// clobbers, and the code that knows about the past is quarantined where it can
// be deleted once no machine needs it.
//
// Like internal/phase and internal/check it imports NO package capable of I/O.
// Everything it does to the machine goes through change.Interface, and the
// architecture test enforces it. That matters more here than anywhere else in
// the design: this is the only code that touches data which does not exist in
// git, so the one place that can destroy something is the one place a reader can
// find it.
//
// # Pending and Run consult one account of the machine
//
// Every migration below is a pair of functions over a single inspect function
// that gathers the facts once. Pending answers from those facts; Run refuses
// unless the same facts say it should proceed. A migration whose Run decided
// separately could destroy something Pending had already said was not its
// business -- and Pending being exact in BOTH directions is load-bearing:
// a false positive runs a migration twice, and a false negative leaves apply
// refusing a path forever with no remedy at all.
package migrate

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
)

// Kind decides whether a bare `./bootstrap migrate` will run a migration. It is
// the whole of the safety rule: declare the kind, and behaviour follows.
type Kind string

const (
	// Reconciling moves or rewrites. Nothing is destroyed that cannot be
	// reconstructed from the checkout, so a bare migrate runs it.
	Reconciling Kind = "reconciling"
	// Reclaiming destroys untracked data irreversibly. A bare migrate LISTS it
	// and runs nothing; it happens only when named.
	Reclaiming Kind = "reclaiming"
)

type Migration struct {
	Name string
	Kind Kind
	// Pending reports whether this machine is in the state the migration
	// reconciles. It reads and nothing more.
	Pending func(Context) (bool, error)
	// Run performs it, and is idempotent: a migration that is not pending
	// reports so and changes nothing.
	Run func(Context) error
}

type Context struct {
	Change change.Interface
	Root   string
	Home   string
	Out    io.Writer
}

func (c Context) logf(format string, args ...any) {
	fmt.Fprintf(c.Out, format+"\n", args...)
}

// All is the declared set, in the order a bare migrate runs them. Task 10 adds
// the first reclaiming migration.
func All() []Migration {
	return []Migration{
		{"fish", Reconciling, fishPending, fishRun},
		{"gitconfig", Reconciling, gitconfigPending, gitconfigRun},
		{"gitignore", Reconciling, gitignorePending, gitignoreRun},
	}
}

// UnknownError is a name that matches no migration. It is malformed INPUT --
// exit 3 -- not a machine bootstrap declined to touch, which is why it has its
// own type rather than being a plain error the caller has to match on text.
type UnknownError struct {
	Name  string
	Known []string
}

func (e *UnknownError) Error() string {
	return fmt.Sprintf("unknown migration %q; known migrations are %s",
		e.Name, strings.Join(e.Known, ", "))
}

// Pending reports every migration this machine is in the state for, of both
// kinds, in All()'s order.
//
// Callers that act on the answer must filter by Kind themselves. Preflight
// refuses on Reconciling only, and deliberately: a reclaiming migration is
// pending for as long as the thing it would reclaim exists, and a bare migrate
// never runs one -- so refusing apply on it would be a deadlock with no remedy,
// which is the failure this package exists to avoid rather than create.
func Pending(c Context) ([]Migration, error) { return pending(c, All()) }

func pending(c Context, all []Migration) ([]Migration, error) {
	var out []Migration
	for _, m := range all {
		is, err := m.Pending(c)
		if err != nil {
			return nil, fmt.Errorf("deciding whether the %s migration applies: %w", m.Name, err)
		}
		if is {
			out = append(out, m)
		}
	}
	return out, nil
}

// Names returns the names of the migrations of one kind, in order.
//
// It exists so that preflight's "refuse on reconciling migrations only" can be
// tested here, where a reclaiming migration can be supplied. Inline in preflight
// the filter was reachable by no test at all until Task 10 landed a real
// reclaiming migration -- and dropping it is a deadlock: preflight would refuse
// apply for something a bare migrate deliberately never runs, so the remedy it
// names would not clear the refusal.
func Names(ms []Migration, kind Kind) []string {
	var names []string
	for _, m := range ms {
		if m.Kind == kind {
			names = append(names, m.Name)
		}
	}
	return names
}

// Run performs one named migration, or -- given "" -- every pending reconciling
// one, listing the reclaiming ones it is eligible to run without performing any.
func Run(c Context, name string) error { return run(c, name, All()) }

// run takes the migration set so a test can exercise the reclaiming mechanism
// before Task 10 supplies a real reclaiming migration. Wiring a safety property
// and testing it in a later commit is how one ships untested.
func run(c Context, name string, all []Migration) error {
	c.logf("== migrate")
	if name != "" {
		for _, m := range all {
			if m.Name == name {
				return runOne(c, m)
			}
		}
		var known []string
		for _, m := range all {
			known = append(known, m.Name)
		}
		return &UnknownError{Name: name, Known: known}
	}

	due, err := pending(c, all)
	if err != nil {
		return err
	}
	var reclaimable []Migration
	ran := 0
	for _, m := range due {
		if m.Kind == Reclaiming {
			reclaimable = append(reclaimable, m)
			continue
		}
		if err := runOne(c, m); err != nil {
			return err
		}
		ran++
	}
	if ran == 0 && len(reclaimable) == 0 {
		c.logf("   nothing to migrate")
	}
	if len(reclaimable) > 0 {
		// Named rather than run, so nothing has to be remembered or
		// rediscovered later while a routine invocation stays unable to destroy
		// untracked data.
		c.logf("")
		c.logf("   these reclaim disk space by destroying untracked data, so a bare")
		c.logf("   migrate never runs them. Run each by name when you want it:")
		for _, m := range reclaimable {
			c.logf("     ./bootstrap migrate %s", m.Name)
		}
	}
	return nil
}

func runOne(c Context, m Migration) error {
	c.logf("   %s", m.Name)
	return m.Run(c)
}

// resolveLink is what a symlink's destination MEANS as a path. Readlink returns
// the raw target, which may be relative -- git and fish both resolve such a
// target against the directory holding the link, and a comparison against an
// absolute path would otherwise report every relative link as pointing
// somewhere else.
func resolveLink(link, dest string) string {
	if !filepath.IsAbs(dest) {
		return filepath.Join(filepath.Dir(link), dest)
	}
	return filepath.Clean(dest)
}
