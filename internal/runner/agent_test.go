package runner

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
)

// newAgentTestRunner builds a runner wired to a MockSandboxBackend so
// tests can drive runAgent without launching real containers. Sibling
// migration tasks will reuse this helper as they migrate their role
// call sites onto runAgent.
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

// happyHeadlessStdout is a minimal agentOutput NDJSON payload that the
// parser accepts. The final line carries the result, session_id, and
// usage fields runAgent propagates.
const happyHeadlessStdout = `{"type":"result","subtype":"success","is_error":false,"result":"hello world","session_id":"s-1","stop_reason":"end_turn","total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":5}}
`

// tokenLimitStdout emulates Claude's in-output token-limit signal so
// the retry path can be exercised.
const tokenLimitStdout = `{"type":"result","subtype":"error","is_error":true,"result":"rate limit exceeded","session_id":"s-1","stop_reason":"","total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0}}
`

func makeRole(name string) AgentRole {
	return AgentRole{
		Activity:    store.SandboxActivityTitle,
		Name:        name,
		Timeout:     func(*store.Task) time.Duration { return 5 * time.Second },
		MountMode:   MountNone,
		SingleTurn:  true,
		ParseResult: func(o *agentOutput) (any, error) { return o.Result, nil },
	}
}

func TestRunAgent_ContainerNameHasUUIDSuffix(t *testing.T) {
	r, backend, _ := newAgentTestRunner(t)
	backend.responses = []ContainerResponse{{Stdout: []byte(happyHeadlessStdout)}}

	res, err := r.runAgent(context.Background(), makeRole("title"), nil, "hi", runAgentOpts{})
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
	// Name shape: wallfacer-<role>-<8 hex chars>.
	re := regexp.MustCompile(`^wallfacer-title-[0-9a-f]{8}$`)
	if !re.MatchString(calls[0].Name) {
		t.Fatalf("container name = %q, want wallfacer-title-<uuid8>", calls[0].Name)
	}
}

func TestRunAgent_HeadlessHappyPath(t *testing.T) {
	r, backend, s := newAgentTestRunner(t)
	backend.responses = []ContainerResponse{{Stdout: []byte(happyHeadlessStdout)}}

	// Create a task so TrackUsage can attribute the invocation.
	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt: "write a title please", Timeout: 10,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	res, err := r.runAgent(
		context.Background(),
		makeRole("title"),
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

	// Usage was attributed: the persisted turn-usage slice holds one
	// record with the role's activity tag.
	records, err := s.GetTurnUsages(task.ID)
	if err != nil {
		t.Fatalf("GetTurnUsages: %v", err)
	}
	if len(records) != 1 || records[0].SubAgent != store.SandboxActivityTitle {
		t.Errorf("turn usage not attributed correctly: %+v", records)
	}
}

func TestRunAgent_RejectsMountReadOnly(t *testing.T) {
	r, _, _ := newAgentTestRunner(t)
	role := makeRole("ro")
	role.MountMode = MountReadOnly

	_, err := r.runAgent(context.Background(), role, nil, "hi", runAgentOpts{})
	if err == nil {
		t.Fatal("expected mount-not-implemented error for MountReadOnly")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "not yet implemented") {
		t.Errorf("error = %q, want contains 'not yet implemented'", got)
	}
}

func TestRunAgent_RejectsMountReadWrite(t *testing.T) {
	r, _, _ := newAgentTestRunner(t)
	role := makeRole("rw")
	role.MountMode = MountReadWrite

	_, err := r.runAgent(context.Background(), role, nil, "hi", runAgentOpts{})
	if err == nil {
		t.Fatal("expected mount-not-implemented error for MountReadWrite")
	}
}

func TestRunAgent_TokenLimitFallback(t *testing.T) {
	r, backend, s := newAgentTestRunner(t)
	// First response: claude returns token-limit in output.
	// Second response: codex returns a clean result.
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

	res, err := r.runAgent(context.Background(), makeRole("title"), task, "p", runAgentOpts{})
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
	_, err := r.runAgent(context.Background(), makeRole("title"), nil, "p", runAgentOpts{})
	if err == nil {
		t.Fatal("expected launch failure to surface")
	}
	if n := len(backend.RunArgsCalls()); n != 1 {
		t.Errorf("expected 1 Launch call (no retry for non-token-limit failure), got %d", n)
	}
}

func TestRunAgent_RequiresRoleName(t *testing.T) {
	r, _, _ := newAgentTestRunner(t)
	role := makeRole("")
	_, err := r.runAgent(context.Background(), role, nil, "p", runAgentOpts{})
	if err == nil {
		t.Fatal("expected error when role.Name is empty")
	}
}

func TestRunAgent_RequiresParseResult(t *testing.T) {
	r, _, _ := newAgentTestRunner(t)
	role := makeRole("title")
	role.ParseResult = nil
	_, err := r.runAgent(context.Background(), role, nil, "p", runAgentOpts{})
	if err == nil {
		t.Fatal("expected error when role.ParseResult is nil")
	}
}

