package runner

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/agents"
	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
)

// registerTestBinding installs a binding under a test-only slug and
// clears it at the end of the test. Tests register their own bindings
// so they can drive custom parse / mount-mode behaviour without
// mutating package-level state across tests.
func registerTestBinding(t *testing.T, slug string, b agentBinding) {
	t.Helper()
	if _, exists := agentBindings[slug]; exists {
		t.Fatalf("registerTestBinding: slug %q already in use", slug)
	}
	agentBindings[slug] = b
	t.Cleanup(func() { delete(agentBindings, slug) })
}

func newAgentTestRunner(t *testing.T) (*Runner, *MockSandboxBackend, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	backend := &MockSandboxBackend{}
	shutdownCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := &Runner{
		store:       s,
		backend:     backend,
		promptsMgr:  prompts.Default,
		shutdownCtx: shutdownCtx,
	}
	return r, backend, s
}

const happyHeadlessStdout = `{"type":"result","subtype":"success","is_error":false,"result":"hello world","session_id":"s-1","stop_reason":"end_turn","total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":5}}
`

const tokenLimitStdout = `{"type":"result","subtype":"error","is_error":true,"result":"rate limit exceeded","session_id":"s-1","stop_reason":"","total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0}}
`

// makeTestRole installs a headless-profile binding under a unique slug
// and returns the matching agents.Role descriptor. Tests use this
// instead of building AgentRole literals.
func makeTestRole(t *testing.T, slug string, mode mountMode) agents.Role {
	t.Helper()
	registerTestBinding(t, slug, agentBinding{
		Activity:    store.SandboxActivityTitle,
		Timeout:     func(*store.Task) time.Duration { return 5 * time.Second },
		MountMode:   mode,
		SingleTurn:  true,
		ParseResult: func(o *agentOutput) (any, error) { return o.Result, nil },
	})
	return agents.Role{Slug: slug, Title: slug}
}

func TestRunAgent_ContainerNameHasUUIDSuffix(t *testing.T) {
	r, backend, _ := newAgentTestRunner(t)
	backend.responses = []ContainerResponse{{Stdout: []byte(happyHeadlessStdout)}}

	role := makeTestRole(t, "t-title", mountNone)
	res, err := r.runAgent(context.Background(), role, nil, "hi", runAgentOpts{})
	if err != nil {
		t.Fatalf("runAgent: %v", err)
	}
	if res.Output == nil || res.Output.Result != "hello world" {
		t.Fatalf("parsed output mismatch: %+v", res.Output)
	}

	calls := backend.RunArgsCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 Launch call, got %d", len(calls))
	}
	re := regexp.MustCompile(`^wallfacer-t-title-[0-9a-f]{8}$`)
	if !re.MatchString(calls[0].Name) {
		t.Fatalf("container name = %q, want wallfacer-t-title-<uuid8>", calls[0].Name)
	}
}

func TestRunAgent_HeadlessHappyPath(t *testing.T) {
	r, backend, s := newAgentTestRunner(t)
	backend.responses = []ContainerResponse{{Stdout: []byte(happyHeadlessStdout)}}

	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt: "write a title please", Timeout: 10,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	role := makeTestRole(t, "t-happy", mountNone)
	res, err := r.runAgent(
		context.Background(),
		role,
		task,
		"prompt body",
		runAgentOpts{TrackUsage: true, Turn: 1},
	)
	if err != nil {
		t.Fatalf("runAgent: %v", err)
	}
	if res.Parsed != "hello world" {
		t.Errorf("Parsed = %v, want 'hello world'", res.Parsed)
	}
	if res.Output.SessionID != "s-1" {
		t.Errorf("SessionID = %q, want s-1", res.Output.SessionID)
	}
	if res.Output.ActualSandbox != sandbox.Claude {
		t.Errorf("ActualSandbox = %q, want claude", res.Output.ActualSandbox)
	}

	records, err := s.GetTurnUsages(task.ID)
	if err != nil {
		t.Fatalf("GetTurnUsages: %v", err)
	}
	if len(records) != 1 || records[0].SubAgent != store.SandboxActivityTitle {
		t.Errorf("turn usage not attributed correctly: %+v", records)
	}
}

// TestRunAgent_MountReadOnly_AddsWorkspaceMounts verifies the inspector
// mount profile picks up every configured workspace as a read-only
// volume.
func TestRunAgent_MountReadOnly_AddsWorkspaceMounts(t *testing.T) {
	r, backend, s := newAgentTestRunner(t)
	ws := t.TempDir()
	r.workspaces = []string{ws}
	backend.responses = []ContainerResponse{{Stdout: []byte(happyHeadlessStdout)}}

	role := makeTestRole(t, "t-ro", mountReadOnly)
	task, _ := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "p", Timeout: 5})
	if _, err := r.runAgent(context.Background(), role, task, "hi", runAgentOpts{}); err != nil {
		t.Fatalf("runAgent: %v", err)
	}
	calls := backend.RunArgsCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 Launch call, got %d", len(calls))
	}
	joined := strings.Join(calls[0].Args, " ")
	wantPath := sandbox.TranslateHostPath(ws, "")
	if !strings.Contains(joined, wantPath) {
		t.Errorf("expected workspace path %q in launch args, got %q", wantPath, joined)
	}
}

// TestRunAgent_HarnessPinOverridesTaskSandbox verifies that an
// agent descriptor with an explicit Harness pin reaches that
// harness even when the task itself picks the other one. This is
// the contract user-authored clones rely on: pinning the role to
// "codex" should route there regardless of task / env settings.
func TestRunAgent_HarnessPinOverridesTaskSandbox(t *testing.T) {
	r, backend, s := newAgentTestRunner(t)
	backend.responses = []ContainerResponse{{Stdout: []byte(happyHeadlessStdout)}}

	// Task pins itself to Claude; the agent role pins itself to Codex.
	// The role pin must win.
	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt: "harness pin", Timeout: 10, Sandbox: sandbox.Claude,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	role := makeTestRole(t, "t-harness-pin", mountNone)
	role.Harness = "codex"

	res, err := r.runAgent(context.Background(), role, task, "hi", runAgentOpts{})
	if err != nil {
		t.Fatalf("runAgent: %v", err)
	}
	if res.SandboxUsed != sandbox.Codex {
		t.Errorf("SandboxUsed = %q, want codex (role.Harness pin)", res.SandboxUsed)
	}
}

// TestRunAgent_HarnessPinEmptyInherits confirms the default path
// is unchanged: an empty Harness pin lets the 4-tier resolver
// pick the task's sandbox, matching pre-pin behaviour.
func TestRunAgent_HarnessPinEmptyInherits(t *testing.T) {
	r, backend, s := newAgentTestRunner(t)
	backend.responses = []ContainerResponse{{Stdout: []byte(happyHeadlessStdout)}}

	task, _ := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt: "inherit", Timeout: 10, Sandbox: sandbox.Codex,
	})
	role := makeTestRole(t, "t-harness-empty", mountNone)
	// role.Harness left empty → inherit from the task's Sandbox.
	res, err := r.runAgent(context.Background(), role, task, "hi", runAgentOpts{})
	if err != nil {
		t.Fatalf("runAgent: %v", err)
	}
	if res.SandboxUsed != sandbox.Codex {
		t.Errorf("SandboxUsed = %q, want codex (task sandbox)", res.SandboxUsed)
	}
}

func TestRunAgent_TokenLimitFallback(t *testing.T) {
	r, backend, s := newAgentTestRunner(t)
	backend.responses = []ContainerResponse{
		{Stdout: []byte(tokenLimitStdout)},
		{Stdout: []byte(happyHeadlessStdout)},
	}

	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt: "probe", Timeout: 10,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	role := makeTestRole(t, "t-tok", mountNone)
	res, err := r.runAgent(context.Background(), role, task, "p", runAgentOpts{})
	if err != nil {
		t.Fatalf("runAgent: %v", err)
	}
	if res.Output.IsError {
		t.Errorf("expected final result to be non-error after fallback; got IsError=true")
	}
	if n := len(backend.RunArgsCalls()); n != 2 {
		t.Errorf("expected 2 Launch calls (primary + fallback), got %d", n)
	}
}

func TestRunAgent_LaunchErrorNoFallbackOnNonTokenLimit(t *testing.T) {
	r, backend, _ := newAgentTestRunner(t)
	backend.responses = []ContainerResponse{
		{Err: errors.New("some unrelated failure")},
	}
	role := makeTestRole(t, "t-err", mountNone)
	_, err := r.runAgent(context.Background(), role, nil, "p", runAgentOpts{})
	if err == nil {
		t.Fatal("expected launch failure to surface")
	}
	if n := len(backend.RunArgsCalls()); n != 1 {
		t.Errorf("expected 1 Launch call (no retry for non-token-limit failure), got %d", n)
	}
}

func TestRunAgent_RequiresRoleSlug(t *testing.T) {
	r, _, _ := newAgentTestRunner(t)
	_, err := r.runAgent(context.Background(), agents.Role{}, nil, "p", runAgentOpts{})
	if err == nil {
		t.Fatal("expected error when role.Slug is empty")
	}
}

func TestRunAgent_RequiresBindingRegistered(t *testing.T) {
	r, _, _ := newAgentTestRunner(t)
	_, err := r.runAgent(context.Background(), agents.Role{Slug: "definitely-not-registered"}, nil, "p", runAgentOpts{})
	if err == nil {
		t.Fatal("expected error when no binding is registered for slug")
	}
	if !strings.Contains(err.Error(), "no binding") {
		t.Errorf("error = %q, want one mentioning 'no binding'", err)
	}
}
