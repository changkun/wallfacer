package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/set"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

func normalizeIdeationPriority(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high", "p1", "critical", "urgent":
		return "high"
	case "medium", "med", "p2", "moderate":
		return "medium"
	case "low", "p3", "minor", "trivial":
		return "low"
	default:
		return ""
	}
}

func normalizeIdeationImpact(idea *IdeateResult) {
	idea.Priority = normalizeIdeationPriority(idea.Priority)
	if idea.ImpactScore < 0 {
		idea.ImpactScore = 0
	}
	if idea.ImpactScore > 100 {
		idea.ImpactScore = 100
	}
	if idea.ImpactScore == 0 {
		switch idea.Priority {
		case "high":
			idea.ImpactScore = 85
		case "medium":
			idea.ImpactScore = 72
		case "low":
			idea.ImpactScore = 35
		default:
			idea.ImpactScore = defaultIdeationImpactScore
		}
	}
	if idea.Priority == "" {
		switch {
		case idea.ImpactScore >= 80:
			idea.Priority = "high"
		case idea.ImpactScore >= 72:
			idea.Priority = "medium"
		default:
			idea.Priority = "low"
		}
	}
	idea.Scope = strings.TrimSpace(idea.Scope)
	idea.Rationale = strings.TrimSpace(idea.Rationale)
	idea.Category = strings.TrimSpace(idea.Category)
	if idea.Title == "" {
		idea.Title = strings.TrimSpace(idea.Title)
	}
	if idea.Prompt == "" {
		idea.Prompt = strings.TrimSpace(idea.Prompt)
	}
}

func isIdeaDuplicateTitle(added *set.Set[string], title string) bool {
	current := strings.ToLower(strings.TrimSpace(title))
	if current == "" {
		return true
	}
	if added.Has(current) {
		return true
	}
	added.Add(current)
	return false
}

// ideaRejectReason identifies why an idea was filtered out during ideation parsing.
type ideaRejectReason string

const (
	ideaRejectEmptyFields    ideaRejectReason = "empty_fields"
	ideaRejectDuplicateTitle ideaRejectReason = "duplicate_title"
)

type ideaRejection struct {
	Title  string
	Reason ideaRejectReason
	Score  int
}

func (r *Runner) emitIdeationRejectionEvents(ctx context.Context, taskID uuid.UUID, rejections []ideaRejection) {
	if len(rejections) == 0 {
		return
	}

	for _, rejection := range rejections {
		label := strings.TrimSpace(rejection.Title)
		if label == "" {
			label = "(untitled)"
		}
		_ = r.store.InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{

			"result": fmt.Sprintf("Idea filtered (%s): %q (score: %d)", rejection.Reason, label, rejection.Score),
		})
	}

	logger.Runner.Debug("ideation: idea filtering summary",
		"task", taskID,
		"rejections", len(rejections),
		"duplicate_title", countIdeaRejections(rejections, ideaRejectDuplicateTitle),
		"empty_fields", countIdeaRejections(rejections, ideaRejectEmptyFields),
	)
}

func countIdeaRejections(rejections []ideaRejection, reason ideaRejectReason) int {
	total := 0
	for _, rejection := range rejections {
		if rejection.Reason == reason {
			total++
		}
	}
	return total
}

// extractIdeas finds a JSON array in the agent's text output and parses it
// into a slice of IdeateResult. It is tolerant of surrounding prose by
// scanning for the first '[' and then counting bracket depth to find its
// matching ']', which avoids capturing stray brackets in trailing prose.
func extractIdeas(text string) ([]IdeateResult, []ideaRejection, error) {
	candidates := extractJSONArrayLikeCandidates(text)
	var parseErr error
	var parseRejections []ideaRejection
	for _, candidate := range candidates {
		ideas, rejections, err := parseIdeaJSONArray(candidate)
		if err == nil {
			return ideas, rejections, nil
		}
		parseErr = err
		parseRejections = rejections
	}
	if parseErr != nil {
		return nil, parseRejections, parseErr
	}
	return nil, nil, fmt.Errorf("no JSON array found in agent output")
}

func extractJSONArrayLikeCandidates(text string) []string {
	candidates := make([]string, 0, 2)
	if text == "" {
		return candidates
	}
	// Accept JSON arrays embedded in prose (old behavior) and in fenced code
	// blocks (newer model variants sometimes wrap payloads in ```json).
	candidates = append(candidates, text)
	candidates = append(candidates, findJSONCodeBlock(text)...)
	return candidates
}

func parseIdeaJSONArray(text string) ([]IdeateResult, []ideaRejection, error) {
	start := strings.Index(text, "[")
	if start == -1 {
		return nil, nil, fmt.Errorf("no JSON array found in candidate output")
	}

	// Walk forward from the opening '[' counting bracket depth to find
	// the matching ']'. This is safe for JSON because brackets inside
	// strings are always escaped or paired, and we only care about
	// finding the correct closing bracket for the top-level array.
	depth := 0
	end := -1
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == '[' {
			depth++
		} else if ch == ']' {
			depth--
			if depth == 0 {
				end = i
				break
			}
		}
	}
	var results []IdeateResult
	if end == -1 {
		repaired := repairTruncatedJSONArray(text, start)
		if repaired == "" {
			return nil, nil, fmt.Errorf("no JSON array found in candidate output")
		}
		logger.Runner.Warn("ideation: JSON array truncated; attempting partial recovery",
			"recovered_bytes", len(repaired))
		if err := json.Unmarshal([]byte(repaired), &results); err != nil {
			return nil, nil, fmt.Errorf("no JSON array found and partial recovery failed: %w", err)
		}
	} else {
		if err := json.Unmarshal([]byte(text[start:end+1]), &results); err != nil {
			return nil, nil, fmt.Errorf("unmarshal ideas: %w", err)
		}
	}

	// Normalize schema and filter out malformed entries.
	// Only reject ideas with truly empty fields or duplicate titles.
	var valid []IdeateResult
	var rejections []ideaRejection
	seen := set.New[string]()
	for _, r := range results {
		title := strings.TrimSpace(r.Title)
		prompt := strings.TrimSpace(r.Prompt)
		if title == "" || prompt == "" {
			rejections = append(rejections, ideaRejection{
				Title:  title,
				Reason: ideaRejectEmptyFields,
			})
			continue
		}
		idea := r
		normalizeIdeationImpact(&idea)
		idea.Title = title
		idea.Prompt = prompt
		if isIdeaDuplicateTitle(&seen, idea.Title) {
			rejections = append(rejections, ideaRejection{
				Title:  title,
				Score:  idea.ImpactScore,
				Reason: ideaRejectDuplicateTitle,
			})
			continue
		}
		valid = append(valid, idea)
	}
	sort.Slice(valid, func(i, j int) bool {
		if valid[i].ImpactScore == valid[j].ImpactScore {
			return valid[i].Title < valid[j].Title
		}
		return valid[i].ImpactScore > valid[j].ImpactScore
	})
	if len(valid) > maxIdeationIdeas {
		valid = valid[:maxIdeationIdeas]
	}
	if len(valid) == 0 {
		if len(results) == 0 {
			// The agent returned an empty JSON array — legitimate when
			// the workspace has no source code to analyse.
			return nil, rejections, nil
		}
		return nil, rejections, fmt.Errorf("no valid ideas in parsed output (all entries had empty title or prompt)")
	}
	return valid, rejections, nil
}

// repairTruncatedJSONArray attempts to recover a valid JSON array from text
// that was cut off before the closing ']'. It scans forward from start
// tracking bracket depth and string state, recording every position where a
// complete top-level JSON object ends (i.e., a '}' that returns depth to 1
// while inside the outer array), then closes the array and returns the
// repaired string. Returns "" if no complete object is found.
func repairTruncatedJSONArray(text string, start int) string {
	// Walk forward tracking depth and string state, recording every position
	// where we return to depth==1 after a '}' (meaning one object just closed).
	depth := 0
	inString := false
	escaped := false
	lastObjEnd := -1
	for i := start; i < len(text); i++ {
		ch := text[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '[', '{':
			depth++
		case '}':
			depth--
			if depth == 1 {
				lastObjEnd = i // closed an object inside the array
			}
		case ']':
			depth--
		}
	}
	if lastObjEnd == -1 {
		return ""
	}
	return text[start:lastObjEnd+1] + "]"
}

func findJSONCodeBlock(text string) []string {
	var blocks []string
	offset := 0
	for {
		start := strings.Index(text[offset:], "```")
		if start == -1 {
			return blocks
		}
		start += offset
		rest := text[start+3:]
		restOffset := strings.Index(rest, "\n")
		if restOffset == -1 {
			return blocks
		}
		firstLine := strings.TrimSpace(rest[:restOffset])
		// Some prompts use raw fences without language tag.
		contentStart := start + 3 + restOffset + 1
		end := strings.Index(text[contentStart:], "```")
		if end == -1 {
			return blocks
		}
		content := strings.TrimSpace(text[contentStart : contentStart+end])
		if firstLine == "" || strings.EqualFold(firstLine, "json") {
			blocks = append(blocks, content)
		}
		offset = contentStart + end + 3
	}
}

// looksLikeNoCodebaseOutput returns true when the agent's result text
// indicates there is no source code in the workspace to analyse.
func looksLikeNoCodebaseOutput(result string) bool {
	lower := strings.ToLower(result)
	markers := []string{
		"no codebase",
		"no source code",
		"empty project",
		"no project files",
		"cannot produce",
		"there is no codebase to analyze",
		"there is no codebase to analyse",
	}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

func extractIdeasFromRunOutput(result string, rawStdout, rawStderr []byte) ([]IdeateResult, []ideaRejection, error) {
	// Prefer the final parsed result if it already contains ideas.
	if ideas, rejections, err := extractIdeas(result); err == nil {
		return ideas, rejections, nil
	}

	text := strings.TrimSpace(string(rawStdout) + "\n" + string(rawStderr))
	if text == "" {
		return nil, nil, fmt.Errorf("no JSON array found in agent output")
	}

	var fallback []IdeateResult
	var fallbackRejections []ideaRejection
	var fallbackErr error
	var candidateRejections []ideaRejection
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var output agentOutput
		if err := json.Unmarshal([]byte(line), &output); err != nil {
			continue
		}
		if strings.TrimSpace(output.Result) == "" {
			continue
		}
		ideas, rejections, err := extractIdeas(output.Result)
		if err != nil {
			fallbackErr = err
			candidateRejections = append(candidateRejections, rejections...)
			continue
		}
		if output.StopReason != "" {
			candidateRejections = append(candidateRejections, rejections...)
			return ideas, candidateRejections, nil
		}
		if fallback == nil {
			fallback = ideas
			fallbackRejections = rejections
		}
	}
	if fallback != nil {
		return fallback, append(fallbackRejections, candidateRejections...), nil
	}
	if fallbackErr != nil {
		return nil, candidateRejections, fallbackErr
	}
	return nil, nil, fmt.Errorf("no JSON array found in agent output")
}
