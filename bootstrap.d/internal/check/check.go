// Package check reports whether this machine is in the state bootstrap would
// put it in. It answers questions; it never converges anything.
//
// Like internal/phase it imports NO package capable of I/O -- not os, not
// os/exec. Every question it asks the machine goes through change.Interface,
// which is what makes "a check cannot mutate" a property of the import graph
// rather than of a lexical scan. The architecture test enforces it.
//
// It does not import internal/phase. phase.Verify runs these checks, so the
// dependency runs one way only, which is why Context is declared here instead
// of reusing phase.Context.
//
// # Context.Change must be an Applier, never a Planner
//
// A Planner's Run records the command and returns nil without running it. A
// check that asks a question by running one -- packages asks `brew bundle check`
// -- would read that nil as success, so the answer would be "everything is
// installed" on a machine where nothing is. That is a silent false pass in the
// layer whose entire job is catching silent failures, and it would appear only
// under `plan`, where nobody is looking for it.
//
// Handing a check an Applier does not weaken the dry-run invariant: every check
// reads, and `brew bundle check` is a query. Checks never mutate, which is why
// they do not need the Planner's protection in the first place. phase.Verify
// therefore builds its own Applier rather than passing its Context's Change
// along.
package check

import (
	"fmt"
	"io"
	"strings"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
)

type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
	NA   Status = "n/a"
)

// Result is one check's verdict. Name is a stable identifier -- tests and
// humans both key off it -- while Detail is prose that may be reworded freely.
type Result struct {
	Status Status
	Name   string
	Detail string
}

type Context struct {
	Change   change.Interface
	Root     string
	Home     string
	Platform string
	Profile  string

	// Shell is $SHELL, read by main and passed in exactly as Home is. Nothing
	// in this package or in internal/phase can reach the environment on its
	// own, which is the point: the login shell is machine state, and machine
	// state arrives through the caller or through change.Interface.
	Shell string
}

// All runs spec 2 §10's eight checks, in the order they are listed there.
//
// The error is separate from the results on purpose. Everything a check can say
// about the machine is a Result; the error carries the one thing that is not
// about the machine at all -- a manifest that does not parse, which is malformed
// INPUT and answers 3, not 2. The results are complete either way, so a caller
// that only reports may ignore it.
func All(c Context) ([]Result, error) {
	rows, rowsErr := loadRows(c)
	results := []Result{
		platform(c),
		manifestOwners(rows, rowsErr),
		manifestKinds(c, rows, rowsErr),
		fishSource(c),
		gitconfigInclude(c),
	}
	for _, m := range machineChecks {
		// Checks 6-8 cover state the dotfiles profile deliberately does not
		// manage -- no sudo, no network, no package manager, no login-shell
		// change. Reporting them as failures would make every container run
		// report three problems that are not problems, which is how a report
		// stops being read at all.
		if !managesMachine(c.Profile) {
			results = append(results, Result{NA, m.name,
				fmt.Sprintf("the %s profile does not manage %s", c.Profile, m.subject)})
			continue
		}
		results = append(results, m.run(c))
	}
	return results, rowsErr
}

// machineChecks are the three that concern machine-wide state.
var machineChecks = []struct {
	name    string
	subject string
	run     func(Context) Result
}{
	{"login-shell", "the login shell", loginShell},
	{"agents", "developer tooling", agentsOnPath},
	{"packages", "installed packages", packages},
}

// managesMachine is the one place this package decides what a profile covers.
// It cannot ask internal/phase, which imports this package; the profile names
// themselves are validated there, before any Context reaches here.
func managesMachine(profile string) bool { return profile == "workstation" }

// ExitCode maps results onto spec 1 §6's shared table: 2 block, 1 advisory,
// 0 ok. NA never affects it -- "this profile does not manage that" is not a
// finding.
func ExitCode(results []Result) int {
	code := 0
	for _, r := range results {
		switch r.Status {
		case Fail:
			return 2
		case Warn:
			code = 1
		}
	}
	return code
}

// Write renders results for a human. The column layout is deliberately not a
// contract: Name and Status are what anything else should key off.
//
// It prints no section header. The same results are written by two callers --
// the check verb and the verify phase -- and each names its own section, so
// `plan workstation` reports "== verify" for the phase it just ran rather than
// a heading for a verb nobody invoked.
//
// A Detail may carry newlines, and every line after the first is indented under
// the first. A check that found eight things wrong says so as eight lines; as
// one line nobody reads past the second finding.
func Write(w io.Writer, results []Result) {
	width := 0
	for _, r := range results {
		if len(r.Name) > width {
			width = len(r.Name)
		}
	}
	const status = 4 // the widest Status, "fail" and "warn"
	indent := status + 1 + width + 2

	counts := map[Status]int{}
	for _, r := range results {
		counts[r.Status]++
		lines := strings.Split(r.Detail, "\n")
		head := fmt.Sprintf("%-*s %-*s  %s", status, r.Status, width, r.Name, lines[0])
		fmt.Fprintln(w, strings.TrimRight(head, " "))
		for _, line := range lines[1:] {
			fmt.Fprintf(w, "%*s%s\n", indent, "", line)
		}
	}
	fmt.Fprintf(w, "-- %d ok, %d warn, %d fail, %d n/a\n",
		counts[OK], counts[Warn], counts[Fail], counts[NA])
}
