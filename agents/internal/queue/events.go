package queue

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Event names. Deliberately including the ones that mean "a human said no",
// because a queue that only records successes cannot tell an instruction that
// works from one that is ignored.
const (
	EventDrafted  = "drafted"
	EventPromoted = "promoted"
	EventBinned   = "binned"
)

// Event is one durable line about a draft's life.
//
// It exists because the draft files themselves are deleted at promotion and at
// binning, so counting the queue answers "what is pending", never "what
// happened". Without this the only observable is an empty queue, which is
// equally consistent with the instruction working, the instruction being
// ignored, and nothing having been worth recording -- three causes and one
// observation.
//
// What it never carries: body, subject, description. Those are content, and the
// same rule that keeps content out of trace records applies here for the same
// reason -- this is a measurement artifact, not a copy of the note. The type has
// no field capable of holding them.
type Event struct {
	When    time.Time `json:"when"`
	Event   string    `json:"event"`
	Session string    `json:"session"`
	Lane    string    `json:"lane"`
	Kind    string    `json:"kind"`
	DraftID string    `json:"draft_id"`
}

func eventsPath(storeDir string) string { return filepath.Join(storeDir, "events.jsonl") }

// AppendEvent records one event. It is best-effort by contract: a caller that
// could not measure something must still have done it.
func AppendEvent(storeDir string, e Event) error {
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return err
	}
	if e.When.IsZero() {
		e.When = time.Now().UTC()
	}
	e.When = e.When.UTC()
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(eventsPath(storeDir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// Events reads the log, oldest first. A missing log is an empty history.
func Events(storeDir string) ([]Event, error) {
	f, err := os.Open(eventsPath(storeDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			// Loud. A measurement with silently dropped rows is worse than no
			// measurement, because it still produces a number.
			return nil, fmt.Errorf("%s: unreadable event line: %w", eventsPath(storeDir), err)
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].When.Before(out[j].When) })
	return out, nil
}

// Stats is what the §3a experiment actually asks.
//
// The headline is DraftRate: of the sessions that did work, how many recorded
// anything. Everything else qualifies it -- a high draft rate with a low
// promotion rate means the instruction fires and produces material nobody
// wants, which asks for different wording rather than a stronger trigger.
type Stats struct {
	Sessions        int // sessions observed doing work, from the trace index
	SessionsDrafted int // of those, how many produced at least one draft
	Drafted         int
	Promoted        int
	Binned          int
	Pending         int
}

// DraftRate is the fraction of working sessions that recorded something. It is
// the number the experiment turns on, and its baseline is zero: twenty sessions
// under the previous instruction produced no handoffs at all.
func (s Stats) DraftRate() float64 {
	if s.Sessions == 0 {
		return 0
	}
	return float64(s.SessionsDrafted) / float64(s.Sessions)
}

// PromotionRate is the fraction of drafts a human kept. A low value means the
// instruction is producing noise, which is a wording problem and not an
// argument for a blocking gate.
func (s Stats) PromotionRate() float64 {
	decided := s.Promoted + s.Binned
	if decided == 0 {
		return 0
	}
	return float64(s.Promoted) / float64(decided)
}

// Summarize joins the event log against the sessions that did work.
//
// workingSessions comes from the trace index and is passed in rather than read
// here, because "a session that did work" is a judgement about records and this
// package has no business making it. Sessions with no draft are the whole point
// of the denominator: counting only sessions that drafted would report a draft
// rate of 100% forever.
func Summarize(storeDir string, workingSessions []string, since time.Time) (Stats, error) {
	events, err := Events(storeDir)
	if err != nil {
		return Stats{}, err
	}
	pending, err := List(storeDir)
	if err != nil {
		return Stats{}, err
	}

	var st Stats
	drafted := map[string]bool{}
	for _, e := range events {
		if !since.IsZero() && e.When.Before(since) {
			continue
		}
		switch e.Event {
		case EventDrafted:
			st.Drafted++
			drafted[e.Session] = true
		case EventPromoted:
			st.Promoted++
		case EventBinned:
			st.Binned++
		}
	}
	for _, d := range pending {
		if since.IsZero() || !d.When.Before(since) {
			st.Pending++
		}
	}
	seen := map[string]bool{}
	for _, s := range workingSessions {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		st.Sessions++
		if drafted[s] {
			st.SessionsDrafted++
		}
	}
	return st, nil
}
