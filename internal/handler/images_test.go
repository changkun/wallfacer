package handler

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetImageStatus_ReturnsImages(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	w := httptest.NewRecorder()
	h.GetImageStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	images, ok := resp["images"].([]any)
	if !ok {
		t.Fatalf("expected images array, got %T", resp["images"])
	}
	// The unified sandbox-agents image returns a single entry; the legacy
	// per-agent images have been collapsed.
	if len(images) != 1 {
		t.Fatalf("expected 1 image (unified sandbox-agents), got %d", len(images))
	}
	m := images[0].(map[string]any)
	if m["cached"].(bool) {
		t.Errorf("expected cached=false for test handler, got true for %v", m["sandbox"])
	}
}

func TestPullImage_NeedsSandboxField(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/images/pull",
		strings.NewReader(`{"sandbox":"claude"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.PullImage(w, req)

	// Test runner has no container command, so this should fail.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing runtime, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStreamImagePull_UnknownPullID(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/images/pull/stream?pull_id=nonexistent", nil)
	w := httptest.NewRecorder()
	h.StreamImagePull(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteImage_NeedsRuntime(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/images",
		strings.NewReader(`{"sandbox":"claude"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.DeleteImage(w, req)

	// Test runner has no container command, so this should fail.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing runtime, got %d: %s", w.Code, w.Body.String())
	}
}

func TestScanLinesOrCR(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"a\nb\nc", []string{"a", "b", "c"}},
		{"a\r\nb\r\nc", []string{"a", "b", "c"}},
		{"a\rb\rc", []string{"a", "b", "c"}},
		{"progress 50%\rprogress 100%\ndone\n", []string{"progress 50%", "progress 100%", "done"}},
		{"", nil},
	}
	for _, tt := range tests {
		scanner := bufio.NewScanner(strings.NewReader(tt.input))
		scanner.Split(scanLinesOrCR)
		var got []string
		for scanner.Scan() {
			if t := scanner.Text(); t != "" {
				got = append(got, t)
			}
		}
		if len(got) != len(tt.want) {
			t.Errorf("input %q: got %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("input %q: token %d = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestParsePullLine(t *testing.T) {
	tests := []struct {
		line           string
		prevLayers     int
		wantPhase      string
		wantLayersDone int
	}{
		{"Trying to pull ghcr.io/foo/bar:latest...", 0, "resolving", 0},
		{"Getting image source signatures", 0, "resolving", 0},
		{"Copying blob sha256:abc123", 0, "copying", 1},
		{"Copying blob sha256:def456", 1, "copying", 2},
		{"Copying config sha256:789abc", 2, "copying", 3},
		{"Writing manifest to image destination", 3, "manifest", 3},
		{"Storing signatures", 3, "done", 3},
		{"Pull complete: ghcr.io/foo/bar:latest", 3, "done", 3},
		{"error: connection refused", 0, "error", 0},
		{"some random output", 0, "unknown", 0},
	}
	for _, tt := range tests {
		prog := parsePullLine(tt.line, tt.prevLayers)
		if prog.Phase != tt.wantPhase {
			t.Errorf("line %q: phase = %q, want %q", tt.line, prog.Phase, tt.wantPhase)
		}
		if prog.LayersDone != tt.wantLayersDone {
			t.Errorf("line %q: layers_done = %d, want %d", tt.line, prog.LayersDone, tt.wantLayersDone)
		}
		if prog.Line != tt.line {
			t.Errorf("line %q: Line = %q, want %q", tt.line, prog.Line, tt.line)
		}
	}
}

func TestStreamImagePull_StructuredProgress(t *testing.T) {
	h := newTestHandler(t)
	p := &imagePull{
		ID:    "test-structured",
		Image: "test:latest",
		Lines: make(chan string, 16),
		Done:  make(chan struct{}),
	}
	h.pulls.store(p)

	// Send structured progress lines (as runPull would).
	prog1 := pullProgress{Line: "Trying to pull test:latest...", Phase: "resolving", LayersDone: 0}
	prog2 := pullProgress{Line: "Copying blob sha256:abc", Phase: "copying", LayersDone: 1}
	data1, _ := json.Marshal(prog1)
	data2, _ := json.Marshal(prog2)
	p.Lines <- string(data1)
	p.Lines <- string(data2)
	p.Success = true
	close(p.Done)

	req := httptest.NewRequest(http.MethodGet, "/api/images/pull/stream?pull_id=test-structured", nil)
	w := httptest.NewRecorder()
	h.StreamImagePull(w, req)

	body := w.Body.String()
	// Should contain both progress events with structured fields.
	if !strings.Contains(body, `"phase":"resolving"`) {
		t.Errorf("expected resolving phase in SSE output, got:\n%s", body)
	}
	if !strings.Contains(body, `"phase":"copying"`) {
		t.Errorf("expected copying phase in SSE output, got:\n%s", body)
	}
	if !strings.Contains(body, `"layers_done":1`) {
		t.Errorf("expected layers_done:1 in SSE output, got:\n%s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("expected done event in SSE output, got:\n%s", body)
	}
}

func TestPullTracker_Deduplication(t *testing.T) {
	pt := newPullTracker()
	p := &imagePull{
		ID:    "test-id",
		Image: "test:latest",
		Lines: make(chan string, 1),
		Done:  make(chan struct{}),
	}
	pt.store(p)

	// Active pull should be found.
	if got := pt.activeForImage("test:latest"); got != p {
		t.Fatalf("expected active pull, got %v", got)
	}

	// Complete the pull.
	close(p.Done)

	// Completed pull should not be returned as active.
	if got := pt.activeForImage("test:latest"); got != nil {
		t.Fatalf("expected nil for completed pull, got %v", got)
	}
}
