// Package flow defines the Flow primitive: an ordered composition of
// sub-agent roles from internal/agents. A Flow is a user-facing
// declaration (pick a sequence of agents, declare which can run in
// parallel, mark which are optional). The flow engine — which lives
// in a sibling task — walks a Flow at execute time and drives the
// underlying agents via the runner's binding table.
//
// This package owns the data model and the built-in catalog. It
// exposes no execution code, no HTTP surface, and no runner wiring;
// those are sibling tasks on the agents-and-flows track.
package flow
