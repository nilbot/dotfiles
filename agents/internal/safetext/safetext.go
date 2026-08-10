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
	"fmt"
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

// CheckSingleLine rejects a control character in a frontmatter field that is
// rendered as one line of a generated index.
//
// A YAML block scalar makes such a field multi-line, and every line after the
// first lands in the index as a row -- or a "## " section -- the author never
// wrote. It is deterministic, so a pre-commit guard that regenerates the index
// and compares it waves the forgery through. Flattening the value instead would
// put text in the generated index that differs from what the entry says, which
// is the silent normalisation this project keeps getting bitten by. The author
// is told to keep the field to one line and put prose where prose belongs, in
// the body.
func CheckSingleLine(base, field, value string) error {
	if r, ok := ControlRune(value); ok {
		return fmt.Errorf("%s: %s contains a control character (%q): %s is rendered as a single line of the index -- keep it to one line and move any prose into the body", base, field, r, field)
	}
	return nil
}

// Flatten maps control characters to spaces for a fixed-width terminal table.
//
// Description is free text out of a harness payload and survives the JSON round
// trip byte for byte: a newline in it prints a second line that reads like a
// record nobody ever wrote, and a tab opens a column that shifts every row after
// it. Cwd comes from a filesystem path, which may legally hold either. The
// listing is an index, so a cell may only ever describe one record on one line.
//
// This flattens where the markdown side rejects, and the difference is not an
// inconsistency: the listing is transient output for a human reading a terminal,
// not a tracked file that a guard will regenerate and compare.
func Flatten(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
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

// MarkdownCell escapes a value for a cell of a markdown table.
func MarkdownCell(s string) string { return markdownCellEscaper.Replace(s) }

// markdownLinkTextEscaper escapes the characters that let text close the link
// text early or open a second link inside it. `name: "br]ack(et"` otherwise
// renders `- [br]ack(et](brk.md)`, which is not a link to anything.
//
// "|" is in the set because a link is also the contents of a table cell in the
// handoff index, and the two escapes have to happen in one pass: escaping
// brackets and then pipes turns "]" into "\\]", whose "]" is live again.
var markdownLinkTextEscaper = strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`, `|`, `\|`)

// MarkdownLinkText escapes a value used as the text of a markdown link.
func MarkdownLinkText(s string) string { return markdownLinkTextEscaper.Replace(s) }

// MarkdownLinkDest percent-encodes what would end a markdown link destination
// early or split it in two: a ")" closes it, a space starts the optional title,
// and a "|" ends the table cell the link sits in. The "%" itself is encoded
// first-class so the encoding stays unambiguous, and everything below "!" covers
// space and the C0 controls in one rule.
//
// "/" is deliberately left alone: a handoff destination is "<lane>/<file>.md",
// and encoding the separator would break the link it is trying to protect.
func MarkdownLinkDest(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c <= ' ', c == 0x7f, c == '%', c == '(', c == ')',
			c == '<', c == '>', c == '"', c == '\'', c == '\\', c == '#', c == '?', c == '|':
			fmt.Fprintf(&b, "%%%02X", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
