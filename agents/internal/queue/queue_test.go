package queue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func handoffDraft() Draft {
	return Draft{
		Kind:    KindHandoff,
		Lane:    "master",
		Session: "s1",
		When:    time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC),
		Subject: "why the retry window is 90s",
		Body:    "- measured against the documented 30s\n",
	}
}

func memoryDraft() Draft {
	d := handoffDraft()
	d.Kind = KindMemory
	d.Name = "retry-window"
	d.Description = "why the retry window is 90s and not the documented 30s"
	d.Type = "reference"
	return d
}

func TestWriteThenGetRoundTripsEveryField(t *testing.T) {
	store := t.TempDir()
	in := memoryDraft()
	out, err := Write(store, in)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if out.ID == "" {
		t.Fatal("Write returned no ID; nothing could name this draft at review")
	}
	got, err := Get(store, out.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	for _, c := range []struct{ field, want, got string }{
		{"kind", in.Kind, got.Kind},
		{"lane", in.Lane, got.Lane},
		{"session", in.Session, got.Session},
		{"subject", in.Subject, got.Subject},
		{"name", in.Name, got.Name},
		{"description", in.Description, got.Description},
		{"type", in.Type, got.Type},
		{"body", in.Body, got.Body},
	} {
		if c.want != c.got {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
	if !got.When.Equal(in.When) {
		t.Errorf("when = %v, want %v", got.When, in.When)
	}
}

func TestValidateRefusesAMemoryDraftMissingItsFrontmatter(t *testing.T) {
	full := memoryDraft()
	if err := Validate(full); err != nil {
		t.Fatalf("a complete memory draft was refused: %v", err)
	}
	for _, missing := range []string{"name", "description", "type"} {
		d := memoryDraft()
		switch missing {
		case "name":
			d.Name = ""
		case "description":
			d.Description = ""
		case "type":
			d.Type = ""
		}
		if err := Validate(d); err == nil {
			t.Errorf("a memory draft with no %s was accepted; promotion would have to guess it", missing)
		}
	}
	// A handoff needs none of them.
	if err := Validate(handoffDraft()); err != nil {
		t.Errorf("a handoff draft was refused: %v", err)
	}
}

func TestWriteRefusesWhatValidateRejects(t *testing.T) {
	store := t.TempDir()
	bad := memoryDraft()
	bad.Name = ""
	if _, err := Write(store, bad); err == nil {
		t.Fatal("Write accepted a draft Validate rejects; the contract has to hold at the write, not only at promotion")
	}
	if entries, err := os.ReadDir(filepath.Join(store, "queue")); err == nil && len(entries) > 0 {
		t.Error("a refused draft still created something on disk")
	}
}

func TestWriteRefusesPathEscapingComponents(t *testing.T) {
	store := t.TempDir()
	for _, c := range []struct {
		name   string
		mutate func(*Draft)
	}{
		{"lane traversal", func(d *Draft) { d.Lane = ".." }},
		{"lane separator", func(d *Draft) { d.Lane = "a/b" }},
		{"session traversal", func(d *Draft) { d.Session = ".." }},
		{"session dotfile", func(d *Draft) { d.Session = ".git" }},
		{"newline in lane", func(d *Draft) { d.Lane = "a\nb" }},
	} {
		d := handoffDraft()
		c.mutate(&d)
		if _, err := Write(store, d); err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
}

func TestTwoDraftsFromOneSessionOnOneDayDoNotCollide(t *testing.T) {
	store := t.TempDir()
	first, err := Write(store, handoffDraft())
	if err != nil {
		t.Fatal(err)
	}
	second := handoffDraft()
	second.Body = "- a second conclusion\n"
	got, err := Write(store, second)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == first.ID {
		t.Fatalf("both drafts got id %q; a session can reach more than one conclusion in a day", got.ID)
	}
	all, err := List(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("List returned %d drafts, want 2", len(all))
	}
}

func TestListIsOldestFirstAcrossLanes(t *testing.T) {
	store := t.TempDir()
	older := handoffDraft()
	older.Lane = "beta"
	older.When = time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	newer := handoffDraft()
	newer.Lane = "alpha"
	newer.When = time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	for _, d := range []Draft{newer, older} {
		if _, err := Write(store, d); err != nil {
			t.Fatal(err)
		}
	}
	got, err := List(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Lane != "beta" {
		t.Fatalf("List = %v, want the older beta draft first", []string{got[0].Lane, got[1].Lane})
	}
}

func TestRemoveLeavesNothingBehind(t *testing.T) {
	store := t.TempDir()
	d, err := Write(store, handoffDraft())
	if err != nil {
		t.Fatal(err)
	}
	if err := Remove(store, d.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := Get(store, d.ID); err == nil {
		t.Error("the removed draft is still readable")
	}
	all, err := List(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("List still returns %d draft(s)", len(all))
	}
}

func TestListOnAnAbsentQueueIsEmptyNotAnError(t *testing.T) {
	got, err := List(filepath.Join(t.TempDir(), "never-created"))
	if err != nil {
		t.Fatalf("List on an absent queue: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List = %d drafts, want 0", len(got))
	}
}

func TestGetRefusesAnIDThatEscapesTheQueue(t *testing.T) {
	store := t.TempDir()
	// A readable file has to exist at the other end of the traversal, or the
	// test passes because nothing was there rather than because the path was
	// refused -- which is the same test with none of the meaning. Written as a
	// real draft so a successful escape would parse and return content.
	outside := filepath.Join(store, "outside.md")
	secret := "PRIVATE-outside-the-queue"
	body := "---\nkind: handoff\nlane: master\nsession: s1\nwhen: 2026-08-12T09:00:00Z\n---\n\n" + secret + "\n"
	if err := os.WriteFile(outside, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(store, handoffDraft()); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{
		"../outside.md",
		"master/../../outside.md",
		"no-slash",
		"master/../outside.md",
	} {
		got, err := Get(store, id)
		if err == nil {
			t.Errorf("Get(%q) was accepted and returned %q", id, got.Body)
			continue
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(got.Body, secret) {
			t.Errorf("Get(%q) leaked content from outside the queue", id)
		}
	}
}

func TestRemoveRefusesAnIDThatEscapesTheQueue(t *testing.T) {
	store := t.TempDir()
	outside := filepath.Join(store, "outside.md")
	if err := os.WriteFile(outside, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(store, "master/../../outside.md"); err == nil {
		t.Error("Remove followed a traversal out of the queue")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("Remove deleted a file outside the queue")
	}
}

// A body whose text happens to contain a frontmatter delimiter must not be able
// to truncate the draft or forge fields on the way back in.
func TestBodyContainingADelimiterSurvivesTheRoundTrip(t *testing.T) {
	store := t.TempDir()
	d := handoffDraft()
	d.Body = "- one\n---\nkind: memory\n---\n- two\n"
	out, err := Write(store, d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Get(store, out.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindHandoff {
		t.Errorf("kind = %q; a body forged the frontmatter", got.Kind)
	}
	if !strings.Contains(got.Body, "- two") {
		t.Errorf("body was truncated at a delimiter: %q", got.Body)
	}
}
