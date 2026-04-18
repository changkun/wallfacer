// Package agents defines the declarative role descriptors the runner's
// runAgent primitive dispatches on, along with the seven built-in
// agent roles wallfacer ships today (title, oversight, commit message,
// refinement, ideation, implementation, testing).
//
// The package intentionally carries no execution logic: it is a pure
// data catalog. internal/runner owns the container launch machinery
// and consumes Role values through runAgent; internal/flow composes
// Role values into pipelines without depending on the runner. Moving
// these types out of internal/runner is what lets a future Flows
// subsystem and the user-facing Agents / Flows sidebar tabs depend on
// "what an agent is" without dragging the full execution stack along.
//
// BuiltinAgents is the exported catalog the handler layer renders for
// GET /api/agents. User-authored agents from ~/.wallfacer/agents/*.yaml
// merge with the built-ins in a later task (see the editable-agents
// child spec); this package is the authoritative registration point.
package agents
