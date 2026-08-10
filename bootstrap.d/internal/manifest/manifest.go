// Package manifest parses links.manifest, the declarative table of paths this
// repository manages. Each row's Kind is spec 1 §8.4's placement rule made
// mechanical: a path another program writes to is seeded, never linked.
package manifest

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strings"
)

type Kind string

const (
	KindLink Kind = "link" // symlink target -> repo source; nothing else writes here
	KindSeed Kind = "seed" // copy once, never overwrite; another program writes here
	KindDir  Kind = "dir"  // a real machine-owned directory
)

type Row struct {
	Kind     Kind
	Source   string // repo-relative; "-" for dir rows
	Target   string // $HOME-relative
	Platform string // "darwin", "linux", or "*"
}

func Parse(data []byte) ([]Row, error) {
	var rows []Row
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 4 {
			return nil, fmt.Errorf("manifest line %d: %d columns, want 4", line, len(fields))
		}
		kind := Kind(fields[0])
		switch kind {
		case KindLink, KindSeed, KindDir:
		default:
			return nil, fmt.Errorf("manifest line %d: unknown kind %q", line, fields[0])
		}
		switch fields[3] {
		case "*", "darwin", "linux":
		default:
			return nil, fmt.Errorf("manifest line %d: unknown platform %q", line, fields[3])
		}
		rows = append(rows, Row{kind, fields[1], fields[2], fields[3]})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func For(rows []Row, platform string) []Row {
	var out []Row
	for _, r := range rows {
		if r.Platform == "*" || r.Platform == platform {
			out = append(out, r)
		}
	}
	return out
}

// DuplicateTargets reports targets claimed more than once. Two owners for one
// path is how softlinks.sh and the Makefile drifted apart.
func DuplicateTargets(rows []Row) []string {
	seen := map[string]int{}
	for _, r := range rows {
		seen[r.Target]++
	}
	var dupes []string
	for target, n := range seen {
		if n > 1 {
			dupes = append(dupes, target)
		}
	}
	sort.Strings(dupes)
	return dupes
}
