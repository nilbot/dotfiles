package phase_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/bootstrap/internal/change"
	"github.com/nilbot/dotfiles/bootstrap/internal/manifest"
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

// Asserting only err != nil would pass if Config failed for an unrelated
// reason, so this pins the type and the attribution: a bad kind is MALFORMED
// INPUT (exit 3), attributable to a line, not a refusal to touch the machine.
func TestConfigRejectsUnparseableManifest(t *testing.T) {
	_, ctx, _ := configCtx(t, "link  a  b  *\nhardlink  c  d  *\n")
	err := phase.Config(ctx)
	if err == nil {
		t.Fatal("an unknown kind must be rejected")
	}
	var syntax *manifest.SyntaxError
	if !errors.As(err, &syntax) {
		t.Fatalf("want *manifest.SyntaxError so main can exit 3, got %T: %v", err, err)
	}
	if syntax.Line != 2 {
		t.Errorf("the error should name the offending line, got %d", syntax.Line)
	}
	if !strings.Contains(err.Error(), "hardlink") {
		t.Errorf("the error should name the kind: %v", err)
	}
}

// Two owners for one path is malformed input too, not a refusal -- and it is
// the one manifest fault not attributable to a single line.
func TestConfigRefusesDuplicateTargetsAsMalformedInput(t *testing.T) {
	_, ctx, _ := configCtx(t,
		"link  one  .config/x  *\nlink  two  .config/x  *\n")
	var syntax *manifest.SyntaxError
	if err := phase.Config(ctx); !errors.As(err, &syntax) {
		t.Fatalf("want *manifest.SyntaxError so main can exit 3, got %T: %v", err, err)
	}
	if syntax.Line != 0 {
		t.Errorf("a duplicate spans two lines, so Line must be 0, got %d", syntax.Line)
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
