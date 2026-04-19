package runner

import (
	"context"
	"testing"

	"changkun.de/x/wallfacer/internal/agents"
	"changkun.de/x/wallfacer/internal/flow"
	"changkun.de/x/wallfacer/internal/store"
)

// TestRunner_FlowRegistryResolvesTasks verifies the flow registry
// attached to a Runner maps tasks to the right slug: explicit FlowID
// wins, legacy Kind falls back to its seeded flow, and everything
// else defaults to "implement".
func TestRunner_FlowRegistryResolvesTasks(t *testing.T) {
	_, _, _ = newAgentTestRunner(t) // ensure package init works
	r := &Runner{flows: flow.NewBuiltinRegistry()}

	cases := []struct {
		name string
		task *store.Task
		want string
	}{
		{"nil task", nil, "implement"},
		{"empty task", &store.Task{}, "implement"},
		{"legacy idea-agent kind", &store.Task{Kind: store.TaskKindIdeaAgent}, "brainstorm"},
		{"explicit FlowID wins over kind", &store.Task{FlowID: "refine-only", Kind: store.TaskKindIdeaAgent}, "refine-only"},
		{"unknown FlowID passes through", &store.Task{FlowID: "custom-flow"}, "custom-flow"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := r.flows.ResolveForTask(c.task)
			if got != c.want {
				t.Errorf("ResolveForTask = %q, want %q", got, c.want)
			}
		})
	}
}

// TestRunner_RunAgentAdapterResolvesSlug verifies the flow.AgentLauncher
// adapter looks up the agents registry by slug and dispatches through
// the runner's existing runAgent machinery. Unknown slugs surface a
// clear error rather than running with zero-value fields.
func TestRunner_RunAgentAdapterResolvesSlug(t *testing.T) {
	r, backend, _ := newAgentTestRunner(t)
	role := makeTestRole(t, "t-adapter", mountNone)

	// Replace the built-in registry with one that exposes our test
	// role under its own slug. The built-in registry is agents-only,
	// so we install the test role alongside it by swapping out to a
	// registry that knows "t-adapter".
	r.agentsReg = agents.NewRegistry(role)

	backend.responses = []ContainerResponse{{Stdout: []byte(happyHeadlessStdout)}}

	parsed, err := r.RunAgent(context.Background(), "t-adapter", nil, "hi")
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if parsed != "hello world" {
		t.Errorf("parsed = %v, want %q", parsed, "hello world")
	}
	if n := len(backend.RunArgsCalls()); n != 1 {
		t.Errorf("expected 1 Launch call, got %d", n)
	}

	if _, err := r.RunAgent(context.Background(), "unknown-slug", nil, "hi"); err == nil {
		t.Fatal("expected error for unknown slug, got nil")
	}
}

// TestRun_CustomFlowExecutesViaEngine seeds a single-step flow that
// references a test agent binding, hands it to the Runner, then
// invokes Run(). The engine should fire the one step and leave the
// task in done state — proving the engine-driven dispatch branch
// actually runs.
func TestRun_CustomFlowExecutesViaEngine(t *testing.T) {
	r, backend, s := newAgentTestRunner(t)
	role := makeTestRole(t, "t-engine-step", mountNone)
	r.agentsReg = agents.NewRegistry(role)

	// Register a custom "test-engine-only" flow that the task will
	// reference via FlowID. The registry cloneFlow guarantees the
	// engine won't see any later mutation.
	r.flows = flow.NewRegistry(flow.Flow{
		Slug:  "test-engine-only",
		Name:  "Test Engine Only",
		Steps: []flow.Step{{AgentSlug: "t-engine-step"}},
	})
	r.flowEngine = flow.NewEngine(r)

	backend.responses = []ContainerResponse{{Stdout: []byte(happyHeadlessStdout)}}

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:  "engine dispatch",
		Timeout: 5,
		FlowID:  "test-engine-only",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	r.Run(task.ID, "engine dispatch", "", false)
	r.WaitBackground()
	s.WaitCompaction()

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusDone {
		t.Fatalf("status = %q, want done (flow_id=%q, launches=%d)",
			updated.Status, updated.FlowID,
			len(filterTaskCalls(backend.RunArgsCalls())))
	}
	calls := filterTaskCalls(backend.RunArgsCalls())
	if n := len(calls); n != 1 {
		t.Errorf("expected exactly 1 Launch call for the single-step flow, got %d", n)
	}
}
