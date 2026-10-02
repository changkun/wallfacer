package agentgraph

import (
	"latere.ai/x/topos"
)

// Test-only views of the topos seam so external tests can assert the
// ModelConfig to topos.Options mapping without running a model.

func ModelOptions(c ModelConfig) (topos.ModelOptions, error) { return modelOptions(c) }

func RunOptions(sessionID string, c ModelConfig) (topos.Options, error) {
	return runOptions(sessionID, c)
}
