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
		SpecID("specs/x/parent.md"):          nil,               // non-leaf: no dispatch
		SpecID("specs/x/parent/childA.md"):   {ActionDispatch},  // validated leaf, undispatched
		SpecID("specs/x/parent/childB.md"):   nil,               // already dispatched
		TaskID(t2.String()):                  nil,               // backlog but blocked
		TaskID(t3.String()):                  {ActionStart},     // backlog, ready
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

func TestBuild_CriticalPath(t *testing.T) {
	specs, tasks := fixture()
	g := Build(specs, tasks, false)
	want := []string{
		SpecID("specs/x/parent.md"),
		SpecID("specs/x/parent/childA.md"),
		SpecID("specs/x/parent/childB.md"),
		TaskID(t1.String()),
		TaskID(t2.String()),
	}
	if !reflect.DeepEqual(g.CriticalPath, want) {
		t.Errorf("critical path = %v, want %v", g.CriticalPath, want)
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
