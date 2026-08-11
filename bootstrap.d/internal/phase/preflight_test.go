package phase_test

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/migrate"
	"github.com/nilbot/dotfiles/bootstrap/internal/phase"
)

func TestForProfileSelectsPhases(t *testing.T) {
	work, err := phase.For("workstation")
	if err != nil {
		t.Fatal(err)
	}
	if len(work) != 6 {
		t.Errorf("workstation has %d phases, want 6", len(work))
	}
	dots, err := phase.For("dotfiles")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range dots {
		names = append(names, p.Name)
	}
	got := strings.Join(names, ",")
	if got != "preflight,config,verify" {
		t.Errorf("dotfiles phases = %s, want preflight,config,verify", got)
	}
}

func TestForRejectsUnknownProfile(t *testing.T) {
	if _, err := phase.For("laptop"); err == nil {
		t.Fatal("an unknown profile must error")
	}
}

func TestPreflightRefusesWithoutStageZeroTools(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeChange{lookPathErr: map[string]bool{"git": true}}
	ctx := phase.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: "darwin",
		Profile: "workstation", Out: &out,
	}
	err := phase.Preflight(ctx)
	if err == nil {
		t.Fatal("preflight must refuse when a stage-zero tool is missing")
	}
	if !strings.Contains(err.Error(), "git") {
		t.Errorf("refusal should name the tool: %v", err)
	}
}

// The same guard internal/check keeps over its own Machine, one layer up.
//
// Task 9 gave change.Interface four destructive operations for the migrations,
// and phase.Context.Change was that interface -- so every phase could suddenly
// call RemoveAll, reopening at this layer exactly the hole check.Machine closed
// at the one below. No phase declares a need for any of the four: converging a
// machine is Dir, Link and Seed, each of which refuses rather than overwrite.
// A capability nobody needs should be one nobody can reach.
//
// The count is pinned as well as the four names, because the failure this
// guards against is a future WIDENING, and a widening does not have to use one
// of today's names.
func TestMachineCannotDestroy(t *testing.T) {
	// change.Interface must satisfy Machine, or every call site would need a
	// wrapper and the narrowing would not be free.
	var _ phase.Machine = change.Interface(nil)

	machine := reflect.TypeOf((*phase.Machine)(nil)).Elem()
	for _, method := range []string{"Copy", "Rename", "RemoveAll", "WriteFile"} {
		if _, found := machine.MethodByName(method); found {
			t.Errorf("phase.Machine exposes %s; a phase must not be able to destroy "+
				"anything -- apply refuses, it never clobbers", method)
		}
	}
	if machine.NumMethod() != 9 {
		t.Errorf("phase.Machine has %d methods, want 9 (Lstat, Readlink, LookPath, "+
			"ReadFile, Dir, Link, Seed, Run, Sudo); widening it widens what a phase "+
			"can do to a machine", machine.NumMethod())
	}
}

// Preflight is where the machine's shape is assessed, so it is where a pending
// migration must stop the run. Without this, apply reaches the config phase and
// refuses one path with a message that explains nothing about why the machine is
// in that state or what the remedy is.
func TestPreflightRefusesAPendingMigration(t *testing.T) {
	var out bytes.Buffer
	// A ~/.gitignore still pointing at the pre-rename target: §8's rename makes
	// apply refuse this path on every machine that exists today.
	fake := &fakeChange{
		info: map[string]change.FileInfo{
			"/home/.gitignore":           {Exists: true, IsLink: true},
			"/repo/git/gitignore_global": {Exists: true, IsRegular: true},
		},
		links: map[string]string{
			"/home/.gitignore": "/repo/git/gitignore_global.symlink",
		},
	}
	ctx := phase.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: "darwin",
		Profile: "dotfiles", Out: &out,
	}

	err := phase.Preflight(ctx)
	var refusal *change.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("want a *change.Refusal, got %T: %v", err, err)
	}
	if !strings.Contains(refusal.Problem, "gitignore") {
		t.Errorf("the refusal must name the pending migration: %q", refusal.Problem)
	}
	if !strings.Contains(refusal.Remediation, "migrate") {
		t.Errorf("the refusal must name './bootstrap migrate': %q", refusal.Remediation)
	}
	if len(fake.Ops) > 0 {
		t.Errorf("preflight must only read; it performed %v", fake.Ops)
	}
}

// The other direction. A preflight that refused unconditionally would pass the
// case above while making every machine unusable.
func TestPreflightAllowsAMigratedMachine(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeChange{
		info: map[string]change.FileInfo{
			"/home/.gitignore":           {Exists: true, IsLink: true},
			"/repo/git/gitignore_global": {Exists: true, IsRegular: true},
			"/home/.config/fish":         {Exists: true, IsDir: true},
		},
		links: map[string]string{
			"/home/.gitignore": "/repo/git/gitignore_global",
		},
	}
	ctx := phase.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: "darwin",
		Profile: "dotfiles", Out: &out,
	}
	if err := phase.Preflight(ctx); err != nil {
		t.Fatalf("a migrated machine must pass preflight: %v", err)
	}
}

// The RECONCILING filter, pinned here rather than only end to end.
//
// A reclaiming migration is pending for as long as the thing it reclaims exists,
// and a bare `./bootstrap migrate` deliberately never runs one -- so refusing on
// it would deadlock apply on an otherwise perfectly healthy machine, naming a
// remedy that does not clear the refusal.
//
// This machine is exactly TestPreflightAllowsAMigratedMachine's, plus a pending
// reclamation. That is the only variable, so a failure here means the filter and
// nothing else.
//
// The premise is asserted through migrate.Names against the two KINDS, not
// against migration names. What matters is that something reclaiming is due and
// nothing reconciling is -- registering a second reclaiming migration later, or
// renaming this one, must not require editing this case.
func TestPreflightAllowsAPendingReclamation(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeChange{
		info: map[string]change.FileInfo{
			"/home/.gitignore":           {Exists: true, IsLink: true},
			"/repo/git/gitignore_global": {Exists: true, IsRegular: true},
			"/home/.config/fish":         {Exists: true, IsDir: true},
			// The reclaimable installation: a real directory, untracked, and
			// pending until somebody deliberately names it.
			"/home/sdk/mambaforge": {Exists: true, IsDir: true},
		},
		links: map[string]string{
			"/home/.gitignore": "/repo/git/gitignore_global",
		},
	}

	due, err := migrate.Pending(migrate.Query{Read: fake, Root: "/repo", Home: "/home"})
	if err != nil {
		t.Fatal(err)
	}
	if len(migrate.Names(due, migrate.Reclaiming)) == 0 {
		t.Fatalf("nothing reclaiming is pending on this fixture, so this case would "+
			"pass without exercising the filter at all: %v", due)
	}
	if names := migrate.Names(due, migrate.Reconciling); len(names) != 0 {
		t.Fatalf("%v is also pending, so a refusal below would be correct and this "+
			"case would prove nothing about the filter", names)
	}

	ctx := phase.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: "darwin",
		Profile: "dotfiles", Out: &out,
	}
	if err := phase.Preflight(ctx); err != nil {
		t.Fatalf("preflight refused over a pending reclamation: %v\n"+
			"a bare migrate never runs one, so the remedy this names cannot clear "+
			"it and apply is deadlocked on a healthy machine", err)
	}
}

// fakeChange satisfies change.Interface with no I/O at all. Phase logic is
// tested against this; only the change package touches a real filesystem.
type fakeChange struct {
	info        map[string]change.FileInfo
	links       map[string]string
	files       map[string][]byte
	lookPathErr map[string]bool
	failOn      string // when a mutation's target contains this, return a Refusal
	Ops         []string
}

func (f *fakeChange) Lstat(p string) (change.FileInfo, error) { return f.info[p], nil }
func (f *fakeChange) Readlink(p string) (string, error)       { return f.links[p], nil }
func (f *fakeChange) ReadFile(p string) ([]byte, error)       { return f.files[p], nil }
func (f *fakeChange) LookPath(n string) (string, error) {
	if f.lookPathErr[n] {
		return "", errNotFound
	}
	return "/usr/bin/" + n, nil
}

// record is every mutating operation: it either refuses or appends, and failOn
// decides which.
//
// failOn is matched against the operation AS RECORDED -- "dir /home/bin",
// "run go build ...", "link /home/x -> /repo/y" -- rather than against the bare
// target. The devtools phase is why: it creates ~/bin and then names
// ~/bin/agents in three separate commands, and ~/bin is a prefix of all of
// them, so no substring of the directory path can select the directory step
// alone. Matching the recorded form lets a case name exactly one operation, and
// a case that cannot say which operation it failed cannot tell a propagated
// error from a swallowed one.
//
// Run and Sudo are covered as well as the three converging operations. Running
// a command is a mutation like any other, and a phase whose steps are
// preconditions for each other -- devtools builds the binary its last step
// points four git hooks at -- can only be shown to stop at the first failure if
// a command can be made to fail.
//
// path is what the Refusal names: the target for the converging operations, the
// command for the two that execute something. That is what lets a test assert
// WHICH step refused.
func (f *fakeChange) record(op, path string) error {
	if f.failOn != "" && strings.Contains(op, f.failOn) {
		return &change.Refusal{Path: path, Problem: "test", Remediation: "test"}
	}
	f.Ops = append(f.Ops, op)
	return nil
}

func (f *fakeChange) Dir(p string) error     { return f.record("dir "+p, p) }
func (f *fakeChange) Link(s, t string) error { return f.record("link "+t+" -> "+s, t) }
func (f *fakeChange) Seed(s, t string) error { return f.record("seed "+t+" from "+s, t) }
func (f *fakeChange) Run(n string, a ...string) error {
	return f.record("run "+n+" "+strings.Join(a, " "), n)
}
func (f *fakeChange) Sudo(n string, a ...string) error {
	return f.record("sudo "+n+" "+strings.Join(a, " "), n)
}

// The four migration operations. No phase calls them -- migrations are their own
// verb -- so recording is all this fake owes them, and an Ops entry appearing
// here would itself be the finding.
func (f *fakeChange) Copy(s, t string) error {
	f.Ops = append(f.Ops, "copy "+s+" -> "+t)
	return nil
}
func (f *fakeChange) Rename(s, t string) error {
	f.Ops = append(f.Ops, "rename "+s+" -> "+t)
	return nil
}
func (f *fakeChange) RemoveAll(p string) error {
	f.Ops = append(f.Ops, "remove "+p)
	return nil
}
func (f *fakeChange) WriteFile(p string, data []byte) error {
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[p] = data
	f.Ops = append(f.Ops, "write "+p)
	return nil
}

var errNotFound = errors.New("not found")
