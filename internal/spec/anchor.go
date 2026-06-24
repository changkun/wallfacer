package spec

// Spec-comment anchoring. A comment thread pins to a line or section of a spec
// body that then changes in git. These helpers compute the anchor and, on each
// spec load, recompute it against the current body so a thread lands on the same
// line for everyone without a server-side authority on position.
//
// The contract lives in
// specs/cloud/latere-integration/coordination-plane/spec-comments.md
// ("Anchoring across spec edits"). Two properties are load-bearing:
//
//   - Portability: normalizeLine + LineHash are FROZEN. The coordinator stores
//     the hash but never computes it (it holds no source); the client and the
//     future git-export path both compute it here, so they must agree on every
//     byte. Changing normalization invalidates every stored hash, so the
//     fixture test in anchor_test.go pins the exact output.
//   - Prefer-orphan over mis-attach: a thread that cannot be reattached above
//     the thresholds is reported as not-ok (the caller marks it orphaned) rather
//     than guessed onto a wrong line.
//
// These are pure functions over the spec body (the post-frontmatter markdown,
// spec.Spec.Body); no I/O, no dependency on coordinator/handler/client packages.

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"golang.org/x/text/unicode/norm"

	"latere.ai/x/wallfacer/internal/speccomment"
)

// Reposition thresholds, kept together so tuning is a constant change, not a
// redesign (see the spec's "Threshold (decided, tunable)" section). The policy
// is prefer-orphan: a high bar to move a pin.
const (
	// ambiguousThreshold is the minimum context similarity for Step 2
	// (exact-hash but multiple matching lines): pick a candidate only if it
	// clears this bar.
	ambiguousThreshold = 0.6
	// ambiguousMargin is how far the best Step 2 candidate must beat the
	// runner-up by. The spec mandates a "clear margin" but leaves the number
	// open, so it is frozen here alongside the thresholds. Without a margin two
	// near-identical contexts would let a coin-flip move the pin.
	ambiguousMargin = 0.15
	// fuzzyThreshold is the minimum context similarity for Step 3 (no exact
	// hash match, search within the section). Deliberately conservative.
	fuzzyThreshold = 0.8
)

// contextWindow is the number of lines captured (and compared) on each side of
// the anchored line, per the spec's "up to 3 normalized lines".
const contextWindow = 3

// normalizeLine applies the frozen normalization the spec mandates before any
// hashing or comparison, so the coordinator and the future git-export path
// compute identical hashes:
//
//  1. strip trailing whitespace,
//  2. collapse runs of internal whitespace to a single space,
//  3. apply Unicode NFC.
//
// Leading whitespace is INTENTIONALLY preserved: indentation is content for
// markdown (a nested list item differs from a top-level one), so collapsing it
// would make distinct lines collide on the same hash. Only runs *between*
// non-space tokens are collapsed. This ordering and behavior are frozen; the
// fixture test guards them.
func normalizeLine(s string) string {
	s = strings.TrimRight(s, " \t\v\f\r\n")

	// Collapse internal whitespace runs to a single space while keeping the
	// leading indentation verbatim. We walk the trimmed string, copying the
	// leading run of spaces/tabs as-is, then collapsing every later whitespace
	// run to one ASCII space.
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		b.WriteByte(s[i])
		i++
	}
	inRun := false
	for ; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\v' || c == '\f' || c == '\r' {
			inRun = true
			continue
		}
		if inRun {
			b.WriteByte(' ')
			inRun = false
		}
		b.WriteByte(c)
	}

	return norm.NFC.String(b.String())
}

// LineHash returns the sha256 hex of the already-normalized line (or the joined
// normalized range for a multi-line anchor). It is exported because it is the
// portability property other code and the future git-export path depend on:
// they must produce the same hash for the same normalized input. Callers pass
// the output of normalizeLine (or normalizeRange); LineHash does not normalize,
// so the normalization step is never accidentally skipped on one path.
func LineHash(normalizedLine string) string {
	sum := sha256.Sum256([]byte(normalizedLine))
	return hex.EncodeToString(sum[:])
}

// bodyLines splits a spec body into lines without a trailing empty element for a
// final newline, so line numbers match what a human sees in an editor.
func bodyLines(body string) []string {
	if body == "" {
		return nil
	}
	lines := strings.Split(body, "\n")
	// A body ending in "\n" yields a trailing "" from Split; drop it so it is
	// not counted as a real (empty) last line.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// normalizeRange joins lines [start,end] (1-based, inclusive) after normalizing
// each, with "\n" as the frozen separator. Used for both the LineHash input and
// the ExactText is taken raw separately.
func normalizeRange(lines []string, start, end int) string {
	var parts []string
	for i := start; i <= end; i++ {
		if i >= 1 && i <= len(lines) {
			parts = append(parts, normalizeLine(lines[i-1]))
		}
	}
	return strings.Join(parts, "\n")
}

// rawRange joins the raw (un-normalized) lines [start,end] (1-based, inclusive)
// with "\n". This is the ExactText snapshot for triage display; it is NOT
// normalized, because triage shows the author exactly what they selected.
func rawRange(lines []string, start, end int) string {
	var parts []string
	for i := start; i <= end; i++ {
		if i >= 1 && i <= len(lines) {
			parts = append(parts, lines[i-1])
		}
	}
	return strings.Join(parts, "\n")
}

// heading is an ATX heading parsed from the body: its level (1..6), text, and
// 1-based line number.
type heading struct {
	level int
	text  string
	line  int
}

// parseHeadings extracts ATX headings (# .. ######) from the body, skipping
// lines inside fenced code blocks. Fences matter: spec bodies embed code blocks
// containing literal '#' lines (struct field comments, shell snippets), and a
// naive "starts with #" scan would inject phantom headings into the section
// path. The same parser is used by ComputeAnchor (build the trail) and
// Reposition (locate the section) so the two never disagree.
func parseHeadings(lines []string) []heading {
	var out []heading
	inFence := false
	var fence string // the fence marker that opened the current block
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		// Toggle fenced-code state on ``` or ~~~ fences. Closing requires the
		// same marker family, matching common markdown behavior closely enough
		// for spec bodies.
		if marker := fenceMarker(trimmed); marker != "" {
			switch {
			case !inFence:
				inFence = true
				fence = marker
			case strings.HasPrefix(trimmed, fence):
				inFence = false
				fence = ""
			}
			continue
		}
		if inFence {
			continue
		}
		if level, text, ok := parseATX(raw); ok {
			out = append(out, heading{level: level, text: text, line: i + 1})
		}
	}
	return out
}

// fenceMarker returns "```" or "~~~" if the trimmed line opens/closes a code
// fence, else "". Only the marker family matters; trailing info strings (e.g.
// "```go") are ignored.
func fenceMarker(trimmed string) string {
	switch {
	case strings.HasPrefix(trimmed, "```"):
		return "```"
	case strings.HasPrefix(trimmed, "~~~"):
		return "~~~"
	default:
		return ""
	}
}

// parseATX parses a single ATX heading line. It requires a space after the run
// of hashes (CommonMark), so "#nothashtag" is not a heading.
func parseATX(raw string) (level int, text string, ok bool) {
	s := strings.TrimLeft(raw, " ")
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0, "", false
	}
	if n >= len(s) || (s[n] != ' ' && s[n] != '\t') {
		return 0, "", false
	}
	text = strings.TrimSpace(s[n:])
	// Trim an optional closing run of hashes ("## Heading ##").
	text = strings.TrimRight(text, "#")
	text = strings.TrimSpace(text)
	return n, text, true
}

// sectionPathAt returns the heading trail (outermost first) that contains the
// given 1-based line. The H1 document title is excluded: it is redundant with
// the thread's spec_path, and the trail locates a line *within* the document
// (the spec's example gives ["Anchoring", "Anchor fields"] for a line three
// headings deep, dropping the H1). So the trail starts at H2.
func sectionPathAt(headings []heading, line int) []string {
	// Track the current heading at each level; a heading resets all deeper
	// levels (a new H2 ends the previous H2's H3 children).
	var current [7]string // index by level 1..6
	for _, h := range headings {
		if h.line > line {
			break
		}
		current[h.level] = h.text
		for l := h.level + 1; l <= 6; l++ {
			current[l] = ""
		}
	}
	var trail []string
	for l := 2; l <= 6; l++ { // start at H2: the H1 doc title is excluded
		if current[l] != "" {
			trail = append(trail, current[l])
		}
	}
	return trail
}

// ComputeAnchor builds the full anchor for a selected line range [startLine,
// endLine] (1-based, inclusive) against the spec body. CommitSHA/BlobSHA are
// left empty: they are advisory git metadata filled elsewhere (the client, when
// the body has a committed blob). A comment made against uncommitted working
// tree text still anchors entirely on content.
func ComputeAnchor(body string, startLine, endLine int) speccomment.Anchor {
	lines := bodyLines(body)
	if startLine < 1 {
		startLine = 1
	}
	if endLine < startLine {
		endLine = startLine
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}

	headings := parseHeadings(lines)

	prefixStart := startLine - contextWindow
	if prefixStart < 1 {
		prefixStart = 1
	}
	suffixEnd := endLine + contextWindow
	if suffixEnd > len(lines) {
		suffixEnd = len(lines)
	}

	return speccomment.Anchor{
		SectionPath: sectionPathAt(headings, startLine),
		LineHash:    LineHash(normalizeRange(lines, startLine, endLine)),
		Prefix:      normalizeRange(lines, prefixStart, startLine-1),
		Suffix:      normalizeRange(lines, endLine+1, suffixEnd),
		ExactText:   rawRange(lines, startLine, endLine),
		LineHint:    startLine,
	}
}

// Reposition recomputes an anchor against the current body using the portable
// content-hash algorithm. It returns the refreshed anchor, the 1-based line it
// reattached to, and ok=false when nothing clears the thresholds (the caller
// marks the thread orphaned). Prefix/Suffix/ExactText are intentionally left as
// the stored snapshot when reattaching by exact hash (Steps 1-2); they are the
// author's original context and triage display. LineHash is refreshed in Step 3
// because the line content itself changed.
//
// Future: the git diff fast-path (Anchor.CommitSHA) is out of scope here. When
// the commit is reachable in the local clone, mapping the anchored line through
// `git diff <commit>..HEAD` is an exact reposition that beats fuzzy matching;
// it will be added ahead of these content-hash steps as an optional accelerator,
// never a requirement (the content hash stays the portable primary).
func Reposition(body string, a speccomment.Anchor) (speccomment.Anchor, int, bool) {
	lines := bodyLines(body)
	if len(lines) == 0 {
		return a, 0, false
	}
	headings := parseHeadings(lines)

	// Find every line whose normalized hash matches the anchor's LineHash.
	// (Single-line match: a multi-line anchor's range hash will simply not
	// match any single line and fall through to the fuzzy path, which is the
	// safe behavior — we never partially reattach a range.)
	var matches []int
	for i := range lines {
		if LineHash(normalizeLine(lines[i])) == a.LineHash {
			matches = append(matches, i+1)
		}
	}

	switch {
	case len(matches) == 1:
		// Step 1: exact, unique. Reattach, refresh the advisory line hint.
		line := matches[0]
		a.LineHint = line
		return a, line, true

	case len(matches) > 1:
		// Step 2: exact, ambiguous. Score each candidate by context similarity
		// and pick it only if it clears the threshold AND beats the runner-up
		// by a clear margin; else fall through to Step 3.
		best := -1
		var bestScore, secondScore float64
		for _, line := range matches {
			score := contextSimilarity(a, lines, line)
			switch {
			case score > bestScore:
				secondScore = bestScore
				best, bestScore = line, score
			case score > secondScore:
				secondScore = score
			}
		}
		if best > 0 && bestScore >= ambiguousThreshold && bestScore-secondScore >= ambiguousMargin {
			a.LineHint = best
			return a, best, true
		}
		// Fall through to fuzzy-within-section.
	}

	// Step 3: fuzzy within section. Locate the section by SectionPath (tolerant
	// of a renamed leaf heading by matching the trail prefix), then score lines
	// within it by context similarity. Reattach only above fuzzyThreshold,
	// updating LineHash to the new line.
	lo, hi := sectionRange(headings, a.SectionPath, len(lines))
	best := -1
	var bestScore float64
	for line := lo; line <= hi; line++ {
		score := contextSimilarity(a, lines, line)
		if score > bestScore {
			best, bestScore = line, score
		}
	}
	if best > 0 && bestScore >= fuzzyThreshold {
		a.LineHash = LineHash(normalizeLine(lines[best-1]))
		a.LineHint = best
		return a, best, true
	}

	// Step 4: orphan. Nothing cleared a threshold.
	return a, 0, false
}

// sectionRange returns the inclusive 1-based line span [lo,hi] of the section
// named by the heading trail. It matches the deepest heading whose own trail
// equals the requested trail; if the leaf heading was renamed, it falls back to
// matching the trail prefix (all but the last element), so a renamed leaf does
// not lose its section. When no heading matches, it returns the whole body so
// the fuzzy search still has somewhere to look.
func sectionRange(headings []heading, trail []string, lineCount int) (lo, hi int) {
	if len(trail) == 0 || len(headings) == 0 {
		return 1, lineCount
	}

	idx := matchHeading(headings, trail)
	if idx < 0 && len(trail) > 1 {
		// Tolerate a renamed leaf heading: match the parent trail instead.
		idx = matchHeading(headings, trail[:len(trail)-1])
	}
	if idx < 0 {
		return 1, lineCount
	}

	start := headings[idx]
	lo = start.line
	hi = lineCount
	// The section ends at the next heading of the same or shallower level.
	for j := idx + 1; j < len(headings); j++ {
		if headings[j].level <= start.level {
			hi = headings[j].line - 1
			break
		}
	}
	return lo, hi
}

// matchHeading returns the index of the heading whose section path (its own
// trail) equals trail, or -1. Comparing full trails (not just the leaf text)
// disambiguates two sections that share a leaf name under different parents.
func matchHeading(headings []heading, trail []string) int {
	for i, h := range headings {
		got := sectionPathAt(headings, h.line)
		if equalTrail(got, trail) {
			return i
		}
	}
	return -1
}

func equalTrail(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		// Compare on heading slugs so trivial formatting differences (case,
		// surrounding whitespace) do not break the match; the spec calls for a
		// "heading slug match".
		if slugify(a[i]) != slugify(b[i]) {
			return false
		}
	}
	return true
}

// slugify lowercases and reduces a heading to alphanumerics separated by single
// hyphens, the conventional GitHub-style anchor slug. Used only for trail
// matching tolerance, never for hashing.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevHyphen := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// contextSimilarity scores how well a candidate line in the current body
// matches the anchor's stored Prefix/Suffix context. It is the combined
// (averaged) token-level Jaccard similarity of the prefix window and the suffix
// window, computed on frozen-normalized lines so cloud and the future
// git-export path score identically.
func contextSimilarity(a speccomment.Anchor, lines []string, line int) float64 {
	prefixStart := line - contextWindow
	if prefixStart < 1 {
		prefixStart = 1
	}
	suffixEnd := line + contextWindow
	if suffixEnd > len(lines) {
		suffixEnd = len(lines)
	}
	curPrefix := normalizeRange(lines, prefixStart, line-1)
	curSuffix := normalizeRange(lines, line+1, suffixEnd)

	return (jaccard(a.Prefix, curPrefix) + jaccard(a.Suffix, curSuffix)) / 2
}

// jaccard is the token-level Jaccard similarity of two strings: |intersection|
// / |union| over their whitespace-split token sets. Edge: two empty inputs
// score 1.0 (a top-of-file anchor with no prefix legitimately matches another
// no-prefix position), one-empty-one-not scores 0.0 (otherwise an empty window
// would silently drag the average down and orphan valid anchors).
func jaccard(a, b string) float64 {
	at, bt := tokenSet(a), tokenSet(b)
	if len(at) == 0 && len(bt) == 0 {
		return 1.0
	}
	if len(at) == 0 || len(bt) == 0 {
		return 0.0
	}
	inter := 0
	for tok := range at {
		if _, ok := bt[tok]; ok {
			inter++
		}
	}
	union := len(at) + len(bt) - inter
	return float64(inter) / float64(union)
}

func tokenSet(s string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, tok := range strings.Fields(s) {
		set[tok] = struct{}{}
	}
	return set
}
