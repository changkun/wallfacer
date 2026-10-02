package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"latere.ai/x/wallfacer/internal/agentgraph"
	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/envconfig"
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
// absence of an env file or an Anthropic key is refused with
// ErrNoModelCredential unless the runner opts into the test model. No model is
// called.
func TestAgenticModelConfig(t *testing.T) {
	noCredential := map[string]func(t *testing.T) *Runner{
		"no env file": func(*testing.T) *Runner { return &Runner{} },
		"env without anthropic key": func(t *testing.T) *Runner {
			return &Runner{envFile: writeEnvFile(t, "WALLFACER_AUTO_PUSH=true\n")}
		},
		"env file missing on disk": func(t *testing.T) *Runner {
			return &Runner{envFile: filepath.Join(t.TempDir(), "absent.env")}
		},
	}
	for name, newRunner := range noCredential {
		t.Run(name+" is refused", func(t *testing.T) {
			cfg, err := newRunner(t).agenticModelConfig()
			if !errors.Is(err, agentgraph.ErrNoModelCredential) {
				t.Fatalf("err = %v, want ErrNoModelCredential", err)
			}
			if cfg != (agentgraph.ModelConfig{}) {
				t.Errorf("config = %+v, want zero alongside the refusal", cfg)
			}
		})

		t.Run(name+" with the test-model opt-in", func(t *testing.T) {
			r := newRunner(t)
			r.allowTestModel = true
			want := agentgraph.ModelConfig{Mode: agentgraph.ModelModeFake}
			if cfg := mustAgenticModelConfig(t, r); cfg != want {
				t.Errorf("config = %+v, want %+v", cfg, want)
			}
		})
	}

	t.Run("a configured key wins over the test-model opt-in", func(t *testing.T) {
		r := &Runner{envFile: writeEnvFile(t, "ANTHROPIC_API_KEY=sk-test\n"), allowTestModel: true}
		if got := mustAgenticModelConfig(t, r).Mode; got != agentgraph.ModelModeDirect {
			t.Errorf("Mode = %q, want direct", got)
		}
	})

	t.Run("bare key selects direct", func(t *testing.T) {
		r := &Runner{envFile: writeEnvFile(t, "ANTHROPIC_API_KEY=sk-test\nCLAUDE_DEFAULT_MODEL=claude-sonnet-4-6\n")}
		cfg := mustAgenticModelConfig(t, r)
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

	t.Run("key plus base url routes through lux at the gateway root", func(t *testing.T) {
		// The .env carries the anthropic-wire URL the container harness
		// dials; the model leg must reduce it to the gateway root the
		// lux-native dialect lives under.
		r := &Runner{envFile: writeEnvFile(t,
			"ANTHROPIC_API_KEY=lux_test\nANTHROPIC_BASE_URL=https://lux.example.com/anthropic\nCLAUDE_DEFAULT_MODEL=claude-sonnet-4-6\n")}
		cfg := mustAgenticModelConfig(t, r)
		want := agentgraph.ModelConfig{
			Mode:     agentgraph.ModelModeLux,
			Provider: "anthropic",
			Model:    "claude-sonnet-4-6",
			BaseURL:  "https://lux.example.com",
			APIKey:   "lux_test",
		}
		if cfg != want {
			t.Errorf("config = %+v, want %+v", cfg, want)
		}
	})

	t.Run("origin-shaped base url passes through unchanged", func(t *testing.T) {
		r := &Runner{envFile: writeEnvFile(t,
			"ANTHROPIC_API_KEY=lux_test\nANTHROPIC_BASE_URL=https://lux.example.com\n")}
		if got := mustAgenticModelConfig(t, r).BaseURL; got != "https://lux.example.com" {
			t.Errorf("BaseURL = %q, want origin unchanged", got)
		}
	})

	// A gateway served under a base path, as Latere's Lux core is under
	// /v1/models, keeps that path: only the /anthropic door is dropped, so the
	// lux-native dialect is dialed at <root>/lux/v1/generate and not at the
	// host's root, where nothing answers it.
	t.Run("base path survives and only the anthropic door is dropped", func(t *testing.T) {
		for in, want := range map[string]string{
			"https://api.example.com/v1/models/anthropic":  "https://api.example.com/v1/models",
			"https://api.example.com/v1/models/anthropic/": "https://api.example.com/v1/models",
			"https://api.example.com/v1/models":            "https://api.example.com/v1/models",
			"https://api.example.com/v1/models/":           "https://api.example.com/v1/models",
		} {
			r := &Runner{envFile: writeEnvFile(t, "ANTHROPIC_API_KEY=lux_test\nANTHROPIC_BASE_URL="+in+"\n")}
			if got := mustAgenticModelConfig(t, r).BaseURL; got != want {
				t.Errorf("ANTHROPIC_BASE_URL=%s: BaseURL = %q, want %q", in, got, want)
			}
		}
	})
}

// TestRun_AgenticFlowReachesDoneWithTrace dispatches a task whose resolved
// flow is marked Agentic. The runner must route it through the topos
// agent-graph runtime (with the deterministic test model), reach done via the
// normal state machine, record the final text, and persist a trace graph with
// the expected two-node / one-next-edge shape. No container backend is invoked.
// TestRun_NativeToposHarnessReachesDoneInProcess covers the native-harness
// dispatch: a plain implement-path task pinned to the topos harness runs
// in-process as a single topos agent (zero container launches), reaches done, and
// persists a one-node trace (no delegation edges). End-to-end worktree
// execution (the agent's tools running in the worktree via topos Options.Workdir)
// is proven directly at the agentgraph layer by TestRunAgent_WithWorktreeExecutesInRepo,
// since this runner harness does not provision a workspace.
func TestRun_NativeToposHarnessReachesDoneInProcess(t *testing.T) {
	r, backend, s := newAgentTestRunner(t)
	r.allowTestModel = true

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

	if updated.Trace == nil {
		t.Fatal("trace was not persisted")
	}
	var lin agentgraph.Trace
	if err := json.Unmarshal([]byte(*updated.Trace), &lin); err != nil {
		t.Fatalf("unmarshal trace: %v", err)
	}
	if len(lin.Nodes) != 1 {
		t.Fatalf("trace nodes = %+v, want 1 (single agent)", lin.Nodes)
	}
	if lin.Nodes[0].Name != "implement" {
		t.Errorf("node name = %q, want implement", lin.Nodes[0].Name)
	}
	if len(lin.Edges) != 0 {
		t.Errorf("trace edges = %+v, want none (no delegation)", lin.Edges)
	}
}

func TestRun_AgenticFlowReachesDoneWithTrace(t *testing.T) {
	r, backend, s := newAgentTestRunner(t)
	r.allowTestModel = true
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

	if updated.Trace == nil {
		t.Fatal("trace was not persisted")
	}
	var lin agentgraph.Trace
	if err := json.Unmarshal([]byte(*updated.Trace), &lin); err != nil {
		t.Fatalf("unmarshal trace: %v", err)
	}
	if len(lin.Nodes) != 2 {
		t.Fatalf("trace nodes = %+v, want 2", lin.Nodes)
	}
	if lin.Nodes[0].Name != "ag-planner" || lin.Nodes[1].Name != "ag-builder" {
		t.Errorf("node names = %q, %q; want ag-planner, ag-builder", lin.Nodes[0].Name, lin.Nodes[1].Name)
	}
	if len(lin.Edges) != 1 || lin.Edges[0].Kind != "next" {
		t.Fatalf("trace edges = %+v, want one next edge", lin.Edges)
	}
}

// TestRun_NativeToposHarnessCommitsWorktreeEdits is the regression guard for the
// bug where the topos native path walked committing -> done without ever making
// a git commit: the agent edited the worktree, but the state machine reported
// success with the work uncommitted. With a real workspace repo and a prompt that
// writes a file, the run must now produce a durable commit merged onto the repo's
// default branch (the full commit pipeline stage -> commit -> rebase -> merge ->
// cleanup, same as the subprocess implement path's auto-submit).
func TestRun_NativeToposHarnessCommitsWorktreeEdits(t *testing.T) {
	repo := setupTestRepo(t)
	s, r := setupTestRunner(t, []string{repo})
	r.allowTestModel = true
	enableCommitMessageGeneration(t, r)
	initialHash := gitRun(t, repo, "rev-parse", "HEAD")

	ctx := context.Background()
	// With this prompt the test model's scripted tool call writes
	// topos-marker.txt into the worktree, standing in for an agent's edit.
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:  "topos change > topos-marker.txt",
		Timeout: 5,
		Sandbox: harness.Topos,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	r.Run(task.ID, task.Prompt, "", false)
	r.WaitBackground()
	s.WaitCompaction()

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusDone {
		t.Fatalf("status = %q, want done", updated.Status)
	}

	// The full pipeline merges the worktree branch onto the default branch, so a
	// new commit must exist on the repo itself (the worktree is cleaned up).
	finalHash := gitRun(t, repo, "rev-parse", "HEAD")
	if finalHash == initialHash {
		t.Fatal("expected a new commit on the default branch, but HEAD is unchanged (topos run did not commit)")
	}
	if _, statErr := os.Stat(filepath.Join(repo, "topos-marker.txt")); statErr != nil {
		t.Fatalf("agent's file was not committed onto the default branch: %v", statErr)
	}
	if len(updated.CommitHashes) == 0 {
		t.Error("no commit hashes were recorded on the task")
	}
	commits, err := s.GetTaskCommits(task.ID)
	if err != nil || len(commits) == 0 {
		t.Fatalf("native execution lost commit history: %v", err)
	}

}

func TestRun_AgenticFlowCommitsWorktreeEdits(t *testing.T) {
	repo := setupTestRepo(t)
	s, r := setupTestRunner(t, []string{repo})
	r.allowTestModel = true
	enableCommitMessageGeneration(t, r)
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
	initialHash := gitRun(t, repo, "rev-parse", "HEAD")

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:  "agentic change > agentic-marker.txt",
		Timeout: 5,
		FlowID:  "agentic-pair",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	r.Run(task.ID, task.Prompt, "", false)
	r.WaitBackground()
	s.WaitCompaction()

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusDone {
		t.Fatalf("status = %q, want done", updated.Status)
	}
	if finalHash := gitRun(t, repo, "rev-parse", "HEAD"); finalHash == initialHash {
		t.Fatal("expected a new commit on the default branch, but HEAD is unchanged")
	}
	if _, statErr := os.Stat(filepath.Join(repo, "agentic-marker.txt")); statErr != nil {
		t.Fatalf("agentic flow file was not committed onto the default branch: %v", statErr)
	}
	if len(updated.CommitHashes) == 0 {
		t.Error("no commit hashes were recorded on the task")
	}
}

// TestRun_ToposWithoutModelCredentialIsRefused covers both in-process paths (a
// task pinned to the topos harness and a delegating fleet) on a runner with no
// model credential configured. The run must be refused before anything is set
// up: the task fails with the model-credential category and the fixed
// sentence, the error event keeps the developer detail in its own field, no
// worktree is created, and the workspace repository is untouched.
func TestRun_ToposWithoutModelCredentialIsRefused(t *testing.T) {
	const wantSentence = harness.ToposCredentialRequired

	cases := []struct {
		name string
		opts store.TaskCreateOptions
	}{
		{"native run", store.TaskCreateOptions{Prompt: "native change > refused-marker.txt", Timeout: 5, Sandbox: harness.Topos}},
		{"delegating fleet", store.TaskCreateOptions{Prompt: "fleet change > refused-marker.txt", Timeout: 5, FlowID: "agentic-pair"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := setupTestRepo(t)
			s, r := setupTestRunner(t, []string{repo})
			enableCommitMessageGeneration(t, r)
			r.agentsReg = agents.NewRegistry(
				agents.Role{Slug: "ag-planner", Title: "Planner", PromptTmpl: "you plan"},
				agents.Role{Slug: "ag-builder", Title: "Builder", PromptTmpl: "you build"},
			)
			r.flows = flow.NewRegistry(flow.Flow{
				Slug:    "agentic-pair",
				Name:    "Agentic Pair",
				Agentic: true,
				Dynamic: true,
				Steps:   []flow.Step{{AgentSlug: "ag-planner"}, {AgentSlug: "ag-builder"}},
			})
			initialHash := gitRun(t, repo, "rev-parse", "HEAD")

			ctx := context.Background()
			task, err := s.CreateTaskWithOptions(ctx, tc.opts)
			if err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
				t.Fatalf("UpdateTaskStatus: %v", err)
			}

			r.Run(task.ID, task.Prompt, "", false)
			r.WaitBackground()
			s.WaitCompaction()

			updated, err := s.GetTask(ctx, task.ID)
			if err != nil {
				t.Fatalf("GetTask: %v", err)
			}
			if updated.Status != store.TaskStatusFailed {
				t.Errorf("status = %q, want failed", updated.Status)
			}
			if updated.FailureCategory != store.FailureCategoryModelCredential {
				t.Errorf("failure category = %q, want %q", updated.FailureCategory, store.FailureCategoryModelCredential)
			}
			if updated.Result == nil || *updated.Result != wantSentence {
				t.Errorf("result = %v, want the fixed sentence %q", updated.Result, wantSentence)
			}
			events, err := s.GetEvents(ctx, task.ID)
			if err != nil {
				t.Fatalf("GetEvents: %v", err)
			}
			var errEvents []map[string]string
			for _, ev := range events {
				if ev.EventType != store.EventTypeError {
					continue
				}
				var data map[string]string
				if err := json.Unmarshal(ev.Data, &data); err != nil {
					t.Fatalf("unmarshal error event: %v", err)
				}
				errEvents = append(errEvents, data)
			}
			if len(errEvents) != 1 {
				t.Fatalf("error events = %v, want exactly one", errEvents)
			}
			if errEvents[0]["error"] != wantSentence {
				t.Errorf("error event message = %q, want %q", errEvents[0]["error"], wantSentence)
			}
			if errEvents[0]["detail"] != agentgraph.ErrNoModelCredential.Error() {
				t.Errorf("error event detail = %q, want %q", errEvents[0]["detail"], agentgraph.ErrNoModelCredential.Error())
			}
			if len(updated.WorktreePaths) != 0 {
				t.Errorf("worktrees = %v, want none for a refused run", updated.WorktreePaths)
			}
			if finalHash := gitRun(t, repo, "rev-parse", "HEAD"); finalHash != initialHash {
				t.Errorf("default branch moved from %s to %s; a refused run must not commit", initialHash, finalHash)
			}
			if _, statErr := os.Stat(filepath.Join(repo, "refused-marker.txt")); statErr == nil {
				t.Error("refused-marker.txt exists in the workspace; a refused run must not run anything")
			}
		})
	}
}

func mustAgenticModelConfig(t *testing.T, r *Runner) agentgraph.ModelConfig {
	t.Helper()
	cfg, err := r.agenticModelConfig()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestAgenticModelConfigRejectsUnavailableSecret(t *testing.T) {
	path := writeEnvFile(t, "WALLFACER_SECRET_STORE=keyring\nWALLFACER_SECRET_BUNDLE=invalid\n")
	r := &Runner{envFile: path}
	if _, err := r.agenticModelConfig(); !errors.Is(err, envconfig.ErrSecretStore) {
		t.Fatalf("err = %v, want ErrSecretStore", err)
	}
}
