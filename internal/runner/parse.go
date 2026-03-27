package runner

import (
	"encoding/json"
	"fmt"
	"strings"
)

// parseOutput tries to parse raw as a single JSON object first; if that fails
// it scans backwards through NDJSON lines looking for the result message.
//
// In stream-json format the final "result" message carries a non-empty
// stop_reason ("end_turn", "max_tokens", etc.). Verbose or debug lines may
// appear after the result message, so we prefer the last line that has
// stop_reason set and fall back to the last valid JSON if none does.
func parseOutput(raw string) (*agentOutput, error) {
	var output agentOutput
	if err := json.Unmarshal([]byte(raw), &output); err == nil {
		normalizeConversationID(&output, raw)
		return &output, nil
	}
	lines := strings.Split(raw, "\n")
	var fallback *agentOutput
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var candidate agentOutput
		if err := json.Unmarshal([]byte(line), &candidate); err != nil {
			continue
		}
		if fallback == nil {
			c := candidate
			fallback = &c
		}
		// Prefer the message that has stop_reason set — that is the "result"
		// message emitted by the agent at the end of every run.
		if candidate.StopReason != "" {
			normalizeConversationID(&candidate, raw)
			return &candidate, nil
		}
	}
	if fallback != nil {
		normalizeConversationID(fallback, raw)
		return fallback, nil
	}
	return nil, fmt.Errorf("no valid JSON object found in output")
}

// normalizeConversationID ensures output.SessionID is populated. It prefers the
// parsed SessionID, falls back to ThreadID (Codex format), and finally scans
// the raw NDJSON for a session_id in early stream messages.
func normalizeConversationID(output *agentOutput, raw string) {
	if output == nil {
		return
	}
	if output.SessionID != "" {
		return
	}
	if output.ThreadID != "" {
		output.SessionID = output.ThreadID
		return
	}
	output.SessionID = extractSessionID([]byte(raw))
}

// extractSessionID scans raw NDJSON output for a session_id field.
// The agent emits session_id in early stream messages, so it is often
// present even when the container is killed mid-execution (e.g. timeout).
func extractSessionID(raw []byte) string {
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var obj struct {
			SessionID string `json:"session_id"`
			ThreadID  string `json:"thread_id"`
		}
		if json.Unmarshal([]byte(line), &obj) == nil {
			if obj.SessionID != "" {
				return obj.SessionID
			}
			if obj.ThreadID != "" {
				return obj.ThreadID
			}
		}
	}
	return ""
}
