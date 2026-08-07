// Package pointer resolves which transcript path in a hook payload belongs to
// the thing the record is about.
//
// It derives rather than looks up. The measured invariant across both harnesses
// is that a transcript's path contains the id of what it transcribes:
//
//	Codex        agent_id=019fdcab-ac94...  ->  rollout-...-019fdcab-ac94-....jsonl
//	Claude Code  agent_id=a4e4a1bc424b2047f ->  subagents/agent-a4e4a1bc424b2047f.jsonl
//
// A per-event field map would encode today's inconsistency in a fast-moving
// vendor contract. Deriving and reporting whether the derivation held survives
// the contract moving.
package pointer

import (
	"path/filepath"
	"strings"
)

// Resolve picks the candidate belonging to key -- an agent_id for subagent
// events, a session_id otherwise -- and reports whether key was actually found
// in the path it chose.
//
// When nothing matches it returns the first usable candidate with verified
// false. Recording an unverified pointer beats dropping the record: the pointer
// is still a lead, and pointer_verified says how much to trust it.
func Resolve(candidates []string, key string) (string, bool) {
	var usable []string
	for _, c := range candidates {
		if c = strings.TrimSpace(c); c != "" {
			usable = append(usable, c)
		}
	}
	if len(usable) == 0 {
		return "", false
	}
	if key == "" {
		return usable[0], false
	}

	// A basename match is the strong signal; a match anywhere in the path is
	// the weaker one that Claude Code's session layout needs. Both verify, but
	// the strong one wins when both are present.
	var pathMatch string
	for _, c := range usable {
		if strings.Contains(filepath.Base(c), key) {
			return c, true
		}
		if pathMatch == "" && strings.Contains(c, key) {
			pathMatch = c
		}
	}
	if pathMatch != "" {
		return pathMatch, true
	}
	return usable[0], false
}
