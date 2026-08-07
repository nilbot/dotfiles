package trace

import (
	"os"
	"path/filepath"
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
	path := filepath.Join(dir, "reports", "traces", "2026-08-10.jsonl")
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
	path := filepath.Join(dir, "reports", "traces", "2026-08-10.jsonl")
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
