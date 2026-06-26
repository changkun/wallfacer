package adversarial

import (
	"context"
	"strings"

	"latere.ai/x/agon/pkg/adversarial"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/runner"
)

// AgonVerifier implements adversarial.Verifier using agon's Engine.
// It wires a Claude SessionProposer (fork-session) and a HarnessCritic
// (wallfacer runner one-shot) into adversarial.Engine and calls Run.
type AgonVerifier struct {
	runner runner.Interface
	sb     harness.ID
}

// NewAgonVerifier creates a verifier that uses the given runner and harness
// for critic invocations. The proposer always uses Claude fork-session.
func NewAgonVerifier(r runner.Interface, sb harness.ID) adversarial.Verifier {
	return &AgonVerifier{runner: r, sb: sb}
}

// Verify runs adversarial verification on a completed task's implementation.
// Returns nil result when SessionID is empty (no fork-session available).
func (v *AgonVerifier) Verify(ctx context.Context, in adversarial.VerifyInput) (*adversarial.VerifyResult, error) {
	if in.SessionID == "" {
		return nil, nil
	}
	proposer := NewSessionProposer(in.SessionID, in.Cwd)

	engine := &adversarial.Engine{
		StateDir:    in.StateDir,
		Cwd:         in.Cwd,
		ForkCount:   in.ForkCount,
		Proposer:    proposer,
		NewCritic:   func(_ int) adversarial.Critic { return NewHarnessCritic(v.runner, v.sb) },
		MaxRounds:   in.MaxRounds,
		CostCap:     in.CostCapTokens,
		TaskContext: buildTaskContext(in.TaskPrompt, in.Criteria),
		DiffPatch:   in.DiffPatch,
	}

	sum, err := engine.Run(ctx)
	if err != nil {
		return nil, err
	}
	return &adversarial.VerifyResult{
		Unresolved: sum.Unresolved,
		Headline:   sum.Headline,
		SessionDir: sum.SessionDir,
		USD:        sum.USD,
	}, nil
}

// buildTaskContext combines the task prompt and acceptance criteria into
// the TaskContext field agon critics see.
func buildTaskContext(prompt, criteria string) string {
	if criteria == "" {
		return prompt
	}
	return strings.Join([]string{prompt, "## Acceptance Criteria", criteria}, "\n\n")
}
