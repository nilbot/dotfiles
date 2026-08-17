package main

import (
	"strings"
	"testing"
)

// Every command reachable by dispatch must carry help. This is the belt over
// the structural braces: the tree makes an undocumented command hard, and this
// makes it loud.
func TestEveryCommandIsDocumented(t *testing.T) {
	var missing []string
	rootCommand().Walk(func(path []string, c *Command) {
		switch {
		case c.Summary == "", c.Usage == "", c.Detail == "":
			missing = append(missing, strings.Join(path, " "))
		case len(c.Audience) == 0:
			missing = append(missing, strings.Join(path, " ")+" (no audience)")
		}
	})
	if len(missing) > 0 {
		t.Errorf("commands missing help or audience:\n  %s", strings.Join(missing, "\n  "))
	}
}

// A leaf must be runnable and a branch must not pretend to be.
func TestEveryLeafHasARunner(t *testing.T) {
	rootCommand().Walk(func(path []string, c *Command) {
		if len(c.Sub) == 0 && c.Run == nil {
			t.Errorf("%s is a leaf with no Run", strings.Join(path, " "))
		}
	})
}

// The commands that existed before the tree must all still be reachable.
func TestTheKnownCommandSetIsPresent(t *testing.T) {
	present := map[string]bool{}
	rootCommand().Walk(func(path []string, _ *Command) { present[strings.Join(path, " ")] = true })
	for _, want := range []string{
		"init", "wire", "doctor", "index", "save",
		"handoff write", "handoff draft", "handoff prune",
		"review",
		"trace ls", "trace show", "trace cache", "trace cache prune", "trace migrate",
		"ls", "update", "guard", "hook",
	} {
		if !present[want] {
			t.Errorf("command %q disappeared in the move to the tree", want)
		}
	}
}

// Find returns (nil, args) -- not the root, and not some partial match --
// whenever nothing at all matches: on a bare invocation and on a first token
// that names no top-level command. TestFindStopsAtTheDeepestMatch in
// command_test.go only covers an unknown token after a real parent; nothing in
// Task 4's suite pins the zero-match case, and main's dispatch (run in main.go)
// is the first caller that can hit it -- a bare `agents` and `agents
// nosuchcommand` both go through this path on their way to exit 3.
func TestFindReturnsNilWhenNothingMatchesAtAll(t *testing.T) {
	root := rootCommand()

	if cmd, rest := root.Find(nil); cmd != nil {
		t.Errorf("Find(nil) = %v, want nil", cmd)
	} else if len(rest) != 0 {
		t.Errorf("Find(nil) rest = %v, want empty", rest)
	}

	if cmd, rest := root.Find([]string{"nosuchcommand"}); cmd != nil {
		t.Errorf("Find([nosuchcommand]) = %v, want nil", cmd)
	} else if len(rest) != 1 || rest[0] != "nosuchcommand" {
		t.Errorf("Find([nosuchcommand]) rest = %v, want [nosuchcommand]", rest)
	}
}
