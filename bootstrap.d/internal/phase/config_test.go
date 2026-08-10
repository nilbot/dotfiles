package phase_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/phase"
)

func configCtx(t *testing.T, manifestBody string) (*fakeChange, phase.Context, *bytes.Buffer) {
	t.Helper()
	fake := &fakeChange{
		info:  map[string]change.FileInfo{},
		links: map[string]string{},
		files: map[string][]byte{
			filepath.Join("/repo", "bootstrap.d", "links.manifest"): []byte(manifestBody),
		},
		lookPathErr: map[string]bool{},
	}
	out := &bytes.Buffer{}
	return fake, phase.Context{
		Change: fake, Root: "/repo", Home: "/home", Platform: "darwin",
		Profile: "workstation", Out: out,
	}, out
}

func TestConfigAppliesEveryKind(t *testing.T) {
	fake, ctx, _ := configCtx(t,
		"link  starship.toml  .config/starship.toml  *\n"+
			"dir   -              .config/fish           *\n"+
			"seed  tmpl           .gitconfig             *\n")
	if err := phase.Config(ctx); err != nil {
		t.Fatalf("Config: %v", err)
	}
	want := []string{
		"link /home/.config/starship.toml -> /repo/starship.toml",
		"dir /home/.config/fish",
		"seed /home/.gitconfig from /repo/tmpl",
	}
	if strings.Join(fake.Ops, "\n") != strings.Join(want, "\n") {
		t.Errorf("ops:\n%s\nwant:\n%s", strings.Join(fake.Ops, "\n"), strings.Join(want, "\n"))
	}
}

func TestConfigSkipsOtherPlatforms(t *testing.T) {
	fake, ctx, _ := configCtx(t,
		"link  a  keep  darwin\nlink  b  skip  linux\n")
	if err := phase.Config(ctx); err != nil {
		t.Fatal(err)
	}
	if len(fake.Ops) != 1 || !strings.Contains(fake.Ops[0], "keep") {
		t.Errorf("expected only the darwin row, got %v", fake.Ops)
	}
}

func TestConfigRefusesDuplicateTargets(t *testing.T) {
	_, ctx, _ := configCtx(t,
		"link  one  .config/x  *\nlink  two  .config/x  *\n")
	err := phase.Config(ctx)
	if err == nil {
		t.Fatal("two owners for one path must be refused")
	}
	if !strings.Contains(err.Error(), "more than once") {
		t.Errorf("error should explain: %v", err)
	}
}

func TestConfigRejectsUnparseableManifest(t *testing.T) {
	_, ctx, _ := configCtx(t, "hardlink  a  b  *\n")
	if err := phase.Config(ctx); err == nil {
		t.Fatal("an unknown kind must be rejected")
	}
}

// A refusal must stop the run, not be logged and skipped.
func TestConfigStopsAtTheFirstRefusal(t *testing.T) {
	fake, ctx, _ := configCtx(t,
		"link  a  first   *\nlink  b  second  *\n")
	fake.failOn = "first"
	err := phase.Config(ctx)
	if err == nil {
		t.Fatal("expected the refusal to propagate")
	}
	for _, op := range fake.Ops {
		if strings.Contains(op, "second") {
			t.Errorf("rows after a refusal must not be processed: %v", fake.Ops)
		}
	}
}
