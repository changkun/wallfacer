package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"latere.ai/x/wallfacer/internal/handler"
	"latere.ai/x/wallfacer/internal/metrics"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/workspace"
)

// newWorkspaceTestServer boots an in-process HTTP server backed by a REAL,
// switchable workspace manager (not the static fallback), so workspace CRUD and
// activate-by-id can be driven end to end over the mux→handler→manager→store
// path. The MockRunner's Cmd is "true", so created tasks stay in backlog and
// never run; no container or trace compaction is involved.
func newWorkspaceTestServer(t *testing.T) (*httptest.Server, *workspace.Manager, string) {
	t.Helper()
	configDir := t.TempDir()
	dataDir := filepath.Join(configDir, "data")
	envPath := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envPath, []byte("ANTHROPIC_API_KEY=test\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	mgr, err := workspace.NewManager(configDir, dataDir, envPath, []string{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	snap := mgr.Snapshot()
	mock := &runner.MockRunner{
		EnvFilePath:  envPath,
		Cmd:          "true",
		WtDir:        filepath.Join(configDir, "wt"),
		WorkspaceMgr: mgr,
	}
	h := handler.NewHandler(snap.Store, mock, configDir, snap.Workspaces, metrics.NewRegistry())
	mux := BuildMux(h, metrics.NewRegistry(), IndexViewData{}, testFS(t), nil, false)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, mgr, configDir
}

func wsReq(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func decodeWS(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return m
}

func countTasks(t *testing.T, srvURL string) int {
	t.Helper()
	resp, err := http.Get(srvURL + "/api/tasks")
	if err != nil {
		t.Fatalf("GET /api/tasks: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var tasks []store.Task
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	return len(tasks)
}

// TestWorkspaceLifecycleE2E drives the whole workspace model over real HTTP and
// proves the two properties that justify the redesign: a task created in a
// workspace SURVIVES a folder edit (identity decoupled from membership), and a
// second workspace over the same folders has INDEPENDENT, empty history.
func TestWorkspaceLifecycleE2E(t *testing.T) {
	srv, _, _ := newWorkspaceTestServer(t)
	dirA, dirB := t.TempDir(), t.TempDir()

	// Create workspace A.
	createA := decodeWS(t, wsReq(t, http.MethodPost, srv.URL+"/api/workspaces",
		`{"name":"A","folders":["`+dirA+`"]}`))
	idA, _ := createA["id"].(string)
	if idA == "" {
		t.Fatalf("create A returned no id: %v", createA)
	}

	// Activate A; config reports it active.
	actResp := wsReq(t, http.MethodPost, srv.URL+"/api/workspaces/"+idA+"/activate", "")
	if actResp.StatusCode != http.StatusOK {
		t.Fatalf("activate A: status %d", actResp.StatusCode)
	}
	cfg := decodeWS(t, actResp)
	if cfg["workspace_id"] != idA {
		t.Fatalf("after activate, workspace_id=%v want %q", cfg["workspace_id"], idA)
	}

	// Create a task in A's board.
	if resp := postJSON(t, srv.URL+"/api/tasks", `{"prompt":"work in A"}`); resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("create task: status %d", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}
	if n := countTasks(t, srv.URL); n != 1 {
		t.Fatalf("expected 1 task in A, got %d", n)
	}

	// Edit A's folders (add dirB). The task must survive.
	editResp := wsReq(t, http.MethodPut, srv.URL+"/api/workspaces/"+idA,
		`{"folders":["`+dirA+`","`+dirB+`"]}`)
	if editResp.StatusCode != http.StatusOK {
		t.Fatalf("edit folders: status %d", editResp.StatusCode)
	}
	_ = editResp.Body.Close()
	if n := countTasks(t, srv.URL); n != 1 {
		t.Fatalf("task lost after folder edit: got %d tasks, want 1 (history orphaned)", n)
	}

	// Config reflects the new folder set, same workspace id.
	cfg2 := decodeWS(t, wsReq(t, http.MethodGet, srv.URL+"/api/config", ""))
	if cfg2["workspace_id"] != idA {
		t.Fatalf("workspace id changed after edit: %v", cfg2["workspace_id"])
	}
	if ws, ok := cfg2["workspaces"].([]any); !ok || len(ws) != 2 {
		t.Fatalf("config folders not updated: %v", cfg2["workspaces"])
	}

	// Create workspace B over the SAME folder; activate it; its board is empty.
	createB := decodeWS(t, wsReq(t, http.MethodPost, srv.URL+"/api/workspaces",
		`{"name":"B","folders":["`+dirA+`"]}`))
	idB, _ := createB["id"].(string)
	if idB == "" || idB == idA {
		t.Fatalf("create B returned bad id %q (== A %q?)", idB, idA)
	}
	if resp := wsReq(t, http.MethodPost, srv.URL+"/api/workspaces/"+idB+"/activate", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("activate B: status %d", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}
	if n := countTasks(t, srv.URL); n != 0 {
		t.Fatalf("workspace B over the same folder inherited history: got %d tasks, want 0", n)
	}

	// Deleting a workspace returns 200 with the new config. The active one is
	// deletable now (it wipes data + auto-switches): delete active B → the board
	// switches to A; then delete A → 200 (no workspaces left).
	delB := wsReq(t, http.MethodDelete, srv.URL+"/api/workspaces/"+idB, "")
	_ = delB.Body.Close()
	if delB.StatusCode != http.StatusOK {
		t.Fatalf("delete active B: status %d, want 200", delB.StatusCode)
	}
	delA := wsReq(t, http.MethodDelete, srv.URL+"/api/workspaces/"+idA, "")
	_ = delA.Body.Close()
	if delA.StatusCode != http.StatusOK {
		t.Fatalf("delete A: status %d, want 200", delA.StatusCode)
	}
}

// TestWorkspaceMigrationE2E seeds a legacy workspace-groups.json plus an orphan
// data directory, runs the startup migration, then boots the manager+handler and
// lists workspaces over HTTP — proving the migration result is consumable by the
// live server (live group + adopted dormant orphan both appear).
func TestWorkspaceMigrationE2E(t *testing.T) {
	configDir := t.TempDir()
	dataDir := filepath.Join(configDir, "data")
	envPath := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envPath, []byte("ANTHROPIC_API_KEY=test\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	dirLive := t.TempDir()

	// Legacy file with one live group.
	legacy := `[{"name":"Live","workspaces":["` + dirLive + `"]}]`
	if err := os.WriteFile(filepath.Join(configDir, "workspace-groups.json"), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	// An orphan data dir with task history (matches no live group).
	orphanDir := filepath.Join(dataDir, "abc0000000000099", "task-1")
	if err := os.MkdirAll(orphanDir, 0o755); err != nil {
		t.Fatalf("mkdir orphan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(orphanDir, "task.json"), []byte(`{"worktree_paths":{}}`), 0o644); err != nil {
		t.Fatalf("write orphan task: %v", err)
	}

	migrated, err := workspace.MigrateToWorkspaces(configDir, dataDir, "e2e")
	if err != nil || !migrated {
		t.Fatalf("migration: migrated=%v err=%v", migrated, err)
	}

	mgr, err := workspace.NewManager(configDir, dataDir, envPath, []string{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	snap := mgr.Snapshot()
	mock := &runner.MockRunner{EnvFilePath: envPath, Cmd: "true", WtDir: filepath.Join(configDir, "wt"), WorkspaceMgr: mgr}
	h := handler.NewHandler(snap.Store, mock, configDir, snap.Workspaces, metrics.NewRegistry())
	mux := BuildMux(h, metrics.NewRegistry(), IndexViewData{}, testFS(t), nil, false)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp := decodeWS(t, wsReq(t, http.MethodGet, srv.URL+"/api/workspaces", ""))
	list, _ := resp["workspaces"].([]any)
	var live, dormant int
	for _, item := range list {
		w, _ := item.(map[string]any)
		if d, _ := w["dormant"].(bool); d {
			dormant++
		} else {
			live++
		}
	}
	if live != 1 {
		t.Errorf("expected 1 live workspace post-migration over HTTP, got %d (%v)", live, list)
	}
	if dormant != 1 {
		t.Errorf("expected 1 dormant (adopted orphan) workspace, got %d", dormant)
	}
}
