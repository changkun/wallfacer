package runner

import (
	"testing"
)

// --- normalizeIdeationPriority tests ---

func TestNormalizeIdeationPriority_High(t *testing.T) {
	for _, input := range []string{"high", "HIGH", "p1", "P1", "critical", "CRITICAL", "urgent"} {
		if got := normalizeIdeationPriority(input); got != "high" {
			t.Errorf("normalizeIdeationPriority(%q) = %q, want %q", input, got, "high")
		}
	}
}

func TestNormalizeIdeationPriority_Medium(t *testing.T) {
	for _, input := range []string{"medium", "MEDIUM", "med", "p2", "P2", "moderate"} {
		if got := normalizeIdeationPriority(input); got != "medium" {
			t.Errorf("normalizeIdeationPriority(%q) = %q, want %q", input, got, "medium")
		}
	}
}

func TestNormalizeIdeationPriority_Low(t *testing.T) {
	for _, input := range []string{"low", "LOW", "p3", "P3", "minor", "trivial"} {
		if got := normalizeIdeationPriority(input); got != "low" {
			t.Errorf("normalizeIdeationPriority(%q) = %q, want %q", input, got, "low")
		}
	}
}

func TestNormalizeIdeationPriority_Unknown(t *testing.T) {
	for _, input := range []string{"", "unknown", "mega", "  ", "p4"} {
		if got := normalizeIdeationPriority(input); got != "" {
			t.Errorf("normalizeIdeationPriority(%q) = %q, want empty", input, got)
		}
	}
}

func TestNormalizeIdeationPriority_Trimmed(t *testing.T) {
	if got := normalizeIdeationPriority("  high  "); got != "high" {
		t.Errorf("normalizeIdeationPriority with whitespace = %q, want %q", got, "high")
	}
}

// --- normalizeIdeationImpact tests ---

func TestNormalizeIdeationImpact_ClampNegative(t *testing.T) {
	idea := &IdeateResult{Priority: "high", ImpactScore: -10}
	normalizeIdeationImpact(idea)
	if idea.ImpactScore != 85 {
		// ImpactScore gets clamped to 0, then set to 85 for high priority
		// Actually after clamping to 0, since Priority="high" it becomes 85
		t.Errorf("expected ImpactScore=85 for high priority (clamped from -10), got %d", idea.ImpactScore)
	}
}

func TestNormalizeIdeationImpact_ClampOver100(t *testing.T) {
	idea := &IdeateResult{Priority: "medium", ImpactScore: 150}
	normalizeIdeationImpact(idea)
	if idea.ImpactScore != 100 {
		t.Errorf("expected ImpactScore=100 (clamped from 150), got %d", idea.ImpactScore)
	}
}

func TestNormalizeIdeationImpact_ZeroImpact_HighPriority(t *testing.T) {
	idea := &IdeateResult{Priority: "high", ImpactScore: 0}
	normalizeIdeationImpact(idea)
	if idea.ImpactScore != 85 {
		t.Errorf("expected ImpactScore=85 for high priority, got %d", idea.ImpactScore)
	}
}

func TestNormalizeIdeationImpact_ZeroImpact_MediumPriority(t *testing.T) {
	idea := &IdeateResult{Priority: "medium", ImpactScore: 0}
	normalizeIdeationImpact(idea)
	if idea.ImpactScore != 72 {
		t.Errorf("expected ImpactScore=72 for medium priority, got %d", idea.ImpactScore)
	}
}

func TestNormalizeIdeationImpact_ZeroImpact_LowPriority(t *testing.T) {
	idea := &IdeateResult{Priority: "low", ImpactScore: 0}
	normalizeIdeationImpact(idea)
	if idea.ImpactScore != 35 {
		t.Errorf("expected ImpactScore=35 for low priority, got %d", idea.ImpactScore)
	}
}

func TestNormalizeIdeationImpact_ZeroImpact_NoPriority(t *testing.T) {
	idea := &IdeateResult{Priority: "", ImpactScore: 0}
	normalizeIdeationImpact(idea)
	if idea.ImpactScore != defaultIdeationImpactScore {
		t.Errorf("expected ImpactScore=%d for no priority, got %d", defaultIdeationImpactScore, idea.ImpactScore)
	}
}

func TestNormalizeIdeationImpact_InfersPriorityFromScore_High(t *testing.T) {
	idea := &IdeateResult{Priority: "", ImpactScore: 85}
	normalizeIdeationImpact(idea)
	if idea.Priority != "high" {
		t.Errorf("expected priority=high for score=85, got %q", idea.Priority)
	}
}

func TestNormalizeIdeationImpact_InfersPriorityFromScore_Medium(t *testing.T) {
	idea := &IdeateResult{Priority: "", ImpactScore: 75}
	normalizeIdeationImpact(idea)
	if idea.Priority != "medium" {
		t.Errorf("expected priority=medium for score=75, got %q", idea.Priority)
	}
}

func TestNormalizeIdeationImpact_InfersPriorityFromScore_Low(t *testing.T) {
	idea := &IdeateResult{Priority: "", ImpactScore: 50}
	normalizeIdeationImpact(idea)
	if idea.Priority != "low" {
		t.Errorf("expected priority=low for score=50, got %q", idea.Priority)
	}
}

func TestNormalizeIdeationImpact_TrimsStringFields(t *testing.T) {
	idea := &IdeateResult{
		Priority:    "high",
		ImpactScore: 90,
		Scope:       "  global  ",
		Rationale:   "  some rationale  ",
		Category:    "  feature  ",
	}
	normalizeIdeationImpact(idea)
	if idea.Scope != "global" {
		t.Errorf("Scope not trimmed: %q", idea.Scope)
	}
	if idea.Rationale != "some rationale" {
		t.Errorf("Rationale not trimmed: %q", idea.Rationale)
	}
	if idea.Category != "feature" {
		t.Errorf("Category not trimmed: %q", idea.Category)
	}
}

// --- isIdeaDuplicateTitle tests ---

func TestIsIdeaDuplicateTitle_EmptyTitle(t *testing.T) {
	added := map[string]struct{}{}
	if !isIdeaDuplicateTitle(added, "") {
		t.Error("empty title should be duplicate")
	}
	if !isIdeaDuplicateTitle(added, "   ") {
		t.Error("whitespace-only title should be duplicate")
	}
}

func TestIsIdeaDuplicateTitle_FirstOccurrence(t *testing.T) {
	added := map[string]struct{}{}
	if isIdeaDuplicateTitle(added, "unique title") {
		t.Error("first occurrence should not be duplicate")
	}
}

func TestIsIdeaDuplicateTitle_ExactMatch(t *testing.T) {
	added := map[string]struct{}{}
	isIdeaDuplicateTitle(added, "foo bar")
	if !isIdeaDuplicateTitle(added, "foo bar") {
		t.Error("exact match should be duplicate")
	}
}

func TestIsIdeaDuplicateTitle_CaseInsensitive(t *testing.T) {
	added := map[string]struct{}{}
	isIdeaDuplicateTitle(added, "Hello World")
	if !isIdeaDuplicateTitle(added, "hello world") {
		t.Error("case-insensitive match should be duplicate")
	}
}

func TestIsIdeaDuplicateTitle_SubstringNoMatch(t *testing.T) {
	added := map[string]struct{}{}
	isIdeaDuplicateTitle(added, "add user authentication")
	// A shorter title contained in an existing title should NOT be a duplicate —
	// only exact case-insensitive matches qualify.
	if isIdeaDuplicateTitle(added, "user authentication") {
		t.Error("substring of existing title should not be duplicate")
	}
}

func TestIsIdeaDuplicateTitle_SuperstringNoMatch(t *testing.T) {
	added := map[string]struct{}{}
	isIdeaDuplicateTitle(added, "auth")
	// Existing is a substring of new title — should NOT be a duplicate.
	if isIdeaDuplicateTitle(added, "add auth feature") {
		t.Error("superstring of existing title should not be duplicate")
	}
}

func TestIsIdeaDuplicateTitle_NoMatch(t *testing.T) {
	added := map[string]struct{}{}
	isIdeaDuplicateTitle(added, "foo")
	if isIdeaDuplicateTitle(added, "bar") {
		t.Error("different titles should not be duplicates")
	}
}

// --- repairTruncatedJSONArray tests ---

func TestRepairTruncatedJSONArray_Empty(t *testing.T) {
	result := repairTruncatedJSONArray("", 0)
	if result != "" {
		t.Errorf("expected empty string for empty input, got %q", result)
	}
}

func TestRepairTruncatedJSONArray_NoCompleteObject(t *testing.T) {
	input := `[{"title": "incomplete`
	result := repairTruncatedJSONArray(input, 0)
	if result != "" {
		t.Errorf("expected empty string for no complete object, got %q", result)
	}
}

func TestRepairTruncatedJSONArray_OneCompleteObject(t *testing.T) {
	input := `[{"title":"idea1","prompt":"p1"},{"title":"truncated`
	result := repairTruncatedJSONArray(input, 0)
	if result == "" {
		t.Fatal("expected repaired array, got empty")
	}
	// Should be a valid JSON array with the first object.
	if result[0] != '[' || result[len(result)-1] != ']' {
		t.Errorf("repaired result should be a JSON array: %q", result)
	}
}

func TestRepairTruncatedJSONArray_TwoCompleteObjects(t *testing.T) {
	input := `[{"title":"idea1","prompt":"p1"},{"title":"idea2","prompt":"p2"},{"title":"trunc`
	result := repairTruncatedJSONArray(input, 0)
	if result == "" {
		t.Fatal("expected repaired array, got empty")
	}
	if result[len(result)-1] != ']' {
		t.Errorf("result should end with ]: %q", result)
	}
}

// --- findJSONCodeBlock tests ---

func TestFindJSONCodeBlock_Empty(t *testing.T) {
	result := findJSONCodeBlock("")
	if len(result) != 0 {
		t.Errorf("expected empty for no code blocks, got %v", result)
	}
}

func TestFindJSONCodeBlock_JSONFence(t *testing.T) {
	input := "Some text\n```json\n[{\"title\":\"test\"}]\n```\nMore text"
	result := findJSONCodeBlock(input)
	if len(result) == 0 {
		t.Fatal("expected at least one code block")
	}
	if result[0] != `[{"title":"test"}]` {
		t.Errorf("unexpected code block content: %q", result[0])
	}
}

func TestFindJSONCodeBlock_PlainFence(t *testing.T) {
	input := "Some text\n```\n[{\"title\":\"test\"}]\n```\nMore text"
	result := findJSONCodeBlock(input)
	if len(result) == 0 {
		t.Fatal("expected at least one code block for plain fence")
	}
}

func TestFindJSONCodeBlock_MultipleFences(t *testing.T) {
	input := "```json\n[{\"a\":1}]\n```\nText\n```json\n[{\"b\":2}]\n```"
	result := findJSONCodeBlock(input)
	if len(result) != 2 {
		t.Errorf("expected 2 code blocks, got %d", len(result))
	}
}

// --- extractIdeas tests ---

func TestExtractIdeas_ValidJSON(t *testing.T) {
	// Build a valid JSON array with ideas above the minimum impact score.
	input := `[
		{"title":"Add dark mode","prompt":"Implement a dark mode toggle for the UI","impact_score":85,"priority":"high"},
		{"title":"Fix auth bug","prompt":"Fix authentication timeout issue in the session handler","impact_score":80,"priority":"high"}
	]`

	ideas, rejections, err := extractIdeas(input)
	if err != nil {
		t.Fatalf("extractIdeas: %v", err)
	}
	if len(ideas) == 0 {
		t.Error("expected at least one idea")
	}
	_ = rejections
}

func TestExtractIdeas_EmptyInput(t *testing.T) {
	_, _, err := extractIdeas("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestExtractIdeas_NoJSONArray(t *testing.T) {
	_, _, err := extractIdeas("just plain text with no JSON")
	if err == nil {
		t.Error("expected error for text with no JSON array")
	}
}

func TestExtractIdeas_DegenerateTitle(t *testing.T) {
	// Prompt equals title: should be rejected.
	input := `[{"title":"build widget","prompt":"build widget","impact_score":90}]`
	ideas, rejections, err := extractIdeas(input)
	// May return error if all ideas are rejected, or return with empty ideas.
	_ = ideas
	_ = err

	// Count degenerate rejections.
	degenerateCount := 0
	for _, r := range rejections {
		if r.Reason == ideaRejectDegenerateTitle {
			degenerateCount++
		}
	}
	if degenerateCount == 0 {
		t.Error("expected at least one degenerate_prompt rejection")
	}
}

func TestExtractIdeas_LowImpactAccepted(t *testing.T) {
	// Impact score filtering is removed — the agent's self-critique is trusted.
	input := `[{"title":"minor tweak","prompt":"make a small change to button color","impact_score":1}]`
	ideas, _, err := extractIdeas(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ideas) != 1 {
		t.Fatalf("expected 1 idea (low impact accepted), got %d", len(ideas))
	}
}

func TestExtractIdeas_EmptyFields(t *testing.T) {
	input := `[{"title":"","prompt":"","impact_score":90}]`
	_, rejections, err := extractIdeas(input)
	_ = err

	emptyCount := 0
	for _, r := range rejections {
		if r.Reason == ideaRejectEmptyFields {
			emptyCount++
		}
	}
	if emptyCount == 0 {
		t.Error("expected at least one empty_fields rejection")
	}
}

func TestExtractIdeas_InCodeBlock(t *testing.T) {
	input := "Here are the ideas:\n```json\n[{\"title\":\"Add search\",\"prompt\":\"Implement full-text search across tasks\",\"impact_score\":85}]\n```"
	ideas, _, err := extractIdeas(input)
	if err != nil {
		t.Fatalf("extractIdeas with code block: %v", err)
	}
	if len(ideas) == 0 {
		t.Error("expected ideas from code block")
	}
}

func TestExtractIdeas_EmptyJSONArray(t *testing.T) {
	// An empty JSON array is a valid response when the workspace has no code.
	ideas, rejections, err := extractIdeas("[]")
	if err != nil {
		t.Fatalf("extractIdeas([]) should not error, got: %v", err)
	}
	if len(ideas) != 0 {
		t.Errorf("expected 0 ideas, got %d", len(ideas))
	}
	if len(rejections) != 0 {
		t.Errorf("expected 0 rejections, got %d", len(rejections))
	}
}

func TestExtractIdeas_EmptyJSONArrayInProse(t *testing.T) {
	input := "There is no source code to analyse.\n\n[]"
	ideas, _, err := extractIdeas(input)
	if err != nil {
		t.Fatalf("extractIdeas with prose + [] should not error, got: %v", err)
	}
	if len(ideas) != 0 {
		t.Errorf("expected 0 ideas, got %d", len(ideas))
	}
}

// --- looksLikeNoCodebaseOutput tests ---

func TestLooksLikeNoCodebaseOutput_Positive(t *testing.T) {
	cases := []string{
		"there is no codebase to analyze",
		"The workspace contains no source code or project files.",
		"This is an empty project with nothing to review.",
		"Cannot produce recommendations — no project files found.",
	}
	for _, c := range cases {
		if !looksLikeNoCodebaseOutput(c) {
			t.Errorf("expected true for: %q", c)
		}
	}
}

func TestLooksLikeNoCodebaseOutput_Negative(t *testing.T) {
	cases := []string{
		`[{"title":"Fix auth","prompt":"Fix the auth bug","impact_score":80}]`,
		"Here are my top 3 improvement ideas for the codebase.",
		"",
	}
	for _, c := range cases {
		if looksLikeNoCodebaseOutput(c) {
			t.Errorf("expected false for: %q", c)
		}
	}
}

// --- countIdeaRejections tests ---

func TestCountIdeaRejections_Empty(t *testing.T) {
	count := countIdeaRejections(nil, ideaRejectLowImpact)
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestCountIdeaRejections_Counts(t *testing.T) {
	rejections := []ideaRejection{
		{Reason: ideaRejectLowImpact},
		{Reason: ideaRejectLowImpact},
		{Reason: ideaRejectDuplicateTitle},
	}
	if got := countIdeaRejections(rejections, ideaRejectLowImpact); got != 2 {
		t.Errorf("expected 2 low_impact rejections, got %d", got)
	}
	if got := countIdeaRejections(rejections, ideaRejectDuplicateTitle); got != 1 {
		t.Errorf("expected 1 duplicate_title rejection, got %d", got)
	}
	if got := countIdeaRejections(rejections, ideaRejectEmptyFields); got != 0 {
		t.Errorf("expected 0 empty_fields rejections, got %d", got)
	}
}
