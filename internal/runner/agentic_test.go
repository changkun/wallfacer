package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"latere.ai/x/wallfacer/internal/agentgraph"
	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/flow"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/store"
)

// writeEnvFile writes lines to a temp .env file and returns its path.
func writeEnvFile(t *testing.T, lines string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	return path
}

// TestAgenticModelConfig covers the runner-side derivation of a ModelConfig from
// wallfacer's .env credential settings: a bare key selects Direct, a key plus a
// base URL routes through Lux (passing the URL/key/model through), and the
// absence of an env file or an Anthropic key yields the zero config (which the
// seam maps to the fake model). No model is called.
func TestAgenticModelConfig(t *testing.T) {
	t.Run("no env file falls back to fake", func(t *testing.T) {
		r := &Runner{}
		if cfg := r.agenticModelConfig(); cfg != (agentgraph.ModelConfig{}) {
			t.Errorf("config = %+v, want zero (fake)", cfg)
		}
	})

	t.Run("env without anthropic key falls back to fake", func(t *testing.T) {
		r := &Runner{envFile: writeEnvFile(t, "WALLFACER_AUTO_PUSH=true\n")}
		if cfg := r.agenticModelConfig(); cfg != (agentgraph.ModelConfig{}) {
			t.Errorf("config = %+v, want zero (fake)", cfg)
		}
	})

	t.Run("bare key selects direct", func(t *testing.T) {
		r := &Runner{envFile: writeEnvFile(t, "ANTHROPIC_API_KEY=sk-test\nCLAUDE_DEFAULT_MODEL=claude-sonnet-4-6\n")}
		cfg := r.agenticModelConfig()
		want := agentgraph.ModelConfig{
			Mode:     agentgraph.ModelModeDirect,
			Provider: "anthropic",
			Model:    "claude-sonnet-4-6",
			APIKey:   "sk-test",
		}
		if cfg != want {
			t.Errorf("config = %+v, want %+v", cfg, want)
		}
	})

	t.Run("key plus base url routes through lux", func(t *testing.T) {
		r := &Runner{envFile: writeEnvFile(t,
			"ANTHROPIC_API_KEY=lux_test\nANTHROPIC_BASE_URL=https://lux.latere.ai/anthropic\nCLAUDE_DEFAULT_MODEL=claude-sonnet-4-6\n")}
		cfg := r.agenticModelConfig()
		want := agentgraph.ModelConfig{
			Mode:     agentgraph.ModelModeLux,
			Provider: "anthropic",
			Model:    "claude-sonnet-4-6",
			BaseURL:  "https://lux.latere.ai/anthropic",
			APIKey:   "lux_test",
		}
		if cfg != want {
			t.Errorf("config = %+v, want %+v", cfg, want)
		}
	})
}

// TestRun_AgenticFlowReachesDoneWithLineage dispatches a task whose resolved
// flow is marked Agentic. The runner must route it through the topos
// agent-graph runtime (with the deterministic fake model), reach done via the
// normal state machine, record the final text, and persist a lineage graph with
// the expected two-node / one-next-edge shape. No container backend is invoked.
// TestRun_NativeToposHarnessReachesDoneInProcess covers the native-harness
// dispatch: a plain implement-path task pinned to the topos harness runs
// in-process as a single topos agent (zero container launches), reaches done,
// and persists a one-node lineage (no delegation edges).
func TestRun_NativeToposHarnessReachesDoneInProcess(t *testing.T) {
	r, backend, s := newAgentTestRunner(t)

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:  "native topos task",
		Timeout: 5,
		Sandbox: harness.Topos,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	r.Run(task.ID, "native topos task", "", false)
	r.WaitBackground()
	s.WaitCompaction()

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusDone {
		t.Fatalf("status = %q, want done", updated.Status)
	}
	if updated.Result == nil || *updated.Result == "" {
		t.Error("result was not recorded")
	}
	// The native harness runs in-process via topos; it must not launch a container.
	if n := len(filterTaskCalls(backend.RunArgsCalls())); n != 0 {
		t.Errorf("expected 0 container launches for the native topos harness, got %d", n)
	}

	if updated.Lineage == nil {
		t.Fatal("lineage was not persisted")
	}
	var lin agentgraph.Lineage
	if err := json.Unmarshal([]byte(*updated.Lineage), &lin); err != nil {
		t.Fatalf("unmarshal lineage: %v", err)
	}
	if len(lin.Nodes) != 1 {
		t.Fatalf("lineage nodes = %+v, want 1 (single agent)", lin.Nodes)
	}
	if lin.Nodes[0].Name != "implement" {
		t.Errorf("node name = %q, want implement", lin.Nodes[0].Name)
	}
	if len(lin.Edges) != 0 {
		t.Errorf("lineage edges = %+v, want none (no delegation)", lin.Edges)
	}
}

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
