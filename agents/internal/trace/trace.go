// Package trace queries the tracked pointer index.
//
// The daily JSONL files are storage; this package is the index. A generated
// index file would drift out of sync with what is on disk the moment anything
// wrote a record without regenerating it. A query cannot drift.
package trace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/record"
)

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
func ParseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && strings.HasSuffix(s, "d") {
		return time.Duration(n) * 24 * time.Hour, nil
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(s, "w")); err == nil && strings.HasSuffix(s, "w") {
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func Query(agentsDir string, f Filter, now time.Time) (Result, error) {
	paths, err := filepath.Glob(filepath.Join(agentsDir, "reports", "traces", "*.jsonl"))
	if err != nil {
		return Result{}, err
	}
	sort.Strings(paths)

	var res Result
	var cutoff time.Time
	if f.Since > 0 {
		cutoff = now.Add(-f.Since)
	}

	for _, p := range paths {
		file, err := os.Open(p)
		if err != nil {
			return res, err
		}
		sc := bufio.NewScanner(file)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var r record.Record
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				res.Skipped++
				continue
			}
			if matches(r, f, cutoff) {
				res.Records = append(res.Records, r)
			}
		}
		file.Close()
		if err := sc.Err(); err != nil {
			return res, err
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
