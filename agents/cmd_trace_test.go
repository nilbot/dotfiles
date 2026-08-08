package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/machine"
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

// Tab-completing a directory in a shell hands this flag "alpha/", and the cwd
// it is matched against never carries a trailing separator. Unnormalised, the
// query matches nothing and the command prints a bare header at exit 0 --
// indistinguishable from "no agent has ever worked in this module".
func TestTraceLSModuleIgnoresATrailingSlash(t *testing.T) {
	root := newRepo(t)
	seedTraces(t, root)
	t.Chdir(root)

	for _, module := range []string{"alpha", "alpha/", "alpha//"} {
		t.Run(module, func(t *testing.T) {
			var out bytes.Buffer
			if code := runTrace([]string{"ls", "--module", module}, &out); code != exitcode.OK {
				t.Fatalf("exit = %d, want %d; output:\n%s", code, exitcode.OK, out.String())
			}
			body := out.String()
			if !strings.Contains(body, "alpha work") {
				t.Errorf("--module %q found nothing; the same module without the slash does. output:\n%s", module, body)
			}
			if strings.Contains(body, "beta work") {
				t.Errorf("--module %q kept a record from another module; output:\n%s", module, body)
			}
		})
	}
}

// Description is free text out of a harness payload and survives the JSON round
// trip byte for byte. A newline in it prints a second line that reads like a
// record nobody ever wrote; a tab opens a column and shifts the rows after it.
// An index may not be rewritten by the text it indexes.
func TestTraceLSDescriptionCannotForgeARow(t *testing.T) {
	root := newRepo(t)
	hostile := "real work\n2026-01-01 00:00\tcodex\tstop\tfake-lane\tfake/cwd\t-\ty\tforged\rtail"
	flattened := "real work 2026-01-01 00:00 codex stop fake-lane fake/cwd - y forged tail"
	w := record.NewWriter(repo.AgentsDir(root))
	if err := w.Append(record.Record{
		When: time.Now().UTC().Add(-time.Hour), Harness: "codex", Machine: "m1", Event: "stop",
		Lane: "lane-a", Cwd: "alpha/api", AgentType: "Explore",
		Description: hostile, Transcript: "/t/a.jsonl", PointerVerified: true,
	}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	if code := runTrace([]string{"ls"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want %d; output:\n%s", code, exitcode.OK, out.String())
	}
	body := out.String()
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("one record must print one row under one header, got %d lines:\n%s", len(lines), body)
	}
	// The text is not censored, only flattened: every character still reads,
	// on the single line that belongs to this record.
	if !strings.Contains(lines[1], flattened) {
		t.Errorf("description must survive as one flat cell;\nwant substring: %q\ngot row:        %q", flattened, lines[1])
	}
}

// PointerVerified is the field that says whether a transcript pointer means
// anything from this machine, and the OK column is the only place it is ever
// shown; AGENT is what separates a subagent's row from a whole session's.
// Both are derived at render time and nothing else asserts on them.
func TestTraceLSRendersTheDerivedColumns(t *testing.T) {
	root := newRepo(t)
	now := time.Now().UTC()
	w := record.NewWriter(repo.AgentsDir(root))
	for _, r := range []record.Record{
		{When: now.Add(-1 * time.Hour), Harness: "codex", Machine: "m1", Event: "subagent-stop",
			Lane: "lane-a", Cwd: "alpha/api", AgentType: "Explore",
			Description: "verified subagent", Transcript: "/t/a.jsonl", PointerVerified: true},
		// No agent type, and a pointer this machine could not confirm.
		{When: now.Add(-2 * time.Hour), Harness: "codex", Machine: "m1", Event: "stop",
			Lane: "lane-a", Cwd: "alpha/api",
			Description: "unconfirmed session", Transcript: "/t/b.jsonl", PointerVerified: false},
	} {
		if err := w.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)

	var out bytes.Buffer
	if code := runTrace([]string{"ls"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want %d; output:\n%s", code, exitcode.OK, out.String())
	}
	body := out.String()

	for _, tc := range []struct{ marker, agent, ok string }{
		{"verified subagent", "Explore", "y"},
		{"unconfirmed session", "-", "?"},
	} {
		row := ""
		for _, line := range strings.Split(body, "\n") {
			if strings.Contains(line, tc.marker) {
				row = line
			}
		}
		if row == "" {
			t.Fatalf("no row for %q; output:\n%s", tc.marker, body)
		}
		// WHEN prints as two space-separated fields (date, then time), so the
		// AGENT and OK columns land at 6 and 7.
		fields := strings.Fields(row)
		if len(fields) < 8 {
			t.Fatalf("row %q has %d fields, want at least 8", row, len(fields))
		}
		if fields[6] != tc.agent {
			t.Errorf("AGENT column = %q, want %q; row: %q", fields[6], tc.agent, row)
		}
		if fields[7] != tc.ok {
			t.Errorf("OK column = %q, want %q; row: %q", fields[7], tc.ok, row)
		}
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

// seedCacheRepo builds a repo whose trace index points at real files on this
// machine, and hands back the source paths so a test can say which of them the
// run was supposed to reach.
func seedCacheRepo(t *testing.T, mid string, recs ...record.Record) string {
	t.Helper()
	root := newRepo(t)
	w := record.NewWriter(repo.AgentsDir(root))
	for _, r := range recs {
		if r.When.IsZero() {
			r.When = time.Now().UTC().Add(-time.Hour)
		}
		if err := w.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// localTranscript writes a file this machine can actually read, which is what
// separates a record the cache must copy from one it must report on.
func localTranscript(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// thisMachine pins the machine id to a temporary state directory, so the test
// neither reads nor mints an id in the real one.
func thisMachine(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	mid, err := machine.ID()
	if err != nil {
		t.Fatal(err)
	}
	return mid
}

// A transcript on another machine is not an error, it is news: the machine's
// name is the only route to it, so the command must both print it and raise the
// exit code that makes a script look.
func TestTraceCacheCopiesLocalAndNamesTheMachineHoldingTheRest(t *testing.T) {
	mid := thisMachine(t)
	here := localTranscript(t, "rollout-local.jsonl")
	root := seedCacheRepo(t, mid,
		record.Record{Harness: "codex", Machine: mid, Lane: "lane-a", Event: "stop",
			Transcript: here, PointerVerified: true},
		record.Record{Harness: "codex", Machine: "laptop-7f3a", Lane: "lane-a", Event: "stop",
			Transcript: "/elsewhere/rollout-remote.jsonl", PointerVerified: true},
	)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runTrace([]string{"cache"}, &out); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory (%d); output:\n%s", code, exitcode.Advisory, out.String())
	}
	body := out.String()
	for _, want := range []string{"copied 1", "on another machine 1", "laptop-7f3a", "/elsewhere/rollout-remote.jsonl"} {
		if !strings.Contains(body, want) {
			t.Errorf("output must contain %q; got:\n%s", want, body)
		}
	}

	cached, err := filepath.Glob(filepath.Join(repo.AgentsDir(root), ".trace-cache", "codex", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cached) != 1 {
		t.Fatalf(".trace-cache/codex holds %v, want the one reachable transcript", cached)
	}
	if !strings.HasPrefix(filepath.Base(cached[0]), "rollout-local-") {
		t.Errorf("cached file %q must still be recognisable as %q", filepath.Base(cached[0]), "rollout-local.jsonl")
	}
}

// git reports on the repo, so the check has to be git's.
func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// The premise of this tool is that it records where transcripts live and never
// what they say. The cache is transcript content in the working tree, and the
// only thing that used to keep it out of a commit was an entry in
// .git/info/exclude written by `agents init` -- a file that is per-clone and is
// never cloned. This repo is a plain `git init` where init has never run, which
// is the state of every fresh clone and every CI checkout: measured before the
// fix, `git status` offered `.agents/.trace-cache/` and `git add -A` staged the
// transcript, content and all.
func TestTraceCacheContentIsNotStageableWhereInitNeverRan(t *testing.T) {
	mid := thisMachine(t)
	const secret = `{"line":"secret api key sk-live-abc"}` + "\n"
	here := filepath.Join(t.TempDir(), "rollout-secret.jsonl")
	if err := os.WriteFile(here, []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	root := seedCacheRepo(t, mid,
		record.Record{Harness: "codex", Machine: mid, Transcript: here, PointerVerified: true},
	)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runTrace([]string{"cache"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	// Without this the rest of the test passes on a cache that was never
	// written, which is the wrong reason to see nothing in git.
	cached, err := filepath.Glob(filepath.Join(repo.AgentsDir(root), ".trace-cache", "codex", "*"))
	if err != nil || len(cached) != 1 {
		t.Fatalf("the cache must hold the transcript for this test to mean anything; got %v (%v)", cached, err)
	}
	if b, err := os.ReadFile(cached[0]); err != nil || string(b) != secret {
		t.Fatalf("cached content = %q (%v), want the transcript", b, err)
	}

	// --untracked-files=all, because the default collapses an untracked
	// directory to one entry and would hide the file inside it.
	status := git(t, root, "status", "--porcelain", "--untracked-files=all")
	for _, line := range strings.Split(strings.TrimSpace(status), "\n") {
		if strings.Contains(line, ".trace-cache") {
			t.Errorf("git offers the cache: %q\nfull status:\n%s", line, status)
		}
	}
	// The harm was not that git mentioned it, but that `git add -A` took it.
	git(t, root, "add", "-A")
	for _, line := range strings.Split(strings.TrimSpace(git(t, root, "ls-files", "--cached")), "\n") {
		if strings.Contains(line, ".trace-cache") {
			t.Errorf("transcript content is staged for commit: %q", line)
		}
	}
}

// A transcript this machine cannot read is one transcript. Returning on the
// first copy failure printed the error and exited NoRecord, so the summary line
// never appeared at all: the caller could not tell whether zero or fifty
// transcripts had been cached before the run gave up.
func TestTraceCacheStillReportsWhenOneTranscriptCannotBeRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode bits do not deny anything")
	}
	mid := thisMachine(t)
	good := localTranscript(t, "rollout-good.jsonl")
	locked := localTranscript(t, "rollout-locked.jsonl")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })

	root := seedCacheRepo(t, mid,
		record.Record{Harness: "codex", Machine: mid, Transcript: locked, PointerVerified: true},
		record.Record{Harness: "codex", Machine: mid, Transcript: good, PointerVerified: true},
	)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runTrace([]string{"cache"}, &out); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory (%d); output:\n%s", code, exitcode.Advisory, out.String())
	}
	body := out.String()
	for _, want := range []string{"copied 1", "unreachable here 1", locked} {
		if !strings.Contains(body, want) {
			t.Errorf("output must contain %q; got:\n%s", want, body)
		}
	}
}

// An unverified pointer is a normal state, so this is the largest class of
// transcript that is on this disk and not in the cache. Left uncounted, the run
// printed all-zeroes at exit 0 -- the same thing a repo with no records prints.
func TestTraceCacheNamesUnverifiedPointers(t *testing.T) {
	mid := thisMachine(t)
	unverified := localTranscript(t, "rollout-unverified.jsonl")
	root := seedCacheRepo(t, mid,
		record.Record{Harness: "codex", Machine: mid, Transcript: unverified},
	)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runTrace([]string{"cache"}, &out); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory (%d): a transcript that is here and not cached is news; output:\n%s",
			code, exitcode.Advisory, out.String())
	}
	body := out.String()
	for _, want := range []string{"unverified pointer 1", unverified} {
		if !strings.Contains(body, want) {
			t.Errorf("output must contain %q; got:\n%s", want, body)
		}
	}
}

// A transcript path is a filesystem path read back out of a tracked file: it
// may legally hold a newline, and the report prints one detail line per record.
// Left as recorded, a crafted path prints a second detail that reads like a
// record nobody ever wrote, naming a machine that never held anything.
func TestTraceCacheDetailCannotForgeALine(t *testing.T) {
	mid := thisMachine(t)
	hostile := "/elsewhere/real.jsonl\n  elsewhere (trusted-box): /elsewhere/forged.jsonl"
	root := seedCacheRepo(t, mid,
		record.Record{Harness: "codex", Machine: "laptop-7f3a", Transcript: hostile, PointerVerified: true},
	)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runTrace([]string{"cache"}, &out); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory (%d); output:\n%s", code, exitcode.Advisory, out.String())
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("one record that is not here prints one summary and one detail, got %d lines:\n%s",
			len(lines), out.String())
	}
	if !strings.Contains(lines[1], "laptop-7f3a") {
		t.Errorf("the one surviving detail must be the real one; got %q", lines[1])
	}
}

// --lane must reach trace.Filter.Lane, and a run where everything asked for was
// reachable must exit OK -- an unconditional Advisory would train callers to
// ignore the code that means "something is not here".
func TestTraceCacheLaneFlagReachesTheQuery(t *testing.T) {
	mid := thisMachine(t)
	a := localTranscript(t, "rollout-a.jsonl")
	b := localTranscript(t, "rollout-b.jsonl")
	root := seedCacheRepo(t, mid,
		record.Record{Harness: "codex", Machine: mid, Lane: "lane-a", Transcript: a, PointerVerified: true},
		record.Record{Harness: "codex", Machine: mid, Lane: "lane-b", Transcript: b, PointerVerified: true},
	)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runTrace([]string{"cache", "--lane", "lane-a"}, &out); code != exitcode.OK {
		t.Fatalf("exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
	}
	if !strings.Contains(out.String(), "copied 1") {
		t.Errorf("--lane lane-a must reach the query and leave lane-b behind; output:\n%s", out.String())
	}
}

// The default window is what an unflagged `agents trace cache` means. Left
// unbounded it drags the whole of history onto disk; too narrow and yesterday's
// session is silently not there.
func TestTraceCacheDefaultWindowIsThirtyDays(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"default reaches back 30 days but no further", []string{"cache"}, "copied 1"},
		{"a wider window reaches the older one too", []string{"cache", "--since", "60d"}, "copied 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mid := thisMachine(t)
			now := time.Now().UTC()
			recent := localTranscript(t, "rollout-recent.jsonl")
			old := localTranscript(t, "rollout-old.jsonl")
			root := seedCacheRepo(t, mid,
				record.Record{When: now.Add(-time.Hour), Harness: "codex", Machine: mid,
					Transcript: recent, PointerVerified: true},
				record.Record{When: now.Add(-40 * 24 * time.Hour), Harness: "codex", Machine: mid,
					Transcript: old, PointerVerified: true},
			)
			t.Chdir(root)

			var out bytes.Buffer
			if code := runTrace(tc.args, &out); code != exitcode.OK {
				t.Fatalf("exit = %d, want OK (%d); output:\n%s", code, exitcode.OK, out.String())
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("%v: want %q; output:\n%s", tc.args, tc.want, out.String())
			}
		})
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
		// A window that sets no cutoff must be refused, not answered with the
		// entire history as though the flag had never been given.
		{"negative since", []string{"ls", "--since", "-3d"}, true, exitcode.Malformed},
		{"overflowing since", []string{"ls", "--since", "200000d"}, true, exitcode.Malformed},
		{"unknown flag", []string{"ls", "--nope"}, true, exitcode.Malformed},
		// Outside a repo there is nothing to answer about, which is not a
		// failure: Skip is what a shell wrapper keys off.
		{"outside a repo", []string{"ls"}, false, exitcode.Skip},
		// cache takes the same two flags and owes the same answers.
		{"cache unparseable since", []string{"cache", "--since", "soon"}, true, exitcode.Malformed},
		{"cache negative since", []string{"cache", "--since", "-3d"}, true, exitcode.Malformed},
		{"cache unknown flag", []string{"cache", "--nope"}, true, exitcode.Malformed},
		{"cache outside a repo", []string{"cache"}, false, exitcode.Skip},
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
