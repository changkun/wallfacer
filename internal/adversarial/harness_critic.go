package adversarial

import (
	"context"
	"time"

	"latere.ai/x/agon/pkg/adversarial"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/runner"
)

// HarnessCritic implements adversarial.Critic using wallfacer's existing
// runner.RunCriticRound infrastructure. Each Round call is a one-shot
// stateless invocation: the critic prompt is assembled by adversarial.AssemblePrompt,
// passed to the harness, and the stdout is returned as CriticResult.Markdown.
//
// The critic harness defaults to harness.Claude; future configuration can
// specify a different harness without any agon-side driver changes.
type HarnessCritic struct {
	runner runner.Interface
	sb     harness.ID
}

// NewHarnessCritic returns a Critic backed by wallfacer's runner.
// sb is the harness to use for critic invocations; pass harness.Claude for
// the default path.
func NewHarnessCritic(r runner.Interface, sb harness.ID) adversarial.Critic {
	return &HarnessCritic{runner: r, sb: sb}
}

// Round assembles the critic prompt and runs it as a one-shot agent call in
// the critic's working directory (in.Cwd), so the critic can read the full
// codebase rather than only the diff patch embedded in the prompt.
func (c *HarnessCritic) Round(ctx context.Context, in adversarial.CriticInput) (*adversarial.CriticResult, error) {
	prompt := adversarial.AssemblePrompt(in)
	deadline := in.Deadline
	if deadline <= 0 {
		deadline = 5 * time.Minute
	}
	text, err := c.runner.RunCriticRound(ctx, prompt, c.sb, in.Cwd, deadline)
	if err != nil {
		return nil, err
	}
	return &adversarial.CriticResult{Markdown: text}, nil
}
