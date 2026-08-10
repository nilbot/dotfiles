package phase_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
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
func (f *fakeChange) Dir(p string) error {
	if f.failOn != "" && strings.Contains(p, f.failOn) {
		return &change.Refusal{Path: p, Problem: "test", Remediation: "test"}
	}
	f.Ops = append(f.Ops, "dir "+p)
	return nil
}
func (f *fakeChange) Link(s, t string) error {
	if f.failOn != "" && strings.Contains(t, f.failOn) {
		return &change.Refusal{Path: t, Problem: "test", Remediation: "test"}
	}
	f.Ops = append(f.Ops, "link "+t+" -> "+s)
	return nil
}
func (f *fakeChange) Seed(s, t string) error {
	if f.failOn != "" && strings.Contains(t, f.failOn) {
		return &change.Refusal{Path: t, Problem: "test", Remediation: "test"}
	}
	f.Ops = append(f.Ops, "seed "+t+" from "+s)
	return nil
}
func (f *fakeChange) Run(n string, a ...string) error {
	f.Ops = append(f.Ops, "run "+n+" "+strings.Join(a, " "))
	return nil
}
func (f *fakeChange) Sudo(n string, a ...string) error {
	f.Ops = append(f.Ops, "sudo "+n+" "+strings.Join(a, " "))
	return nil
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
