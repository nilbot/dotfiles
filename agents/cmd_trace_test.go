package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/record"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

// seedTraces writes two records that differ in every filterable field, so a
// flag bound to the wrong Filter field cannot pass by accident. The trace
// package's own tests exercise matching; nothing there can catch `--module`
// wired to Filter.Machine.
func seedTraces(t *testing.T, root string) {
	t.Helper()
	w := record.NewWriter(repo.AgentsDir(root))
	now := time.Now().UTC()
	recs := []record.Record{
		{When: now.Add(-1 * time.Hour), Harness: "codex", Machine: "m1", Event: "stop",
			Lane: "lane-a", Cwd: "alpha/api", AgentType: "Explore",
			Description: "alpha work", Transcript: "/t/a.jsonl", PointerVerified: true},
		{When: now.Add(-48 * time.Hour), Harness: "claude-code", Machine: "m2", Event: "subagent-stop",
			Lane: "lane-b", Cwd: "beta/web", AgentType: "Plan",
			Description: "beta work", Transcript: "/t/b.jsonl"},
	}
	for _, r := range recs {
		if err := w.Append(r); err != nil {
			t.Fatal(err)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = saved
	w.Close()
	out := <-done
	r.Close()
	return out
}

func TestTraceLSPrintsRecordsNewestFirst(t *testing.T) {
	root := newRepo(t)
	seedTraces(t, root)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runTrace([]string{"ls"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want %d; output:\n%s", code, exitcode.OK, out.String())
	}
	body := out.String()
	a, b := strings.Index(body, "alpha work"), strings.Index(body, "beta work")
	if a < 0 || b < 0 {
		t.Fatalf("both records must be listed; output:\n%s", body)
	}
	if a > b {
		t.Errorf("newest record must come first; output:\n%s", body)
	}
}

// Each flag must reach the Filter field it names. A single record surviving the
// wrong filter is indistinguishable from the right one unless the two records
// differ in every field, which is why seedTraces makes them.
func TestTraceLSFlagsReachTheirFilterField(t *testing.T) {
	root := newRepo(t)
	seedTraces(t, root)
	t.Chdir(root)

	cases := []struct {
		name string
		args []string
	}{
		{"lane", []string{"ls", "--lane", "lane-a"}},
		{"module", []string{"ls", "--module", "alpha"}},
		{"machine", []string{"ls", "--machine", "m1"}},
		{"harness", []string{"ls", "--harness", "codex"}},
		{"event", []string{"ls", "--event", "stop"}},
		{"grep", []string{"ls", "--grep", "Explore"}},
		{"since", []string{"ls", "--since", "3h"}},
		{"limit", []string{"ls", "--limit", "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if code := runTrace(tc.args, &out); code != exitcode.OK {
				t.Fatalf("exit = %d, want %d; output:\n%s", code, exitcode.OK, out.String())
			}
			body := out.String()
			if !strings.Contains(body, "alpha work") {
				t.Errorf("%v dropped the record it should keep; output:\n%s", tc.args, body)
			}
			if strings.Contains(body, "beta work") {
				t.Errorf("%v kept the record it should exclude; output:\n%s", tc.args, body)
			}
		})
	}
}

// Unreadable lines shrink the history silently unless the command says so. The
// exit code is the part a script can act on.
func TestTraceLSAdvisoryOnUnreadableLines(t *testing.T) {
	root := newRepo(t)
	seedTraces(t, root)
	t.Chdir(root)

	path := filepath.Join(repo.AgentsDir(root), "reports", "traces",
		time.Now().UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("<<<<<<< HEAD\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	var out bytes.Buffer
	if code := runTrace([]string{"ls"}, &out); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory (%d); output:\n%s", code, exitcode.Advisory, out.String())
	}
	if !strings.Contains(out.String(), "unreadable") {
		t.Errorf("the advisory must name what happened; output:\n%s", out.String())
	}
}

func TestTraceExitCodes(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		inRepo bool
		want   int
	}{
		{"no subcommand", nil, true, exitcode.Malformed},
		{"unknown subcommand", []string{"teatime"}, true, exitcode.Malformed},
		{"unparseable since", []string{"ls", "--since", "soon"}, true, exitcode.Malformed},
		{"unknown flag", []string{"ls", "--nope"}, true, exitcode.Malformed},
		// Outside a repo there is nothing to answer about, which is not a
		// failure: Skip is what a shell wrapper keys off.
		{"outside a repo", []string{"ls"}, false, exitcode.Skip},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.inRepo {
				dir = newRepo(t)
			}
			t.Chdir(dir)
			var out bytes.Buffer
			if code := runTrace(tc.args, &out); code != tc.want {
				t.Fatalf("exit = %d, want %d; output:\n%s", code, tc.want, out.String())
			}
			if out.Len() == 0 {
				t.Error("a refusal must say why")
			}
		})
	}
}

// A subcommand that is not registered in main.go is unreachable however well it
// works. Skip is only reachable through the trace switch; an unregistered
// command returns Malformed.
func TestMainRegistersTrace(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	out := captureStdout(t, func() { code = run([]string{"trace", "ls"}) })
	if code != exitcode.Skip {
		t.Fatalf("run(trace ls) = %d, want Skip (%d); stdout:\n%s", code, exitcode.Skip, out)
	}
}
