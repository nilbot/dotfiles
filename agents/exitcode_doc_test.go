package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

// Spec 1 §6 defines the shared vocabulary. The code is the implementation and
// the spec is design intent; when they disagree this test is what surfaces it,
// and a human decides which was wrong. Prose cannot be generated from code, so
// a pinning test is the only thing that can hold the two together.
func TestExitCodeTableMatchesSpecOne(t *testing.T) {
	want := []struct {
		code int
		name string
	}{
		{exitcode.OK, "ok"},
		{exitcode.Advisory, "advisory"},
		{exitcode.Block, "block"},
		{exitcode.Malformed, "malformed"},
		{exitcode.Skip, "skip"},
		{exitcode.NoRecord, "could not complete"},
	}
	for i, w := range want {
		if w.code != i {
			t.Fatalf("%s must be %d, is %d; spec 1 §6 fixes these values", w.name, i, w.code)
		}
	}

	var out bytes.Buffer
	RenderExitCodes(&out)
	for _, w := range want {
		if !strings.Contains(out.String(), w.name) {
			t.Errorf("the rendered table omits %q:\n%s", w.name, out.String())
		}
	}
}

// The table must be rendered, not restated. If someone reintroduces a literal,
// changing a constant stops changing the help text and the drift returns.
func TestExitCodeTableIsRenderedFromTheConstants(t *testing.T) {
	var out bytes.Buffer
	RenderExitCodes(&out)
	for _, digit := range []string{"0", "1", "2", "3", "4", "5"} {
		if !strings.Contains(out.String(), digit) {
			t.Errorf("code %s missing from the rendered table:\n%s", digit, out.String())
		}
	}
}

func TestHelpForALeafExitsZeroAndNamesTheCommand(t *testing.T) {
	var out bytes.Buffer
	if code := runHelp([]string{"doctor"}, &out); code != exitcode.OK {
		t.Fatalf("help exit = %d, want 0", code)
	}
	for _, want := range []string{"agents doctor", "observe"} {
		if !strings.Contains(strings.ToLower(out.String()), strings.ToLower(want)) {
			t.Errorf("help omitted %q:\n%s", want, out.String())
		}
	}
}

func TestHelpForAnUnknownPathExitsThree(t *testing.T) {
	var out bytes.Buffer
	if code := runHelp([]string{"nosuchcommand"}, &out); code != exitcode.Malformed {
		t.Fatalf("help for an unknown command exit = %d, want 3", code)
	}
}

// --all is what makes a harness-only or git-only command discoverable without
// putting it in the list a person reads.
func TestHelpAllIncludesTheAutomatedCommands(t *testing.T) {
	var narrow, wide bytes.Buffer
	runHelp(nil, &narrow)
	runHelp([]string{"--all"}, &wide)
	// guard is the automated command now: git invokes it, a person rarely
	// types it, so the default listing leaves it out and --all adds it.
	if strings.Contains(narrow.String(), "agents guard") {
		t.Errorf("default help lists an automated command:\n%s", narrow.String())
	}
	if !strings.Contains(wide.String(), "agents guard") {
		t.Errorf("--all omits an automated command:\n%s", wide.String())
	}
}
