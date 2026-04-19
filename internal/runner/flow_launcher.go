package runner

import (
	"context"
	"fmt"

	"changkun.de/x/wallfacer/internal/store"
)

// RunAgent is the flow.AgentLauncher adapter around the unexported
// runAgent. The flow engine drives non-implement / non-brainstorm
// flows by walking the step chain and calling this method once per
// step.
//
// Keeping the public shape narrow (slug string + task + prompt)
// keeps the flow package free of runner internals — opts, circuit
// breakers, container-registry hooks all stay on the runner side.
// The returned value is the binding's ParseResult output; for
// single-turn text roles this is the assistant's final text.
func (r *Runner) RunAgent(ctx context.Context, slug string, task *store.Task, prompt string) (any, error) {
	if r.agentsReg == nil {
		return nil, fmt.Errorf("runner: agents registry not initialised")
	}
	role, ok := r.agentsReg.Get(slug)
	if !ok {
		return nil, fmt.Errorf("runner: unknown agent slug %q", slug)
	}
	res, err := r.runAgent(ctx, role, task, prompt, runAgentOpts{
		Context:        ctx,
		EmitSpanEvents: true,
		TrackUsage:     task != nil,
		Turn:           1,
	})
	if err != nil {
		return nil, err
	}
	return res.Parsed, nil
}
