//go:build desktop

package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTrayManagerNew verifies that NewTrayManager initializes without panic
// and stores the callbacks.
func TestTrayManagerNew(t *testing.T) {
	called := false
	showFn := func() { called = true }
	quitFn := func() {}

	tm := NewTrayManager(showFn, quitFn, "http://localhost:1234", "")
	if tm == nil {
		t.Fatal("expected non-nil TrayManager")
	}
	if tm.showWindow == nil {
		t.Fatal("expected non-nil showWindow callback")
	}
	if tm.quit == nil {
		t.Fatal("expected non-nil quit callback")
	}
	if tm.serverURL != "http://localhost:1234" {
		t.Fatalf("unexpected serverURL: %s", tm.serverURL)
	}

	tm.showWindow()
	if !called {
		t.Fatal("showWindow callback was not invoked")
	}
}

func TestIconState(t *testing.T) {
	tests := []struct {
		name  string
		tasks map[string]int
		want  string
	}{
		{"all zeros", map[string]int{}, "idle"},
		{"only backlog", map[string]int{"backlog": 5}, "idle"},
		{"in_progress", map[string]int{"in_progress": 2}, "active"},
		{"committing", map[string]int{"committing": 1}, "active"},
		{"in_progress and committing", map[string]int{"in_progress": 1, "committing": 1}, "active"},
		{"waiting only", map[string]int{"waiting": 1}, "attention"},
		{"failed only", map[string]int{"failed": 2}, "attention"},
		{"waiting beats active", map[string]int{"in_progress": 3, "waiting": 1}, "attention"},
		{"failed beats active", map[string]int{"in_progress": 1, "failed": 1}, "attention"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := iconState(tc.tasks)
			if got != tc.want {
				t.Errorf("iconState(%v) = %q, want %q", tc.tasks, got, tc.want)
			}
		})
	}
}

func TestFormatTooltip(t *testing.T) {
	tests := []struct {
		name   string
		tasks  map[string]int
		uptime float64
		want   string
	}{
		{
			"idle",
			map[string]int{},
			3600,
			"Wallfacer — Idle",
		},
		{
			"running only",
			map[string]int{"in_progress": 2, "committing": 1},
			8100,
			"Wallfacer — 3 running · 0 waiting · uptime 2h 15m",
		},
		{
			"running and waiting",
			map[string]int{"in_progress": 1, "waiting": 2},
			300,
			"Wallfacer — 1 running · 2 waiting · uptime 5m",
		},
		{
			"waiting only shows tooltip",
			map[string]int{"waiting": 1},
			60,
			"Wallfacer — 0 running · 1 waiting · uptime 1m",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatTooltip(tc.tasks, tc.uptime)
			if got != tc.want {
				t.Errorf("formatTooltip() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds float64
		want    string
	}{
		{0, "0m"},
		{59, "0m"},
		{60, "1m"},
		{3600, "1h 0m"},
		{8100, "2h 15m"},
	}
	for _, tc := range tests {
		got := formatDuration(tc.seconds)
		if got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}

func TestPollHealthResponse(t *testing.T) {
	// Mock server returning a health response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/debug/health" {
			http.NotFound(w, r)
			return
		}
		resp := map[string]any{
			"goroutines":         42,
			"tasks_by_status":    map[string]int{"backlog": 4, "in_progress": 2, "waiting": 1},
			"running_containers": map[string]any{"count": 2, "items": []any{}},
			"uptime_seconds":     8100.5,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tm := NewTrayManager(func() {}, func() {}, srv.URL, "")
	data, err := tm.fetchHealth()
	if err != nil {
		t.Fatalf("fetchHealth() error: %v", err)
	}
	if data.TasksByStatus["backlog"] != 4 {
		t.Errorf("backlog = %d, want 4", data.TasksByStatus["backlog"])
	}
	if data.TasksByStatus["in_progress"] != 2 {
		t.Errorf("in_progress = %d, want 2", data.TasksByStatus["in_progress"])
	}
	if data.TasksByStatus["waiting"] != 1 {
		t.Errorf("waiting = %d, want 1", data.TasksByStatus["waiting"])
	}
	if data.UptimeSeconds < 8100 || data.UptimeSeconds > 8101 {
		t.Errorf("uptime = %f, want ~8100.5", data.UptimeSeconds)
	}
}

func TestPollHealthWithAPIKey(t *testing.T) {
	// Server that requires an API key.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		resp := map[string]any{
			"tasks_by_status": map[string]int{},
			"uptime_seconds":  10.0,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Without key — should fail.
	tm := NewTrayManager(func() {}, func() {}, srv.URL, "")
	_, err := tm.fetchHealth()
	if err == nil {
		t.Fatal("expected error without API key")
	}

	// With key — should succeed.
	tm = NewTrayManager(func() {}, func() {}, srv.URL, "test-key")
	data, err := tm.fetchHealth()
	if err != nil {
		t.Fatalf("fetchHealth() with key error: %v", err)
	}
	if data.UptimeSeconds != 10.0 {
		t.Errorf("uptime = %f, want 10.0", data.UptimeSeconds)
	}
}
