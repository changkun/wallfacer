package trajectory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
)

// CodexAdapter parses the NDJSON output of `codex exec --json`. One
// instance is safe to reuse across trajectories.
//
// Note: the Codex CLI does not advertise its version in-stream the way
// Claude Code does via the system.init message, so ProviderVersion on
// the returned Trajectory is left empty. Callers that need it should
// capture `codex --version` out-of-band at container launch and
// populate ProviderVersion themselves.
type CodexAdapter struct{}

// NewCodexAdapter returns a zero-configuration Codex adapter.
func NewCodexAdapter() CodexAdapter { return CodexAdapter{} }

// Provider reports the provider this adapter handles.
func (CodexAdapter) Provider() Provider { return ProviderCodex }

// Parse decodes rawNDJSON line-by-line into stream events. Returns an
// error on the first malformed line, identifying the 1-based line
// number. Empty lines are skipped.
func (a CodexAdapter) Parse(rawNDJSON []byte) (Trajectory, error) {
	scanner := bufio.NewScanner(bytes.NewReader(rawNDJSON))
	// Codex items can carry full command output; bump the default
	// 64 KiB line limit to 16 MiB.
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
		tr.Events = append(tr.Events, ev)
	}
	if err := scanner.Err(); err != nil {
		return Trajectory{}, fmt.Errorf("trajectory: scan: %w", err)
	}
	return tr, nil
}
