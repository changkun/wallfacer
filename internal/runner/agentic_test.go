package runner

import (
	"context"
	"encoding/json"
	"testing"

	"latere.ai/x/wallfacer/internal/agentgraph"
	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/flow"
	"latere.ai/x/wallfacer/internal/store"
)

// TestRun_AgenticFlowReachesDoneWithLineage dispatches a task whose resolved
// flow is marked Agentic. The runner must route it through the topos
// agent-graph runtime (with the deterministic fake model), reach done via the
// normal state machine, record the final text, and persist a lineage graph with
// the expected two-node / one-next-edge shape. No container backend is invoked.
func TestRun_AgenticFlowReachesDoneWithLineage(t *testing.T) {
	r, backend, s := newAgentTestRunner(t)
	r.agentsReg = agents.NewRegistry(
		agents.Role{Slug: "ag-planner", Title: "Planner", PromptTmpl: "you plan"},
		agents.Role{Slug: "ag-builder", Title: "Builder", PromptTmpl: "you build"},
	)
	r.flows = flow.NewRegistry(flow.Flow{
		Slug:    "agentic-pair",
		Name:    "Agentic Pair",
		Agentic: true,
		Steps:   []flow.Step{{AgentSlug: "ag-planner"}, {AgentSlug: "ag-builder"}},
	})

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:  "agentic dispatch",
		Timeout: 5,
		FlowID:  "agentic-pair",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	r.Run(task.ID, "agentic dispatch", "", false)
	r.WaitBackground()
	s.WaitCompaction()

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusDone {
		t.Fatalf("status = %q, want done", updated.Status)
	}
	if updated.Result == nil || *updated.Result == "" {
		t.Error("result was not recorded")
	}
	// The agentic path runs in-process via topos; it must not touch the
	// container backend at all.
	if n := len(filterTaskCalls(backend.RunArgsCalls())); n != 0 {
		t.Errorf("expected 0 container launches for an agentic flow, got %d", n)
	}

	if updated.Lineage == nil {
		t.Fatal("lineage was not persisted")
	}
	var lin agentgraph.Lineage
	if err := json.Unmarshal([]byte(*updated.Lineage), &lin); err != nil {
		t.Fatalf("unmarshal lineage: %v", err)
	}
	if len(lin.Nodes) != 2 {
		t.Fatalf("lineage nodes = %+v, want 2", lin.Nodes)
	}
	if lin.Nodes[0].Name != "ag-planner" || lin.Nodes[1].Name != "ag-builder" {
		t.Errorf("node names = %q, %q; want ag-planner, ag-builder", lin.Nodes[0].Name, lin.Nodes[1].Name)
	}
	if len(lin.Edges) != 1 || lin.Edges[0].Kind != "next" {
		t.Fatalf("lineage edges = %+v, want one next edge", lin.Edges)
	}
}
