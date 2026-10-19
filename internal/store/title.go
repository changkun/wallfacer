package store

import (
	"slices"
	"strings"
	"unicode"
)

// TitleMaxRunes is the longest title DeriveTitle returns, in runes, counting
// the ellipsis it appends when it shortens a line.
const TitleMaxRunes = 60

// DeriveTitle returns the title a new task or chat thread takes from its
// text: the first line that is not blank once trimmed and stripped of a
// leading Markdown heading marker, with runs of whitespace collapsed, cut at a
// word boundary to at most TitleMaxRunes runes. It returns "" when the text
// has no such line.
func DeriveTitle(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.Join(strings.Fields(stripHeadingMarker(strings.TrimSpace(line))), " ")
		if line != "" {
			return shortenTitle(line)
		}
	}
	return ""
}

// stripHeadingMarker removes a leading ATX heading marker (one to six '#'
// followed by whitespace or the end of the line) from a trimmed line. A line
// such as "#tag" or "#######" is not a heading and is returned unchanged.
func stripHeadingMarker(line string) string {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return line
	}
	rest := line[n:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return line
	}
	return strings.TrimSpace(rest)
}

// shortenTitle returns line unchanged when it fits in TitleMaxRunes runes.
// Otherwise it keeps the words that fit before an appended ellipsis; a first
// word longer than that is cut mid-word.
func shortenTitle(line string) string {
	runes := []rune(line)
	if len(runes) <= TitleMaxRunes {
		return line
	}
	keep := runes[:TitleMaxRunes-1]
	if !unicode.IsSpace(runes[TitleMaxRunes-1]) {
		if i := lastSpace(keep); i > 0 {
			keep = keep[:i]
		}
	}
	return strings.TrimRightFunc(string(keep), func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	}) + "…"
}

// lastSpace returns the index of the last whitespace rune in rs, or -1.
func lastSpace(rs []rune) int {
	for i, r := range slices.Backward(rs) {
		if unicode.IsSpace(r) {
			return i
		}
	}
	return -1
}
