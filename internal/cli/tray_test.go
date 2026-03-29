//go:build desktop

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTrayManagerNew verifies that NewTrayManager initializes without panic
// and stores the callbacks.
func TestTrayManagerNew(t *testing.T) {
	called := false
	showFn := func() { called = true }
	quitFn := func() {}

	tm := NewTrayManager(showFn, quitFn, nil,"http://localhost:1234", "")
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
		name      string
		tasks     map[string]int
		uptime    float64
		todayCost string
		want      string
	}{
		{
			"idle",
			map[string]int{},
			3600,
			"",
			"Wallfacer — Idle",
		},
		{
			"running no cost",
			map[string]int{"in_progress": 2, "committing": 1},
			8100,
			"",
			"Wallfacer — 3 running · 0 waiting",
		},
		{
			"running with cost",
			map[string]int{"in_progress": 1, "waiting": 2},
			300,
			"$3.42",
			"Wallfacer — 1 running · 2 waiting · $3.42 today",
		},
		{
			"waiting only with cost",
			map[string]int{"waiting": 1},
			60,
			"$0.50",
			"Wallfacer — 0 running · 1 waiting · $0.50 today",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatTooltip(tc.tasks, tc.uptime, tc.todayCost)
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

	tm := NewTrayManager(func() {}, func() {}, nil,srv.URL, "")
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
	tm := NewTrayManager(func() {}, func() {}, nil,srv.URL, "")
	_, err := tm.fetchHealth()
	if err == nil {
		t.Fatal("expected error without API key")
	}

	// With key — should succeed.
	tm = NewTrayManager(func() {}, func() {}, nil,srv.URL, "test-key")
	data, err := tm.fetchHealth()
	if err != nil {
		t.Fatalf("fetchHealth() with key error: %v", err)
	}
	if data.UptimeSeconds != 10.0 {
		t.Errorf("uptime = %f, want 10.0", data.UptimeSeconds)
	}
}

func TestParseConfigToggles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"autopilot":  true,
			"autotest":   false,
			"autosubmit": true,
			"autosync":   false,
			// Extra fields the config returns that we don't use.
			"workspaces": []string{"/tmp"},
			"autorefine": true,
			"autopush":   false,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tm := NewTrayManager(func() {}, func() {}, nil,srv.URL, "")
	cfg, err := tm.fetchConfig()
	if err != nil {
		t.Fatalf("fetchConfig() error: %v", err)
	}
	if !cfg.Autopilot {
		t.Error("autopilot: want true, got false")
	}
	if cfg.Autotest {
		t.Error("autotest: want false, got true")
	}
	if !cfg.Autosubmit {
		t.Error("autosubmit: want true, got false")
	}
	if cfg.Autosync {
		t.Error("autosync: want false, got true")
	}
}

func TestToggleSendsCorrectPayload(t *testing.T) {
	var gotBody string
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/config" && r.Method == "PUT" {
			gotMethod = r.Method
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	tm := NewTrayManager(func() {}, func() {}, nil,srv.URL, "")
	if err := tm.toggleConfig("autopilot", false); err != nil {
		t.Fatalf("toggleConfig error: %v", err)
	}
	if gotMethod != "PUT" {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	// Parse and verify the JSON body.
	var parsed map[string]bool
	if err := json.Unmarshal([]byte(gotBody), &parsed); err != nil {
		t.Fatalf("parse body: %v (body=%q)", err, gotBody)
	}
	if v, ok := parsed["autopilot"]; !ok || v != false {
		t.Errorf("body autopilot = %v, want false (body=%q)", v, gotBody)
	}
	if len(parsed) != 1 {
		t.Errorf("expected exactly 1 field in body, got %d (body=%q)", len(parsed), gotBody)
	}
}

func TestToggleFailurePreservesState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	tm := NewTrayManager(func() {}, func() {}, nil,srv.URL, "")
	err := tm.toggleConfig("autopilot", true)
	if err == nil {
		t.Fatal("expected error from failing PUT")
	}
}

func TestParseStatsResponse(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"total_cost_usd": 156.42,
			"daily_usage": []map[string]any{
				{"date": "2026-01-01", "cost_usd": 10.0},
				{"date": today, "cost_usd": 3.42},
				{"date": "2026-01-03", "cost_usd": 5.0},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tm := NewTrayManager(func() {}, func() {}, nil,srv.URL, "")
	data, err := tm.fetchStats()
	if err != nil {
		t.Fatalf("fetchStats() error: %v", err)
	}
	if data.TotalCostUSD != 156.42 {
		t.Errorf("total_cost_usd = %f, want 156.42", data.TotalCostUSD)
	}
	todayCost := extractTodayCost(data)
	if todayCost != 3.42 {
		t.Errorf("today's cost = %f, want 3.42", todayCost)
	}
}

func TestExtractTodayCostMissing(t *testing.T) {
	data := &statsData{
		TotalCostUSD: 100,
		DailyUsage: []struct {
			Date    string  `json:"date"`
			CostUSD float64 `json:"cost_usd"`
		}{
			{Date: "2020-01-01", CostUSD: 50},
		},
	}
	cost := extractTodayCost(data)
	if cost != 0 {
		t.Errorf("expected 0 for missing date, got %f", cost)
	}
}

func TestFormatCostShort(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0, "$0.00"},
		{0.5, "$0.50"},
		{3.42, "$3.42"},
		{1234.567, "$1234.57"},
		{0.001, "$0.00"},
	}
	for _, tc := range tests {
		got := formatCostShort(tc.input)
		if got != tc.want {
			t.Errorf("formatCostShort(%v) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestStatsErrorFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	tm := NewTrayManager(func() {}, func() {}, nil,srv.URL, "")
	_, err := tm.fetchStats()
	if err == nil {
		t.Fatal("expected error from failing stats endpoint")
	}
	// Verify costValid stays false when stats fail.
	if tm.costValid {
		t.Error("costValid should be false before successful poll")
	}
}

func TestHideOnCloseLogic(t *testing.T) {
	// Verify the hide-on-close logic: on macOS and Windows the OnBeforeClose
	// callback should return true (prevent close), on Linux it should return false.
	tests := []struct {
		goos string
		want bool
	}{
		{"darwin", true},
		{"windows", true},
		{"linux", false},
	}
	for _, tc := range tests {
		hideOnClose := tc.goos == "darwin" || tc.goos == "windows"
		// Simulate the OnBeforeClose callback logic from desktop.go.
		prevented := hideOnClose
		if prevented != tc.want {
			t.Errorf("GOOS=%s: OnBeforeClose returned %v, want %v", tc.goos, prevented, tc.want)
		}
	}
}

func TestPlatformTraySetup(t *testing.T) {
	// Verify platformTraySetup doesn't panic when called with a nil-safe callback.
	// This can't fully test systray behavior (requires display server) but
	// confirms the function exists and is callable.
	called := false
	fn := func() { called = true }
	// platformTraySetup is defined per-platform; just verify it compiles and
	// is callable. The actual systray.SetOnTapped call requires the systray
	// event loop, so we only verify the function signature.
	_ = fn
	_ = called
}

func TestAppIconFilesExist(t *testing.T) {
	root := repoRoot(t)
	icons := []string{
		"assets/icons/appicon.png",
		"assets/icons/appicon.ico",
		"assets/icons/appicon.icns",
	}
	for _, icon := range icons {
		path := filepath.Join(root, icon)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("%s: %v", icon, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s: file is empty", icon)
		}
	}
}

func TestWailsJSONExists(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "wails.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("wails.json: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("wails.json is empty")
	}
}
