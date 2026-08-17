package main

import (
	"bytes"
	"strings"
	"testing"
)

// A synthetic tree, so these tests exercise the mechanism rather than the
// binary's real command set. The real tree is asserted in Task 5.
func fixtureTree() *Command {
	leaf := &Command{
		Name: "prune", Summary: "remove copies", Usage: "agents trace prune --lane <n>",
		Detail:   "Removes cached copies for one lane. Never touches the index.",
		Audience: []Audience{Human},
	}
	trace := &Command{
		Name: "trace", Summary: "query records", Usage: "agents trace <sub>",
		Detail:   "Reads the machine-local trace store.",
		Audience: []Audience{Human, Agent},
		Sub:      []*Command{leaf},
	}
	hook := &Command{
		Name: "hook", Summary: "harness entrypoint", Usage: "agents hook <event>",
		Detail:   "Invoked by a harness. Not for people.",
		Audience: []Audience{Harness},
	}
	return &Command{Name: "agents", Sub: []*Command{trace, hook}}
}

func TestFindResolvesANestedPath(t *testing.T) {
	cmd, rest := fixtureTree().Find([]string{"trace", "prune", "--lane", "x"})
	if cmd == nil || cmd.Name != "prune" {
		t.Fatalf("Find returned %v", cmd)
	}
	if len(rest) != 2 || rest[0] != "--lane" {
		t.Fatalf("remaining args = %v, want [--lane x]", rest)
	}
}

func TestFindStopsAtTheDeepestMatch(t *testing.T) {
	cmd, rest := fixtureTree().Find([]string{"trace", "nosuch"})
	if cmd == nil || cmd.Name != "trace" {
		t.Fatalf("Find returned %v; an unknown subcommand must resolve to its parent", cmd)
	}
	if len(rest) != 1 || rest[0] != "nosuch" {
		t.Fatalf("remaining args = %v, want [nosuch]", rest)
	}
}

func TestWalkVisitsEveryNodeWithItsFullPath(t *testing.T) {
	seen := map[string]bool{}
	fixtureTree().Walk(func(path []string, _ *Command) { seen[strings.Join(path, " ")] = true })
	for _, want := range []string{"trace", "trace prune", "hook"} {
		if !seen[want] {
			t.Errorf("Walk never visited %q; saw %v", want, seen)
		}
	}
	if seen["agents"] {
		t.Error("Walk visited the root; only commands are nodes")
	}
}

// The coverage check that makes an undocumented command fail loudly.
func TestWalkFindsAnEmptyDetail(t *testing.T) {
	tree := fixtureTree()
	tree.Sub[0].Sub[0].Detail = ""
	var missing []string
	tree.Walk(func(path []string, c *Command) {
		if c.Summary == "" || c.Usage == "" || c.Detail == "" {
			missing = append(missing, strings.Join(path, " "))
		}
	})
	if len(missing) != 1 || missing[0] != "trace prune" {
		t.Fatalf("missing = %v, want [trace prune]", missing)
	}
}

// Audience decides help visibility: a command no person invokes does not belong
// in the usage text a person reads.
func TestRenderUsageHidesNonHumanCommandsUnlessAllIsSet(t *testing.T) {
	var narrow, wide bytes.Buffer
	RenderUsage(fixtureTree(), &narrow, false)
	RenderUsage(fixtureTree(), &wide, true)

	if strings.Contains(narrow.String(), "hook") {
		t.Errorf("default usage shows a harness-only command:\n%s", narrow.String())
	}
	if !strings.Contains(narrow.String(), "trace") {
		t.Errorf("default usage hides a human command:\n%s", narrow.String())
	}
	if !strings.Contains(wide.String(), "hook") {
		t.Errorf("--all usage hides a harness-only command:\n%s", wide.String())
	}
}

func TestRenderHelpPrintsUsageAndDetail(t *testing.T) {
	cmd, _ := fixtureTree().Find([]string{"trace", "prune"})
	var out bytes.Buffer
	RenderHelp(cmd, []string{"trace", "prune"}, &out)
	for _, want := range []string{"agents trace prune --lane <n>", "Never touches the index"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help omitted %q:\n%s", want, out.String())
		}
	}
}

func TestAutomatedAudiences(t *testing.T) {
	for _, a := range []Audience{Git, Harness, CI} {
		if !a.Automated() {
			t.Errorf("%s must be automated: it acts on the exit code and cannot read prose", a)
		}
	}
	for _, a := range []Audience{Human, Agent} {
		if a.Automated() {
			t.Errorf("%s must be attentional: it reads the output", a)
		}
	}
}
