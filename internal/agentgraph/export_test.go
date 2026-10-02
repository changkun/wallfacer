package agentgraph

import (
	"context"

	"latere.ai/x/topos"

	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/flow"
)

// Test-only views of the topos seam so external tests can assert the
// ModelConfig and flow to topos.Options mapping without running a model.

func ModelOptions(c ModelConfig) (topos.ModelOptions, error) { return modelOptions(c) }

func RunOptions(sessionID string, c ModelConfig, f flow.Flow) (topos.Options, error) {
	return runOptions(sessionID, c, f)
}

// RunFlowFake runs a flow with the deterministic test model, named explicitly:
// no worktree, no observer.
func RunFlowFake(ctx context.Context, sessionID string, f flow.Flow, reg *agents.Registry, prompt string) (Result, error) {
	return RunFlowWithModel(ctx, sessionID, ModelConfig{Mode: ModelModeFake}, f, reg, prompt, "", nil)
}
