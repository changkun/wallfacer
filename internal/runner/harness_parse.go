package runner

import (
	"fmt"
	"strings"

	"latere.ai/x/wallfacer/internal/harness"
)

// parseAgentStream parses raw agent NDJSON output via the harness for sb.
// Falls back to the legacy harness-agnostic parseOutput when no harness is
// registered for sb (defensive; every production agent type is registered).
func (r *Runner) parseAgentStream(sb harness.ID, raw string) (*agentOutput, error) {
	if h, ok := harness.Lookup(sb); ok {
		return parseHarnessOutput(h, raw)
	}
	return parseOutput(raw)
}

// parseHarnessOutput derives an agentOutput from a harness's per-line
// ParseEvent over the raw NDJSON stdout. This is the harness-based read
// path: the runner no longer hard-codes Claude's wire format (parseOutput)
// nor relies on the codex launcher synthesizing a Claude-shaped result
// line — each harness owns the mapping from its own events to canonical
// harness.Event values, and this accumulator collapses the event stream
// into the result fields the runner acts on.
//
// Semantics mirror the legacy parseOutput:
//   - the terminal result is the LAST KindResult / KindError event (codex
//     and the waiting state both surface as such);
//   - the result text prefers the terminal event's Text, falling back to
//     the last assistant-text event (codex carries its final message as an
//     assistant/result event, not a top-level result string);
//   - session id is taken from any event that carries one (init events
//     expose it even when a run is killed before producing a result);
//   - if no line parses into a recognised event, it is an error, matching
//     parseOutput's "no valid JSON object found".
func parseHarnessOutput(h harness.Harness, raw string) (*agentOutput, error) {
	var (
		terminal     *harness.Event
		sessionID    string
		lastText     string
		sawAnyResult bool
		sawAnyEvent  bool
	)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		evt, err := h.ParseEvent([]byte(line))
		if err != nil {
			continue
		}
		if evt.Kind != harness.KindUnknown {
			sawAnyEvent = true
		}
		if evt.SessionID != "" {
			sessionID = evt.SessionID
		}
		if evt.Text != "" {
			lastText = evt.Text
		}
		if evt.Kind == harness.KindResult || evt.Kind == harness.KindError {
			e := evt
			terminal = &e
			sawAnyResult = true
		}
	}

	if !sawAnyResult {
		if !sawAnyEvent {
			return nil, fmt.Errorf("no valid JSON object found in output")
		}
		// Recognised non-terminal events only (e.g. an init line with a
		// session id but no result yet). Surface what we have so callers
		// that tolerate a missing result still see the session id.
		return &agentOutput{SessionID: sessionID}, nil
	}

	out := &agentOutput{
		SessionID:  terminal.SessionID,
		StopReason: terminal.StopReason,
		IsError:    terminal.Kind == harness.KindError,
		Result:     terminal.Text,
	}
	if out.Result == "" {
		out.Result = lastText
	}
	if out.SessionID == "" {
		out.SessionID = sessionID
	}
	if terminal.Usage != nil {
		out.TotalCostUSD = terminal.Usage.CostUSD
		out.Usage = agentUsage{
			InputTokens:              terminal.Usage.InputTokens,
			OutputTokens:             terminal.Usage.OutputTokens,
			CacheReadInputTokens:     terminal.Usage.CacheReadTokens,
			CacheCreationInputTokens: terminal.Usage.CacheCreationTokens,
		}
	}
	return out, nil
}
