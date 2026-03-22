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
	if len(images) != 2 {
		t.Fatalf("expected 2 images (claude + codex), got %d", len(images))
	}
	// With test runner (empty config), both should be uncached.
	for _, img := range images {
		m := img.(map[string]any)
		if m["cached"].(bool) {
			t.Errorf("expected cached=false for test handler, got true for %v", m["sandbox"])
		}
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
