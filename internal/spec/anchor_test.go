package spec

import (
	"reflect"
	"strings"
	"testing"
)

// TestNormalizeLine checks the three frozen normalization steps and the
// deliberate preservation of leading indentation (content, not noise).
func TestNormalizeLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"trailing whitespace stripped", "hello world   ", "hello world"},
		{"internal runs collapsed", "a    b\t\tc", "a b c"},
		{"leading indentation preserved", "    - nested item", "    - nested item"},
		{"tabs in indent preserved", "\t- tabbed", "\t- tabbed"},
		{"heading hashes kept", "##   Heading   text  ", "## Heading text"},
		{"bullet kept", "- a   bullet", "- a bullet"},
		{"mixed indent then collapse", "  word1    word2  ", "  word1 word2"},
		{"empty", "", ""},
		{"only whitespace", "   \t ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeLine(tt.in); got != tt.want {
				t.Errorf("normalizeLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestLineHashFixture is the shared-fixture portability test (acceptance
// criterion 7). It pins exact hashes for fixed inputs so ANY future change to
// normalizeLine or LineHash breaks this test on purpose: the coordinator stores
// these hashes and the future git-export serializer must recompute the same
// values for the same normalized lines, so the contract is frozen here.
func TestLineHashFixture(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "heading",
			in:   "## Anchoring across spec edits",
			want: "0cda45d229b4538c8e5ac4ce68e47ac62a25c775b44802b49676ec6dc9bb5460",
		},
		{
			name: "messy whitespace bullet",
			in:   "  - a nested bullet   with   spaces  ",
			want: "b7364ea6652e0bae100b6356c7853b627793ee81a74d4d402f291805061c7532",
		},
		{
			name: "plain sentence",
			in:   "The anchor must recompute identically.",
			want: "e0fc5e57e1e8979f625a1e171bc8563083714242bd8f876acc85f33b8e9e1b88",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LineHash(normalizeLine(tt.in))
			if got != tt.want {
				t.Errorf("LineHash(normalizeLine(%q)) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

// TestLineHashUnicodeNFC proves NFC actually runs: a decomposed "e + combining
// acute" must hash identically to the precomposed "é", and to the frozen
// fixture value. Building the input from explicit runes makes the byte sequence
// unambiguous regardless of how this source file is saved.
func TestLineHashUnicodeNFC(t *testing.T) {
	const want = "5a5b25a1d214cd6bdd0d1fb91ffaa37525021e5af737aa5653273ed807eeefe3"

	decomposed := "caf" + "e" + "́" + " normalization" // e + U+0301
	precomposed := "café normalization"                // é

	gotD := LineHash(normalizeLine(decomposed))
	gotP := LineHash(normalizeLine(precomposed))

	if gotD != gotP {
		t.Errorf("decomposed hash %s != precomposed hash %s (NFC did not run)", gotD, gotP)
	}
	if gotD != want {
		t.Errorf("unicode fixture hash = %s, want %s", gotD, want)
	}
}

const sampleBody = `# Inline Spec Comments

Intro paragraph one.

## Anchoring across spec edits

The hard part. A thread pins to a line.

### Anchor fields

Field one is the section path.
Field two is the line hash.
Field three is the prefix window.

### Normalization

Before hashing each line is normalized.

## Real-time relay

Two hops, kept distinct.
`

func TestComputeAnchorSectionPath(t *testing.T) {
	// Line "Field two is the line hash." is under
	// Anchoring across spec edits > Anchor fields.
	lines := bodyLines(sampleBody)
	var target int
	for i, l := range lines {
		if l == "Field two is the line hash." {
			target = i + 1
		}
	}
	if target == 0 {
		t.Fatal("target line not found")
	}

	a := ComputeAnchor(sampleBody, target, target)
	want := []string{"Anchoring across spec edits", "Anchor fields"}
	if !reflect.DeepEqual(a.SectionPath, want) {
		t.Errorf("SectionPath = %v, want %v", a.SectionPath, want)
	}
	if a.LineHint != target {
		t.Errorf("LineHint = %d, want %d", a.LineHint, target)
	}
	if a.ExactText != "Field two is the line hash." {
		t.Errorf("ExactText = %q", a.ExactText)
	}
	if a.LineHash != LineHash(normalizeLine("Field two is the line hash.")) {
		t.Errorf("LineHash mismatch")
	}
	if a.CommitSHA != "" || a.BlobSHA != "" {
		t.Errorf("git SHAs should be empty, got %q/%q", a.CommitSHA, a.BlobSHA)
	}
	// Prefix/Suffix are the 3 physical adjacent lines, normalized (including
	// blanks and heading lines), per "up to 3 normalized lines before/after".
	if a.Prefix != "### Anchor fields\n\nField one is the section path." {
		t.Errorf("Prefix = %q", a.Prefix)
	}
	if a.Suffix != "Field three is the prefix window.\n\n### Normalization" {
		t.Errorf("Suffix = %q", a.Suffix)
	}
}

// TestComputeAnchorIgnoresFencedHashes verifies a '#' line inside a code fence
// is not treated as a heading in the section path.
func TestComputeAnchorIgnoresFencedHashes(t *testing.T) {
	body := `# Title

## Real section

` + "```" + `
# this is a shell comment, not a heading
some code line
` + "```" + `

target line here
`
	lines := bodyLines(body)
	var target int
	for i, l := range lines {
		if l == "target line here" {
			target = i + 1
		}
	}
	a := ComputeAnchor(body, target, target)
	// H1 "Title" is excluded from the trail; the fenced "# ..." line must not
	// appear either.
	want := []string{"Real section"}
	if !reflect.DeepEqual(a.SectionPath, want) {
		t.Errorf("SectionPath = %v, want %v (fenced # leaked or H1 included)", a.SectionPath, want)
	}
}

func TestRepositionExactUniqueAfterUnrelatedEdit(t *testing.T) {
	a := ComputeAnchor(sampleBody, lineOf(t, sampleBody, "Field two is the line hash."), lineOf(t, sampleBody, "Field two is the line hash."))

	// Edit elsewhere: add lines near the top, shifting the target down without
	// touching its line or context.
	edited := replace(sampleBody, "Intro paragraph one.", "Intro paragraph one.\nNEW extra line one.\nNEW extra line two.")

	got, line, ok := Reposition(edited, a)
	if !ok {
		t.Fatal("expected reattach, got orphan")
	}
	wantLine := lineOf(t, edited, "Field two is the line hash.")
	if line != wantLine {
		t.Errorf("reattached line = %d, want %d", line, wantLine)
	}
	if got.LineHint != wantLine {
		t.Errorf("LineHint = %d, want %d", got.LineHint, wantLine)
	}
	if got.LineHash != a.LineHash {
		t.Errorf("exact-unique should not change LineHash")
	}
}

// TestRepositionMultiLineRoundTrip is the regression for the orphan-on-display
// bug: a multi-line selection (a paragraph or several lines, the common case)
// must reattach when repositioned against the same body, not orphan. Before the
// fix, ComputeAnchor hashed the joined range while Reposition matched single
// lines, so every multi-line comment landed in triage immediately.
func TestRepositionMultiLineRoundTrip(t *testing.T) {
	start := lineOf(t, sampleBody, "Field one is the section path.")
	end := lineOf(t, sampleBody, "Field three is the prefix window.")
	if end <= start {
		t.Fatalf("expected a multi-line range, got [%d,%d]", start, end)
	}
	a := ComputeAnchor(sampleBody, start, end)

	// Same body: must reattach exactly to the range start.
	_, line, ok := Reposition(sampleBody, a)
	if !ok {
		t.Fatal("multi-line anchor orphaned against its own body (the bug)")
	}
	if line != start {
		t.Errorf("reattached to line %d, want range start %d", line, start)
	}

	// After an unrelated edit above, the range shifts down but still reattaches.
	edited := replace(sampleBody, "Intro paragraph one.", "Intro paragraph one.\nNEW line A.\nNEW line B.")
	_, line2, ok2 := Reposition(edited, a)
	if !ok2 {
		t.Fatal("multi-line anchor orphaned after an unrelated edit")
	}
	if line2 != lineOf(t, edited, "Field one is the section path.") {
		t.Errorf("reattached to wrong line %d after edit", line2)
	}
}

func TestRepositionFuzzyWithinSection(t *testing.T) {
	// Anchor on a line, then edit that very line slightly (so the hash no
	// longer matches) but keep its prefix/suffix context, forcing the fuzzy
	// path to reattach within the section and update LineHash.
	a := ComputeAnchor(sampleBody, lineOf(t, sampleBody, "Field two is the line hash."), lineOf(t, sampleBody, "Field two is the line hash."))

	edited := replace(sampleBody, "Field two is the line hash.", "Field two is the primary line hash key.")

	got, line, ok := Reposition(edited, a)
	if !ok {
		t.Fatal("expected fuzzy reattach, got orphan")
	}
	wantLine := lineOf(t, edited, "Field two is the primary line hash key.")
	if line != wantLine {
		t.Errorf("fuzzy line = %d, want %d", line, wantLine)
	}
	if got.LineHash != LineHash(normalizeLine("Field two is the primary line hash key.")) {
		t.Errorf("fuzzy path should update LineHash to the new line")
	}
}

// TestRepositionMultiLineFuzzyUsesSuffixAfterRange exercises the off-by-(rangeLen-1)
// suffix-window bug in contextSimilarity. A 2-line anchor has its first line edited
// (so the range hash no longer matches), forcing the fuzzy Step 3 path. The
// candidate's true context still surrounds it, so it must reattach. With the old
// suffix window (line+1 .. line+contextWindow) the window overlapped the inside of
// the range and stopped short, depressing suffix similarity below fuzzyThreshold
// and orphaning the anchor.
func TestRepositionMultiLineFuzzyUsesSuffixAfterRange(t *testing.T) {
	body := `# Doc

## Section

prefix alpha one
prefix bravo two
prefix charlie three
range first line delta
range second line echo
suffix foxtrot one
suffix golf two
suffix hotel three
trailing india filler
`
	start := lineOf(t, body, "range first line delta")
	end := lineOf(t, body, "range second line echo")
	if end != start+1 {
		t.Fatalf("expected a 2-line range, got [%d,%d]", start, end)
	}
	a := ComputeAnchor(body, start, end)

	// Edit the FIRST range line so the 2-line hash no longer matches, forcing
	// the fuzzy path. Prefix and suffix context are unchanged.
	edited := replace(body, "range first line delta", "range first line delta CHANGED")

	_, line, ok := Reposition(edited, a)
	if !ok {
		t.Fatal("multi-line anchor orphaned in fuzzy path (suffix window off by rangeLen-1)")
	}
	if want := lineOf(t, edited, "range first line delta CHANGED"); line != want {
		t.Errorf("reattached to line %d, want %d", line, want)
	}
}

func TestRepositionAmbiguousDisambiguatedByContext(t *testing.T) {
	// Two identical anchored lines in different contexts; the anchor's stored
	// context should pick the right one.
	body := `# Doc

## Section A

alpha before A
shared duplicate line
alpha after A

## Section B

beta before B
shared duplicate line
beta after B
`
	// Anchor on the FIRST "shared duplicate line" (in Section A context).
	first := lineOf(t, body, "shared duplicate line")
	a := ComputeAnchor(body, first, first)

	got, line, ok := Reposition(body, a)
	if !ok {
		t.Fatal("expected disambiguation, got orphan")
	}
	if line != first {
		t.Errorf("disambiguated to line %d, want %d (Section A copy)", line, first)
	}
	_ = got
}

func TestRepositionOrphanWhenTextDestroyed(t *testing.T) {
	a := ComputeAnchor(sampleBody, lineOf(t, sampleBody, "Field two is the line hash."), lineOf(t, sampleBody, "Field two is the line hash."))

	// Destroy the anchored text AND its surrounding context entirely.
	edited := `# Inline Spec Comments

## Anchoring across spec edits

Completely rewritten body with nothing in common.
Totally different words everywhere now.

## Real-time relay

Two hops, kept distinct.
`
	_, line, ok := Reposition(edited, a)
	if ok {
		t.Errorf("expected orphan, reattached to line %d", line)
	}
	if line != 0 {
		t.Errorf("orphan should return line 0, got %d", line)
	}
}

// TestRepositionRenamedLeafHeading verifies Step 3 tolerates a renamed leaf
// heading by matching the trail prefix.
func TestRepositionRenamedLeafHeading(t *testing.T) {
	a := ComputeAnchor(sampleBody, lineOf(t, sampleBody, "Field two is the line hash."), lineOf(t, sampleBody, "Field two is the line hash."))

	// Rename the leaf heading "Anchor fields" -> "The anchor fields" and tweak
	// the anchored line so only the fuzzy-in-section path can recover it.
	edited := replace(sampleBody, "### Anchor fields", "### The anchor fields")
	edited = replace(edited, "Field two is the line hash.", "Field two is the line hash value.")

	_, line, ok := Reposition(edited, a)
	if !ok {
		t.Fatal("expected reattach via trail-prefix match, got orphan")
	}
	if line != lineOf(t, edited, "Field two is the line hash value.") {
		t.Errorf("reattached to wrong line %d", line)
	}
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Anchor fields":            "anchor-fields",
		"  Real-time relay  ":      "real-time-relay",
		"Step 2 (exact-ambiguous)": "step-2-exact-ambiguous",
		"UPPER Case":               "upper-case",
	}
	for in, want := range tests {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJaccardEdges(t *testing.T) {
	if jaccard("", "") != 1.0 {
		t.Error("both-empty should be 1.0")
	}
	if jaccard("a b", "") != 0.0 {
		t.Error("one-empty should be 0.0")
	}
	if jaccard("a b c", "a b c") != 1.0 {
		t.Error("identical should be 1.0")
	}
	// {a,b} vs {b,c}: inter=1, union=3 -> 1/3
	if got := jaccard("a b", "b c"); got < 0.33 || got > 0.34 {
		t.Errorf("partial overlap = %v, want ~0.333", got)
	}
}

// Helpers.

func lineOf(t *testing.T, body, want string) int {
	t.Helper()
	for i, l := range bodyLines(body) {
		if l == want {
			return i + 1
		}
	}
	t.Fatalf("line %q not found in body", want)
	return 0
}

func replace(body, old, repl string) string {
	return strings.ReplaceAll(body, old, repl)
}
