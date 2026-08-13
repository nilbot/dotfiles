package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/queue"
	"github.com/nilbot/dotfiles/agents/internal/record"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

// reviewRepo is a repository with .agents/ scaffolded and one commit, so a
// promotion has somewhere to commit onto.
func reviewRepo(t *testing.T) string {
	t.Helper()
	root := newRepo(t)
	if err := scaffold.Create(root, false); err != nil {
		t.Fatalf("scaffold.Create: %v", err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-m", "seed")
	return root
}

func queueDraft(t *testing.T, root string, d queue.Draft) queue.Draft {
	t.Helper()
	store, err := repo.StoreDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if d.When.IsZero() {
		d.When = time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	}
	out, err := queue.Write(store, d)
	if err != nil {
		t.Fatalf("queue.Write: %v", err)
	}
	return out
}

func runReviewIn(t *testing.T, root string, args []string) (int, string) {
	t.Helper()
	t.Chdir(root)
	var out bytes.Buffer
	code := runReview(args, &out)
	return code, out.String()
}

func TestKeepPromotesAHandoffAndCommitsScoped(t *testing.T) {
	root := reviewRepo(t)
	d := queueDraft(t, root, queue.Draft{
		Kind: queue.KindHandoff, Lane: "master", Session: "s1",
		Subject: "retry window", Body: "- the retry window is 90s\n",
	})
	// A dirty code path must survive untouched: promotion is scoped to .agents/.
	if err := os.WriteFile(filepath.Join(root, "code.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := runReviewIn(t, root, []string{"--keep", d.ID})
	if code != exitcode.OK {
		t.Fatalf("exit = %d, want OK; output:\n%s", code, out)
	}

	store, _ := repo.StoreDir(root)
	if _, err := queue.Get(store, d.ID); err == nil {
		t.Error("the promoted draft is still in the queue")
	}
	files := git(t, root, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(files, ".agents/reports/handoff/") {
		t.Errorf("the handoff was not committed:\n%s", files)
	}
	if strings.Contains(files, "code.go") {
		t.Errorf("promotion swept an unrelated code path into the commit:\n%s", files)
	}
	if !strings.Contains(git(t, root, "log", "-1", "--format=%s"), "retry window") {
		t.Error("the draft subject did not become the commit subject")
	}
	// Reviewed provenance: a human chose this, which is what the index legend
	// says the word means.
	body, err := os.ReadFile(filepath.Join(root, ".agents", "reports", "handoff", "master",
		"2026-08-12-s1.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "status: reviewed") {
		t.Errorf("promoted handoff is not marked reviewed:\n%s", body)
	}
}

func TestKeepPromotesAMemoryEntryAndRegeneratesTheIndex(t *testing.T) {
	root := reviewRepo(t)
	d := queueDraft(t, root, queue.Draft{
		Kind: queue.KindMemory, Lane: "master", Session: "s1",
		Name: "retry-window", Description: "why the retry window is 90s", Type: "reference",
		Body: "- measured\n",
	})
	code, out := runReviewIn(t, root, []string{"--keep", d.ID})
	if code != exitcode.OK {
		t.Fatalf("exit = %d, want OK; output:\n%s", code, out)
	}
	entry, err := os.ReadFile(filepath.Join(root, ".agents", "memory", "retry-window.md"))
	if err != nil {
		t.Fatalf("promoted memory entry missing: %v", err)
	}
	for _, want := range []string{"name: retry-window", "type: reference", "- measured"} {
		if !strings.Contains(string(entry), want) {
			t.Errorf("entry missing %q:\n%s", want, entry)
		}
	}
	index, err := os.ReadFile(filepath.Join(root, ".agents", "memory", "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "retry-window") {
		t.Errorf("INDEX.md was not regenerated in the same operation:\n%s", index)
	}
	// Both files in one commit, or the pre-commit guard blocks on a mismatch
	// this command created.
	files := git(t, root, "show", "--name-only", "--format=", "HEAD")
	for _, want := range []string{"memory/retry-window.md", "memory/INDEX.md"} {
		if !strings.Contains(files, want) {
			t.Errorf("%s was not in the promotion commit:\n%s", want, files)
		}
	}
}

func TestKeepRefusesAnInvalidMemoryDraftAndLeavesTheTreeClean(t *testing.T) {
	root := reviewRepo(t)
	store, _ := repo.StoreDir(root)
	// Written past queue.Write so an invalid draft can exist at all -- the
	// point is that promotion validates rather than trusting the queue.
	dir := filepath.Join(store, "queue", "master")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := "---\nkind: memory\nlane: master\nsession: s1\nwhen: 2026-08-12T09:00:00Z\n---\n\n- no name, no description, no type\n"
	if err := os.WriteFile(filepath.Join(dir, "2026-08-12-s1-1.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	id := "master/2026-08-12-s1-1.md"

	before := git(t, root, "rev-parse", "HEAD")
	code, out := runReviewIn(t, root, []string{"--keep", id})
	if code != exitcode.Malformed {
		t.Fatalf("exit = %d, want Malformed; output:\n%s", code, out)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".agents", "memory"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.EqualFold(e.Name(), "INDEX.md") && e.Name() != ".gitkeep" {
			t.Fatalf("an invalid draft reached the tracked tree as %s", e.Name())
		}
	}
	if git(t, root, "rev-parse", "HEAD") != before {
		t.Error("a refused promotion made a commit")
	}
	if _, err := queue.Get(store, id); err != nil {
		t.Error("a refused promotion consumed the draft")
	}
}

func TestKeepWarnsWhenPromotingMemoryOffTheDefaultBranch(t *testing.T) {
	root := reviewRepo(t)
	git(t, root, "checkout", "-q", "-b", "feature")
	d := queueDraft(t, root, queue.Draft{
		Kind: queue.KindMemory, Lane: "feature", Session: "s1",
		Name: "retry-window", Description: "why 90s", Type: "reference",
		Body: "- measured\n",
	})
	code, out := runReviewIn(t, root, []string{"--keep", d.ID})
	if code != exitcode.OK {
		t.Fatalf("exit = %d, want OK: promotion warns and proceeds; output:\n%s", code, out)
	}
	if !strings.Contains(out, "feature") {
		t.Errorf("the warning did not name the branch:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "memory", "retry-window.md")); err != nil {
		t.Error("the warning became a refusal; it must proceed")
	}
}

func TestKeepDoesNotWarnOnTheDefaultBranch(t *testing.T) {
	root := reviewRepo(t)
	// The fixture deliberately sits on a ticket-shaped branch, which is the
	// warning case. Move to the default branch to test the other side.
	git(t, root, "checkout", "-q", "-b", "master")
	d := queueDraft(t, root, queue.Draft{
		Kind: queue.KindMemory, Lane: "master", Session: "s1",
		Name: "retry-window", Description: "why 90s", Type: "reference",
		Body: "- measured\n",
	})
	_, out := runReviewIn(t, root, []string{"--keep", d.ID})
	if strings.Contains(out, "invisible to every other lane") {
		t.Errorf("warned on the default branch:\n%s", out)
	}
}

func TestKeepRefusesToOverwriteAnExistingMemoryEntry(t *testing.T) {
	root := reviewRepo(t)
	existing := filepath.Join(root, ".agents", "memory", "retry-window.md")
	if err := os.WriteFile(existing, []byte("---\nname: retry-window\ndescription: curated by hand\nmetadata:\n  type: reference\n---\n\ncurated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := queueDraft(t, root, queue.Draft{
		Kind: queue.KindMemory, Lane: "master", Session: "s1",
		Name: "retry-window", Description: "from a draft", Type: "reference",
		Body: "- drafted\n",
	})
	code, out := runReviewIn(t, root, []string{"--keep", d.ID})
	if code == exitcode.OK {
		t.Fatalf("promotion overwrote curated knowledge; output:\n%s", out)
	}
	b, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "curated by hand") {
		t.Errorf("the existing entry was overwritten:\n%s", b)
	}
}

// The absence of bulk promotion is a design constraint, not an oversight: an
// agent able to promote in bulk closes the review loop with no human in it.
func TestThereIsNoBulkPromotion(t *testing.T) {
	root := reviewRepo(t)
	queueDraft(t, root, queue.Draft{
		Kind: queue.KindHandoff, Lane: "master", Session: "s1", Body: "- x\n",
	})
	for _, args := range [][]string{
		{"--keep", "--all"},
		{"--keep-all"},
		{"--keep", "*"},
	} {
		code, _ := runReviewIn(t, root, args)
		if code == exitcode.OK {
			t.Errorf("%v was accepted", args)
		}
	}
	files := git(t, root, "show", "--name-only", "--format=", "HEAD")
	if strings.Contains(files, "reports/handoff/master") {
		t.Error("something promoted a draft without an explicit id")
	}
}

func TestBinRemovesTheDraftWithoutTouchingTheTree(t *testing.T) {
	root := reviewRepo(t)
	d := queueDraft(t, root, queue.Draft{
		Kind: queue.KindHandoff, Lane: "master", Session: "s1", Body: "- x\n",
	})
	before := git(t, root, "rev-parse", "HEAD")
	code, out := runReviewIn(t, root, []string{"--bin", d.ID})
	if code != exitcode.OK {
		t.Fatalf("exit = %d; output:\n%s", code, out)
	}
	store, _ := repo.StoreDir(root)
	if _, err := queue.Get(store, d.ID); err == nil {
		t.Error("the binned draft survived")
	}
	if git(t, root, "rev-parse", "HEAD") != before {
		t.Error("binning made a commit")
	}
}

func TestListReportsPendingDraftsAsAdvisory(t *testing.T) {
	root := reviewRepo(t)
	code, out := runReviewIn(t, root, nil)
	if code != exitcode.OK || !strings.Contains(out, "no drafts pending") {
		t.Fatalf("empty queue: exit = %d, output:\n%s", code, out)
	}
	d := queueDraft(t, root, queue.Draft{
		Kind: queue.KindHandoff, Lane: "master", Session: "s1",
		Subject: "the retry window", Body: "- x\n",
	})
	code, out = runReviewIn(t, root, nil)
	if code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: a script has to tell pending from empty", code)
	}
	for _, want := range []string{d.ID, "the retry window"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q:\n%s", want, out)
		}
	}
}

func TestShowPrintsTheDraftBody(t *testing.T) {
	root := reviewRepo(t)
	d := queueDraft(t, root, queue.Draft{
		Kind: queue.KindHandoff, Lane: "master", Session: "s1",
		Body: "- the retry window is 90s\n",
	})
	code, out := runReviewIn(t, root, []string{"--show", d.ID})
	if code != exitcode.OK {
		t.Fatalf("exit = %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, "the retry window is 90s") {
		t.Errorf("--show did not print the body:\n%s", out)
	}
}

func TestReviewRefusesTwoActionsAtOnce(t *testing.T) {
	root := reviewRepo(t)
	d := queueDraft(t, root, queue.Draft{
		Kind: queue.KindHandoff, Lane: "master", Session: "s1", Body: "- x\n",
	})
	if code, _ := runReviewIn(t, root, []string{"--keep", d.ID, "--bin", d.ID}); code != exitcode.Malformed {
		t.Errorf("exit = %d, want Malformed", code)
	}
	store, _ := repo.StoreDir(root)
	if _, err := queue.Get(store, d.ID); err != nil {
		t.Error("an ambiguous invocation consumed the draft")
	}
}

// The experiment's denominator is sessions that did work, not sessions that
// drafted. Getting that wrong reports 100% forever and measures nothing.
func TestStatsCountsSessionsThatDraftedNothing(t *testing.T) {
	root := reviewRepo(t)
	store, _ := repo.StoreDir(root)
	now := time.Now().UTC()
	w := record.NewWriter(store)
	for _, id := range []string{"worked-and-drafted", "worked-silently"} {
		if err := w.Append(record.Record{
			When: now, Harness: "claude-code", Machine: "m1", Event: "stop",
			Lane: "master", SessionID: id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := queue.AppendEvent(store, queue.Event{
		When: now, Event: queue.EventDrafted, Session: "worked-and-drafted",
		Lane: "master", Kind: queue.KindHandoff, DraftID: "master/a.md",
	}); err != nil {
		t.Fatal(err)
	}

	code, out := runReviewIn(t, root, []string{"--stats"})
	if !strings.Contains(out, "sessions that did work") {
		t.Fatalf("no stats printed:\n%s", out)
	}
	if !strings.Contains(out, "50%") {
		t.Errorf("draft rate is not 1 of 2; the silent session was dropped from the denominator:\n%s", out)
	}
	if code != exitcode.OK && code != exitcode.Advisory {
		t.Errorf("exit = %d", code)
	}
}

// Capture was given a trigger that measurably works; review was not, so it
// still runs on the muscle memory `agents save` died of. This number is the
// leading indicator of that failure, and it has to arrive with the advice
// attached -- a bare age reads as trivia.
func TestStatsWarnsWhenTheQueueHasBeenSittingUnreviewed(t *testing.T) {
	root := reviewRepo(t)
	store, _ := repo.StoreDir(root)
	now := time.Now().UTC()
	if err := record.NewWriter(store).Append(record.Record{
		When: now, Harness: "claude-code", Machine: "m1", Event: "stop",
		Lane: "master", SessionID: "s1",
	}); err != nil {
		t.Fatal(err)
	}
	d := queue.Draft{
		Kind: queue.KindHandoff, Lane: "master", Session: "s1",
		When: now.Add(-20 * 24 * time.Hour), Subject: "old", Body: "- stale\n",
	}
	queueDraft(t, root, d)

	_, out := runReviewIn(t, root, []string{"--stats"})
	if !strings.Contains(out, "20 days") {
		t.Errorf("the age of the oldest pending draft is not reported:\n%s", out)
	}
	if !strings.Contains(out, "re-clone") {
		t.Errorf("the age is reported without saying why it matters:\n%s", out)
	}
}

// The counterweight: a draft written minutes ago is not a backlog. Reporting
// "0 hours" every run is how a reader learns to skip the line that matters.
func TestStatsSaysNothingAboutTheAgeOfAFreshQueue(t *testing.T) {
	root := reviewRepo(t)
	store, _ := repo.StoreDir(root)
	now := time.Now().UTC()
	if err := record.NewWriter(store).Append(record.Record{
		When: now, Harness: "claude-code", Machine: "m1", Event: "stop",
		Lane: "master", SessionID: "s1",
	}); err != nil {
		t.Fatal(err)
	}
	queueDraft(t, root, queue.Draft{
		Kind: queue.KindHandoff, Lane: "master", Session: "s1",
		When: now, Subject: "fresh", Body: "- fresh\n",
	})

	_, out := runReviewIn(t, root, []string{"--stats"})
	if strings.Contains(out, "oldest pending") {
		t.Errorf("a queue drafted seconds ago was reported as a backlog:\n%s", out)
	}
	if strings.Contains(out, "re-clone") {
		t.Errorf("the stale-queue advice fired on a fresh queue:\n%s", out)
	}
}

func TestStatsReportsNothingDraftedAsTheResultThatJustifiesEscalation(t *testing.T) {
	root := reviewRepo(t)
	store, _ := repo.StoreDir(root)
	if err := record.NewWriter(store).Append(record.Record{
		When: time.Now().UTC(), Harness: "claude-code", Machine: "m1",
		Event: "stop", Lane: "master", SessionID: "silent",
	}); err != nil {
		t.Fatal(err)
	}
	code, out := runReviewIn(t, root, []string{"--stats"})
	if code != exitcode.Advisory {
		t.Errorf("exit = %d, want Advisory", code)
	}
	if !strings.Contains(out, "nothing drafted") {
		t.Errorf("the empty result was not named:\n%s", out)
	}
	// It must point at revising the wording before escalating, or the first
	// bad week buys a subsystem.
	if !strings.Contains(out, "revise") {
		t.Errorf("stats did not say to revise the wording before escalating:\n%s", out)
	}
}

func TestStatsSeparatesDiscardedFromKept(t *testing.T) {
	root := reviewRepo(t)
	store, _ := repo.StoreDir(root)
	now := time.Now().UTC()
	if err := record.NewWriter(store).Append(record.Record{
		When: now, Harness: "claude-code", Machine: "m1", Event: "stop",
		Lane: "master", SessionID: "s1",
	}); err != nil {
		t.Fatal(err)
	}
	for _, e := range []queue.Event{
		{When: now, Event: queue.EventDrafted, Session: "s1"},
		{When: now, Event: queue.EventDrafted, Session: "s1"},
		{When: now, Event: queue.EventDrafted, Session: "s1"},
		{When: now, Event: queue.EventBinned, Session: "s1"},
		{When: now, Event: queue.EventBinned, Session: "s1"},
		{When: now, Event: queue.EventBinned, Session: "s1"},
	} {
		if err := queue.AppendEvent(store, e); err != nil {
			t.Fatal(err)
		}
	}
	_, out := runReviewIn(t, root, []string{"--stats"})
	if !strings.Contains(out, "wording problem") {
		t.Errorf("drafting-but-discarding was not distinguished from not drafting:\n%s", out)
	}
}

func TestDraftAndReviewRecordTheirEvents(t *testing.T) {
	root := reviewRepo(t)
	store, _ := repo.StoreDir(root)
	t.Chdir(root)

	var out bytes.Buffer
	if code := runHandoffDraft([]string{"--session", "s1", "--lane", "master", "--subject", "x"},
		strings.NewReader("- a conclusion\n"), &out); code != exitcode.OK {
		t.Fatalf("draft exit = %d: %s", code, out.String())
	}
	id := strings.TrimSpace(strings.SplitN(out.String(), "\n", 2)[0])

	if code, o := runReviewIn(t, root, []string{"--keep", id}); code != exitcode.OK {
		t.Fatalf("keep exit = %d: %s", code, o)
	}

	events, err := queue.Events(store)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Event)
	}
	// Both halves, or the measurement cannot tell a draft that was kept from
	// one that was never written.
	if len(events) != 2 || kinds[0] != queue.EventDrafted || kinds[1] != queue.EventPromoted {
		t.Fatalf("events = %v, want drafted then promoted", kinds)
	}
	if events[0].Session != "s1" {
		t.Errorf("event lost the session: %+v", events[0])
	}
}

// An unreviewed draft is not evidence the instruction produces anything worth
// having. A readout that treats drafting as the result is marking its own
// homework.
func TestStatsDoesNotClaimSuccessBeforeAnythingIsReviewed(t *testing.T) {
	root := reviewRepo(t)
	store, _ := repo.StoreDir(root)
	now := time.Now().UTC()
	if err := record.NewWriter(store).Append(record.Record{
		When: now, Harness: "claude-code", Machine: "m1", Event: "stop",
		Lane: "master", SessionID: "s1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := queue.AppendEvent(store, queue.Event{
		When: now, Event: queue.EventDrafted, Session: "s1",
		Lane: "master", Kind: queue.KindHandoff, DraftID: "master/a.md",
	}); err != nil {
		t.Fatal(err)
	}

	code, out := runReviewIn(t, root, []string{"--stats"})
	if strings.Contains(out, "not justified") {
		t.Errorf("stats declared success with nothing reviewed:\n%s", out)
	}
	if !strings.Contains(out, "nothing is settled") {
		t.Errorf("stats did not say the result is undecided:\n%s", out)
	}
	if code != exitcode.Advisory {
		t.Errorf("exit = %d, want Advisory while undecided", code)
	}
}
