// Package agentgraph embeds the topos runtime SDK (latere.ai/x/topos) as
// wallfacer's in-process agent-graph execution path. It is the single seam
// through which wallfacer uses topos: no other wallfacer package imports the
// runtime, and nothing imports its engine subpackages, so the dependency stays
// the curated embeddable surface. A boundary test enforces that.
//
// This is the M1 wiring slice (see specs/local/topos-runtime-integration.md). The
// flow/agents -> region adapter, model and sandbox wiring, and lineage mapping
// land in later milestones; here the seam only constructs and runs a region.
package agentgraph

import (
	"context"

	"latere.ai/x/topos"
)

// Runner is wallfacer's wrapper over a topos.Runner.
type Runner struct {
	inner *topos.Runner
}

// NewRunner builds an agent-graph runner from topos options.
func NewRunner(opts topos.Options) (*Runner, error) {
	r, err := topos.NewRunner(opts)
	if err != nil {
		return nil, err
	}
	return &Runner{inner: r}, nil
}

// Run executes a region and returns its result (final text + lineage graph).
func (a *Runner) Run(ctx context.Context, region topos.Region, task string) (topos.RunResult, error) {
	return a.inner.Run(ctx, region, task)
}
