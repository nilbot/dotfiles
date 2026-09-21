// Package safetext renders text this tool did not author into places where
// punctuation is structure.
//
// Three call sites need it: the terminal listing in `agents trace ls`, the
// markdown memory index, and the markdown handoff index. They were on their way
// to being three copies of one idea, and a copy fixed in one place and not the
// others is how an injection hole reopens.
//
// The behaviours are kept apart by medium rather than merged. A fixed-width
// terminal table separates columns with whitespace, so a control character
// breaks it and a "|" is inert; markdown is the other way round. Nothing here
// tries to be a general escaper.
//
// The two layers are the split internal/memory settled on:
//
//   - reject a control character where the value is authored, because
//     flattening it would put text in a generated file that disagrees with the
//     source it came from; and
//   - escape the punctuation of the target format where the value is rendered,
//     because rejecting a "]" in prose would be wrong.
package safetext

import (
	"strings"
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

// markdownCellEscaper escapes what would end a markdown table cell early. A "|"
// in a value opens a column that no header describes and shifts every cell after
// it, and enough of them forge a whole row.
//
// The backslash is escaped too, and first, so a value already ending in one
// cannot escape the escape: "a\" followed by "|" would otherwise render as an
// escaped backslash followed by a live "|". strings.Replacer makes a single
// left-to-right pass, so no replacement is rescanned.
var markdownCellEscaper = strings.NewReplacer(`\`, `\\`, `|`, `\|`)

// markdownLinkTextEscaper escapes the characters that let text close the link
// text early or open a second link inside it. `name: "br]ack(et"` otherwise
// renders `- [br]ack(et](brk.md)`, which is not a link to anything.
//
// "|" is in the set because a link is also the contents of a table cell in the
// handoff index, and the two escapes have to happen in one pass: escaping
// brackets and then pipes turns "]" into "\\]", whose "]" is live again.
var markdownLinkTextEscaper = strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`, `|`, `\|`)
