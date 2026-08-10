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

// fakeChange satisfies change.Interface with no I/O at all. Phase logic is
// tested against this; only the change package touches a real filesystem.
type fakeChange struct {
	info        map[string]change.FileInfo
	links       map[string]string
	files       map[string][]byte
	lookPathErr map[string]bool
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
func (f *fakeChange) Dir(p string) error { f.Ops = append(f.Ops, "dir "+p); return nil }
func (f *fakeChange) Link(s, t string) error {
	f.Ops = append(f.Ops, "link "+t+" -> "+s)
	return nil
}
func (f *fakeChange) Seed(s, t string) error {
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

var errNotFound = errors.New("not found")
