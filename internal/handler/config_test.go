package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/store"
)

// newTestHandlerWithWorkspaces creates a Handler with real workspace directories
// and an env file, so config/git/files endpoints can function.
func newTestHandlerWithWorkspaces(t *testing.T) (*Handler, string) {
	t.Helper()
	ws := t.TempDir()
	configDir := t.TempDir()

	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:    envPath,
		Workspaces: ws,
	})
	t.Cleanup(r.WaitBackground)
	h := NewHandler(s, r, configDir, []string{ws})
	return h, ws
}

// --- GetConfig ---

func TestGetConfig_ReturnsWorkspaces(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	workspaces, ok := resp["workspaces"].([]any)
	if !ok || len(workspaces) == 0 {
		t.Fatalf("expected workspaces array, got %v", resp["workspaces"])
	}
	if workspaces[0].(string) != ws {
		t.Errorf("expected workspace %q, got %q", ws, workspaces[0])
	}
}

func TestGetConfig_AutopilotFalseByDefault(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if autopilot, ok := resp["autopilot"].(bool); !ok || autopilot {
		t.Errorf("expected autopilot=false by default, got %v", resp["autopilot"])
	}
}

func TestGetConfig_ReturnsInstructionsPath(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if _, ok := resp["instructions_path"]; !ok {
		t.Error("expected instructions_path in response")
	}
}

func TestGetConfig_ExposesIdeationCategories(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	cats, ok := resp["ideation_categories"].([]any)
	if !ok {
		t.Fatalf("expected ideation_categories to be an array, got %T", resp["ideation_categories"])
	}
	need := map[string]struct{}{
		"accessibility":         {},
		"documentation update":  {},
		"architecture / design": {},
		"dependency management": {},
	}
	found := map[string]bool{}
	for _, c := range cats {
		if s, ok := c.(string); ok {
			found[s] = true
		}
	}
	for k := range need {
		if !found[k] {
			t.Fatalf("expected ideation_categories to include %q, got %v", k, cats)
		}
	}
}

func TestGetConfig_AlwaysIncludesCodexSandbox(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw, ok := resp["sandboxes"].([]any)
	if !ok {
		t.Fatalf("expected sandboxes array, got %T (%v)", resp["sandboxes"], resp["sandboxes"])
	}
	sandboxes := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			sandboxes = append(sandboxes, s)
		}
	}
	if !slices.Contains(sandboxes, "claude") {
		t.Fatalf("expected sandboxes to include claude, got %v", sandboxes)
	}
	if !slices.Contains(sandboxes, "codex") {
		t.Fatalf("expected sandboxes to include codex, got %v", sandboxes)
	}
}

func TestGetConfig_ReportsCodexUnavailableWhenUntested(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	reqEnv := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(`{"openai_api_key":"sk-test"}`))
	wEnv := httptest.NewRecorder()
	h.UpdateEnvConfig(wEnv, reqEnv)
	if wEnv.Code != http.StatusNoContent {
		t.Fatalf("expected env update 204, got %d: %s", wEnv.Code, wEnv.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	usable, ok := resp["sandbox_usable"].(map[string]any)
	if !ok {
		t.Fatalf("expected sandbox_usable object, got %T (%v)", resp["sandbox_usable"], resp["sandbox_usable"])
	}
	if codex, ok := usable["codex"].(bool); !ok || codex {
		t.Fatalf("expected sandbox_usable.codex=false before test, got %v", usable["codex"])
	}
}

func TestGetConfig_ReportsCodexUsableWithHostAuth(t *testing.T) {
	h, _, _ := newTestHandlerWithEnvAndCodexAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	usable, ok := resp["sandbox_usable"].(map[string]any)
	if !ok {
		t.Fatalf("expected sandbox_usable object, got %T (%v)", resp["sandbox_usable"], resp["sandbox_usable"])
	}
	if codex, ok := usable["codex"].(bool); !ok || !codex {
		t.Fatalf("expected sandbox_usable.codex=true with host auth + passed test, got %v", usable["codex"])
	}
}

// --- UpdateConfig ---

func TestUpdateConfig_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestUpdateConfig_EnableAutopilot(t *testing.T) {
	h := newTestHandler(t)
	if h.AutopilotEnabled() {
		t.Fatal("autopilot should be off initially")
	}

	body := `{"autopilot": true}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if enabled, ok := resp["autopilot"].(bool); !ok || !enabled {
		t.Errorf("expected autopilot=true in response, got %v", resp["autopilot"])
	}
	if !h.AutopilotEnabled() {
		t.Error("expected autopilot to be enabled after update")
	}
}

func TestUpdateConfig_DisableAutopilot(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)

	body := `{"autopilot": false}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if h.AutopilotEnabled() {
		t.Error("expected autopilot to be disabled")
	}
}

func TestUpdateConfig_NoFieldChangesNothing(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)

	// Empty body — should not change autopilot.
	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !h.AutopilotEnabled() {
		t.Error("expected autopilot to remain enabled when not specified in request")
	}
}

// --- GetFiles ---

func TestGetFiles_EmptyWorkspace(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	w := httptest.NewRecorder()
	h.GetFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	files, ok := resp["files"].([]any)
	if !ok {
		t.Fatalf("expected files array, got %v", resp["files"])
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files in empty workspace, got %d", len(files))
	}
}

func TestGetFiles_ListsWorkspaceFiles(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	// Create some files in the workspace.
	os.WriteFile(filepath.Join(ws, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(ws, "README.md"), []byte("# readme"), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	w := httptest.NewRecorder()
	h.GetFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	files, ok := resp["files"].([]any)
	if !ok {
		t.Fatalf("expected files array, got %v", resp["files"])
	}
	if len(files) < 2 {
		t.Errorf("expected at least 2 files, got %d: %v", len(files), files)
	}

	// Files should be prefixed with the workspace basename.
	base := filepath.Base(ws)
	for _, f := range files {
		if !strings.HasPrefix(f.(string), base+"/") {
			t.Errorf("file path %q should be prefixed with %q", f, base+"/")
		}
	}
}

func TestGetFiles_SkipsHiddenDirs(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	// Create files in a hidden dir (should be skipped).
	hiddenDir := filepath.Join(ws, ".git")
	os.MkdirAll(hiddenDir, 0755)
	os.WriteFile(filepath.Join(hiddenDir, "config"), []byte("git config"), 0644)

	// Create a visible file.
	os.WriteFile(filepath.Join(ws, "visible.txt"), []byte("visible"), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	w := httptest.NewRecorder()
	h.GetFiles(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	files, _ := resp["files"].([]any)

	for _, f := range files {
		if strings.Contains(f.(string), ".git") {
			t.Errorf("files should not include hidden directory entries, got: %s", f)
		}
	}
}

func TestGetFiles_SkipsNodeModules(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	nodeModules := filepath.Join(ws, "node_modules")
	os.MkdirAll(nodeModules, 0755)
	os.WriteFile(filepath.Join(nodeModules, "package.js"), []byte("module"), 0644)
	os.WriteFile(filepath.Join(ws, "index.js"), []byte("main"), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	w := httptest.NewRecorder()
	h.GetFiles(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	files, _ := resp["files"].([]any)

	for _, f := range files {
		if strings.Contains(f.(string), "node_modules") {
			t.Errorf("node_modules should be skipped, got: %s", f)
		}
	}
}

// --- GetContainers ---

func TestGetContainers_ReturnsResult(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	w := httptest.NewRecorder()
	h.GetContainers(w, req)

	// Either a list (possibly empty) or an error — both return JSON.
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("expected 200 or 500, got %d", w.Code)
	}
}

// --- GitStatus ---

func TestGitStatus_NoWorkspaces(t *testing.T) {
	h := newTestHandler(t) // no workspaces configured
	req := httptest.NewRequest(http.MethodGet, "/api/git/status", nil)
	w := httptest.NewRecorder()
	h.GitStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var statuses []any
	json.NewDecoder(w.Body).Decode(&statuses)
	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses (no workspaces), got %d", len(statuses))
	}
}

func TestGitStatus_WithWorkspace(t *testing.T) {
	repo := setupRepo(t)
	h, _ := newTestHandlerWithWorkspacesFromRepo(t, repo)
	req := httptest.NewRequest(http.MethodGet, "/api/git/status", nil)
	w := httptest.NewRecorder()
	h.GitStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// --- isAllowedWorkspace ---

func TestIsAllowedWorkspace_AllowsConfigured(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	if !h.isAllowedWorkspace(ws) {
		t.Errorf("expected %q to be allowed workspace", ws)
	}
}

func TestIsAllowedWorkspace_RejectsUnknown(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	if h.isAllowedWorkspace("/tmp/not-a-workspace") {
		t.Error("expected /tmp/not-a-workspace to be rejected")
	}
}

// --- GitPush (error cases) ---

func TestGitPush_InvalidJSON(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodPost, "/api/git/push", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	h.GitPush(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestGitPush_RejectsUnknownWorkspace(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	body := `{"workspace": "/tmp/not-configured"}`
	req := httptest.NewRequest(http.MethodPost, "/api/git/push", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.GitPush(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown workspace, got %d", w.Code)
	}
}

// --- GitBranches ---

func TestGitBranches_MissingWorkspace(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/git/branches", nil)
	w := httptest.NewRecorder()
	h.GitBranches(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing workspace param, got %d", w.Code)
	}
}

func TestGitBranches_UnknownWorkspace(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/git/branches?workspace=/unknown", nil)
	w := httptest.NewRecorder()
	h.GitBranches(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown workspace, got %d", w.Code)
	}
}

func TestGitBranches_ValidRepo(t *testing.T) {
	repo := setupRepo(t)
	h, _ := newTestHandlerWithWorkspacesFromRepo(t, repo)
	req := httptest.NewRequest(http.MethodGet, "/api/git/branches?workspace="+repo, nil)
	w := httptest.NewRecorder()
	h.GitBranches(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if _, ok := resp["branches"]; !ok {
		t.Error("expected branches in response")
	}
	if _, ok := resp["current"]; !ok {
		t.Error("expected current in response")
	}
}

// --- GitCheckout (validation) ---

func TestGitCheckout_InvalidJSON(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodPost, "/api/git/checkout", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	h.GitCheckout(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestGitCheckout_RejectsUnknownWorkspace(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	body := `{"workspace": "/not/configured", "branch": "main"}`
	req := httptest.NewRequest(http.MethodPost, "/api/git/checkout", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.GitCheckout(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestGitCheckout_RejectsInvalidBranchName(t *testing.T) {
	repo := setupRepo(t)
	h, _ := newTestHandlerWithWorkspacesFromRepo(t, repo)

	tests := []struct {
		branch string
	}{
		{"branch with spaces"},
		{"branch..dotdot"},
		{""},
	}
	for _, tc := range tests {
		body := `{"workspace": "` + repo + `", "branch": "` + tc.branch + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/git/checkout", strings.NewReader(body))
		w := httptest.NewRecorder()
		h.GitCheckout(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for branch %q, got %d", tc.branch, w.Code)
		}
	}
}

func TestGitCheckout_RejectsWhenTasksInProgress(t *testing.T) {
	repo := setupRepo(t)
	h, _ := newTestHandlerWithWorkspacesFromRepo(t, repo)
	ctx := context.Background()

	// Create a task and move it to in_progress.
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

	body := `{"workspace": "` + repo + `", "branch": "main"}`
	req := httptest.NewRequest(http.MethodPost, "/api/git/checkout", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.GitCheckout(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 when tasks in progress, got %d", w.Code)
	}
}

// --- GitCreateBranch (validation) ---

func TestGitCreateBranch_InvalidJSON(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodPost, "/api/git/create-branch", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	h.GitCreateBranch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestGitCreateBranch_RejectsUnknownWorkspace(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	body := `{"workspace": "/not/configured", "branch": "new-branch"}`
	req := httptest.NewRequest(http.MethodPost, "/api/git/create-branch", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.GitCreateBranch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestGitCreateBranch_RejectsInvalidBranchName(t *testing.T) {
	repo := setupRepo(t)
	h, _ := newTestHandlerWithWorkspacesFromRepo(t, repo)

	body := `{"workspace": "` + repo + `", "branch": "bad..branch"}`
	req := httptest.NewRequest(http.MethodPost, "/api/git/create-branch", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.GitCreateBranch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid branch name, got %d", w.Code)
	}
}

func TestGitCreateBranch_RejectsWhenTasksInProgress(t *testing.T) {
	repo := setupRepo(t)
	h, _ := newTestHandlerWithWorkspacesFromRepo(t, repo)
	ctx := context.Background()

	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

	body := `{"workspace": "` + repo + `", "branch": "new-branch"}`
	req := httptest.NewRequest(http.MethodPost, "/api/git/create-branch", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.GitCreateBranch(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 when tasks in progress, got %d", w.Code)
	}
}

// --- GitSyncWorkspace ---

func TestGitSyncWorkspace_InvalidJSON(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodPost, "/api/git/sync", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	h.GitSyncWorkspace(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestGitSyncWorkspace_RejectsUnknownWorkspace(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	body := `{"workspace": "/not/configured"}`
	req := httptest.NewRequest(http.MethodPost, "/api/git/sync", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.GitSyncWorkspace(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- GitRebaseOnMain ---

func TestGitRebaseOnMain_InvalidJSON(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodPost, "/api/git/rebase", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	h.GitRebaseOnMain(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestGitRebaseOnMain_RejectsUnknownWorkspace(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	body := `{"workspace": "/not/configured"}`
	req := httptest.NewRequest(http.MethodPost, "/api/git/rebase", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.GitRebaseOnMain(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- helpers ---

// newTestHandlerWithWorkspacesFromRepo creates a Handler configured with the
// given repo directory as its workspace.
func newTestHandlerWithWorkspacesFromRepo(t *testing.T, repo string) (*Handler, string) {
	t.Helper()
	configDir := t.TempDir()
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{Workspaces: repo})
	t.Cleanup(r.WaitBackground)
	return NewHandler(s, r, configDir, []string{repo}), repo
}

// --- strict JSON decoding ---

// TestUpdateConfig_RejectsUnknownFields verifies that unknown JSON keys return 400.
func TestUpdateConfig_RejectsUnknownFields(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	body := `{"autopilot": true, "unknown_field": "surprise"}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown fields, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateConfig_RejectsTrailingContent verifies that trailing data after
// the JSON object returns 400.
func TestUpdateConfig_RejectsTrailingContent(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	body := `{"autopilot": true} trailing`
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for trailing content, got %d: %s", w.Code, w.Body.String())
	}
}
