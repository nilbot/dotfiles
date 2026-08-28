package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
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
		"init", "wire", "doctor", "save",
		"trace ls", "trace show", "trace cache", "trace cache prune", "trace migrate",
		"ls", "update", "version", "guard", "hook",
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

// Every declared Audience must be one of the five known constants, and every
// command must declare at least one. visibleToPeople in command.go compares
// by equality against Human, so a typo'd audience -- []Audience{"typo"} --
// passes TestEveryCommandIsDocumented (which only checks for zero audiences)
// while silently dropping the command from every audience-filtered listing.
// That is a check that cannot fail sitting inside the tree built to make
// omissions structurally impossible, so it gets pinned on its own.
func TestEveryCommandDeclaresAKnownAudience(t *testing.T) {
	known := map[Audience]bool{Git: true, Harness: true, CI: true, Human: true, Agent: true}
	rootCommand().Walk(func(path []string, c *Command) {
		if len(c.Audience) == 0 {
			t.Errorf("%s declares no audience", strings.Join(path, " "))
		}
		for _, a := range c.Audience {
			if !known[a] {
				t.Errorf("%s declares unknown audience %q", strings.Join(path, " "), a)
			}
		}
	})
}

// captureStdoutAndStderr swaps both os.Stdout and os.Stderr for the duration
// of fn and returns what each collected. cmd_trace_test.go's captureStdout
// covers stdout alone; the dispatch behaviours pinned below turn on which of
// the two streams a message lands on, so both must be observed at once.
func captureStdoutAndStderr(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	outDone := make(chan string, 1)
	errDone := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(outR)
		outDone <- string(b)
	}()
	go func() {
		b, _ := io.ReadAll(errR)
		errDone <- string(b)
	}()
	fn()
	os.Stdout, os.Stderr = savedOut, savedErr
	outW.Close()
	errW.Close()
	stdout = <-outDone
	stderr = <-errDone
	outR.Close()
	errR.Close()
	return stdout, stderr
}

// A bare `agents` is a usage error, not a request for help: it must print to
// stderr (where a usage error belongs) and exit Malformed, not print to
// stdout and exit OK the way `agents help` does. The two mutations collapse
// into each other if tested separately -- swapping the stream alone still
// leaves a nonzero exit code that looks like failure for the wrong reason,
// and swapping the exit code alone still leaves the text somewhere a script
// grepping stderr would miss it -- so both are asserted together.
func TestDispatchOnNoArgsWritesUsageToStderrAndExitsMalformed(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	stdout, stderr := captureStdoutAndStderr(t, func() { code = run(nil) })
	if code != exitcode.Malformed {
		t.Errorf("run(nil) exit = %d, want Malformed (%d)", code, exitcode.Malformed)
	}
	if stdout != "" {
		t.Errorf("run(nil) wrote to stdout, want stderr only:\n%s", stdout)
	}
	if !strings.Contains(stderr, "usage: agents") {
		t.Errorf("run(nil) stderr = %q, want it to contain the usage text", stderr)
	}
}

// `agents <unknown-garbage>` is a usage error, so its complaint belongs on
// stderr next to the usage text it is printed beside -- not on stdout, where
// a leaf command that legitimately prints to stdout (like `agents help`)
// lives.
func TestDispatchOnUnknownCommandWritesToStderrAndExitsMalformed(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	stdout, stderr := captureStdoutAndStderr(t, func() { code = run([]string{"nosuchcommand"}) })
	if code != exitcode.Malformed {
		t.Errorf("run([nosuchcommand]) exit = %d, want Malformed (%d)", code, exitcode.Malformed)
	}
	if strings.Contains(stdout, "unknown command") {
		t.Errorf("run([nosuchcommand]) wrote its complaint to stdout, want stderr:\n%s", stdout)
	}
	if !strings.Contains(stderr, `unknown command "nosuchcommand"`) {
		t.Errorf("run([nosuchcommand]) stderr = %q, want the unknown-command complaint", stderr)
	}
}

// runHook writes its diagnostics to stderr, not stdout, because a harness
// consumes the hook's stdout as the channel it parses -- the sole reason the
// IO bundle carries a separate Err field rather than flattening to one
// writer. That is documented in the tree's wiring (commands.go) and in the
// commit that introduced it, but nothing exercised it: this pins it by
// dispatching through the real tree with an event that is guaranteed to fail
// validation before touching any repository state.
func TestDispatchRoutesHookDiagnosticsToStderrNotStdout(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	stdout, stderr := captureStdoutAndStderr(t, func() {
		code = run([]string{"hook", "not-a-real-event", "--harness", "codex"})
	})
	if code != exitcode.OK {
		t.Errorf("run(hook ...) exit = %d, want OK (%d); a failed record must never disrupt a dispatch", code, exitcode.OK)
	}
	if stdout != "" {
		t.Errorf("run(hook ...) wrote to stdout, want stderr only (the harness parses stdout):\n%s", stdout)
	}
	if !strings.Contains(stderr, "not recorded") {
		t.Errorf("run(hook ...) stderr = %q, want the not-recorded diagnostic", stderr)
	}
}
