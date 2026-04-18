package agents

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/store"
)

// oversightPhaseRaw is the JSON shape for a single phase as the agent
// produces it. Lives next to the parser so the descriptor stays the
// authoritative definition of the oversight contract.
type oversightPhaseRaw struct {
	Timestamp string   `json:"timestamp"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	ToolsUsed []string `json:"tools_used"`
	Commands  []string `json:"commands"`
	Actions   []string `json:"actions"`
}

// oversightResultRaw is the outer JSON shape when the agent wraps its
// phases in an object.
type oversightResultRaw struct {
	Phases []oversightPhaseRaw `json:"phases"`
}

// parseOversightResult is the Oversight descriptor's ParseResult. It
// extracts structured phases from the raw Output.Result string,
// stripping optional markdown fences and coping with both the bare-
// array and phases-key shapes Claude occasionally emits.
func parseOversightResult(o *Output) (any, error) {
	phases, err := parseOversightPhases(o.Result)
	if err != nil {
		return nil, fmt.Errorf("parse oversight JSON: %w", err)
	}
	return phases, nil
}

// parseOversightPhases is the underlying phase-extraction logic,
// factored out so unit tests can exercise it without constructing an
// Output value.
func parseOversightPhases(result string) ([]store.OversightPhase, error) {
	result = strings.TrimSpace(result)
	if result == "" {
		return []store.OversightPhase{}, nil
	}

	// Strip markdown code fences if present.
	if idx := strings.Index(result, "```"); idx != -1 {
		start := strings.Index(result, "\n")
		end := strings.LastIndex(result, "```")
		if start != -1 && end > start {
			result = strings.TrimSpace(result[start+1 : end])
		}
	}

	var rawPhases []oversightPhaseRaw
	switch {
	case strings.HasPrefix(result, "["):
		if err := json.Unmarshal([]byte(result), &rawPhases); err != nil {
			return nil, err
		}
	case strings.HasPrefix(result, "{"):
		var r oversightResultRaw
		if err := json.Unmarshal([]byte(result), &r); err != nil {
			return nil, err
		}
		rawPhases = r.Phases
	default:
		// Skip preamble text before the first '{'.
		i := strings.Index(result, "{")
		if i < 0 {
			return nil, fmt.Errorf("no JSON object found in oversight result")
		}
		result = result[i:]
		var r oversightResultRaw
		if err := json.Unmarshal([]byte(result), &r); err != nil {
			return nil, err
		}
		rawPhases = r.Phases
	}

	phases := make([]store.OversightPhase, 0, len(rawPhases))
	for _, p := range rawPhases {
		phase := store.OversightPhase{
			Title:     strings.TrimSpace(p.Title),
			Summary:   strings.TrimSpace(p.Summary),
			ToolsUsed: p.ToolsUsed,
			Commands:  p.Commands,
			Actions:   p.Actions,
		}
		if p.Timestamp != "" {
			for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
				if t, err := time.Parse(layout, p.Timestamp); err == nil {
					phase.Timestamp = t
					break
				}
			}
		}
		phases = append(phases, phase)
	}
	return phases, nil
}
