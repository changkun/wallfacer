package adversarial

import (
	"context"
	"time"

	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/toposadv"
)

// HarnessCritic implements toposadv.Critic using wallfacer's existing
// runner.RunCriticRound infrastructure. Each Round call is a one-shot
// stateless invocation: the critic prompt is assembled by toposadv.AssemblePrompt,
// passed to the harness, and the stdout is returned as CriticResult.Markdown.
//
// The critic harness defaults to harness.Claude; future configuration can
// specify a different harness without any review-side driver changes.
type HarnessCritic struct {
	runner runner.Interface
	sb     harness.ID
	cwd    string // worktree the critic runs in; overrides CriticInput.Cwd
}

// NewHarnessCritic returns a Critic backed by wallfacer's runner.
// sb is the harness to use for critic invocations; pass harness.Claude for
// the default path. cwd is the working directory the critic runs in (a
// throwaway worktree); when empty, the critic falls back to CriticInput.Cwd.
func NewHarnessCritic(r runner.Interface, sb harness.ID, cwd string) toposadv.Critic {
	return &HarnessCritic{runner: r, sb: sb, cwd: cwd}
}

// Round assembles the critic prompt and runs it as a one-shot agent call in
// the critic's working directory (in.Cwd), so the critic can read the full
// codebase rather than only the diff patch embedded in the prompt.
func (c *HarnessCritic) Round(ctx context.Context, in toposadv.CriticInput) (*toposadv.CriticResult, error) {
	prompt := toposadv.AssemblePrompt(in)
	deadline := in.Deadline
	if deadline <= 0 {
		deadline = 5 * time.Minute
	}
	cwd := c.cwd
	if cwd == "" {
		cwd = in.Cwd
	}
	res, err := c.runner.RunCriticRound(ctx, prompt, c.sb, cwd, deadline)
	if err != nil {
		return nil, err
	}
	// Report the critic's token usage and cost back to review so its session
	// accounting (end.json, summary USD) includes the critic instead of
	// undercounting it. wallfacer runs the critics, so this is the only place
	// the spend is visible.
	return &toposadv.CriticResult{
		Markdown: res.Text,
		Tokens:   res.InputTokens + res.OutputTokens,
		Usage: toposadv.TokenUsage{
			Input:       res.InputTokens,
			Output:      res.OutputTokens,
			CacheRead:   res.CacheReadTokens,
			CacheCreate: res.CacheCreateTokens,
		},
		USD: res.CostUSD,
	}, nil
}
