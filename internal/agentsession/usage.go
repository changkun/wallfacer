package agentsession

import (
	"latere.ai/x/wallfacer/internal/pkg/ndjson"
)

// RoundUsage captures the token and cost fields from a single agent-session round's
// stream-json output. It mirrors the shape of agent usage reporting without
// pulling in the internal/store package, so the runtime stays free of a
// persistence dependency.
type RoundUsage struct {
	InputTokens              int
	OutputTokens             int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
	CostUSD                  float64
	StopReason               string
}

// resultLine is the subset of the agent stream-json "result" message that the
// runtime needs to record per-round usage. The field layout matches the
// agentOutput shape used by internal/runner, but we decode locally to avoid
// pulling runner-side types into the runtime.
type resultLine struct {
	Type         string  `json:"type"`
	StopReason   string  `json:"stop_reason"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	Usage        struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

func (r resultLine) toRoundUsage() RoundUsage {
	return RoundUsage{
		InputTokens:              r.Usage.InputTokens,
		OutputTokens:             r.Usage.OutputTokens,
		CacheReadInputTokens:     r.Usage.CacheReadInputTokens,
		CacheCreationInputTokens: r.Usage.CacheCreationInputTokens,
		CostUSD:                  r.TotalCostUSD,
		StopReason:               r.StopReason,
	}
}

// ExtractUsage scans NDJSON output for the final "result" line and returns
// its token and cost fields. It prefers a line with a non-empty stop_reason
// (the terminal result message emitted at the end of a round) and falls
// back to the last well-formed result object if none is present.
//
// Returns ok=false when no usable result line is found.
func ExtractUsage(raw []byte) (RoundUsage, bool) {
	// Scan backwards for the terminal result line. The candidate gate accepts
	// only "type":"result" lines and plain result-shaped lines (single-blob
	// outputs carry no explicit type field), excluding non-result lines from
	// both the terminal match and the fallback.
	obj, ok := ndjson.PreferResultLine(string(raw), true,
		func(r *resultLine) bool { return r.Type == "result" || r.Type == "" },
		func(r *resultLine) bool { return r.StopReason != "" })
	if !ok {
		return RoundUsage{}, false
	}
	return obj.toRoundUsage(), true
}
