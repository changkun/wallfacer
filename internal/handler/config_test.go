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
	"time"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/workspace"
)

// newTestHandlerWithWorkspaces creates a Handler with real workspace directories
// and an env file, so config/git/files endpoints can function.
func newTestHandlerWithWorkspaces(t *testing.T) (*Handler, string) {
	t.Helper()
	ws := t.TempDir()
	configDir := t.TempDir()

	s, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:    envPath,
		Workspaces: []string{ws},
	})
	t.Cleanup(r.WaitBackground)
	h := NewHandler(s, r, configDir, []string{ws}, nil)
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
	if got, ok := resp["workspace_browser_path"].(string); !ok || got != ws {
		t.Fatalf("expected workspace_browser_path %q, got %#v", ws, resp["workspace_browser_path"])
	}
	groups, ok := resp["workspace_groups"].([]any)
	if !ok || len(groups) == 0 {
		t.Fatalf("expected workspace_groups array, got %#v", resp["workspace_groups"])
	}
}

func TestGetConfig_UsesCWDForWorkspaceBrowserPathWithoutWorkspaces(t *testing.T) {
	h := newTestHandler(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
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
	if got, ok := resp["workspace_browser_path"].(string); !ok || got != cwd {
		t.Fatalf("expected workspace_browser_path %q, got %#v", cwd, resp["workspace_browser_path"])
	}
}

func TestUpdateConfig_PersistsWorkspaceGroups(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	type wsGroup struct {
		Workspaces []string `json:"workspaces"`
	}
	type wsGroupReq struct {
		WorkspaceGroups []wsGroup `json:"workspace_groups"`
	}
	b, _ := json.Marshal(wsGroupReq{WorkspaceGroups: []wsGroup{{Workspaces: []string{ws, ws + "/../" + filepath.Base(ws)}}}})
	body := strings.NewReader(string(b))
	req := httptest.NewRequest(http.MethodPut, "/api/config", body)
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cfgReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	cfgW := httptest.NewRecorder()
	h.GetConfig(cfgW, cfgReq)
	var resp map[string]any
	if err := json.NewDecoder(cfgW.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	groups, ok := resp["workspace_groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("expected 1 workspace group, got %#v", resp["workspace_groups"])
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatalf("expected group object, got %#v", groups[0])
	}
	workspaces, ok := group["workspaces"].([]any)
	if !ok || len(workspaces) != 1 || workspaces[0] != ws {
		t.Fatalf("expected normalized workspace group [%q], got %#v", ws, group["workspaces"])
	}
}

func TestGetConfig_AutopilotFalseByDefault(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
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
	_ = json.NewDecoder(w.Body).Decode(&resp)

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
	_ = json.NewDecoder(w.Body).Decode(&resp)

	cats, ok := resp["ideation_categories"].([]any)
	if !ok {
		t.Fatalf("expected ideation_categories to be an array, got %T", resp["ideation_categories"])
	}
	need := map[string]struct{}{
		"product feature":          {},
		"performance optimization": {},
		"architecture / design":    {},
		"security hardening":       {},
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

func TestGetConfig_ExposesIdeationExploitRatio(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

	ratio, ok := resp["ideation_exploit_ratio"].(float64)
	if !ok {
		t.Fatalf("expected ideation_exploit_ratio to be a number, got %T", resp["ideation_exploit_ratio"])
	}
	if ratio != 0.8 {
		t.Errorf("expected default exploit ratio 0.8, got %f", ratio)
	}
}

func TestUpdateConfig_SetsIdeationExploitRatio(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	body := strings.NewReader(`{"ideation_exploit_ratio": 0.6}`)
	req := httptest.NewRequest(http.MethodPut, "/api/config", body)
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

	ratio, ok := resp["ideation_exploit_ratio"].(float64)
	if !ok {
		t.Fatalf("expected ideation_exploit_ratio in response, got %T", resp["ideation_exploit_ratio"])
	}
	if ratio != 0.6 {
		t.Errorf("expected exploit ratio 0.6, got %f", ratio)
	}

	// Verify getter reflects the change.
	if got := h.IdeationExploitRatio(); got != 0.6 {
		t.Errorf("IdeationExploitRatio() = %f; want 0.6", got)
	}
}

func TestUpdateConfig_ClampsExploitRatio(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)

	// Test clamping above 1.
	body := strings.NewReader(`{"ideation_exploit_ratio": 1.5}`)
	req := httptest.NewRequest(http.MethodPut, "/api/config", body)
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)
	if got := h.IdeationExploitRatio(); got != 1.0 {
		t.Errorf("expected clamped to 1.0, got %f", got)
	}

	// Test clamping below 0.
	body = strings.NewReader(`{"ideation_exploit_ratio": -0.5}`)
	req = httptest.NewRequest(http.MethodPut, "/api/config", body)
	w = httptest.NewRecorder()
	h.UpdateConfig(w, req)
	if got := h.IdeationExploitRatio(); got != 0.0 {
		t.Errorf("expected clamped to 0.0, got %f", got)
	}
}

func TestBrowseWorkspaces_HiddenFoldersExcludedByDefault(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	hidden := filepath.Join(ws, ".hidden-repo")
	visible := filepath.Join(ws, "visible-repo")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(visible, 0o755); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/browse?path="+ws, nil)
	w := httptest.NewRecorder()
	h.BrowseWorkspaces(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Entries []workspaceBrowseEntry `json:"entries"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, entry := range resp.Entries {
		if entry.Name == ".hidden-repo" {
			t.Fatal("expected hidden folder to be excluded by default")
		}
	}
}

func TestBrowseWorkspaces_AcceptsTrailingSlash(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/browse?path="+ws+"/", nil)
	w := httptest.NewRecorder()
	h.BrowseWorkspaces(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Path != ws {
		t.Fatalf("expected normalized path %q, got %q", ws, resp.Path)
	}
}

func TestBrowseWorkspaces_HiddenFoldersIncludedWhenRequested(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	hidden := filepath.Join(ws, ".hidden-repo")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/browse?path="+ws+"&include_hidden=true", nil)
	w := httptest.NewRecorder()
	h.BrowseWorkspaces(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Entries []workspaceBrowseEntry `json:"entries"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, entry := range resp.Entries {
		if entry.Name == ".hidden-repo" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected hidden folder to be included when include_hidden=true")
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

func TestGetConfig_SandboxActivities(t *testing.T) {
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

	raw, ok := resp["sandbox_activities"].([]any)
	if !ok {
		t.Fatalf("expected sandbox_activities to be an array, got %T (%v)", resp["sandbox_activities"], resp["sandbox_activities"])
	}

	// Must contain at least the seven canonical entries.
	want := store.SandboxActivities
	if len(raw) < len(want) {
		t.Fatalf("expected at least %d sandbox_activities, got %d: %v", len(want), len(raw), raw)
	}

	got := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("sandbox_activities entry is not a string: %T (%v)", v, v)
		}
		got = append(got, s)
	}

	// Value must exactly equal store.SandboxActivities.
	for i, key := range want {
		if !slices.Contains(got, string(key)) {
			t.Errorf("sandbox_activities[%d] = %q not found in response %v", i, key, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("expected sandbox_activities length %d, got %d: %v", len(want), len(got), got)
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
	_ = json.NewDecoder(w.Body).Decode(&resp)

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
	_ = json.NewDecoder(w.Body).Decode(&resp)

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
	_ = os.WriteFile(filepath.Join(ws, "main.go"), []byte("package main"), 0644)

	_ = os.WriteFile(filepath.Join(ws, "README.md"), []byte("# readme"), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	w := httptest.NewRecorder()
	h.GetFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

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
	_ = os.MkdirAll(hiddenDir, 0755)

	_ = os.WriteFile(filepath.Join(hiddenDir, "config"), []byte("git config"), 0644)

	// Create a visible file.
	_ = os.WriteFile(filepath.Join(ws, "visible.txt"), []byte("visible"), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	w := httptest.NewRecorder()
	h.GetFiles(w, req)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

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
	_ = os.MkdirAll(nodeModules, 0755)

	_ = os.WriteFile(filepath.Join(nodeModules, "package.js"), []byte("module"), 0644)

	_ = os.WriteFile(filepath.Join(ws, "index.js"), []byte("main"), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	w := httptest.NewRecorder()
	h.GetFiles(w, req)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

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
	_ = json.NewDecoder(w.Body).Decode(&statuses)

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
	_ = json.NewDecoder(w.Body).Decode(&resp)

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
		body := jsonObj("workspace", repo, "branch", tc.branch)
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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: filepath.Join(t.TempDir(), "wt")}, "task-branch")

	body := jsonObj("workspace", repo, "branch", "main")
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

	body := jsonObj("workspace", repo, "branch", "bad..branch")
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

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: filepath.Join(t.TempDir(), "wt")}, "task-branch")

	body := jsonObj("workspace", repo, "branch", "new-branch")
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
	s, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{Workspaces: []string{repo}})
	t.Cleanup(r.WaitBackground)
	return NewHandler(s, r, configDir, []string{repo}, nil), repo
}

// --- UpdateWorkspaces ---

// newTestHandlerWithRealWorkspaceManager creates a Handler backed by a real
// workspace.Manager (not a static one) so that UpdateWorkspaces exercises the
// full transactional switch pipeline.
func newTestHandlerWithRealWorkspaceManager(t *testing.T) (*Handler, *workspace.Manager, string) {
	t.Helper()
	configDir := t.TempDir()
	dataDir := t.TempDir()

	storeDir, err := os.MkdirTemp("", "wallfacer-handler-wsmgr-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(storeDir) })

	s, err := store.NewFileStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}

	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	ws := t.TempDir()
	wsMgr, err := workspace.NewManager(configDir, dataDir, envFile, []string{ws})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:          envFile,
		WorkspaceManager: wsMgr,
	})
	t.Cleanup(r.WaitBackground)
	t.Cleanup(r.Shutdown)

	h := NewHandler(s, r, configDir, []string{ws}, nil)
	return h, wsMgr, ws
}

// TestUpdateWorkspaces_SwitchesToNewWorkspace verifies that POST /api/workspaces
// with a valid new workspace switches the active workspace and returns a config
// response that reflects the new workspace set.
func TestUpdateWorkspaces_SwitchesToNewWorkspace(t *testing.T) {
	h, _, _ := newTestHandlerWithRealWorkspaceManager(t)

	newWS := t.TempDir()
	type wsReq struct {
		Workspaces []string `json:"workspaces"`
	}
	b, _ := json.Marshal(wsReq{Workspaces: []string{newWS}})
	body := strings.NewReader(string(b))
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", body)
	w := httptest.NewRecorder()
	h.UpdateWorkspaces(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	workspaces, ok := resp["workspaces"].([]any)
	if !ok || len(workspaces) == 0 {
		t.Fatalf("expected workspaces in response, got %v", resp["workspaces"])
	}
	if workspaces[0].(string) != newWS {
		t.Errorf("expected new workspace %q in response, got %q", newWS, workspaces[0])
	}
}

// TestUpdateWorkspaces_AllowedDuringInProgress verifies that workspace switching
// succeeds even when tasks are in progress (multi-store support keeps old stores alive).
func TestUpdateWorkspaces_AllowedDuringInProgress(t *testing.T) {
	h, _, _ := newTestHandlerWithRealWorkspaceManager(t)

	// Create a task and move it to in_progress.
	s, ok := h.currentStore()
	if !ok || s == nil {
		t.Fatal("expected store to be available")
	}
	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	newWS := t.TempDir()
	type wsReq struct {
		Workspaces []string `json:"workspaces"`
	}
	b, _ := json.Marshal(wsReq{Workspaces: []string{newWS}})
	body := strings.NewReader(string(b))
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", body)
	w := httptest.NewRecorder()
	h.UpdateWorkspaces(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when switching with tasks in progress, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateWorkspaces_InvalidWorkspaceReturns400 verifies that an invalid
// (non-existent) workspace path causes a 400 response.
func TestUpdateWorkspaces_InvalidWorkspaceReturns400(t *testing.T) {
	h, _, _ := newTestHandlerWithRealWorkspaceManager(t)

	body := strings.NewReader(`{"workspaces":["/does/not/exist/at/all"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", body)
	w := httptest.NewRecorder()
	h.UpdateWorkspaces(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-existent workspace, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateWorkspaces_SubscriptionUpdatesHandlerStore verifies that after a
// successful workspace switch the handler's mirrored store field is updated via
// the workspace subscription goroutine (not via a direct assignment).
func TestUpdateWorkspaces_SubscriptionUpdatesHandlerStore(t *testing.T) {
	h, wsMgr, _ := newTestHandlerWithRealWorkspaceManager(t)

	// Record the store pointer before the switch.
	storeBefore, ok := h.currentStore()
	if !ok || storeBefore == nil {
		t.Fatal("expected initial store")
	}

	newWS := t.TempDir()
	type wsReq struct {
		Workspaces []string `json:"workspaces"`
	}
	b, _ := json.Marshal(wsReq{Workspaces: []string{newWS}})
	body := strings.NewReader(string(b))
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", body)
	w := httptest.NewRecorder()
	h.UpdateWorkspaces(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The workspace manager's snapshot should already reflect the new workspace.
	snap := wsMgr.Snapshot()
	if len(snap.Workspaces) != 1 || snap.Workspaces[0] != newWS {
		t.Errorf("workspace manager snapshot not updated: got %v", snap.Workspaces)
	}

	// The subscription goroutine runs asynchronously; give it a short window to
	// propagate the new snapshot into h.store.
	mirroredStore := func() *store.Store {
		h.snapshotMu.RLock()
		defer h.snapshotMu.RUnlock()
		return h.store
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if mirroredStore() != storeBefore {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if mirroredStore() == storeBefore {
		t.Error("expected h.store to be updated by the subscription goroutine after workspace switch")
	}
	if mirroredStore() == nil {
		t.Error("expected h.store to be non-nil after workspace switch")
	}
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

// --- ssrfHardenedTransport ---

// TestSsrfHardenedTransport_ReturnsNonNil verifies that ssrfHardenedTransport
// returns a non-nil transport.
func TestSsrfHardenedTransport_ReturnsNonNil(t *testing.T) {
	transport := ssrfHardenedTransport()
	if transport == nil {
		t.Error("expected non-nil transport")
	}
}

// TestSsrfHardenedTransport_BlocksLocalhostRequests verifies that the hardened
// transport blocks requests to loopback addresses.
func TestSsrfHardenedTransport_BlocksLocalhostRequests(t *testing.T) {
	transport := ssrfHardenedTransport()
	if transport == nil {
		t.Fatal("nil transport")
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	_, err := client.Get("http://localhost/test")
	if err == nil {
		t.Error("expected ssrfHardenedTransport to block localhost requests")
	}
}

// TestSsrfHardenedTransport_BlocksPrivateIPRequests verifies that the hardened
// transport blocks requests to RFC-1918 private addresses.
func TestSsrfHardenedTransport_BlocksPrivateIPRequests(t *testing.T) {
	transport := ssrfHardenedTransport()
	if transport == nil {
		t.Fatal("nil transport")
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	_, err := client.Get("http://192.168.1.1/test")
	if err == nil {
		t.Error("expected ssrfHardenedTransport to block private IP requests")
	}
}

// --- defaultSandbox ---

// TestDefaultSandbox_ExplicitSandboxReturned verifies that an explicitly
// configured default sandbox is returned as-is.
func TestDefaultSandbox_ExplicitSandboxReturned(t *testing.T) {
	cfg := envconfig.Config{DefaultSandbox: sandbox.Codex}
	result := defaultSandbox(cfg)
	if result != sandbox.Codex {
		t.Errorf("expected %q, got %q", sandbox.Codex, result)
	}
}

// TestDefaultSandbox_ClaudeModelFallsBackToClaude verifies that when only a
// Claude default model is set (no explicit sandbox), the function returns the
// Claude sandbox.
func TestDefaultSandbox_ClaudeModelFallsBackToClaude(t *testing.T) {
	cfg := envconfig.Config{DefaultModel: "claude-opus-4-6"}
	result := defaultSandbox(cfg)
	if result != sandbox.Claude {
		t.Errorf("expected %q, got %q", sandbox.Claude, result)
	}
}

// TestDefaultSandbox_CodexModelFallsBackToCodex verifies that when only a Codex
// model is set (no explicit sandbox, no Claude model), the function returns the
// Codex sandbox.
func TestDefaultSandbox_CodexModelFallsBackToCodex(t *testing.T) {
	cfg := envconfig.Config{CodexDefaultModel: "codex-mini-latest"}
	result := defaultSandbox(cfg)
	if result != sandbox.Codex {
		t.Errorf("expected %q, got %q", sandbox.Codex, result)
	}
}

// TestDefaultSandbox_EmptyConfigReturnsClaude verifies that with no config at
// all the function falls back to the Claude sandbox.
func TestDefaultSandbox_EmptyConfigReturnsClaude(t *testing.T) {
	cfg := envconfig.Config{}
	result := defaultSandbox(cfg)
	if result != sandbox.Claude {
		t.Errorf("expected %q (default), got %q", sandbox.Claude, result)
	}
}

// --- MkdirWorkspace ---

func TestMkdirWorkspace_CreatesDirectory(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	body := `{"path":"` + ws + `","name":"new-folder"}`
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/mkdir", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.MkdirWorkspace(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	target := filepath.Join(ws, "new-folder")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("expected directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected a directory")
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["path"] != target {
		t.Errorf("expected path %q, got %q", target, resp["path"])
	}
}

func TestMkdirWorkspace_RejectsRelativePath(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	body := `{"path":"relative/path","name":"folder"}`
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/mkdir", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.MkdirWorkspace(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestMkdirWorkspace_RejectsPathTraversal(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	cases := []string{"..", "../escape", "a/b"}
	for _, name := range cases {
		body := `{"path":"` + ws + `","name":"` + name + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces/mkdir", strings.NewReader(body))
		w := httptest.NewRecorder()
		h.MkdirWorkspace(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("name %q: expected 400, got %d", name, w.Code)
		}
	}
}

func TestMkdirWorkspace_ConflictOnExisting(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	existing := filepath.Join(ws, "existing")
	if err := os.Mkdir(existing, 0755); err != nil {
		t.Fatal(err)
	}

	body := `{"path":"` + ws + `","name":"existing"}`
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/mkdir", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.MkdirWorkspace(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

// --- RenameWorkspace ---

func TestRenameWorkspace_RenamesDirectory(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	old := filepath.Join(ws, "old-name")
	if err := os.Mkdir(old, 0755); err != nil {
		t.Fatal(err)
	}

	body := `{"path":"` + old + `","name":"new-name"}`
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/rename", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.RenameWorkspace(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	newPath := filepath.Join(ws, "new-name")
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("expected old directory to be gone")
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected new directory to exist: %v", err)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["path"] != newPath {
		t.Errorf("expected path %q, got %q", newPath, resp["path"])
	}
}

func TestRenameWorkspace_ConflictOnExisting(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	src := filepath.Join(ws, "source")
	dst := filepath.Join(ws, "target")
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}

	body := `{"path":"` + src + `","name":"target"}`
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/rename", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.RenameWorkspace(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestRenameWorkspace_RejectsPathTraversal(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	src := filepath.Join(ws, "source")
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatal(err)
	}

	cases := []string{"..", "a/b"}
	for _, name := range cases {
		body := `{"path":"` + src + `","name":"` + name + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces/rename", strings.NewReader(body))
		w := httptest.NewRecorder()
		h.RenameWorkspace(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("name %q: expected 400, got %d", name, w.Code)
		}
	}
}

// --- activeGroupInfos tests ---

// TestConfigResponseIncludesActiveGroups verifies that buildConfigResponse
// includes the active_groups field with per-status task counts.
func TestConfigResponseIncludesActiveGroups(t *testing.T) {
	h, _, _ := newTestHandlerWithRealWorkspaceManager(t)
	ctx := context.Background()

	s, ok := h.currentStore()
	if !ok || s == nil {
		t.Fatal("expected store")
	}
	task1, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "p1", Timeout: 5})
	_ = s.UpdateTaskStatus(ctx, task1.ID, store.TaskStatusInProgress)
	task2, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "p2", Timeout: 5})
	_ = s.UpdateTaskStatus(ctx, task2.ID, store.TaskStatusInProgress)

	resp := h.buildConfigResponse(ctx, nil)
	raw, ok := resp["active_groups"]
	if !ok {
		t.Fatal("expected active_groups in config response")
	}
	infos, ok := raw.([]activeGroupInfo)
	if !ok {
		t.Fatalf("expected []activeGroupInfo, got %T", raw)
	}
	if len(infos) == 0 {
		t.Fatal("expected at least one active group")
	}
	found := false
	for _, info := range infos {
		if info.InProgress == 2 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected an active group with 2 in-progress tasks, got %+v", infos)
	}
}

// TestActiveGroupInfosNilManager verifies that activeGroupInfos returns
// an empty slice when the workspace manager is nil.
func TestActiveGroupInfosNilManager(t *testing.T) {
	h := &Handler{workspace: nil}
	infos := h.activeGroupInfos(context.Background())
	if infos != nil {
		t.Fatalf("expected nil, got %+v", infos)
	}
}
