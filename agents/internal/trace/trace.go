// Package trace queries the machine-local pointer index.
//
// The daily JSONL files are storage; this package is the index. A generated
// index file would drift out of sync with what is on disk the moment anything
// wrote a record without regenerating it. A query cannot drift.
package trace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/record"
	"github.com/nilbot/dotfiles/agents/internal/safeio"
)

// maxDuration is the widest window a time.Duration can hold. A request wider
// than this wraps to a negative duration, which reads downstream as "no window
// at all" -- the caller asked to narrow and would get everything back.
const maxDuration = time.Duration(1<<63 - 1)

type Filter struct {
	Lane    string        // exact
	Module  string        // path prefix on the repo-relative cwd
	Machine string        // exact
	Harness string        // exact
	Event   string        // exact
	Grep    string        // case-insensitive substring of description or agent_type
	Since   time.Duration // zero means no time bound
	Limit   int           // zero means no limit
}

type Result struct {
	Records []record.Record
	// Skipped counts lines that were not valid JSON. Conflict markers from a
	// merge that lost its merge=union attribute look exactly like this, and
	// reporting the count is how that gets noticed instead of silently
	// shrinking the history.
	Skipped int
}

// ParseSince accepts d/w on top of Go's own duration units, because "3d" is
// what a person types and time.ParseDuration does not know about days.
//
// Only a positive window is a window. A negative, zero or overflowed one puts
// the cutoff at or after now, and Query treats any such duration as no bound at
// all -- so a caller who asked to narrow the history would be answered with the
// whole of it and no sign that the flag was ignored. Refusing the input is the
// only answer that is not a lie.
func ParseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && strings.HasSuffix(s, "d") {
		return scaleWindow(s, n, 24*time.Hour)
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(s, "w")); err == nil && strings.HasSuffix(s, "w") {
		return scaleWindow(s, n, 7*24*time.Hour)
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("%q is not a positive time window", s)
	}
	return d, nil
}

// scaleWindow multiplies a whole number of units, refusing both ways the
// product stops describing a window: non-positive, and too wide to hold.
func scaleWindow(s string, n int, unit time.Duration) (time.Duration, error) {
	if n <= 0 {
		return 0, fmt.Errorf("%q is not a positive time window", s)
	}
	if int64(n) > int64(maxDuration/unit) {
		return 0, fmt.Errorf("%q is wider than the maximum window of %v", s, maxDuration)
	}
	return time.Duration(n) * unit, nil
}

// Query reads the index out of the machine-local store, resolved by
// repo.StoreDir. An absent store is an empty history, not an error: a
// repository that has never recorded anything is a normal state.
func Query(storeDir string, f Filter, now time.Time) (Result, error) {
	storeRoot, err := os.OpenRoot(storeDir)
	if os.IsNotExist(err) {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	defer storeRoot.Close()
	tracesRoot, err := safeio.OpenDirAt(storeRoot, "traces")
	if os.IsNotExist(err) {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	defer tracesRoot.Close()
	entries, err := fs.ReadDir(tracesRoot.FS(), ".")
	if err != nil {
		return Result{}, err
	}

	var res Result
	var cutoff time.Time
	if f.Since > 0 {
		cutoff = now.Add(-f.Since)
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		p := filepath.Join(storeDir, "traces", name)
		file, _, err := safeio.OpenRegularAt(tracesRoot, name)
		if err != nil {
			// Loud, never skipped. A daily file we cannot open is history we
			// cannot see, and continuing past it would answer with a smaller
			// history wearing the same face as a complete one. os.Open's error
			// already names the file it failed on; keep it that way.
			return res, err
		}
		sc := bufio.NewScanner(file)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lines := 0
		for sc.Scan() {
			lines++
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var r record.Record
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				// A conflict block is several bad lines in the middle of a
				// file with good records on both sides of it. Count each one
				// and keep reading: stopping here would silently drop every
				// record below the damage.
				res.Skipped++
				continue
			}
			if matches(r, f, cutoff) {
				res.Records = append(res.Records, r)
			}
		}
		_ = file.Close()
		if err := sc.Err(); err != nil {
			// The scanner stops at the first line it cannot hold, and every
			// later daily file then goes unread -- so the bare "token too
			// long" points at nothing. Name the file and the line, because
			// that is what someone has to go and repair.
			return res, fmt.Errorf("%s:%d: %w", p, lines+1, err)
		}
	}

	// Newest first: the question is almost always "what happened recently".
	sort.SliceStable(res.Records, func(i, j int) bool {
		return res.Records[i].When.After(res.Records[j].When)
	})
	if f.Limit > 0 && len(res.Records) > f.Limit {
		res.Records = res.Records[:f.Limit]
	}
	return res, nil
}

func matches(r record.Record, f Filter, cutoff time.Time) bool {
	if f.Lane != "" && r.Lane != f.Lane {
		return false
	}
	if f.Machine != "" && r.Machine != f.Machine {
		return false
	}
	if f.Harness != "" && r.Harness != f.Harness {
		return false
	}
	if f.Event != "" && r.Event != f.Event {
		return false
	}
	if !cutoff.IsZero() && r.When.Before(cutoff) {
		return false
	}
	if f.Module != "" && r.Cwd != f.Module && !strings.HasPrefix(r.Cwd, f.Module+"/") {
		return false
	}
	if f.Grep != "" {
		// Mechanical filters collapse the candidate set; choosing among the
		// survivors is semantic, and this is the honest limit of what the
		// record can offer. Codex does not populate description at all.
		hay := strings.ToLower(r.Description + " " + r.AgentType)
		if !strings.Contains(hay, strings.ToLower(f.Grep)) {
			return false
		}
	}
	return true
}
