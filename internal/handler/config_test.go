package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/store/storetest"
	"latere.ai/x/wallfacer/internal/workspace"
)

// workspacesBody returns a JSON body for POST /api/workspaces containing the
// provided paths. It uses json.Marshal so that path separators are escaped
// correctly on Windows (backslashes would otherwise produce invalid JSON).
func workspacesBody(t *testing.T, paths ...string) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(map[string][]string{"workspaces": paths})
	if err != nil {
		t.Fatalf("marshal workspaces body: %v", err)
	}
	return bytes.NewReader(b)
}

// syncResponseWriter wraps httptest.ResponseRecorder with a mutex so that
// concurrent writes (from an SSE handler goroutine) and reads (from the test
// goroutine polling for events) do not race on the underlying bytes.Buffer.
type syncResponseWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
	rec *httptest.ResponseRecorder
}

func newSyncResponseWriter() *syncResponseWriter {
	w := &syncResponseWriter{rec: httptest.NewRecorder()}
	// Replace the recorder's body so Header()/WriteHeader() still work,
	// but all Write calls go through our mutex-protected buffer.
	w.rec.Body = &w.buf
	return w
}

func (w *syncResponseWriter) Header() http.Header  { return w.rec.Header() }
func (w *syncResponseWriter) WriteHeader(code int) { w.rec.WriteHeader(code) }
func (w *syncResponseWriter) Flush()               {} // no-op; satisfies http.Flusher

func (w *syncResponseWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rec.Write(p)
}

// bodyString returns the accumulated response body in a thread-safe manner.
func (w *syncResponseWriter) bodyString() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// newTestHandlerWithWorkspaces creates a Handler with real workspace directories
// and an env file, so config/git/files endpoints can function.
func newTestHandlerWithWorkspaces(t *testing.T) (*Handler, string) {
	t.Helper()
	ws := t.TempDir()
	configDir := t.TempDir()

	// storetest.NewFileStore registers s.Close so background trace compaction
	// is drained before t.TempDir's RemoveAll runs (a terminal-state
	// transition would otherwise race the cleanup with "directory not empty").
	s, err := storetest.NewFileStore(t, t.TempDir())
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
	if got, ok := resp["workspace_browser_path"].(string); !ok || got != ws {
		t.Fatalf("expected workspace_browser_path %q, got %#v", ws, resp["workspace_browser_path"])
	}
	groups, ok := resp["workspace_groups"].([]any)
	if !ok || len(groups) == 0 {
		t.Fatalf("expected workspace_groups array, got %#v", resp["workspace_groups"])
	}
}

// TestGetConfig_ExcludesDormantFromWorkspaceGroups is the regression guard for
// "the sidebar disappears after switching projects": a dormant workspace
// (recovered orphan history, possibly with no folders) must NOT appear in
// workspace_groups — that list is the legacy path-switcher, and a nil folder set
// marshals to "workspaces": null, which the Sidebar popover renders with
// g.workspaces.join(...) and crashes the whole layout. Every workspace_groups
// entry must also carry a non-null workspaces array.
func TestGetConfig_ExcludesDormantFromWorkspaceGroups(t *testing.T) {
	h, _, ws := newTestHandlerWithRealWorkspaceManager(t)
	if err := workspace.SaveGroups(h.configDir, []workspace.Workspace{
		{Folders: []string{ws}, Name: "Live"},
		{ID: "dormant-1", DataKey: "deadbeefdeadbeef", Dormant: true, Name: "Recovered"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		WorkspaceGroups []struct {
			Name       string    `json:"name"`
			Workspaces *[]string `json:"workspaces"` // pointer distinguishes null from []
		} `json:"workspace_groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, g := range resp.WorkspaceGroups {
		if g.Name == "Recovered" {
			t.Errorf("dormant workspace leaked into workspace_groups: %+v", g)
		}
		if g.Workspaces == nil {
			t.Errorf("workspace_groups entry %q has null workspaces (crashes the Sidebar)", g.Name)
		}
	}
}

// TestGetConfig_PromotesActiveGroupToFront pins the promote-to-front rewrite in
// buildConfigResponse: the group matching the active workspace moves to index 0
// with its Name intact, every other group keeps its relative order, and the
// promoted group is not duplicated by the in-place removal.
func TestGetConfig_PromotesActiveGroupToFront(t *testing.T) {
	h, _, ws := newTestHandlerWithRealWorkspaceManager(t)
	other1, other2 := t.TempDir(), t.TempDir()
	if err := workspace.SaveGroups(h.configDir, []workspace.Workspace{
		{Folders: []string{other1}, Name: "First"},
		{Folders: []string{ws}, Name: "Active"},
		{Folders: []string{other2}, Name: "Last"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		WorkspaceGroups []struct {
			Name       string   `json:"name"`
			Workspaces []string `json:"workspaces"`
		} `json:"workspace_groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := make([]string, 0, len(resp.WorkspaceGroups))
	for _, g := range resp.WorkspaceGroups {
		got = append(got, g.Name)
	}
	want := []string{"Active", "First", "Last"}
	if !slices.Equal(got, want) {
		t.Fatalf("group order = %v, want %v", got, want)
	}
	if fs := resp.WorkspaceGroups[0].Workspaces; !slices.Equal(fs, []string{ws}) {
		t.Errorf("promoted group folders = %v, want %v", fs, []string{ws})
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

// TestUpdateWorkspaces_RoundTripsToConfig pins the picker "Activate" flow:
// PUT /api/workspaces with a folder must make that folder visible on the very
// next GET /api/config, so the board leaves the "Pick a workspace" empty state.
func TestUpdateWorkspaces_RoundTripsToConfig(t *testing.T) {
	h, _, _ := newTestHandlerWithRealWorkspaceManager(t)
	pick := t.TempDir()

	body, _ := json.Marshal(map[string]any{"workspaces": []string{pick}})
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces", bytes.NewReader(body))
	w := httptest.NewRecorder()
	switchWorkspacesViaConfig(h, w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT /api/workspaces: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cfgReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	cfgW := httptest.NewRecorder()
	h.GetConfig(cfgW, cfgReq)
	if cfgW.Code != http.StatusOK {
		t.Fatalf("GET /api/config: expected 200, got %d", cfgW.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(cfgW.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	workspaces, ok := resp["workspaces"].([]any)
	if !ok || len(workspaces) == 0 {
		t.Fatalf("expected activated workspace in config, got %#v", resp["workspaces"])
	}
	if workspaces[0].(string) != pick {
		t.Errorf("expected workspace %q, got %q", pick, workspaces[0])
	}
}

func TestGetConfig_OmitsRetiredIdeationSchedulerFields(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

	for _, key := range []string{"ideation", "ideation_running", "ideation_interval", "ideation_next_run"} {
		if _, ok := resp[key]; ok {
			t.Fatalf("config response included retired key %q", key)
		}
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
	// Every registered harness reaches the UI; cursor is registered.
	if !slices.Contains(sandboxes, "cursor") {
		t.Fatalf("expected sandboxes to include cursor, got %v", sandboxes)
	}
	if !slices.Contains(sandboxes, "opencode") {
		t.Fatalf("expected sandboxes to include opencode, got %v", sandboxes)
	}
	if !slices.Contains(sandboxes, "pi") {
		t.Fatalf("expected sandboxes to include pi, got %v", sandboxes)
	}
}

// TestAvailableSandboxes_IncludesRegisteredHarnesses asserts the built-in
// sandbox list is driven by the harness registry, so a registered harness
// (cursor) appears without being hardcoded.
func TestAvailableSandboxes_IncludesRegisteredHarnesses(t *testing.T) {
	got := availableSandboxes(envconfig.Config{})
	for _, want := range []harness.ID{harness.Claude, harness.Codex, harness.Cursor, harness.OpenCode, harness.Pi} {
		if !slices.Contains(got, want) {
			t.Errorf("availableSandboxes missing %q: %v", want, got)
		}
	}
}

// TestGetConfig_CursorUsableByDefault asserts cursor is reported usable in
// the GetConfig response (it has no codex-style key gate in v1).
func TestGetConfig_CursorUsableByDefault(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

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
	if usable["cursor"] != true {
		t.Fatalf("expected sandbox_usable.cursor=true, got %v", usable["cursor"])
	}
}

// TestGetConfig_OpenCodeUsableByDefault asserts opencode is reported usable in
// the GetConfig response. opencode manages provider auth itself, so it has no
// codex-style key gate.
func TestGetConfig_OpenCodeUsableByDefault(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

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
	if usable["opencode"] != true {
		t.Fatalf("expected sandbox_usable.opencode=true, got %v", usable["opencode"])
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
	if !h.isAllowedWorkspace(context.Background(), ws) {
		t.Errorf("expected %q to be allowed workspace", ws)
	}
}

func TestIsAllowedWorkspace_RejectsUnknown(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	if h.isAllowedWorkspace(context.Background(), "/tmp/not-a-workspace") {
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
	s, err := storetest.NewFileStore(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{Workspaces: []string{repo}})
	t.Cleanup(r.WaitBackground)
	return NewHandler(s, r, configDir, []string{repo}), repo
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

	s, err := storetest.NewFileStore(t, storeDir)
	if err != nil {
		t.Fatal(err)
	}

	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	ws := t.TempDir()
	wsMgr, err := workspace.NewManager(configDir, dataDir, []string{ws})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:          envFile,
		WorkspaceManager: wsMgr,
	})
	t.Cleanup(r.WaitBackground)
	t.Cleanup(r.Shutdown)

	h := NewHandler(s, r, configDir, []string{ws})
	return h, wsMgr, ws
}

// switchWorkspacesViaConfig performs the workspace switch the retired
// PUT /api/workspaces handler used to do (decode {workspaces}, switch the
// manager) and renders the config response via GetConfig, so the
// buildConfigResponse behavior these tests assert is still exercised end to end.
func switchWorkspacesViaConfig(h *Handler, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Workspaces []string `json:"workspaces"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if _, err := h.workspace.Switch(body.Workspaces); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil).WithContext(r.Context())
	h.GetConfig(w, getReq)
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
	switchWorkspacesViaConfig(h, w, req)

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
	switchWorkspacesViaConfig(h, w, req)

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
	switchWorkspacesViaConfig(h, w, req)

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
	switchWorkspacesViaConfig(h, w, req)

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

// TestMaxConcurrentTasks_PerGroupOverride pins that setting a per-group
// limit via workspace_groups in PUT /api/config overrides
// WALLFACER_MAX_PARALLEL only for the viewed group, and that a stored
// override of 0 means "unlimited" (rendered as a large sentinel so the
// >= limit guard never trips).
func TestMaxConcurrentTasks_PerGroupOverride(t *testing.T) {
	h, _, ws := newTestHandlerWithRealWorkspaceManager(t)

	// Baseline: no override -> env default applies.
	baseline := h.maxConcurrentTasks()
	if baseline <= 0 {
		t.Fatalf("baseline maxConcurrentTasks should be positive, got %d", baseline)
	}

	// Save a group entry with MaxParallel=2 for the viewed workspace.
	type reqBody struct {
		WorkspaceGroups []workspace.Workspace `json:"workspace_groups"`
	}
	two := 2
	body := reqBody{WorkspaceGroups: []workspace.Workspace{{Folders: []string{ws}, MaxParallel: &two}}}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b)))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateConfig: %d: %s", w.Code, w.Body.String())
	}

	if got := h.maxConcurrentTasks(); got != 2 {
		t.Errorf("per-group override: maxConcurrentTasks = %d, want 2", got)
	}

	// Stored override of 0 means unlimited (sentinel huge value).
	zero := 0
	body = reqBody{WorkspaceGroups: []workspace.Workspace{{Folders: []string{ws}, MaxParallel: &zero}}}
	b, _ = json.Marshal(body)
	req = httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b)))
	w = httptest.NewRecorder()
	h.UpdateConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateConfig unlimited: %d: %s", w.Code, w.Body.String())
	}
	if got := h.maxConcurrentTasks(); got < 1_000_000 {
		t.Errorf("unlimited override: expected large sentinel, got %d", got)
	}

	// Removing the override (nil pointer) falls back to the env default.
	body = reqBody{WorkspaceGroups: []workspace.Workspace{{Folders: []string{ws}}}}
	b, _ = json.Marshal(body)
	req = httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b)))
	w = httptest.NewRecorder()
	h.UpdateConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateConfig reset: %d: %s", w.Code, w.Body.String())
	}
	if got := h.maxConcurrentTasks(); got != baseline {
		t.Errorf("after clearing override: maxConcurrentTasks = %d, want baseline %d", got, baseline)
	}
}

// --- strict JSON decoding ---

// TestUpdateConfig_RejectsUnknownFields verifies that unknown JSON keys return 400.
func TestUpdateConfig_RejectsUnknownFields(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	body := `{"workspace_groups": [], "unknown_field": "surprise"}`
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
	body := `{"workspace_groups": []} trailing`
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for trailing content, got %d: %s", w.Code, w.Body.String())
	}
}

// --- defaultSandbox ---

// TestDefaultSandbox_ExplicitSandboxReturned verifies that an explicitly
// configured default sandbox is returned as-is.
func TestDefaultSandbox_ExplicitSandboxReturned(t *testing.T) {
	cfg := envconfig.Config{DefaultSandbox: harness.Codex}
	result := defaultSandbox(cfg)
	if result != harness.Codex {
		t.Errorf("expected %q, got %q", harness.Codex, result)
	}
}

// TestDefaultSandbox_ClaudeModelFallsBackToClaude verifies that when only a
// Claude default model is set (no explicit sandbox), the function returns the
// Claude sandbox.
func TestDefaultSandbox_ClaudeModelFallsBackToClaude(t *testing.T) {
	cfg := envconfig.Config{DefaultModel: "claude-opus-4-6"}
	result := defaultSandbox(cfg)
	if result != harness.Claude {
		t.Errorf("expected %q, got %q", harness.Claude, result)
	}
}

// TestDefaultSandbox_CodexModelFallsBackToCodex verifies that when only a Codex
// model is set (no explicit sandbox, no Claude model), the function returns the
// Codex sandbox.
func TestDefaultSandbox_CodexModelFallsBackToCodex(t *testing.T) {
	cfg := envconfig.Config{CodexDefaultModel: "codex-mini-latest"}
	result := defaultSandbox(cfg)
	if result != harness.Codex {
		t.Errorf("expected %q, got %q", harness.Codex, result)
	}
}

// TestDefaultSandbox_EmptyConfigReturnsClaude verifies that with no config at
// all the function falls back to the Claude sandbox.
func TestDefaultSandbox_EmptyConfigReturnsClaude(t *testing.T) {
	cfg := envconfig.Config{}
	result := defaultSandbox(cfg)
	if result != harness.Claude {
		t.Errorf("expected %q (default), got %q", harness.Claude, result)
	}
}

// --- MkdirWorkspace ---

// jsonStr returns the JSON-encoded form of s (with surrounding quotes).
// Needed for Windows paths that contain backslashes.
func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestMkdirWorkspace_CreatesDirectory(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)
	body := `{"path":` + jsonStr(ws) + `,"name":"new-folder"}`
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
	cases := []string{"..", "../escape", "..\\escape", "a/b", "a\\b"}
	for _, name := range cases {
		body := `{"path":` + jsonStr(ws) + `,"name":` + jsonStr(name) + `}`
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

	body := `{"path":` + jsonStr(ws) + `,"name":"existing"}`
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

	body := `{"path":` + jsonStr(old) + `,"name":"new-name"}`
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

	body := `{"path":` + jsonStr(src) + `,"name":"target"}`
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

	cases := []string{"..", "../escape", "..\\escape", "a/b", "a\\b"}
	for _, name := range cases {
		body := `{"path":` + jsonStr(src) + `,"name":` + jsonStr(name) + `}`
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

// TestCountInProgress_ScopedToViewedGroup pins that the parallel budget
// (WALLFACER_MAX_PARALLEL) is per-group:
// tasks in-flight in other groups do not consume the viewed group's
// concurrency budget.
func TestCountInProgress_ScopedToViewedGroup(t *testing.T) {
	h, _, _ := newTestHandlerWithRealWorkspaceManager(t)
	ctx := context.Background()

	sA, _ := h.currentStore()
	// Create one regular in-progress task and one test in-progress task in A.
	tA, err := sA.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "regular A", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask regular A: %v", err)
	}
	if err := sA.UpdateTaskStatus(ctx, tA.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus regular A: %v", err)
	}
	tAT, err := sA.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test A", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask test A: %v", err)
	}
	if err := sA.UpdateTaskTestRun(ctx, tAT.ID, true, ""); err != nil {
		t.Fatalf("UpdateTaskTestRun A: %v", err)
	}
	if err := sA.UpdateTaskStatus(ctx, tAT.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus test A: %v", err)
	}

	// Sanity: viewed group is A; counts reflect A.
	if got := h.countGlobalInProgress(); got != 1 {
		t.Fatalf("viewing A: countGlobalInProgress = %d, want 1", got)
	}

	// Switch to group B (A stays active via its in-progress tasks).
	newWS := t.TempDir()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", workspacesBody(t, newWS))
	w := httptest.NewRecorder()
	switchWorkspacesViaConfig(h, w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("switch group: %d: %s", w.Code, w.Body.String())
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if s, _ := h.currentStore(); s != nil && s != sA {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Workspace B is empty; its concurrency budget must be fresh (0), even
	// though group A still has tasks running.
	if got := h.countGlobalInProgress(); got != 0 {
		t.Errorf("viewing empty B: countGlobalInProgress = %d, want 0 (A's in-flight must not consume B's budget)", got)
	}
}
