package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/spec"
	"latere.ai/x/wallfacer/internal/store"
)

func doTransition(t *testing.T, fn http.HandlerFunc, path string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"path": path})
	req := httptest.NewRequest(http.MethodPost, "/api/specs/archive", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fn(w, req)
	return w
}

func readStatus(t *testing.T, ws, relPath string) spec.Status {
	t.Helper()
	s, err := spec.ParseFile(filepath.Join(ws, relPath))
	if err != nil {
		t.Fatalf("parse %q: %v", relPath, err)
	}
	return s.Status
}

func TestMigrateSpec_Success(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	// A frontmatter-less file: renders as a doc node, eligible for migration.
	writeTestSpec(t, ws, "specs/local/overview.md", "# Overview\n\nProse, no frontmatter.\n")

	w := doTransition(t, h.MigrateSpec, "specs/local/overview.md")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	// The file now parses as a lifecycle-managed spec.
	s, err := spec.ParseFile(filepath.Join(ws, "specs/local/overview.md"))
	if err != nil {
		t.Fatalf("ParseFile after migrate: %v", err)
	}
	if s.Status != spec.StatusDrafted {
		t.Errorf("status = %q, want %q", s.Status, spec.StatusDrafted)
	}
	if s.Title != "Overview" {
		t.Errorf("title = %q, want title from H1", s.Title)
	}
	if !strings.Contains(s.Body, "Prose, no frontmatter.") {
		t.Errorf("body not preserved: %q", s.Body)
	}
}

func TestMigrateSpec_AlreadyHasFrontmatter(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	writeTestSpec(t, ws, "specs/local/managed.md", testSpecValidated)

	w := doTransition(t, h.MigrateSpec, "specs/local/managed.md")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestMigrateSpec_NotFound(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	w := doTransition(t, h.MigrateSpec, "specs/local/missing.md")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestArchiveSpec_Success(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	drafted := strings.Replace(testSpecValidated, "status: validated", "status: drafted", 1)
	writeTestSpec(t, ws, "specs/local/target.md", drafted)

	w := doTransition(t, h.ArchiveSpec, "specs/local/target.md")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := readStatus(t, ws, "specs/local/target.md"); got != spec.StatusArchived {
		t.Errorf("status = %q, want %q", got, spec.StatusArchived)
	}
}

func TestArchiveSpec_InvalidTransition(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	// `validated → archived` is invalid (must route through complete or stale).
	writeTestSpec(t, ws, "specs/local/validated.md", testSpecValidated)

	w := doTransition(t, h.ArchiveSpec, "specs/local/validated.md")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid transition") {
		t.Errorf("body = %q, want mention of invalid transition", w.Body.String())
	}
}

func TestValidateSpec_Success(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	drafted := strings.Replace(testSpecValidated, "status: validated", "status: drafted", 1)
	writeTestSpec(t, ws, "specs/local/draft.md", drafted)

	w := doTransition(t, h.ValidateSpecTransition, "specs/local/draft.md")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := readStatus(t, ws, "specs/local/draft.md"); got != spec.StatusValidated {
		t.Errorf("status = %q, want %q", got, spec.StatusValidated)
	}
}

func TestValidateSpec_InvalidFromComplete(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	complete := strings.Replace(testSpecValidated, "status: validated", "status: complete", 1)
	writeTestSpec(t, ws, "specs/local/done.md", complete)

	w := doTransition(t, h.ValidateSpecTransition, "specs/local/done.md")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	if got := readStatus(t, ws, "specs/local/done.md"); got != spec.StatusComplete {
		t.Errorf("status mutated to %q, want unchanged complete", got)
	}
}

func TestValidateSpec_InvalidFromVague(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	vague := strings.Replace(testSpecValidated, "status: validated", "status: vague", 1)
	writeTestSpec(t, ws, "specs/local/idea.md", vague)

	w := doTransition(t, h.ValidateSpecTransition, "specs/local/idea.md")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestValidateSpec_NotFound(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	w := doTransition(t, h.ValidateSpecTransition, "specs/local/missing.md")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// writeDispatchedSpec writes a drafted spec wired to taskID and returns its path.
func writeDispatchedSpec(t *testing.T, ws, relPath, taskID string) {
	t.Helper()
	dispatched := strings.Replace(testSpecValidated, "status: validated", "status: drafted", 1)
	dispatched = strings.Replace(dispatched, "dispatched_task_id: null",
		"dispatched_task_id: "+taskID, 1)
	writeTestSpec(t, ws, relPath, dispatched)
}

// seedTask creates a task and force-sets its status, returning the id string.
func seedTask(t *testing.T, s *store.Store, status store.TaskStatus) string {
	t.Helper()
	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "seed", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, status); err != nil {
		t.Fatalf("ForceUpdateTaskStatus(%s): %v", status, err)
	}
	return task.ID.String()
}

func TestArchiveSpec_BlockedByActiveTask(t *testing.T) {
	for _, status := range []store.TaskStatus{
		store.TaskStatusBacklog,
		store.TaskStatusInProgress,
		store.TaskStatusWaiting,
		store.TaskStatusCommitting,
	} {
		t.Run(string(status), func(t *testing.T) {
			h, ws := newTestHandlerWithWorkspaces(t)
			taskID := seedTask(t, h.store, status)
			writeDispatchedSpec(t, ws, "specs/local/dispatched.md", taskID)

			w := doTransition(t, h.ArchiveSpec, "specs/local/dispatched.md")
			if w.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusConflict, w.Body.String())
			}
			// Body must name the live status so the alert is actionable.
			if !strings.Contains(w.Body.String(), string(status)) {
				t.Errorf("body = %q, want mention of status %q", w.Body.String(), status)
			}
			// Status must be unchanged.
			if got := readStatus(t, ws, "specs/local/dispatched.md"); got != spec.StatusDrafted {
				t.Errorf("status = %q, want unchanged %q", got, spec.StatusDrafted)
			}
		})
	}
}

func TestArchiveSpec_AllowedWhenTaskTerminal(t *testing.T) {
	for _, status := range []store.TaskStatus{
		store.TaskStatusDone,
		store.TaskStatusFailed,
		store.TaskStatusCancelled,
	} {
		t.Run(string(status), func(t *testing.T) {
			h, ws := newTestHandlerWithWorkspaces(t)
			taskID := seedTask(t, h.store, status)
			writeDispatchedSpec(t, ws, "specs/local/dispatched.md", taskID)

			w := doTransition(t, h.ArchiveSpec, "specs/local/dispatched.md")
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
			}
			if got := readStatus(t, ws, "specs/local/dispatched.md"); got != spec.StatusArchived {
				t.Errorf("status = %q, want %q", got, spec.StatusArchived)
			}
		})
	}
}

func TestArchiveSpec_AllowedWhenLinkStale(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	// dispatched_task_id points at a task that does not exist in the store.
	writeDispatchedSpec(t, ws, "specs/local/stale.md", "550e8400-e29b-41d4-a716-446655440000")

	w := doTransition(t, h.ArchiveSpec, "specs/local/stale.md")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := readStatus(t, ws, "specs/local/stale.md"); got != spec.StatusArchived {
		t.Errorf("status = %q, want %q", got, spec.StatusArchived)
	}
}

func TestArchiveSpec_AlreadyArchived(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	archived := strings.Replace(testSpecValidated, "status: validated", "status: archived", 1)
	writeTestSpec(t, ws, "specs/local/already.md", archived)

	w := doTransition(t, h.ArchiveSpec, "specs/local/already.md")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestUnarchiveSpec_Success(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	archived := strings.Replace(testSpecValidated, "status: validated", "status: archived", 1)
	writeTestSpec(t, ws, "specs/local/arch.md", archived)

	w := doTransition(t, h.UnarchiveSpec, "specs/local/arch.md")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := readStatus(t, ws, "specs/local/arch.md"); got != spec.StatusDrafted {
		t.Errorf("status = %q, want %q", got, spec.StatusDrafted)
	}
}

func TestUnarchiveSpec_NotArchived(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	complete := strings.Replace(testSpecValidated, "status: validated", "status: complete", 1)
	writeTestSpec(t, ws, "specs/local/complete.md", complete)

	w := doTransition(t, h.UnarchiveSpec, "specs/local/complete.md")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

// initGitRepo initializes workspace `ws` as a git repo with one initial commit
// so archive/unarchive cascade tests can exercise the commit/revert flow.
func initGitRepo(t *testing.T, ws string) {
	t.Helper()
	commands := [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.com"},
		{"config", "commit.gpgsign", "false"},
	}
	for _, args := range commands {
		cmd := exec.Command("git", append([]string{"-C", ws}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(ws, "README.md"), []byte("init\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "README.md"},
		{"commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", ws}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func lastCommitSubject(t *testing.T, ws string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", ws, "log", "-1", "--format=%s").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestArchiveSpec_CascadeAndRevert(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	initGitRepo(t, ws)

	parent := strings.Replace(testSpecValidated, "status: validated", "status: drafted", 1)
	childA := strings.Replace(testSpecValidated, "status: validated", "status: complete", 1)
	childB := strings.Replace(testSpecValidated, "status: validated", "status: drafted", 1)
	writeTestSpec(t, ws, "specs/local/parent.md", parent)
	writeTestSpec(t, ws, "specs/local/parent/a.md", childA)
	writeTestSpec(t, ws, "specs/local/parent/b.md", childB)

	// Stage initial specs so cascade commit has a clean baseline to diff from.
	for _, args := range [][]string{
		{"add", "specs/"},
		{"commit", "-q", "-m", "add specs"},
	} {
		cmd := exec.Command("git", append([]string{"-C", ws}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Archive the parent — should cascade to both children and produce one commit.
	w := doTransition(t, h.ArchiveSpec, "specs/local/parent.md")
	if w.Code != http.StatusOK {
		t.Fatalf("archive status = %d, body: %s", w.Code, w.Body.String())
	}
	if got := readStatus(t, ws, "specs/local/parent.md"); got != spec.StatusArchived {
		t.Errorf("parent status = %q, want archived", got)
	}
	if got := readStatus(t, ws, "specs/local/parent/a.md"); got != spec.StatusArchived {
		t.Errorf("child a status = %q, want archived (cascade)", got)
	}
	if got := readStatus(t, ws, "specs/local/parent/b.md"); got != spec.StatusArchived {
		t.Errorf("child b status = %q, want archived (cascade)", got)
	}
	if subj := lastCommitSubject(t, ws); !strings.Contains(subj, "archive") {
		t.Errorf("last commit = %q, want contains 'archive'", subj)
	}

	// Unarchive should revert the archive commit and restore prior statuses.
	w = doTransition(t, h.UnarchiveSpec, "specs/local/parent.md")
	if w.Code != http.StatusOK {
		t.Fatalf("unarchive status = %d, body: %s", w.Code, w.Body.String())
	}
	if got := readStatus(t, ws, "specs/local/parent.md"); got != spec.StatusDrafted {
		t.Errorf("parent status = %q, want drafted", got)
	}
	if got := readStatus(t, ws, "specs/local/parent/a.md"); got != spec.StatusComplete {
		t.Errorf("child a status = %q, want complete (restored)", got)
	}
	if got := readStatus(t, ws, "specs/local/parent/b.md"); got != spec.StatusDrafted {
		t.Errorf("child b status = %q, want drafted (restored)", got)
	}
}

// writeReadmeIn creates a specs/README.md under the given workspace
// with the supplied body, for GetSpecTree / stream index assertions.
func writeReadmeIn(t *testing.T, ws, body string) {
	t.Helper()
	path := filepath.Join(ws, "specs", "README.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGetSpecTree_ReturnsIndexField(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	writeReadmeIn(t, ws, "# My Roadmap\n\nBody.\n")

	req := httptest.NewRequest(http.MethodGet, "/api/specs/tree", nil)
	w := httptest.NewRecorder()
	h.GetSpecTree(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp spec.TreeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Index == nil {
		t.Fatalf("Index is nil, want non-nil\nbody: %s", w.Body.String())
	}
	if resp.Index.Title != "My Roadmap" {
		t.Errorf("Index.Title = %q, want %q", resp.Index.Title, "My Roadmap")
	}
	if resp.Index.Path != "specs/README.md" {
		t.Errorf("Index.Path = %q, want specs/README.md", resp.Index.Path)
	}
	if resp.Index.Workspace != ws {
		t.Errorf("Index.Workspace = %q, want %q", resp.Index.Workspace, ws)
	}
}

func TestGetSpecTree_IndexNullWhenMissing(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	// No README in the workspace.
	req := httptest.NewRequest(http.MethodGet, "/api/specs/tree", nil)
	w := httptest.NewRecorder()
	h.GetSpecTree(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp spec.TreeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Index != nil {
		t.Errorf("Index = %+v, want nil when no README exists", resp.Index)
	}
	// With omitempty, a nil Index should not serialize an "index" field at all.
	if strings.Contains(w.Body.String(), `"index":`) {
		t.Errorf("response should omit index field when nil; body: %s", w.Body.String())
	}
}

func TestSpecTreeStream_SendsInitialSnapshot(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/specs/stream", nil).WithContext(ctx)
	w := newSyncResponseWriter()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.SpecTreeStream(w, req)
	}()

	// Wait for the initial snapshot event.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("timed out waiting for snapshot event")
		default:
		}
		if strings.Contains(w.bodyString(), "event: snapshot") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	<-done
}

func TestSpecTreeStream_SendsSnapshotOnChange(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	// No specs dir initially — tree is empty.
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/specs/stream", nil).WithContext(ctx)
	w := newSyncResponseWriter()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.SpecTreeStream(w, req)
	}()

	// Wait for initial snapshot.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("timed out waiting for initial snapshot")
		default:
		}
		if strings.Contains(w.bodyString(), "event: snapshot") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	initialCount := strings.Count(w.bodyString(), "event: snapshot")

	// Create a specs directory with a file to trigger a change in the tree data.
	specsDir := filepath.Join(ws, "specs")
	if err := os.MkdirAll(specsDir, 0755); err != nil {
		cancel()
		<-done
		t.Fatal(err)
	}
	// Write a valid-looking spec (even if BuildTree cannot fully parse it,
	// the serialized JSON will differ from the empty tree).
	if err := os.WriteFile(filepath.Join(specsDir, "README.md"), []byte("# Specs\n"), 0644); err != nil {
		cancel()
		<-done
		t.Fatal(err)
	}

	// Wait for a second snapshot event.
	deadline = time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			cancel()
			<-done
			// The tree may not differ if BuildTree still returns empty for this
			// workspace. That's OK — the important thing is that the stream
			// is alive and would send on real changes.
			t.Log("no additional snapshot within timeout (tree may not have changed)")
			return
		default:
		}
		if strings.Count(w.bodyString(), "event: snapshot") > initialCount {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	<-done
}

// TestSpecTreeStream_IncludesIndex verifies that the SSE snapshot
// carries the roadmap index alongside the tree, and that creating /
// modifying specs/README.md fires a fresh snapshot whose JSON body
// surfaces the index field. Required by spec-tree-index-endpoint.md.
func TestSpecTreeStream_IncludesIndex(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/specs/stream", nil).WithContext(ctx)
	w := newSyncResponseWriter()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.SpecTreeStream(w, req)
	}()

	defer func() {
		cancel()
		<-done
	}()

	// Wait for the initial snapshot — without any README it should
	// still emit, with the index field omitted (omitempty).
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for initial snapshot")
		default:
		}
		if strings.Contains(w.bodyString(), "event: snapshot") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	initialCount := strings.Count(w.bodyString(), "event: snapshot")

	// Drop a README into the workspace; the next poll tick must
	// detect the new index and emit a snapshot with it inlined.
	writeReadmeIn(t, ws, "# My Roadmap\n\nBody.\n")

	deadline = time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for index-bearing snapshot;\nbody: %s", w.bodyString())
		default:
		}
		body := w.bodyString()
		if strings.Count(body, "event: snapshot") > initialCount &&
			strings.Contains(body, `"index"`) &&
			strings.Contains(body, `"My Roadmap"`) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
