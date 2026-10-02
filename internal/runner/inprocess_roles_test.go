package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/store"
)

// The tests in this file cover the sub-agent roles of a task pinned to the
// in-process topos harness. The subprocess executor has no such harness, so a
// role that inherits the pin and is launched through it fails. Each test runs
// against the real host backend (the mock backend accepts any harness id and
// would hide the failure) with a fake agent binary standing in for the
// subprocess harnesses, and a scripted model standing in for the gateway the
// in-process harness calls.

// scriptedModel serves the gateway's streaming generate endpoint with one
// fixed text reply and records every request body. It returns an env file that
// points the in-process harness at it (key, base URL, default and title model)
// and an accessor for the recorded bodies.
func scriptedModel(t *testing.T, reply string) (envFile string, requests func() []string) {
	t.Helper()
	delta, err := json.Marshal(reply)
	if err != nil {
		t.Fatalf("marshal reply: %v", err)
	}
	var (
		mu     sync.Mutex
		bodies []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		frame := func(kind, data string) { _, _ = io.WriteString(w, "event: "+kind+"\ndata: "+data+"\n\n") }
		frame("message_start", `{"type":"message_start","id":"msg","model":"test"}`)
		frame("block_start", `{"type":"block_start","index":0,"block":{"type":"text"}}`)
		frame("text_delta", `{"type":"text_delta","index":0,"delta":`+string(delta)+`}`)
		frame("block_stop", `{"type":"block_stop","index":0}`)
		frame("message_delta", `{"type":"message_delta","stop_reason":"end_turn"}`)
		frame("message_stop", `{"type":"message_stop"}`)
	}))
	t.Cleanup(srv.Close)
	envFile = writeEnvFile(t, "ANTHROPIC_API_KEY=test-key\nANTHROPIC_BASE_URL="+srv.URL+
		"\nCLAUDE_DEFAULT_MODEL=default-model\nCLAUDE_TITLE_MODEL=title-model\n")
	return envFile, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
}

// newToposPinnedTask creates a task pinned to the topos harness.
func newToposPinnedTask(t *testing.T, s *store.Store, prompt string) *store.Task {
	t.Helper()
	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt:  prompt,
		Timeout: 5,
		Sandbox: harness.Topos,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

// writeTurnFile gives buildActivityLog one turn of agent activity to summarize.
func writeTurnFile(t *testing.T, s *store.Store, task *store.Task, name string) {
	t.Helper()
	outputsDir := filepath.Join(s.DataDir(), task.ID.String(), "outputs")
	if err := os.MkdirAll(outputsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	turnData := `{"type":"assistant","message":{"content":[{"type":"text","text":"Starting work"}]}}`
	if err := os.WriteFile(filepath.Join(outputsDir, name), []byte(turnData), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestGenerateTitle_ToposPinnedTask asserts a topos-pinned task gets a title.
// The title role runs in-process, so the title is the model's reply and not
// the fake subprocess binary's output, and the request names the title model.
func TestGenerateTitle_ToposPinnedTask(t *testing.T) {
	cmd := fakeCmdScript(t, `{"result":"Subprocess Title","session_id":"s1","stop_reason":"end_turn","is_error":false}`, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	envFile, requests := scriptedModel(t, `"Health Endpoint"`)
	r.envFile = envFile
	task := newToposPinnedTask(t, s, "add a health endpoint")

	r.GenerateTitle(task.ID, task.Prompt)

	updated, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.Title != "Health Endpoint" {
		t.Fatalf("title = %q, want the in-process model's reply %q", updated.Title, "Health Endpoint")
	}
	got := requests()
	if len(got) != 1 {
		t.Fatalf("model requests = %d, want 1", len(got))
	}
	if !strings.Contains(got[0], "title-model") {
		t.Errorf("title request does not name the configured title model: %s", got[0])
	}
}

// TestGenerateTitle_ToposPinnedTaskWithoutCredential asserts the title role of
// a topos-pinned task is not handed to a subprocess harness when the in-process
// harness has no model credential: no title is set.
func TestGenerateTitle_ToposPinnedTaskWithoutCredential(t *testing.T) {
	cmd := fakeCmdScript(t, `{"result":"Subprocess Title","session_id":"s1","stop_reason":"end_turn","is_error":false}`, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	task := newToposPinnedTask(t, s, "add a health endpoint")

	r.GenerateTitle(task.ID, task.Prompt)

	updated, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.Title != "" {
		t.Errorf("title = %q, want none without a model credential", updated.Title)
	}
}

// TestRunTestRun_ToposPinnedTask asserts a test run of a topos-pinned task
// reaches a verdict. Verification needs a subprocess harness, so the testing
// role falls back to the configured default one; the test oversight that
// follows is prompt-only and runs in-process.
func TestRunTestRun_ToposPinnedTask(t *testing.T) {
	repo := setupTestRepo(t)
	testOutput := `{"result":"All checks passed. **PASS**","session_id":"test-sess","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`
	cmd := fakeCmdScript(t, testOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	envFile, _ := scriptedModel(t, `{"phases":[{"timestamp":"2024-01-15T10:00:00Z","title":"Ran the tests","summary":"All passed","tools_used":["Bash"],"actions":["go test"]}]}`)
	r.envFile = envFile
	ctx := context.Background()
	task := newToposPinnedTask(t, s, "verify the health endpoint")

	if err := s.UpdateTaskTestRun(ctx, task.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "verify the implementation", "", false)
	r.WaitBackground()

	after, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if after.Status != store.TaskStatusWaiting {
		t.Fatalf("status = %q, want waiting after a test run", after.Status)
	}
	if after.LastTestResult != "pass" {
		t.Errorf("last_test_result = %q, want pass", after.LastTestResult)
	}
	oversight, err := s.GetTestOversight(task.ID)
	if err != nil {
		t.Fatalf("GetTestOversight: %v", err)
	}
	if oversight.Status != store.OversightStatusReady {
		t.Fatalf("test oversight status = %q (error: %s), want ready", oversight.Status, oversight.Error)
	}
	if len(oversight.Phases) != 1 || oversight.Phases[0].Title != "Ran the tests" {
		t.Errorf("test oversight phases = %+v, want the in-process model's single phase", oversight.Phases)
	}
}

// TestGenerateOversight_ToposPinnedTask asserts both oversight variants produce
// a summary for a topos-pinned task that has turn output: the role runs
// in-process and the phases are the ones the model returned.
func TestGenerateOversight_ToposPinnedTask(t *testing.T) {
	cmd := fakeCmdScript(t, `{"result":"not an oversight summary","session_id":"s1","stop_reason":"end_turn","is_error":false}`, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	envFile, _ := scriptedModel(t, `{"phases":[{"timestamp":"2024-01-15T10:00:00Z","title":"Explored codebase","summary":"Read key files","tools_used":["Read"],"actions":["Read main.go"]}]}`)
	r.envFile = envFile
	task := newToposPinnedTask(t, s, "add a health endpoint")
	writeTurnFile(t, s, task, "turn-0001.json")

	r.GenerateOversight(task.ID)
	oversight, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("GetOversight: %v", err)
	}
	if oversight.Status != store.OversightStatusReady {
		t.Fatalf("oversight status = %q (error: %s), want ready", oversight.Status, oversight.Error)
	}
	if len(oversight.Phases) != 1 || oversight.Phases[0].Title != "Explored codebase" {
		t.Errorf("oversight phases = %+v, want the in-process model's single phase", oversight.Phases)
	}

	r.GenerateTestOversight(task.ID, 0)
	testOversight, err := s.GetTestOversight(task.ID)
	if err != nil {
		t.Fatalf("GetTestOversight: %v", err)
	}
	if testOversight.Status != store.OversightStatusReady {
		t.Fatalf("test oversight status = %q (error: %s), want ready", testOversight.Status, testOversight.Error)
	}
	if len(testOversight.Phases) != 1 || testOversight.Phases[0].Title != "Explored codebase" {
		t.Errorf("test oversight phases = %+v, want the in-process model's single phase", testOversight.Phases)
	}
}

// TestSandboxForTaskActivity_TestingSkipsInProcess covers the resolver rule the
// test-run fallback rests on: the testing activity never resolves to an
// in-process harness, while every other activity keeps the task's pin.
func TestSandboxForTaskActivity_TestingSkipsInProcess(t *testing.T) {
	r := &Runner{}
	pinned := &store.Task{Sandbox: harness.Topos}
	if got := r.sandboxForTaskActivity(pinned, activityTesting); got != harness.Default() {
		t.Errorf("testing on a topos-pinned task = %q, want the default harness %q", got, harness.Default())
	}
	for _, activity := range []store.SandboxActivity{activityImplementation, activityTitle, activityOversight, activityCommitMessage} {
		if got := r.sandboxForTaskActivity(pinned, activity); got != harness.Topos {
			t.Errorf("%s on a topos-pinned task = %q, want topos", activity, got)
		}
	}

	// A subprocess harness named at a higher tier still wins for testing.
	perActivity := &store.Task{
		Sandbox:           harness.Topos,
		SandboxByActivity: map[store.SandboxActivity]harness.ID{activityTesting: harness.Codex},
	}
	if got := r.sandboxForTaskActivity(perActivity, activityTesting); got != harness.Codex {
		t.Errorf("testing with a per-activity codex override = %q, want codex", got)
	}

	// An in-process per-activity override is skipped; the task's own harness decides.
	inProcessOverride := &store.Task{
		Sandbox:           harness.Codex,
		SandboxByActivity: map[store.SandboxActivity]harness.ID{activityTesting: harness.Topos},
	}
	if got := r.sandboxForTaskActivity(inProcessOverride, activityTesting); got != harness.Codex {
		t.Errorf("testing with an in-process per-activity override = %q, want codex", got)
	}

	// The env tier is skipped the same way.
	envTopos := &Runner{envFile: writeEnvFile(t, "WALLFACER_DEFAULT_SANDBOX=topos\n")}
	if got := envTopos.sandboxForTaskActivity(&store.Task{}, activityTesting); got != harness.Default() {
		t.Errorf("testing with an in-process env default = %q, want the default harness %q", got, harness.Default())
	}
}
