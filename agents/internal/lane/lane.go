// Package lane resolves the unit of work-in-progress that handoffs and traces
// are grouped by.
//
// The branch is the strongest default because it already exists, already tracks
// the ticket, and requires nothing of the user. Everything else is a fallback
// for when there is no branch to read.
package lane

import (
	"regexp"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/repo"
)

const maxLen = 64

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify makes a string safe as a directory name and stable as a join key.
func Slugify(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > maxLen {
		s = strings.TrimRight(s[:maxLen], "-")
	}
	return s
}

// Resolve applies the precedence in spec 4: explicit flag, then branch, then
// worktree name, then "default".
func Resolve(explicit string, rc *repo.Context) string {
	candidates := []string{explicit}
	if rc != nil {
		candidates = append(candidates, rc.Branch, rc.Worktree)
	}
	for _, c := range candidates {
		if s := Slugify(c); s != "" {
			return s
		}
	}
	return "default"
}
