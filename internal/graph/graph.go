// Package graph builds the unified spec+task dependency graph that the Map
// surface renders and drives. It is the authoritative, server-side replacement
// for the node/edge derivation that used to live in the vendored client-side
// depgraph renderer: pure functions of a spec tree plus the task list, with no
// store access or mutation, so the whole thing is table-testable.
package graph

import (
	"latere.ai/x/wallfacer/internal/spec"
	"latere.ai/x/wallfacer/internal/store"
)

// NodeKind distinguishes spec nodes from task nodes.
type NodeKind string

// Node kinds.
const (
	NodeSpec NodeKind = "spec"
	NodeTask NodeKind = "task"
)

// EdgeKind enumerates the four relationship types the graph draws.
type EdgeKind string

// Edge kinds.
const (
	// EdgeContainment is a parent spec → child spec tree edge.
	EdgeContainment EdgeKind = "containment"
	// EdgeDispatch is a leaf spec → the task it materialized.
	EdgeDispatch EdgeKind = "dispatch"
	// EdgeSpecDep is a prerequisite spec → a spec that depends_on it.
	EdgeSpecDep EdgeKind = "spec_dep"
	// EdgeTaskDep is a prerequisite task → a task that depends_on it.
	EdgeTaskDep EdgeKind = "task_dep"
)

// Action names the inline operations the Map may offer on a node. Each one only
// *reports* what the existing transition / task APIs already allow — the graph
// never introduces a new lifecycle transition.
// Action names mirror the verbs the spec-transition API (POST
// /api/specs/transition) and the task routes already accept, so the client can
// fire a node action without translating. The graph only *reports* which are
// legal for a node's current state; the server still enforces them.
const (
	ActionDispatch      = "dispatch"       // validated, undispatched leaf spec → board
	ActionUndispatch    = "undispatch"     // dispatched spec → cancel its task + clear link
	ActionValidate      = "validate"       // drafted spec → validated
	ActionUnstale       = "unstale"        // stale spec → drafted/validated
	ActionForceComplete = "force-complete" // testing spec → complete (skip the drift gate)
	ActionUnarchive     = "unarchive"      // archived spec → drafted
	ActionStart         = "start"          // ready (unblocked) backlog task → in_progress
)

// Node is one vertex of the unified graph.
type Node struct {
	ID               string   `json:"id"`     // "spec:<path>" or "task:<uuid>"
	Kind             NodeKind `json:"kind"`   // spec | task
	Label            string   `json:"label"`  // display label
	Status           string   `json:"status"` // spec lifecycle or task status
	Ref              string   `json:"ref"`    // spec path or task id, for deep-jumps + actions
	Depth            int      `json:"depth"`  // tree depth hint (specs); 0 for tasks
	AvailableActions []string `json:"available_actions,omitempty"`
}

// Edge is one directed relationship; direction is prerequisite → dependent so a
// longest-path walk follows the flow of work.
type Edge struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Kind EdgeKind `json:"kind"`
}

// Graph is the full serialized response for GET /api/graph.
type Graph struct {
	Nodes        []Node   `json:"nodes"`
	Edges        []Edge   `json:"edges"`
	CriticalPath []string `json:"critical_path"` // longest dependency chain (node IDs)
	Blocked      []string `json:"blocked"`       // node IDs whose prerequisites are unmet
}

// SpecID and TaskID are the node-ID conventions, exported so the handler and
// tests don't reinvent the prefixes.
func SpecID(path string) string { return "spec:" + path }

// TaskID returns the node ID for a task UUID string.
func TaskID(id string) string { return "task:" + id }

// Build assembles the unified graph from a spec tree (as returned by
// spec.SerializeTree / Handler.collectSpecTree) and the task list. When
// includeArchived is false, archived specs and archived tasks are excluded
// (matching the Map's "Show archived" toggle). Free-form doc nodes (no
// lifecycle, no edges) are always excluded from the graph.
func Build(specs []spec.NodeResponse, tasks []store.Task, includeArchived bool) Graph {
	g := Graph{Nodes: []Node{}, Edges: []Edge{}, CriticalPath: []string{}, Blocked: []string{}}

	// Readiness is computed over the FULL task set, independent of which nodes
	// are displayed: a prerequisite that is done-and-archived must still count
	// as satisfied even when archived nodes are hidden, or a ready backlog task
	// would wrongly read as blocked in the default view.
	taskDone := func(id string) bool {
		for _, t := range tasks {
			if t.ID.String() == id {
				return t.Status == store.TaskStatusDone
			}
		}
		return false // unknown / absent prerequisite is unmet
	}

	// Display set: which tasks render (and therefore which task-incident edges
	// are kept). Edges to a hidden task are dropped as dangling.
	shown := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		if t.Archived && !includeArchived {
			continue
		}
		shown[t.ID.String()] = true
	}

	// --- spec nodes + spec edges ---
	specPresent := make(map[string]bool, len(specs))
	for _, n := range specs {
		if n.Spec == nil || n.Spec.Doc {
			continue
		}
		if n.Spec.Status == spec.StatusArchived && !includeArchived {
			continue
		}
		specPresent[n.Path] = true
	}

	for _, n := range specs {
		s := n.Spec
		if s == nil || s.Doc {
			continue
		}
		if s.Status == spec.StatusArchived && !includeArchived {
			continue
		}
		id := SpecID(n.Path)
		node := Node{
			ID:     id,
			Kind:   NodeSpec,
			Label:  specLabel(n),
			Status: string(s.Status),
			Ref:    n.Path,
			Depth:  n.Depth,
		}
		node.AvailableActions = specActions(s, n.IsLeaf)
		g.Nodes = append(g.Nodes, node)

		// containment: parent → each child spec present in the set.
		for _, child := range n.Children {
			if specPresent[child] {
				g.Edges = append(g.Edges, Edge{From: id, To: SpecID(child), Kind: EdgeContainment})
			}
		}
		// spec_dep: each prerequisite spec → this spec.
		for _, dep := range s.DependsOn {
			if specPresent[dep] {
				g.Edges = append(g.Edges, Edge{From: SpecID(dep), To: id, Kind: EdgeSpecDep})
			}
		}
		// dispatch: leaf spec → its materialized task, when displayed.
		if s.DispatchedTaskID != nil && shown[*s.DispatchedTaskID] {
			g.Edges = append(g.Edges, Edge{From: id, To: TaskID(*s.DispatchedTaskID), Kind: EdgeDispatch})
		}
	}

	// --- task nodes + task_dep edges + blocked set + start action ---
	// Iterate the original slice (not the map) for deterministic ordering.
	for _, t := range tasks {
		if t.Archived && !includeArchived {
			continue
		}
		tid := t.ID.String()
		id := TaskID(tid)

		blocked := false
		for _, dep := range t.DependsOn {
			if !taskDone(dep) {
				blocked = true
			}
			if shown[dep] {
				g.Edges = append(g.Edges, Edge{From: TaskID(dep), To: id, Kind: EdgeTaskDep})
			}
		}

		node := Node{
			ID:     id,
			Kind:   NodeTask,
			Label:  taskLabel(t),
			Status: string(t.Status),
			Ref:    tid,
		}
		if t.Status == store.TaskStatusBacklog && blocked {
			g.Blocked = append(g.Blocked, id)
		}
		// Start action: a ready (unblocked) backlog task can be promoted.
		if t.Status == store.TaskStatusBacklog && !blocked {
			node.AvailableActions = append(node.AvailableActions, ActionStart)
		}
		g.Nodes = append(g.Nodes, node)
	}

	g.CriticalPath = criticalPath(g.Nodes, g.Edges)
	return g
}

// specActions returns the coordination-flow verbs legal for a spec in its
// current state: the forward path (validate → dispatch → run → complete) plus
// its reversals (undispatch, unstale, unarchive). Each verb that maps to a
// lifecycle edge is checked against the canonical spec.StatusMachine so the
// graph never drifts from the lifecycle; dispatch/undispatch (which create or
// cancel a task without changing the spec's own status) are gated on the
// leaf/dispatched flags, mirroring internal/handler/specs_dispatch.go.
//
// Maintenance verbs (mark-stale, archive) are intentionally omitted from the
// inline menu — they stay available via Plan and the transition API — to keep
// the node menu focused on the pipeline flow.
func specActions(s *spec.Spec, isLeaf bool) []string {
	if s == nil {
		return nil
	}
	can := func(to spec.Status) bool { return spec.StatusMachine.CanTransition(s.Status, to) }
	dispatched := s.DispatchedTaskID != nil

	var a []string
	switch s.Status {
	case spec.StatusDrafted:
		if can(spec.StatusValidated) {
			a = append(a, ActionValidate)
		}
	case spec.StatusValidated:
		switch {
		case dispatched:
			a = append(a, ActionUndispatch)
		case isLeaf:
			a = append(a, ActionDispatch)
		}
	case spec.StatusTesting:
		if can(spec.StatusComplete) {
			a = append(a, ActionForceComplete)
		}
	case spec.StatusStale:
		if can(spec.StatusValidated) || can(spec.StatusDrafted) {
			a = append(a, ActionUnstale)
		}
	case spec.StatusArchived:
		if can(spec.StatusDrafted) {
			a = append(a, ActionUnarchive)
		}
	}
	return a
}

// specLabel prefers the spec title, falling back to the path so a titleless
// spec still reads.
func specLabel(n spec.NodeResponse) string {
	if n.Spec != nil && n.Spec.Title != "" {
		return n.Spec.Title
	}
	return n.Path
}

// taskLabel prefers the task title, falling back to a short UUID prefix.
func taskLabel(t store.Task) string {
	if t.Title != "" {
		return t.Title
	}
	id := t.ID.String()
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// criticalPath returns the longest directed chain (as a node-ID sequence)
// along *dependency* edges across the combined spec+task DAG. Containment is
// organizational, not work-ordering, so it is excluded — otherwise a spec with
// many independent children would report a spurious chain through them. Edges
// referencing absent nodes are ignored; cycles (which should not occur in a
// well-formed tree) are guarded so the walk always terminates. A path of fewer
// than two nodes is reported as empty, since a single node is not a chain.
func criticalPath(nodes []Node, edges []Edge) []string {
	present := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		present[n.ID] = true
	}
	adj := make(map[string][]string)
	for _, e := range edges {
		if e.Kind == EdgeContainment {
			continue // organizational, not a work-ordering hop
		}
		if present[e.From] && present[e.To] {
			adj[e.From] = append(adj[e.From], e.To)
		}
	}

	type result struct {
		length int
		next   string
	}
	memo := make(map[string]result)
	onStack := make(map[string]bool)

	var walk func(id string) result
	walk = func(id string) result {
		if r, ok := memo[id]; ok {
			return r
		}
		if onStack[id] {
			return result{0, ""} // cycle guard
		}
		onStack[id] = true
		best := result{0, ""}
		for _, nx := range adj[id] {
			r := walk(nx)
			if r.length+1 > best.length {
				best = result{r.length + 1, nx}
			}
		}
		onStack[id] = false
		memo[id] = best
		return best
	}

	bestStart, bestLen := "", -1
	for _, n := range nodes {
		if r := walk(n.ID); r.length > bestLen {
			bestLen, bestStart = r.length, n.ID
		}
	}
	if bestStart == "" {
		return []string{}
	}
	path := []string{bestStart}
	for cur := bestStart; ; {
		r := memo[cur]
		if r.next == "" {
			break
		}
		path = append(path, r.next)
		cur = r.next
	}
	if len(path) < 2 {
		return []string{}
	}
	return path
}
