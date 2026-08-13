package queue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEventsSurviveTheDraftFileBeingDeleted(t *testing.T) {
	store := t.TempDir()
	d, err := Write(store, handoffDraft())
	if err != nil {
		t.Fatal(err)
	}
	if err := AppendEvent(store, Event{Event: EventDrafted, Session: d.Session, Lane: d.Lane, Kind: d.Kind, DraftID: d.ID}); err != nil {
		t.Fatal(err)
	}
	// Promotion and binning both delete the draft. If the only record of a
	// draft is the draft, review erases the measurement.
	if err := Remove(store, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := AppendEvent(store, Event{Event: EventPromoted, Session: d.Session, Lane: d.Lane, Kind: d.Kind, DraftID: d.ID}); err != nil {
		t.Fatal(err)
	}

	got, err := Events(store)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Events = %d, want 2 after the draft was deleted", len(got))
	}
	if got[0].Event != EventDrafted || got[1].Event != EventPromoted {
		t.Errorf("events out of order or wrong: %+v", got)
	}
}

func TestEventsCarryNoContent(t *testing.T) {
	store := t.TempDir()
	secret := "PRIVATE-body-text"
	d := handoffDraft()
	d.Body = secret + "\n"
	d.Subject = secret
	out, err := Write(store, d)
	if err != nil {
		t.Fatal(err)
	}
	if err := AppendEvent(store, Event{Event: EventDrafted, Session: out.Session, Lane: out.Lane, Kind: out.Kind, DraftID: out.ID}); err != nil {
		t.Fatal(err)
	}
	// Structural: the type has no field that could carry it, and this pins that
	// nothing routed content in through a field that looks harmless.
	b, err := os.ReadFile(filepath.Join(store, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		t.Errorf("the event log carries draft content:\n%s", b)
	}
}

func TestSummarizeCountsSessionsThatDraftedNothing(t *testing.T) {
	store := t.TempDir()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	// Two sessions worked; one drafted.
	if err := AppendEvent(store, Event{When: now, Event: EventDrafted, Session: "s1", Lane: "master", Kind: KindHandoff, DraftID: "master/a.md"}); err != nil {
		t.Fatal(err)
	}
	st, err := Summarize(store, []string{"s1", "s2"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Sessions != 2 || st.SessionsDrafted != 1 {
		t.Fatalf("stats = %+v, want 2 sessions and 1 that drafted", st)
	}
	if got := st.DraftRate(); got != 0.5 {
		t.Errorf("DraftRate = %v, want 0.5", got)
	}
	// The denominator is the whole point. Counting only sessions that drafted
	// would report 100%% forever and answer nothing.
	if st.DraftRate() == 1 {
		t.Error("a session that drafted nothing was excluded from the denominator")
	}
}

func TestSummarizeSeparatesPromotedFromBinned(t *testing.T) {
	store := t.TempDir()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	for _, e := range []Event{
		{When: now, Event: EventDrafted, Session: "s1"},
		{When: now, Event: EventDrafted, Session: "s1"},
		{When: now, Event: EventDrafted, Session: "s2"},
		{When: now, Event: EventPromoted, Session: "s1"},
		{When: now, Event: EventBinned, Session: "s1"},
		{When: now, Event: EventBinned, Session: "s2"},
	} {
		if err := AppendEvent(store, e); err != nil {
			t.Fatal(err)
		}
	}
	st, err := Summarize(store, []string{"s1", "s2", "s3"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Drafted != 3 || st.Promoted != 1 || st.Binned != 2 {
		t.Fatalf("stats = %+v", st)
	}
	// One kept of three decided. A low promotion rate means the instruction
	// produces noise -- a wording problem, not an argument for a stronger
	// trigger, which is exactly the distinction the experiment has to make.
	if got := st.PromotionRate(); got < 0.33 || got > 0.34 {
		t.Errorf("PromotionRate = %v, want ~1/3", got)
	}
}

func TestSummarizeHonoursTheWindow(t *testing.T) {
	store := t.TempDir()
	old := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	for _, e := range []Event{
		{When: old, Event: EventDrafted, Session: "s-old"},
		{When: recent, Event: EventDrafted, Session: "s-new"},
	} {
		if err := AppendEvent(store, e); err != nil {
			t.Fatal(err)
		}
	}
	st, err := Summarize(store, []string{"s-old", "s-new"}, time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if st.Drafted != 1 {
		t.Errorf("Drafted = %d inside the window, want 1", st.Drafted)
	}
	if st.SessionsDrafted != 1 {
		t.Errorf("SessionsDrafted = %d, want 1: the old session drafted outside the window", st.SessionsDrafted)
	}
}

func TestSummarizeOnAnEmptyStoreIsZeroNotAnError(t *testing.T) {
	st, err := Summarize(t.TempDir(), nil, time.Time{})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if st.Sessions != 0 || st.Drafted != 0 || st.DraftRate() != 0 {
		t.Errorf("stats = %+v, want zero", st)
	}
}

func TestUnreadableEventLineIsLoudRatherThanSkipped(t *testing.T) {
	store := t.TempDir()
	if err := os.WriteFile(filepath.Join(store, "events.jsonl"), []byte("{\"event\":\"drafted\"}\nnot-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A measurement that silently drops rows still produces a number, which is
	// worse than producing none.
	if _, err := Events(store); err == nil {
		t.Error("an unreadable event line was skipped silently")
	}
}

func TestSummarizeLaneSlicesPerScenario(t *testing.T) {
	store := t.TempDir()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	for _, e := range []Event{
		{When: now, Event: EventDrafted, Session: "s-cache", Lane: "s3-cache"},
		{When: now, Event: EventDrafted, Session: "s-auth", Lane: "s4-auth"},
	} {
		if err := AppendEvent(store, e); err != nil {
			t.Fatal(err)
		}
	}
	// Per-scenario slicing is what makes expected-versus-actual mechanical
	// instead of inferred from subject lines.
	st, err := SummarizeLane(store, []string{"s-cache", "s-auth"}, "s3-cache", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Drafted != 1 || st.SessionsDrafted != 1 {
		t.Fatalf("lane slice = %+v, want only the s3-cache draft", st)
	}
	all, err := Summarize(store, []string{"s-cache", "s-auth"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if all.Drafted != 2 {
		t.Errorf("unsliced = %+v, want both", all)
	}
}

func TestSummarizeReportsTheOldestPendingDraftNotTheNewest(t *testing.T) {
	store := t.TempDir()
	old := handoffDraft()
	old.Lane = "old-lane"
	old.When = time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	if _, err := Write(store, old); err != nil {
		t.Fatal(err)
	}
	recent := handoffDraft()
	recent.Lane = "recent-lane"
	recent.When = time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	if _, err := Write(store, recent); err != nil {
		t.Fatal(err)
	}

	st, err := Summarize(store, nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Pending != 2 {
		t.Fatalf("Pending = %d, want 2", st.Pending)
	}
	// The newest is the wrong end. A queue whose oldest item is a fortnight
	// stale reads as fresh if the age tracks whatever arrived last, which is
	// precisely the failure this number exists to catch.
	if !st.OldestPending.Equal(old.When) {
		t.Errorf("OldestPending = %v, want the older draft at %v", st.OldestPending, old.When)
	}
	now := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	if got, want := st.PendingAge(now), 14*24*time.Hour; got != want {
		t.Errorf("PendingAge = %v, want %v", got, want)
	}
}

func TestPendingAgeIsZeroWhenNothingIsPending(t *testing.T) {
	st, err := Summarize(t.TempDir(), nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	// An empty queue and a queue nobody has touched in a fortnight must not
	// render the same; zero here is what lets the caller tell them apart.
	if got := st.PendingAge(time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)); got != 0 {
		t.Errorf("PendingAge on an empty queue = %v, want 0", got)
	}
}

func TestPromotingTheOldestDraftAdvancesTheAge(t *testing.T) {
	store := t.TempDir()
	old := handoffDraft()
	old.Lane = "old-lane"
	old.When = time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	written, err := Write(store, old)
	if err != nil {
		t.Fatal(err)
	}
	recent := handoffDraft()
	recent.Lane = "recent-lane"
	recent.When = time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	if _, err := Write(store, recent); err != nil {
		t.Fatal(err)
	}
	if err := Remove(store, written.ID); err != nil {
		t.Fatal(err)
	}

	st, err := Summarize(store, nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	// Draining the backlog has to be visible, or the number reports a problem
	// that has been fixed and stops meaning anything.
	if !st.OldestPending.Equal(recent.When) {
		t.Errorf("OldestPending = %v, want %v after the oldest was decided", st.OldestPending, recent.When)
	}
}
