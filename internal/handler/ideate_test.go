package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- IdeationInterval state ---

func TestIdeationInterval_DefaultIs60Minutes(t *testing.T) {
	h := newTestHandler(t)
	if h.IdeationInterval() != 60*time.Minute {
		t.Errorf("expected default interval=60m, got %v", h.IdeationInterval())
	}
}

func TestSetIdeationInterval_StoresValue(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeationInterval(30 * time.Minute)
	if h.IdeationInterval() != 30*time.Minute {
		t.Errorf("expected 30m, got %v", h.IdeationInterval())
	}
}

func TestSetIdeationInterval_CancelsPendingTimer(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)

	// Arm a 10-second timer by calling scheduleIdeation with a non-zero interval.
	h.SetIdeationInterval(10 * time.Second)
	h.scheduleIdeation(context.Background())

	// Timer should be pending.
	h.ideationMu.Lock()
	timerBefore := h.ideationTimer
	h.ideationMu.Unlock()
	if timerBefore == nil {
		t.Fatal("expected a pending timer after scheduleIdeation")
	}

	// Changing the interval should cancel the timer.
	h.SetIdeationInterval(20 * time.Second)

	h.ideationMu.Lock()
	timerAfter := h.ideationTimer
	h.ideationMu.Unlock()
	if timerAfter != nil {
		t.Error("expected pending timer to be cancelled after SetIdeationInterval")
	}
}

func TestSetIdeation_DisablingCancelsPendingTimer(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)
	h.SetIdeationInterval(10 * time.Second)
	h.scheduleIdeation(context.Background())

	// Disable ideation — timer should be cancelled.
	h.SetIdeation(false)

	h.ideationMu.Lock()
	timer := h.ideationTimer
	h.ideationMu.Unlock()
	if timer != nil {
		t.Error("expected pending timer to be cancelled when ideation is disabled")
	}
}

func TestIdeationNextRun_ZeroWhenNoTimerPending(t *testing.T) {
	h := newTestHandler(t)
	if !h.IdeationNextRun().IsZero() {
		t.Error("expected zero next-run time when no timer is pending")
	}
}

func TestScheduleIdeation_ImmediateWhenIntervalZero(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)
	h.SetIdeationInterval(0) // explicitly test the zero-interval path
	// interval = 0: scheduleIdeation should create the task directly, no timer.
	h.scheduleIdeation(context.Background())

	h.ideationMu.Lock()
	timer := h.ideationTimer
	h.ideationMu.Unlock()
	if timer != nil {
		t.Error("expected no timer when interval is zero (immediate scheduling)")
	}
}

func TestScheduleIdeation_SetsTimerWhenIntervalNonZero(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)
	h.SetIdeationInterval(5 * time.Minute)
	h.scheduleIdeation(context.Background())

	h.ideationMu.Lock()
	timer := h.ideationTimer
	nextRun := h.ideationNextRun
	h.ideationMu.Unlock()

	if timer == nil {
		t.Fatal("expected a pending timer when interval > 0")
	}
	if nextRun.IsZero() {
		t.Error("expected ideationNextRun to be set")
	}
	if nextRun.Before(time.Now()) {
		t.Error("expected ideationNextRun to be in the future")
	}

	// Stop timer so it doesn't fire during test cleanup.
	h.ideationMu.Lock()
	h.cancelIdeationTimerLocked()
	h.ideationMu.Unlock()
}

func TestScheduleIdeation_NoDuplicateTimer(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)
	h.SetIdeationInterval(5 * time.Minute)

	// Call scheduleIdeation twice — should not create a second timer.
	h.scheduleIdeation(context.Background())

	h.ideationMu.Lock()
	first := h.ideationTimer
	h.ideationMu.Unlock()

	h.scheduleIdeation(context.Background())

	h.ideationMu.Lock()
	second := h.ideationTimer
	h.ideationMu.Unlock()

	if first != second {
		t.Error("expected the same timer pointer (no double-scheduling)")
	}

	h.ideationMu.Lock()
	h.cancelIdeationTimerLocked()
	h.ideationMu.Unlock()
}

// --- UpdateConfig ideation_interval ---

func TestUpdateConfig_SetsIdeationInterval(t *testing.T) {
	h := newTestHandler(t)

	body := `{"ideation_interval": 60}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	got, ok := resp["ideation_interval"].(float64)
	if !ok {
		t.Fatalf("expected ideation_interval in response, got %v", resp["ideation_interval"])
	}
	if int(got) != 60 {
		t.Errorf("expected ideation_interval=60 in response, got %v", got)
	}

	if h.IdeationInterval() != 60*time.Minute {
		t.Errorf("expected handler interval=60m, got %v", h.IdeationInterval())
	}
}

func TestUpdateConfig_IdeationIntervalClampedToZero(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(false)

	body := `{"ideation_interval": -5}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if h.IdeationInterval() != 0 {
		t.Errorf("expected negative interval to be clamped to 0, got %v", h.IdeationInterval())
	}
}

func TestUpdateConfig_ReturnsIdeationIntervalByDefault(t *testing.T) {
	h := newTestHandler(t)

	// Empty body — should still return ideation_interval (0).
	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if _, ok := resp["ideation_interval"]; !ok {
		t.Error("expected ideation_interval in UpdateConfig response")
	}
}

// --- GetConfig ideation_interval ---

func TestGetConfig_ReturnsIdeationInterval(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	h.SetIdeationInterval(120 * time.Minute)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	got, ok := resp["ideation_interval"].(float64)
	if !ok {
		t.Fatalf("expected ideation_interval in GetConfig response, got %v", resp["ideation_interval"])
	}
	if int(got) != 120 {
		t.Errorf("expected ideation_interval=120, got %v", got)
	}
}

func TestGetConfig_IdeationNextRunAbsentWhenNotPending(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	if _, ok := resp["ideation_next_run"]; ok {
		t.Error("expected ideation_next_run to be absent when no timer is pending")
	}
}
