// Package agentgraph embeds the topos runtime SDK (latere.ai/x/topos) as
// wallfacer's in-process execution path: the native Topos harness runs a task
// as one agent through RunAgent, on the model a ModelConfig selects, in the
// task's worktree. The package maps the runtime's options, events and result
// onto topos-free types (ModelConfig, Event, Result), so the runner consumes a
// run without naming a topos type. No wallfacer package imports a topos engine
// subpackage outside a designated seam; a boundary test enforces that.
package agentgraph

import (
	"context"
	"errors"

	"latere.ai/x/topos"
)

// errNoModelSelected reports topos options that name no model. The runtime
// resolves such options to its test model, so the seam refuses them: the test
// model is reachable only by naming it.
var errNoModelSelected = errors.New("agentgraph: options select no model")

// Runner is wallfacer's wrapper over a topos.Runner.
type Runner struct {
	inner *topos.Runner
}

// NewRunner builds an agent-graph runner from topos options. Options that
// select no model (no kind and no client) return an error.
func NewRunner(opts topos.Options) (*Runner, error) {
	if opts.Model.Kind == "" && opts.Model.Client == nil {
		return nil, errNoModelSelected
	}
	r, err := topos.NewRunner(opts)
	if err != nil {
		return nil, err
	}
	return &Runner{inner: r}, nil
}

// Run executes a region and returns its result (final text + trace graph).
func (a *Runner) Run(ctx context.Context, region topos.Region, task string) (topos.RunResult, error) {
	return a.inner.Run(ctx, region, task)
}
