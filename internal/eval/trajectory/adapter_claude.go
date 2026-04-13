package trajectory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
)

// ClaudeCodeAdapter parses Claude Code's --output-format stream-json
// NDJSON output. One instance is safe to reuse across trajectories.
type ClaudeCodeAdapter struct{}

// NewClaudeCodeAdapter returns a zero-configuration Claude Code adapter.
func NewClaudeCodeAdapter() ClaudeCodeAdapter { return ClaudeCodeAdapter{} }

// Provider reports the provider this adapter handles.
func (ClaudeCodeAdapter) Provider() Provider { return ProviderClaudeCode }

// Parse decodes rawNDJSON line-by-line into stream events. The
// ProviderVersion on the returned Trajectory is set to the first
// claude_code_version string found in a system init message; absent
// init message = empty version.
//
// Parse returns an error on the first malformed line, identifying the
// 1-based line number. Empty lines are skipped.
func (a ClaudeCodeAdapter) Parse(rawNDJSON []byte) (Trajectory, error) {
	scanner := bufio.NewScanner(bytes.NewReader(rawNDJSON))
	// SDK messages can be large (long tool results); bump the default
	// 64 KiB line limit to 16 MiB to accommodate realistic runs.
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)

	tr := Trajectory{Provider: a.Provider()}
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var ev StreamEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return Trajectory{}, fmt.Errorf("trajectory: line %d: %w", lineNum, err)
		}
		// Copy raw out of the scanner's buffer before it is reused on
		// the next iteration.
		ev.Raw = append(json.RawMessage(nil), raw...)

		if tr.ProviderVersion == "" && ev.Type == ClaudeTypeSystem && ev.Subtype == ClaudeSubtypeInit {
			var init SDKSystemInit
			if err := ev.Decode(&init); err == nil && init.ClaudeCodeVersion != "" {
				tr.ProviderVersion = "claude-code/" + init.ClaudeCodeVersion
			}
		}
		tr.Events = append(tr.Events, ev)
	}
	if err := scanner.Err(); err != nil {
		return Trajectory{}, fmt.Errorf("trajectory: scan: %w", err)
	}
	return tr, nil
}
