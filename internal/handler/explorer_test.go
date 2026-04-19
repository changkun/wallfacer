package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/store"
)

func TestExplorerTree_Basic(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	// Create dirs and files in the workspace.
	for _, d := range []string{"Beta", "alpha"} {
		if err := os.Mkdir(filepath.Join(ws, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"zebra.txt", "apple.txt"} {
		if err := os.WriteFile(filepath.Join(ws, f), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/explorer/tree?path="+ws+"&workspace="+ws, nil)
	w := httptest.NewRecorder()
	h.ExplorerTree(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var entries []explorerEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}

	// Expect dirs first (case-insensitive: alpha, Beta), then files (apple.txt, zebra.txt).
	want := []string{"alpha", "Beta", "apple.txt", "zebra.txt"}
	if len(entries) != len(want) {
		t.Fatalf("expected %d entries, got %d: %+v", len(want), len(entries), entries)
	}
	for i, name := range want {
		if entries[i].Name != name {
			t.Errorf("entry[%d]: expected %q, got %q", i, name, entries[i].Name)
		}
	}

	// Verify types.
	if entries[0].Type != "dir" || entries[1].Type != "dir" {
		t.Error("first two entries should be dirs")
	}
	if entries[2].Type != "file" || entries[3].Type != "file" {
		t.Error("last two entries should be files")
	}

	// Files should have size > 0.
	if entries[2].Size == 0 {
		t.Error("file entry should have non-zero size")
	}
	// Dirs should omit size (zero value).
	if entries[0].Size != 0 {
		t.Error("dir entry should have zero size")
	}
}

func TestExplorerTree_HiddenEntries(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	if err := os.Mkdir(filepath.Join(ws, ".hidden"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".gitignore"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/explorer/tree?path="+ws+"&workspace="+ws, nil)
	w := httptest.NewRecorder()
	h.ExplorerTree(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var entries []explorerEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (hidden dir + hidden file), got %d", len(entries))
	}
	if entries[0].Name != ".hidden" || entries[0].Type != "dir" {
		t.Errorf("expected .hidden dir, got %+v", entries[0])
	}
	if entries[1].Name != ".gitignore" || entries[1].Type != "file" {
		t.Errorf("expected .gitignore file, got %+v", entries[1])
	}
}

func TestExplorerTree_MissingParams(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	tests := []struct {
		name string
		url  string
	}{
		{"missing both", "/api/explorer/tree"},
		{"missing path", "/api/explorer/tree?workspace=" + ws},
		{"missing workspace", "/api/explorer/tree?path=" + ws},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			w := httptest.NewRecorder()
			h.ExplorerTree(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestExplorerTree_WorkspaceNotConfigured(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)

	bogus := t.TempDir()
	req := httptest.NewRequest(http.MethodGet, "/api/explorer/tree?path="+bogus+"&workspace="+bogus, nil)
	w := httptest.NewRecorder()
	h.ExplorerTree(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIsWithinWorkspace_Valid(t *testing.T) {
	ws := t.TempDir()
	sub := filepath.Join(ws, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}

	got, err := isWithinWorkspace(sub, ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The resolved path should end with /sub.
	if filepath.Base(got) != "sub" {
		t.Errorf("expected resolved path ending in 'sub', got %q", got)
	}
}

func TestIsWithinWorkspace_TraversalAttack(t *testing.T) {
	ws := t.TempDir()
	// Create a sibling directory to traverse into.
	sibling := t.TempDir()

	// Construct a path that tries to escape via ..
	attack := filepath.Join(ws, "..", filepath.Base(sibling))
	_, err := isWithinWorkspace(attack, ws)
	if err == nil {
		t.Error("expected error for traversal attack, got nil")
	}
}

func TestIsWithinWorkspace_SymlinkEscape(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()

	// Create a symlink inside ws that points outside.
	link := filepath.Join(ws, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	_, err := isWithinWorkspace(link, ws)
	if err == nil {
		t.Error("expected error for symlink escape, got nil")
	}
}

func TestIsWithinWorkspace_ExactWorkspaceRoot(t *testing.T) {
	ws := t.TempDir()

	got, err := isWithinWorkspace(ws, ws)
	if err != nil {
		t.Fatalf("workspace root should be allowed, got error: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty resolved path")
	}
}

// --- ExplorerReadFile tests ---

func TestExplorerReadFile_TextFile(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	content := "hello world\nline two\n"
	fp := filepath.Join(ws, "readme.txt")
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/explorer/file?path="+fp+"&workspace="+ws, nil)
	w := httptest.NewRecorder()
	h.ExplorerReadFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("expected text/plain content type, got %q", ct)
	}
	if w.Body.String() != content {
		t.Errorf("expected %q, got %q", content, w.Body.String())
	}
	if sz := w.Header().Get("X-File-Size"); sz == "" {
		t.Error("expected X-File-Size header")
	}
	if w.Header().Get("X-File-Binary") != "" {
		t.Error("text file should not have X-File-Binary header")
	}
}

func TestExplorerReadFile_BinaryFile(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	// Create a file with null bytes (binary).
	data := []byte("ELF\x00\x01\x02\x03")
	fp := filepath.Join(ws, "binary.bin")
	if err := os.WriteFile(fp, data, 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/explorer/file?path="+fp+"&workspace="+ws, nil)
	w := httptest.NewRecorder()
	h.ExplorerReadFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-File-Binary") != "true" {
		t.Error("expected X-File-Binary: true header")
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["binary"] != true {
		t.Errorf("expected binary=true, got %v", resp["binary"])
	}
	if resp["size"] == nil {
		t.Error("expected size field in response")
	}
}

func TestExplorerReadFile_LargeFile(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	// Create a file larger than the limit.
	fp := filepath.Join(ws, "huge.txt")
	size := constants.ExplorerMaxFileSize + 1
	if err := os.WriteFile(fp, make([]byte, size), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/explorer/file?path="+fp+"&workspace="+ws, nil)
	w := httptest.NewRecorder()
	h.ExplorerReadFile(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "file too large" {
		t.Errorf("expected 'file too large' error, got %v", resp["error"])
	}
	if resp["max"] == nil || resp["size"] == nil {
		t.Error("expected max and size fields in response")
	}
}

func TestExplorerReadFile_NotFound(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	fp := filepath.Join(ws, "nonexistent.txt")
	req := httptest.NewRequest(http.MethodGet, "/api/explorer/file?path="+fp+"&workspace="+ws, nil)
	w := httptest.NewRecorder()
	h.ExplorerReadFile(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExplorerReadFile_Directory(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	dir := filepath.Join(ws, "subdir")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/explorer/file?path="+dir+"&workspace="+ws, nil)
	w := httptest.NewRecorder()
	h.ExplorerReadFile(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "directory") {
		t.Errorf("expected error about directory, got %q", w.Body.String())
	}
}

func TestExplorerReadFile_OutsideWorkspace(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)

	// Use a path outside the configured workspace.
	outside := t.TempDir()
	fp := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(fp, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	// The workspace param must be a configured one, but the path escapes it.
	// Since 'outside' is not a configured workspace, this will fail at
	// workspace validation.
	req := httptest.NewRequest(http.MethodGet, "/api/explorer/file?path="+fp+"&workspace="+outside, nil)
	w := httptest.NewRecorder()
	h.ExplorerReadFile(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExplorerReadFile_MissingParams(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	tests := []struct {
		name string
		url  string
	}{
		{"missing both", "/api/explorer/file"},
		{"missing path", "/api/explorer/file?workspace=" + ws},
		{"missing workspace", "/api/explorer/file?path=/tmp/x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			w := httptest.NewRecorder()
			h.ExplorerReadFile(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}
		})
	}
}

// --- ExplorerWriteFile tests ---

func writeFileRequest(t *testing.T, path, workspace, content string) *http.Request {
	t.Helper()
	body := `{"path":` + jsonString(path) + `,"workspace":` + jsonString(workspace) + `,"content":` + jsonString(content) + `}`
	return httptest.NewRequest(http.MethodPut, "/api/explorer/file", strings.NewReader(body))
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestExplorerWriteFile_Success(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	fp := filepath.Join(ws, "hello.txt")
	// Create the file first so isWithinWorkspace can resolve it.
	if err := os.WriteFile(fp, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	content := "new content\nline two\n"
	req := writeFileRequest(t, fp, ws, content)
	w := httptest.NewRecorder()
	h.ExplorerWriteFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", resp["status"])
	}
	if int(resp["size"].(float64)) != len(content) {
		t.Errorf("expected size=%d, got %v", len(content), resp["size"])
	}

	// Read back and verify.
	got, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("file content mismatch: got %q, want %q", got, content)
	}
}

func TestExplorerWriteFile_AtomicWrite(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	fp := filepath.Join(ws, "atomic.txt")
	original := "original content"
	if err := os.WriteFile(fp, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	// Write new content; after the call the file should contain the new
	// content completely (no partial writes visible).
	newContent := "replaced atomically"
	req := writeFileRequest(t, fp, ws, newContent)
	w := httptest.NewRecorder()
	h.ExplorerWriteFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	got, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != newContent {
		t.Errorf("expected %q, got %q", newContent, got)
	}

	// Verify no leftover temp files in the directory.
	entries, err := os.ReadDir(ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".wallfacer-write-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestExplorerWriteFile_GitPathRejection(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	gitDir := filepath.Join(ws, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	gitFile := filepath.Join(gitDir, "config")
	if err := os.WriteFile(gitFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	paths := []string{
		filepath.Join(ws, ".git/config"),
		filepath.Join(ws, ".git"),
	}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			req := writeFileRequest(t, p, ws, "hacked")
			w := httptest.NewRecorder()
			h.ExplorerWriteFile(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for git path %q, got %d: %s", p, w.Code, w.Body.String())
			}
		})
	}
}

func TestExplorerWriteFile_OutsideWorkspace(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	outside := t.TempDir()
	fp := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(fp, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	req := writeFileRequest(t, fp, ws, "overwrite attempt")
	w := httptest.NewRecorder()
	h.ExplorerWriteFile(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExplorerWriteFile_TooLarge(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	fp := filepath.Join(ws, "big.txt")
	if err := os.WriteFile(fp, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// Content exceeding 2 MB.
	bigContent := strings.Repeat("x", maxFileWriteSize+1)
	req := writeFileRequest(t, fp, ws, bigContent)
	w := httptest.NewRecorder()
	h.ExplorerWriteFile(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExplorerWriteFile_WorkspaceNotConfigured(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)

	bogus := t.TempDir()
	fp := filepath.Join(bogus, "file.txt")
	if err := os.WriteFile(fp, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	req := writeFileRequest(t, fp, bogus, "content")
	w := httptest.NewRecorder()
	h.ExplorerWriteFile(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "workspace not configured") {
		t.Errorf("expected 'workspace not configured' error, got %q", w.Body.String())
	}
}

func TestExplorerWriteFile_CreateParentDirs(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	// Parent directory "nonexistent" does not exist.
	fp := filepath.Join(ws, "nonexistent", "file.txt")

	req := writeFileRequest(t, fp, ws, "content")
	w := httptest.NewRecorder()
	h.ExplorerWriteFile(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing parent dir, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "parent directory") {
		t.Errorf("expected error about parent directory, got %q", w.Body.String())
	}
}

func TestExplorerStream_SendsRefreshOnChange(t *testing.T) {
	h, ws := newTestHandlerWithWorkspaces(t)

	// Use a short-lived context so the SSE handler exits quickly.
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/explorer/stream", nil).WithContext(ctx)
	w := newSyncResponseWriter()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ExplorerStream(w, req)
	}()

	// Wait for the connected event.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("timed out waiting for connected event")
		default:
		}
		if strings.Contains(w.bodyString(), "event: connected") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Create a file to trigger a change.
	if err := os.WriteFile(filepath.Join(ws, "newfile.txt"), []byte("hello"), 0644); err != nil {
		cancel()
		<-done
		t.Fatal(err)
	}

	// Wait for a refresh event (poll interval is 3s).
	deadline = time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("timed out waiting for refresh event")
		default:
		}
		if strings.Contains(w.bodyString(), "event: refresh") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	<-done

	// Verify the refresh event contains workspace info.
	body := w.bodyString()
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "workspaces") {
			var payload struct {
				Workspaces []string `json:"workspaces"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
				t.Fatalf("failed to parse refresh data: %v", err)
			}
			if len(payload.Workspaces) == 0 {
				t.Error("expected at least one workspace in refresh event")
			}
			return
		}
	}
	t.Error("refresh event data not found in response body")
}

func TestExplorerStream_NoRefreshWhenUnchanged(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/explorer/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ExplorerStream(w, req)
	}()

	// Wait for connected event + one poll cycle (3s).
	time.Sleep(4 * time.Second)
	cancel()
	<-done

	body := w.Body.String()
	if strings.Contains(body, "event: refresh") {
		t.Error("expected no refresh event when workspace is unchanged")
	}
	if !strings.Contains(body, "event: connected") {
		t.Error("expected connected event")
	}
}

// --- ExplorerTaskPrompts tests ---

func TestTaskPromptsEndpoint_DefaultBacklog(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	ctx := t.Context()

	// Create one backlog task and one waiting task.
	backlog, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "backlog task", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "waiting task", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, waiting.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, waiting.ID, store.TaskStatusWaiting); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/explorer/task-prompts", nil)
	w := httptest.NewRecorder()
	h.ExplorerTaskPrompts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var entries []taskPromptEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Default returns only backlog.
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d: %+v", len(entries), entries)
	}
	if entries[0].TaskID != backlog.ID.String() {
		t.Errorf("expected backlog task %s, got %s", backlog.ID, entries[0].TaskID)
	}
	if entries[0].Status != "backlog" {
		t.Errorf("expected status backlog, got %s", entries[0].Status)
	}
}

func TestTaskPromptsEndpoint_WithWaiting(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	ctx := t.Context()

	_, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "backlog task", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "waiting task", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, waiting.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, waiting.ID, store.TaskStatusWaiting); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/explorer/task-prompts?status=backlog,waiting", nil)
	w := httptest.NewRecorder()
	h.ExplorerTaskPrompts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var entries []taskPromptEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	statuses := make(map[string]bool)
	for _, e := range entries {
		statuses[e.Status] = true
	}
	if !statuses["backlog"] || !statuses["waiting"] {
		t.Errorf("expected both backlog and waiting, got statuses: %v", statuses)
	}
}

func TestTaskPromptsEndpoint_RejectsFailed(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)

	for _, badStatus := range []string{"failed", "done", "cancelled", "in_progress", "committing"} {
		req := httptest.NewRequest(http.MethodGet, "/api/explorer/task-prompts?status="+badStatus, nil)
		w := httptest.NewRecorder()
		h.ExplorerTaskPrompts(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("status=%s: expected 422, got %d: %s", badStatus, w.Code, w.Body.String())
		}
	}
}

func TestTaskPromptsEndpoint_ExcludesArchivedAndTombstoned(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	ctx := t.Context()

	visible, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "visible backlog", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}

	// Create and soft-delete a task (tombstoned).
	tombstoned, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "tombstoned task", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.DeleteTask(ctx, tombstoned.ID, "test"); err != nil {
		t.Fatal(err)
	}

	// Archived tasks: only done/cancelled can be archived, so create a done task and archive it.
	// (Backlog tasks cannot be archived per the API contract, but the store allows it.)
	archived, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "archived done", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, archived.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, archived.ID, store.TaskStatusDone); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetTaskArchived(ctx, archived.ID, true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/explorer/task-prompts", nil)
	w := httptest.NewRecorder()
	h.ExplorerTaskPrompts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var entries []taskPromptEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (only visible backlog), got %d: %+v", len(entries), entries)
	}
	if entries[0].TaskID != visible.ID.String() {
		t.Errorf("expected visible task %s, got %s", visible.ID, entries[0].TaskID)
	}
}
