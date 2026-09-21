package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/doctor"
	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

// This suite replaces the one deleted with the checks it exercised. These tests
// cover the contract of the doctor COMMAND, which is a layer above the checks
// themselves: what it exits with, what it does with output it cannot trust, and
// that it observes without writing. Nothing here asserts which checks exist,
// because that is the package's business; these assert the command reports them
// honestly.

func fakeDoctorDeps(t *testing.T, root string, checks []doctor.Check) doctorCommandDependencies {
	t.Helper()
	return doctorCommandDependencies{
		Getwd:      func() (string, error) { return root, nil },
		Discover:   repo.Discover,
		BinaryPath: func() (string, error) { return filepath.Join(root, "agents"), nil },
		Now:        func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) },
		DoctorDeps: doctor.Dependencies{},
		Run: func(string, string, doctor.Dependencies) ([]doctor.Check, error) {
			return checks, nil
		},
	}
}

// Exit 0 when every check is ok, and 1 when any is not. The distinction is the
// whole contract: a CI job or a hook keys off it, and reporting a warning as
// success is the silent failure this tool exists to prevent.
func TestDoctorCommandExitContract(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)

	for _, tc := range []struct {
		name   string
		checks []doctor.Check
		want   int
	}{
		{"all ok", []doctor.Check{{Name: "a", Status: doctor.OK, Detail: "fine"}}, exitcode.OK},
		{"one warn", []doctor.Check{{Name: "a", Status: doctor.Warn, Detail: "hmm"}}, exitcode.Advisory},
		{"one fail", []doctor.Check{{Name: "a", Status: doctor.Fail, Detail: "no"}}, exitcode.Advisory},
		{"mixed", []doctor.Check{{Name: "a", Status: doctor.OK}, {Name: "b", Status: doctor.Warn}}, exitcode.Advisory},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			got := runDoctorWithDependencies(nil, &out, fakeDoctorDeps(t, root, tc.checks))
			if got != tc.want {
				t.Errorf("exit = %d, want %d\n%s", got, tc.want, out.String())
			}
		})
	}
}

// A remedy is printed only for a check that is not ok. The rule matters because
// a remedy is an instruction, and instructing someone to act on a passing check
// is how a report teaches people to ignore it.
func TestDoctorPrintsARemedyOnlyForANonOKCheck(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)
	checks := []doctor.Check{
		{Name: "passing", Status: doctor.OK, Detail: "fine", Remedy: "do not print me"},
		{Name: "failing", Status: doctor.Fail, Detail: "broken", Remedy: "do print me"},
	}
	var out bytes.Buffer
	runDoctorWithDependencies(nil, &out, fakeDoctorDeps(t, root, checks))
	if strings.Contains(out.String(), "do not print me") {
		t.Errorf("a passing check printed its remedy:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "do print me") {
		t.Errorf("a failing check did not print its remedy:\n%s", out.String())
	}
}

// An unusable diagnostic is exit 5, not 0 and not a partial report. A doctor
// that could not read the machine has nothing to say about it.
func TestDoctorReportsAnUnusableDiagnosticAsNoRecord(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)
	deps := fakeDoctorDeps(t, root, nil)
	deps.Run = func(string, string, doctor.Dependencies) ([]doctor.Check, error) {
		return nil, errors.New("boom")
	}
	var out bytes.Buffer
	if got := runDoctorWithDependencies(nil, &out, deps); got != exitcode.NoRecord {
		t.Errorf("exit = %d, want %d (could not complete)\n%s", got, exitcode.NoRecord, out.String())
	}
}

// A check naming a status the renderer does not know is a fault, not something
// to print as a blank column. This guards against a status added to the package
// later rendering as nothing.
func TestDoctorRefusesAnUnknownStatus(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)
	checks := []doctor.Check{{Name: "a", Status: "catastrophe", Detail: "?"}}
	var out bytes.Buffer
	if got := runDoctorWithDependencies(nil, &out, fakeDoctorDeps(t, root, checks)); got != exitcode.NoRecord {
		t.Errorf("exit = %d, want %d (could not complete)\n%s", got, exitcode.NoRecord, out.String())
	}
}

// Outside a git repository there is nothing to inspect, and that is a skip
// rather than a failure: running doctor in a scratch directory is not a fault.
func TestDoctorOutsideRepositorySkips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	nested := filepath.Join(dir, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	var out bytes.Buffer
	if got := runDoctorWithDependencies(nil, &out, defaultDoctorCommandDependencies()); got != exitcode.Skip {
		t.Errorf("exit = %d, want %d (skip)\n%s", got, exitcode.Skip, out.String())
	}
}

// A control character in a check name or detail must not reach the terminal
// raw: it can move the cursor, forge a second line, or hide the line above it.
// The value still appears, so the reader learns what the check saw.
func TestDoctorEscapesHostileFieldsInOutput(t *testing.T) {
	root := newRepo(t)
	t.Chdir(root)
	checks := []doctor.Check{{
		Name:   "wiring:forged",
		Status: doctor.Fail,
		Detail: "line one\nFAIL  wiring:everything  looks fine here\u001b[2K",
	}}
	var out bytes.Buffer
	runDoctorWithDependencies(nil, &out, fakeDoctorDeps(t, root, checks))
	if strings.Contains(out.String(), "\u001b") {
		t.Errorf("a control sequence reached stdout:\n%q", out.String())
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("a newline in a detail forged %d lines, want 1:\n%s", len(lines), out.String())
	}
}

// doctor observes; it never writes. A diagnostic that mutates the machine it
// reports on cannot be trusted to describe it.
func TestDoctorChangesNothing(t *testing.T) {
	root := newRepoWithAgents(t)
	t.Chdir(root)
	before := snapshotTree(t, root)
	var out bytes.Buffer
	runDoctorWithDependencies(nil, &out, defaultDoctorCommandDependencies())
	after := snapshotTree(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("doctor changed the tree it inspected:\nbefore\n%v\nafter\n%v", before, after)
	}
}
