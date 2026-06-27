package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/harness"
)

// CriticRoundResult is the output of one agon critic round: the markdown text
// plus the token usage and cost the agent reported, so callers can report it
// back to agon (which records it in the session's end.json) and attribute the
// spend to the task.
type CriticRoundResult struct {
	Text              string
	InputTokens       int
	OutputTokens      int
	CacheReadTokens   int
	CacheCreateTokens int
	CostUSD           float64
}

// RunCriticRound runs a one-shot stateless agent invocation with the given
// prompt and returns the raw markdown text plus the agent's reported usage. It
// is the runner-side entry point for HarnessCritic.Round calls: no task
// context, no session resumption, no span events. The usage is surfaced (not
// dropped) so agon's accounting includes the critic rather than undercounting
// it — wallfacer runs the critics, so it is the only place that sees their
// token spend.
//
// The deadline parameter caps total wall time; callers should set it from
// CriticInput.Deadline (default 5 minutes in the engine).
// The sb parameter selects which harness drives the critic invocation;
// pass harness.Claude for the default path.
func (r *Runner) RunCriticRound(ctx context.Context, prompt string, sb harness.ID, cwd string, deadline time.Duration) (CriticRoundResult, error) {
	if deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, deadline)
		defer cancel()
	}

	containerName := "wallfacer-agon-critic-" + uuid.NewString()[:8]
	labels := map[string]string{"wallfacer.task.activity": "agon_critic"}

	output, err := r.runOneShotContainer(ctx, containerName, prompt, sb, cwd, labels)
	if err != nil {
		return CriticRoundResult{}, fmt.Errorf("agon critic: %w", err)
	}
	if output == nil {
		return CriticRoundResult{}, fmt.Errorf("agon critic: nil output")
	}
	if output.IsError {
		msg := strings.TrimSpace(output.Result)
		if msg == "" {
			msg = "agent returned an error result"
		}
		return CriticRoundResult{}, fmt.Errorf("agon critic: %s", msg)
	}
	return CriticRoundResult{
		Text:              output.Result,
		InputTokens:       output.Usage.InputTokens,
		OutputTokens:      output.Usage.OutputTokens,
		CacheReadTokens:   output.Usage.CacheReadInputTokens,
		CacheCreateTokens: output.Usage.CacheCreationInputTokens,
		CostUSD:           output.TotalCostUSD,
	}, nil
}
