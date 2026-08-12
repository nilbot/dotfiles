package trace

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/record"
)

func seed(t *testing.T) (string, time.Time) {
	t.Helper()
	dir := t.TempDir()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	w := record.NewWriter(dir)

	recs := []record.Record{
		{When: now.Add(-1 * time.Hour), Harness: "codex", Machine: "m1", Event: "subagent_stop",
			Lane: "sq-123-payments", Cwd: "payments/api", AgentID: "a1",
			Transcript: "/t/a1.jsonl", PointerVerified: true},
		{When: now.Add(-2 * time.Hour), Harness: "claude-code", Machine: "m1", Event: "subagent_stop",
			Lane: "sq-123-payments", Cwd: "payments/web", AgentID: "a2",
			Description: "trace the retry window", Transcript: "/t/a2.jsonl", PointerVerified: true},
		{When: now.Add(-72 * time.Hour), Harness: "codex", Machine: "m2", Event: "stop",
			Lane: "other-lane", Cwd: ".", Transcript: "/t/a3.jsonl"},
	}
	for _, r := range recs {
		if err := w.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	return dir, now
}

// seedRecords writes an arbitrary fixture, for the boundaries the shared seed
// cannot express without changing every count it feeds.
func seedRecords(t *testing.T, recs ...record.Record) string {
	t.Helper()
	dir := t.TempDir()
	w := record.NewWriter(dir)
	for _, r := range recs {
		if err := w.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// writeTraceFile puts an exact byte sequence in one daily file. record.Writer
// can only append well-formed lines, and damage is the point of some fixtures.
func writeTraceFile(t *testing.T, dir, day, body string) string {
	t.Helper()
	tdir := filepath.Join(dir, "traces")
	if err := os.MkdirAll(tdir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tdir, day+".jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func jsonLine(t *testing.T, r record.Record) string {
	t.Helper()
	b, err := r.Line()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestQueryNewestFirst(t *testing.T) {
	dir, now := seed(t)
	res, err := Query(dir, Filter{}, now)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Records) != 3 {
		t.Fatalf("got %d records, want 3", len(res.Records))
	}
	if res.Records[0].AgentID != "a1" {
		t.Errorf("first = %q, want the newest (a1)", res.Records[0].AgentID)
	}
}

func TestQueryFilters(t *testing.T) {
	dir, now := seed(t)
	cases := []struct {
		name string
		f    Filter
		want int
	}{
		{"lane", Filter{Lane: "sq-123-payments"}, 2},
		{"module prefix", Filter{Module: "payments"}, 2},
		{"module exact", Filter{Module: "payments/api"}, 1},
		{"machine", Filter{Machine: "m2"}, 1},
		{"harness", Filter{Harness: "codex"}, 2},
		{"event", Filter{Event: "stop"}, 1},
		{"since 3h", Filter{Since: 3 * time.Hour}, 2},
		{"grep description", Filter{Grep: "retry window"}, 1},
		{"limit", Filter{Limit: 1}, 1},
		{"combined", Filter{Lane: "sq-123-payments", Harness: "codex"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Query(dir, tc.f, now)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(res.Records) != tc.want {
				t.Fatalf("got %d, want %d: %+v", len(res.Records), tc.want, res.Records)
			}
		})
	}
}

// --module names a path, so its boundary is the separator. A bare
// strings.HasPrefix makes "payments" swallow the unrelated sibling
// "payments-legacy", and the shared fixture has no sibling to notice it with.
func TestQueryModuleStopsAtThePathBoundary(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	dir := seedRecords(t,
		record.Record{When: now, Cwd: "payments/api", AgentID: "inside"},
		record.Record{When: now.Add(-time.Minute), Cwd: "payments-legacy", AgentID: "sibling"},
		record.Record{When: now.Add(-2 * time.Minute), Cwd: "payments", AgentID: "self"},
	)
	res, err := Query(dir, Filter{Module: "payments"}, now)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	var got []string
	for _, r := range res.Records {
		got = append(got, r.AgentID)
	}
	// The module itself and everything under it; nothing that merely shares a
	// name prefix.
	want := []string{"inside", "self"}
	if len(got) != len(want) {
		t.Fatalf("matched %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("matched %v, want %v", got, want)
		}
	}
}

// Blank lines are not damage. Counting them as unreadable turns every
// hand-edited or union-merged file into a false alarm.
func TestQueryDoesNotCountBlankLines(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	dir := seedRecords(t, record.Record{When: now, AgentID: "a1"})
	path := filepath.Join(dir, "traces", "2026-08-10.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n   \n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	res, err := Query(dir, Filter{}, now)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0: blank lines are not unreadable", res.Skipped)
	}
	if len(res.Records) != 1 {
		t.Errorf("got %d records, want 1", len(res.Records))
	}
}

// Limit is "the newest N", so it has to be taken after the sort. Applied
// before, it keeps whichever records happened to be read first and still
// returns the right count.
func TestQueryLimitKeepsTheNewest(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	// Older first, and in an older daily file, so file order is the wrong order.
	dir := seedRecords(t,
		record.Record{When: now.Add(-48 * time.Hour), AgentID: "old"},
		record.Record{When: now.Add(-time.Hour), AgentID: "new"},
	)
	res, err := Query(dir, Filter{Limit: 1}, now)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Records) != 1 {
		t.Fatalf("got %d records, want 1", len(res.Records))
	}
	if res.Records[0].AgentID != "new" {
		t.Errorf("kept %q, want the newest (new)", res.Records[0].AgentID)
	}
}

// Grep is documented as case-insensitive across description and agent type.
// The shared fixture greps a lowercase description and so proves neither.
func TestQueryGrepSpansAgentTypeAndIgnoresCase(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	dir := seedRecords(t,
		record.Record{When: now, AgentType: "Explore", AgentID: "typed"},
		record.Record{When: now.Add(-time.Minute), Description: "Retry Window", AgentID: "described"},
		record.Record{When: now.Add(-2 * time.Minute), AgentID: "neither"},
	)
	for _, tc := range []struct{ grep, want string }{
		{"explore", "typed"},   // agent_type, lowered needle
		{"EXPLORE", "typed"},   // agent_type, raised needle
		{"retry", "described"}, // description, lowered needle
	} {
		res, err := Query(dir, Filter{Grep: tc.grep}, now)
		if err != nil {
			t.Fatalf("Query(%q): %v", tc.grep, err)
		}
		if len(res.Records) != 1 || res.Records[0].AgentID != tc.want {
			t.Errorf("grep %q matched %+v, want exactly %q", tc.grep, res.Records, tc.want)
		}
	}
}

// A merge that went wrong leaves conflict markers, which are not valid JSON.
// Dropping them silently is how a reader lies about coverage; count them.
func TestQueryCountsUnreadableLines(t *testing.T) {
	dir, now := seed(t)
	path := filepath.Join(dir, "traces", "2026-08-10.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("<<<<<<< HEAD\n")
	f.Close()

	res, err := Query(dir, Filter{}, now)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", res.Skipped)
	}
}

// A merge=union attribute that goes missing does not leave one marker at the
// end of a file. It leaves a three-line conflict block in the middle of one,
// with a record on each side of the divider and more records below it. Both
// halves of the requirement live here: all three marker lines are counted, and
// every record around the damage is still returned. A reader that stops at the
// first line it cannot parse answers with a shorter history wearing the face of
// a complete one, which is the exact failure the count exists to expose.
func TestQueryReadsPastAConflictBlockAndCountsEveryMarker(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	writeTraceFile(t, dir, "2026-08-10",
		jsonLine(t, record.Record{When: now.Add(-1 * time.Minute), AgentID: "before"})+
			"<<<<<<< HEAD\n"+
			jsonLine(t, record.Record{When: now.Add(-2 * time.Minute), AgentID: "ours"})+
			"=======\n"+
			jsonLine(t, record.Record{When: now.Add(-3 * time.Minute), AgentID: "theirs"})+
			">>>>>>> feature/other\n"+
			jsonLine(t, record.Record{When: now.Add(-4 * time.Minute), AgentID: "after"}))

	res, err := Query(dir, Filter{}, now)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Skipped != 3 {
		t.Errorf("Skipped = %d, want 3: <<<<<<<, ======= and >>>>>>> are three unreadable lines", res.Skipped)
	}
	var got []string
	for _, r := range res.Records {
		got = append(got, r.AgentID)
	}
	// Newest first, and the block is not a wall: what sits above it, inside it
	// and below it all belong to the history.
	want := []string{"before", "ours", "theirs", "after"}
	if len(got) != len(want) {
		t.Fatalf("returned %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("returned %v, want %v", got, want)
		}
	}
}

// An over-long line stops the scanner, and with it every daily file that sorts
// after the damaged one -- so failing quietly here would hide whole days.
// Failing loudly is right; failing anonymously is not, because "token too long"
// alone names nothing to go and repair.
func TestQueryNamesTheFileAndLineItCouldNotRead(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	overlong := strings.Repeat("x", 2*1024*1024)
	writeTraceFile(t, dir, "2026-08-09",
		jsonLine(t, record.Record{When: now.Add(-24 * time.Hour), AgentID: "day-before"})+overlong+"\n")
	// A newer file the scanner never reaches: the loss is not one line.
	writeTraceFile(t, dir, "2026-08-10", jsonLine(t, record.Record{When: now, AgentID: "newer"}))

	_, err := Query(dir, Filter{}, now)
	if err == nil {
		t.Fatal("a file that cannot be read through must be an error, not a shorter history")
	}
	if !strings.Contains(err.Error(), "2026-08-09.jsonl:2") {
		t.Errorf("error must name the file and line to repair, got: %v", err)
	}
}

func TestQueryFailsLoudlyOnAnUnopenableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens a 0000 file regardless of the mode bits")
	}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	unreadable := writeTraceFile(t, dir, "2026-08-09",
		jsonLine(t, record.Record{When: now.Add(-24 * time.Hour), AgentID: "day-before"}))
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	writeTraceFile(t, dir, "2026-08-10", jsonLine(t, record.Record{When: now, AgentID: "newer"}))

	// Skipping the file would answer with only the newer day and no sign that
	// a day is missing -- an index that quietly shrinks.
	_, err := Query(dir, Filter{}, now)
	if err == nil {
		t.Fatal("a daily file that will not open must be an error, not a shorter history")
	}
	if !strings.Contains(err.Error(), "2026-08-09.jsonl") {
		t.Errorf("error must name the file it could not open, got: %v", err)
	}
}

// Following a matched leaf lets a repository make the observer consume bytes
// from anywhere the user can read. The record in the target is deliberately
// valid: an unsafe os.Open implementation returns it instead of failing.
func TestQueryRejectsAnExistingTraceSymlinkWithoutConsumingItsTarget(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	traceDir := filepath.Join(dir, "traces")
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	private := "PRIVATE-external-trace-sentinel"
	target := filepath.Join(t.TempDir(), "outside.jsonl")
	if err := os.WriteFile(target, []byte(jsonLine(t, record.Record{When: now, AgentID: private})), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(traceDir, "2026-08-10.jsonl")); err != nil {
		t.Fatal(err)
	}

	res, err := Query(dir, Filter{}, now)
	if err == nil || len(res.Records) != 0 {
		t.Fatalf("Query followed an external trace leaf: records=%+v err=%v", res.Records, err)
	}
	if strings.Contains(err.Error(), private) {
		t.Fatalf("trace leaf failure exposed target content: %v", err)
	}
}

func TestQueryRejectsARedirectedTraceDirectoryWithoutConsumingExternalRecords(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	storeDir := t.TempDir()
	external := t.TempDir()
	// One hop, not two: the index moved to <store>/traces, so "reports" is no
	// longer a directory Query walks through and cannot be redirected.
	if err := os.Symlink(external, filepath.Join(storeDir, "traces")); err != nil {
		t.Fatal(err)
	}
	private := "PRIVATE-redirected-trace-directory"
	if err := os.WriteFile(filepath.Join(external, "2026-08-10.jsonl"), []byte(jsonLine(t, record.Record{When: now, AgentID: private})), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := Query(storeDir, Filter{}, now)
	if err == nil || len(res.Records) != 0 {
		t.Fatalf("Query followed a redirected traces directory: records=%+v err=%v", res.Records, err)
	}
	if strings.Contains(err.Error(), private) {
		t.Fatalf("redirected directory failure exposed record content: %v", err)
	}
}

// A plain os.Open blocks on a FIFO before it can learn that the leaf is not a
// trace file. The bounded writer only releases that buggy implementation; the
// required implementation returns before the release point with an error.
func TestQueryRejectsATraceFIFOPromptly(t *testing.T) {
	dir := t.TempDir()
	traceDir := filepath.Join(dir, "traces")
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(traceDir, "2026-08-10.jsonl")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}

	const releaseAfter = 300 * time.Millisecond
	released := make(chan struct{})
	stop := make(chan struct{})
	go func() {
		defer close(released)
		select {
		case <-time.After(releaseAfter):
			f, _ := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0)
			if f != nil {
				_ = f.Close()
			}
		case <-stop:
		}
	}()
	start := time.Now()
	_, err := Query(dir, Filter{}, time.Now())
	elapsed := time.Since(start)
	close(stop)
	<-released

	if elapsed >= releaseAfter/2 {
		t.Fatalf("Query blocked on a trace FIFO for %v", elapsed)
	}
	if err == nil {
		t.Fatal("Query accepted a trace FIFO")
	}
}

func TestQueryOnEmptyRepo(t *testing.T) {
	res, err := Query(t.TempDir(), Filter{}, time.Now())
	if err != nil {
		t.Fatalf("an uninitialized repo is not an error: %v", err)
	}
	if len(res.Records) != 0 {
		t.Fatalf("got %d records", len(res.Records))
	}
}

func TestParseSince(t *testing.T) {
	cases := map[string]time.Duration{
		"3d":  72 * time.Hour,
		"12h": 12 * time.Hour,
		"90m": 90 * time.Minute,
		"2w":  14 * 24 * time.Hour,
		"":    0,
	}
	for in, want := range cases {
		got, err := ParseSince(in)
		if err != nil {
			t.Fatalf("ParseSince(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("ParseSince(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := ParseSince("soon"); err == nil {
		t.Error("want an error for unparseable input")
	}
}

// A window that is not positive is not a window. Every input here parses into
// a duration Query reads as "no time bound" -- a negative one directly, an
// overflowed one after it wraps -- so accepting any of them answers a request
// to narrow the history with the whole of it, at exit 0. The refusal has to
// happen here, because by the time Query sees the duration it cannot tell the
// difference between "no --since" and "--since that meant nothing".
func TestParseSinceRejectsWindowsThatWouldSilentlyMeanNoWindow(t *testing.T) {
	for _, in := range []string{"-3d", "-2w", "-1h", "0h", "0", "200000d", "20000w"} {
		got, err := ParseSince(in)
		if err == nil {
			t.Errorf("ParseSince(%q) = %v, want an error: that duration sets no cutoff and returns the full history", in, got)
		}
	}
}

func TestMigrateTrackedIndexIsIdempotentAndLossless(t *testing.T) {
	agentsDir, store := t.TempDir(), t.TempDir()
	src := filepath.Join(agentsDir, "reports", "traces")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := `{"when":"2026-08-10T00:00:00Z","event":"stop"}` + "\n"
	if err := os.WriteFile(filepath.Join(src, "2026-08-10.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := MigrateTrackedIndex(agentsDir, store); err != nil {
			t.Fatalf("MigrateTrackedIndex run %d: %v", i, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(store, "traces", "2026-08-10.jsonl"))
	if err != nil {
		t.Fatalf("migrated file missing: %v", err)
	}
	// Running twice must not double the history. A migration that can only be
	// run once is one nobody can re-run after a merge brings more records in.
	if string(got) != lines {
		t.Errorf("content = %q, want %q", got, lines)
	}
}

func TestMigrateTrackedIndexMergesWithRecordsTheStoreAlreadyHas(t *testing.T) {
	agentsDir, store := t.TempDir(), t.TempDir()
	src := filepath.Join(agentsDir, "reports", "traces")
	dst := filepath.Join(store, "traces")
	for _, d := range []string{src, dst} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	shared := `{"when":"2026-08-10T00:00:00Z","event":"stop"}`
	trackedOnly := `{"when":"2026-08-10T01:00:00Z","event":"session-start"}`
	storeOnly := `{"when":"2026-08-10T02:00:00Z","event":"subagent-stop"}`
	if err := os.WriteFile(filepath.Join(src, "2026-08-10.jsonl"), []byte(shared+"\n"+trackedOnly+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "2026-08-10.jsonl"), []byte(shared+"\n"+storeOnly+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateTrackedIndex(agentsDir, store); err != nil {
		t.Fatalf("MigrateTrackedIndex: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "2026-08-10.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	for _, want := range []string{shared, trackedOnly, storeOnly} {
		if !strings.Contains(body, want) {
			t.Errorf("migration lost a record: %s", want)
		}
	}
	if strings.Count(body, shared) != 1 {
		t.Errorf("the shared record was duplicated:\n%s", body)
	}
}

func TestMigrateTrackedIndexOnAnAbsentSourceIsNotAnError(t *testing.T) {
	n, err := MigrateTrackedIndex(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("MigrateTrackedIndex: %v", err)
	}
	if n != 0 {
		t.Errorf("moved = %d, want 0", n)
	}
}
