package ndjson

import (
	"encoding/json"
	"strings"
)

// PreferResultLine scans NDJSON text for the terminal "result" line, the common
// shape behind several agent-output parsers: decode each non-empty,
// brace-prefixed line into T, skip lines failing isCandidate, and return the
// first line (in scan order) for which isTerminal reports true — the result
// message carrying a non-empty stop_reason. If no terminal line is found, it
// returns the first candidate encountered in scan order (the fallback). ok is
// false only when no candidate line decodes at all.
//
// backward=true scans newest-line-first (the result is normally the last line;
// verbose/debug lines may follow it). Lines failing isCandidate are excluded
// from BOTH the terminal match and the fallback, so a trailing non-result line
// never becomes the fallback. isCandidate may be nil (every decoded line is a
// candidate).
func PreferResultLine[T any](raw string, backward bool, isCandidate, isTerminal func(*T) bool) (T, bool) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	var fallback *T

	// consider decodes one line and returns the terminal match when isTerminal
	// holds. It records the first qualifying candidate as the fallback.
	consider := func(line string) (T, bool) {
		var c T
		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			return c, false
		}
		if json.Unmarshal([]byte(line), &c) != nil {
			return c, false
		}
		if isCandidate != nil && !isCandidate(&c) {
			return c, false
		}
		if fallback == nil {
			cp := c
			fallback = &cp
		}
		if isTerminal(&c) {
			return c, true
		}
		return c, false
	}

	if backward {
		for i := len(lines) - 1; i >= 0; i-- {
			if c, ok := consider(lines[i]); ok {
				return c, true
			}
		}
	} else {
		for _, line := range lines {
			if c, ok := consider(line); ok {
				return c, true
			}
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	var zero T
	return zero, false
}
