package flow

import (
	"context"

	"changkun.de/x/wallfacer/internal/store"
)

// AgentLauncher is the narrow interface the flow Engine uses to
// dispatch a single agent step. The runner satisfies it via a thin
// method wrapper around its existing unexported runAgent; tests
// supply a fake recorder.
//
// Placing the interface here (rather than in internal/runner) keeps
// the engine free of runner imports — Go's structural interface
// satisfaction means the runner does not need to import flow to
// implement it. That break lets runner-flow-integration import
// this package without creating a cycle.
//
// The returned parsed value is whatever the runner's per-agent
// binding produced (today a string for most built-ins). The engine
// stringifies it via fmt.Sprint when feeding it to a downstream
// step's InputFrom.
type AgentLauncher interface {
	RunAgent(ctx context.Context, slug string, task *store.Task, prompt string) (parsed any, err error)
}
