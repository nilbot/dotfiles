package record

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The whole redaction argument in spec 3.2 rests on this: the type has no field
// capable of holding message content, so no writer can emit it by mistake. This
// asserts the structure, not the output of one code path.
func TestRecordCannotCarryForbiddenFields(t *testing.T) {
	rt := reflect.TypeOf(Record{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		for _, bad := range ForbiddenFields {
			if strings.EqualFold(tag, bad) || strings.EqualFold(f.Name, bad) {
				t.Errorf("Record has field %s (json:%q) matching forbidden %q", f.Name, tag, bad)
			}
		}
	}
}

func TestLineIsOneJSONObjectPerLine(t *testing.T) {
	r := Record{
		When:            time.Date(2026, 8, 7, 15, 41, 14, 987654321, time.UTC),
		Harness:         "codex",
		Machine:         "m1-mbp-a7f3",
		Event:           "subagent_stop",
		Lane:            "sq-123-payments",
		Cwd:             "payments/api",
		SessionID:       "019fdcab-9733-72e3-ba7c-d2e0cc7fb334",
		AgentID:         "019fdcab-ac94-7502-a322-d01f047c274a",
		AgentType:       "default",
		Transcript:      "/Users/nilbot/.codex/sessions/2026/08/07/rollout-x.jsonl",
		PointerVerified: true,
	}

	line, err := r.Line()
	if err != nil {
		t.Fatalf("Line() error: %v", err)
	}
	if strings.Count(string(line), "\n") != 1 || !strings.HasSuffix(string(line), "\n") {
		t.Fatalf("Line() must be exactly one trailing newline, got %q", line)
	}

	var back map[string]any
	if err := json.Unmarshal(line, &back); err != nil {
		t.Fatalf("Line() is not valid JSON: %v", err)
	}
	if back["when"] != "2026-08-07T15:41:14Z" {
		t.Fatalf("when = %v, want RFC3339 UTC", back["when"])
	}
	if back["pointer_verified"] != true {
		t.Fatalf("pointer_verified = %v, want true", back["pointer_verified"])
	}
}

func TestAppendWritesDatePartitionedJSONL(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	when := time.Date(2026, 8, 7, 23, 59, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if err := w.Append(Record{When: when, Harness: "claude-code", Machine: "m", Event: "stop"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	path := filepath.Join(dir, "traces", "2026-08-07.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s: %v", path, err)
	}
	if got := strings.Count(string(b), "\n"); got != 2 {
		t.Fatalf("got %d lines, want 2", got)
	}
}

// Partitioning is by UTC date, not local date, so records from two machines in
// different timezones land in the same file and merge=union works as intended.
func TestAppendPartitionsByUTC(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	// 2026-08-07T17:30 in UTC-8 is 2026-08-08T01:30Z; local date differs from UTC date.
	when := time.Date(2026, 8, 7, 17, 30, 0, 0, time.FixedZone("UTC-8", -8*3600))
	if err := w.Append(Record{When: when, Harness: "codex", Machine: "m", Event: "stop"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "traces", "2026-08-08.jsonl")); err != nil {
		t.Fatalf("expected UTC-dated file: %v", err)
	}
}

func TestWriterAppendsUnderTheStoreNotTheTrackedTree(t *testing.T) {
	store := t.TempDir()
	rec := Record{
		When:    time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC),
		Harness: "claude-code",
		Machine: "m",
		Event:   "stop",
		Lane:    "master",
	}
	if err := NewWriter(store).Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, "traces", "2026-08-12.jsonl")); err != nil {
		t.Fatalf("record not written under the store: %v", err)
	}
	// The tracked layout must not be recreated by a write. A reports/ tree
	// appearing under the store would mean the old path survived a rename
	// rather than a relocation.
	if _, err := os.Stat(filepath.Join(store, "reports")); !os.IsNotExist(err) {
		t.Error("the writer recreated a reports/ tree under the store")
	}
}
