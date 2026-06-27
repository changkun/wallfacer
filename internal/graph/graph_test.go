package graph

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/spec"
	"latere.ai/x/wallfacer/internal/store"
)

var (
	t1 = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	t2 = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	t3 = uuid.MustParse("33333333-3333-3333-3333-333333333333")
	t4 = uuid.MustParse("44444444-4444-4444-4444-444444444444")
)

func strptr(s string) *string { return &s }

// fixture builds a representative spec tree + task list exercising all four
// edge kinds, both actions, the blocked set, and the critical path.
//
//	spec:parent (validated, non-leaf)
//	  ├─containment─ spec:childA (validated leaf, undispatched)  -> dispatch action
//	  └─containment─ spec:childB (validated leaf, dispatched->t1)
//	  spec_dep: childA -> childB
//	  dispatch: childB -> task:t1 (in_progress)
//	  task_dep: t1 -> t2 (backlog, blocked since t1 not done)
//	  task:t3 (backlog, no deps) -> start action
func fixture() ([]spec.NodeResponse, []store.Task) {
	specs := []spec.NodeResponse{
		{
			Path:     "specs/x/parent.md",
			Children: []string{"specs/x/parent/childA.md", "specs/x/parent/childB.md"},
			IsLeaf:   false,
			Depth:    0,
			Spec:     &spec.Spec{Title: "Parent", Status: spec.StatusValidated},
		},
		{
			Path:   "specs/x/parent/childA.md",
			IsLeaf: true,
			Depth:  1,
			Spec:   &spec.Spec{Title: "Child A", Status: spec.StatusValidated},
		},
		{
			Path:   "specs/x/parent/childB.md",
			IsLeaf: true,
			Depth:  1,
			Spec: &spec.Spec{
				Title:            "Child B",
				Status:           spec.StatusValidated,
				DependsOn:        []string{"specs/x/parent/childA.md"},
				DispatchedTaskID: strptr(t1.String()),
			},
		},
	}
	tasks := []store.Task{
		{ID: t1, Title: "Task 1", Status: store.TaskStatusInProgress, SpecSourcePath: "specs/x/parent/childB.md"},
		{ID: t2, Title: "Task 2", Status: store.TaskStatusBacklog, DependsOn: []string{t1.String()}},
		{ID: t3, Title: "Task 3", Status: store.TaskStatusBacklog},
	}
	return specs, tasks
}

func edgeSet(g Graph) map[Edge]bool {
	m := make(map[Edge]bool, len(g.Edges))
	for _, e := range g.Edges {
		m[e] = true
	}
	return m
}

func nodeByID(g Graph, id string) (Node, bool) {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

func TestBuild_NodesAndEdges(t *testing.T) {
	specs, tasks := fixture()
	g := Build(specs, tasks, false)

	if len(g.Nodes) != 6 {
		t.Fatalf("want 6 nodes, got %d", len(g.Nodes))
	}
	es := edgeSet(g)
	want := []Edge{
		{From: SpecID("specs/x/parent.md"), To: SpecID("specs/x/parent/childA.md"), Kind: EdgeContainment},
		{From: SpecID("specs/x/parent.md"), To: SpecID("specs/x/parent/childB.md"), Kind: EdgeContainment},
		{From: SpecID("specs/x/parent/childA.md"), To: SpecID("specs/x/parent/childB.md"), Kind: EdgeSpecDep},
		{From: SpecID("specs/x/parent/childB.md"), To: TaskID(t1.String()), Kind: EdgeDispatch},
		{From: TaskID(t1.String()), To: TaskID(t2.String()), Kind: EdgeTaskDep},
	}
	for _, e := range want {
		if !es[e] {
			t.Errorf("missing edge %+v", e)
		}
	}
	if len(g.Edges) != len(want) {
		t.Errorf("want %d edges, got %d: %+v", len(want), len(g.Edges), g.Edges)
	}
}

func TestBuild_AvailableActions(t *testing.T) {
	specs, tasks := fixture()
	g := Build(specs, tasks, false)

	cases := map[string][]string{
		SpecID("specs/x/parent.md"):        nil,                // validated non-leaf: nothing in the flow
		SpecID("specs/x/parent/childA.md"): {ActionDispatch},   // validated leaf, undispatched
		SpecID("specs/x/parent/childB.md"): {ActionUndispatch}, // validated, already dispatched
		TaskID(t2.String()):                nil,                // backlog but blocked
		TaskID(t3.String()):                {ActionStart},      // backlog, ready
	}
	for id, want := range cases {
		n, ok := nodeByID(g, id)
		if !ok {
			t.Fatalf("node %s missing", id)
		}
		if !reflect.DeepEqual(n.AvailableActions, want) {
			t.Errorf("%s actions = %v, want %v", id, n.AvailableActions, want)
		}
	}
}

func TestSpecActions_PerState(t *testing.T) {
	leaf := func(st spec.Status, dispatched bool) *spec.Spec {
		s := &spec.Spec{Status: st}
		if dispatched {
			s.DispatchedTaskID = strptr(t1.String())
		}
		return s
	}
	cases := []struct {
		name     string
		s        *spec.Spec
		isLeaf   bool
		expected []string
	}{
		{"drafted", leaf(spec.StatusDrafted, false), true, []string{ActionValidate}},
		{"validated-leaf", leaf(spec.StatusValidated, false), true, []string{ActionDispatch}},
		{"validated-nonleaf", leaf(spec.StatusValidated, false), false, nil},
		{"validated-dispatched", leaf(spec.StatusValidated, true), true, []string{ActionUndispatch}},
		{"testing", leaf(spec.StatusTesting, false), true, []string{ActionForceComplete}},
		{"stale", leaf(spec.StatusStale, false), true, []string{ActionUnstale}},
		{"archived", leaf(spec.StatusArchived, false), true, []string{ActionUnarchive}},
		{"complete", leaf(spec.StatusComplete, false), true, nil}, // terminal in the flow
		{"vague", leaf(spec.StatusVague, false), true, nil},       // advances via the agent, not a server transition
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := specActions(c.s, c.isLeaf)
			if !reflect.DeepEqual(got, c.expected) {
				t.Errorf("specActions(%s) = %v, want %v", c.name, got, c.expected)
			}
		})
	}
}

// TestSpecActions_RespectLifecycleMachine guards against drift: every
// state-changing action the builder offers must correspond to a legal edge in
// the canonical spec.StatusMachine.
func TestSpecActions_RespectLifecycleMachine(t *testing.T) {
	// action → the lifecycle target it drives (dispatch/undispatch change no
	// spec status, so they are exempt).
	target := map[string]spec.Status{
		ActionValidate:      spec.StatusValidated,
		ActionForceComplete: spec.StatusComplete,
		ActionUnarchive:     spec.StatusDrafted,
	}
	for _, st := range spec.ValidStatuses() {
		for _, act := range specActions(&spec.Spec{Status: st}, true) {
			to, ok := target[act]
			if !ok {
				continue // dispatch/undispatch/unstale: not a single fixed edge
			}
			if !spec.StatusMachine.CanTransition(st, to) {
				t.Errorf("action %q offered in state %q but %q→%q is not a legal edge", act, st, st, to)
			}
		}
	}
}

func TestBuild_Blocked(t *testing.T) {
	specs, tasks := fixture()
	g := Build(specs, tasks, false)
	if !reflect.DeepEqual(g.Blocked, []string{TaskID(t2.String())}) {
		t.Errorf("blocked = %v, want [%s]", g.Blocked, TaskID(t2.String()))
	}

	// When the prerequisite is done, t2 is no longer blocked and gains start.
	tasks[0].Status = store.TaskStatusDone
	g = Build(specs, tasks, false)
	if len(g.Blocked) != 0 {
		t.Errorf("blocked = %v, want empty after prereq done", g.Blocked)
	}
	n, _ := nodeByID(g, TaskID(t2.String()))
	if !reflect.DeepEqual(n.AvailableActions, []string{ActionStart}) {
		t.Errorf("t2 actions = %v, want [start] after prereq done", n.AvailableActions)
	}
}

func TestBuild_ArchivedDonePrereqDoesNotBlock(t *testing.T) {
	// A backlog task whose only prerequisite is done-and-archived must read as
	// ready in the default (archived-hidden) view: readiness is computed over
	// the full task set, not the displayed subset.
	tasks := []store.Task{
		{ID: t1, Status: store.TaskStatusDone, Archived: true},
		{ID: t2, Status: store.TaskStatusBacklog, DependsOn: []string{t1.String()}},
	}
	g := Build(nil, tasks, false)

	if _, shown := nodeByID(g, TaskID(t1.String())); shown {
		t.Error("archived prerequisite should be hidden in default view")
	}
	if len(g.Blocked) != 0 {
		t.Errorf("blocked = %v, want empty (prereq is done, just archived)", g.Blocked)
	}
	n, ok := nodeByID(g, TaskID(t2.String()))
	if !ok {
		t.Fatal("t2 node missing")
	}
	if !reflect.DeepEqual(n.AvailableActions, []string{ActionStart}) {
		t.Errorf("t2 actions = %v, want [start]", n.AvailableActions)
	}
}

func TestBuild_CriticalPath(t *testing.T) {
	specs, tasks := fixture()
	g := Build(specs, tasks, false)
	// Containment (parent → child) is organizational and excluded; the chain
	// follows only dependency edges: spec_dep, dispatch, task_dep.
	want := []string{
		SpecID("specs/x/parent/childA.md"),
		SpecID("specs/x/parent/childB.md"),
		TaskID(t1.String()),
		TaskID(t2.String()),
	}
	if !reflect.DeepEqual(g.CriticalPath, want) {
		t.Errorf("critical path = %v, want %v", g.CriticalPath, want)
	}
}

func TestBuild_CriticalPathIgnoresContainmentStar(t *testing.T) {
	// A parent with many dependency-less children has no real dependency chain;
	// containment hops must not fabricate one.
	specs := []spec.NodeResponse{
		{Path: "p.md", Children: []string{"a.md", "b.md", "c.md"}, Spec: &spec.Spec{Status: spec.StatusDrafted}},
		{Path: "a.md", IsLeaf: true, Spec: &spec.Spec{Status: spec.StatusDrafted}},
		{Path: "b.md", IsLeaf: true, Spec: &spec.Spec{Status: spec.StatusDrafted}},
		{Path: "c.md", IsLeaf: true, Spec: &spec.Spec{Status: spec.StatusDrafted}},
	}
	g := Build(specs, nil, false)
	if len(g.CriticalPath) != 0 {
		t.Errorf("critical path = %v, want empty (containment is not a chain)", g.CriticalPath)
	}
}

func TestBuild_ArchivedExcludedByDefault(t *testing.T) {
	specs, tasks := fixture()
	specs = append(specs, spec.NodeResponse{
		Path:   "specs/x/old.md",
		IsLeaf: true,
		Spec:   &spec.Spec{Title: "Old", Status: spec.StatusArchived},
	})
	tasks = append(tasks, store.Task{ID: t4, Title: "Archived task", Status: store.TaskStatusDone, Archived: true})

	g := Build(specs, tasks, false)
	if _, ok := nodeByID(g, SpecID("specs/x/old.md")); ok {
		t.Error("archived spec should be excluded by default")
	}
	if _, ok := nodeByID(g, TaskID(t4.String())); ok {
		t.Error("archived task should be excluded by default")
	}

	g = Build(specs, tasks, true)
	if _, ok := nodeByID(g, SpecID("specs/x/old.md")); !ok {
		t.Error("archived spec should appear when includeArchived")
	}
	if _, ok := nodeByID(g, TaskID(t4.String())); !ok {
		t.Error("archived task should appear when includeArchived")
	}
}

func TestBuild_SkipsDocNodes(t *testing.T) {
	specs := []spec.NodeResponse{
		{Path: "specs/readme.md", IsLeaf: true, Spec: &spec.Spec{Doc: true}},
	}
	g := Build(specs, nil, false)
	if len(g.Nodes) != 0 {
		t.Errorf("doc node should be skipped, got %d nodes", len(g.Nodes))
	}
}

func TestBuild_DanglingEdgesDropped(t *testing.T) {
	// A spec depends_on a spec that isn't in the set; a task depends_on an
	// absent task; a dispatch points at an absent task. None should produce an
	// edge.
	specs := []spec.NodeResponse{
		{Path: "specs/a.md", IsLeaf: true, Spec: &spec.Spec{
			Status:           spec.StatusValidated,
			DependsOn:        []string{"specs/missing.md"},
			DispatchedTaskID: strptr(t4.String()),
		}},
	}
	tasks := []store.Task{
		{ID: t2, Status: store.TaskStatusBacklog, DependsOn: []string{t4.String()}},
	}
	g := Build(specs, tasks, false)
	if len(g.Edges) != 0 {
		t.Errorf("dangling edges should be dropped, got %+v", g.Edges)
	}
}
