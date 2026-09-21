// Package safetext rejects text this tool did not author from places where
// punctuation is structure.
//
// One call site needs it: the guard, which refuses a path carrying a control
// character before that path reaches a commit-message block or a terminal line.
// Callers phrase their own refusal, because what a control character does
// depends on where the value lands -- that is why this package exposes the
// primitive rather than a message.
//
// This package used to be larger. It also escaped markdown table cells and link
// text for the memory and handoff indexes, and both of those indexes were
// deleted with the record feature they rendered, so the escapers went with them
// rather than staying as the two copies of one idea the package was written to
// prevent. Nothing here tries to be a general escaper.
package safetext

import (
	"unicode"
)

// ControlRune returns the first control character in s, and whether there was
// one. It is the primitive under the rejection layer; callers phrase their own
// refusal, because what a control character does depends on where the value
// lands.
func ControlRune(s string) (rune, bool) {
	for _, r := range s {
		if unicode.IsControl(r) { // \n, \r and \t among them
			return r, true
		}
	}
	return 0, false
}
