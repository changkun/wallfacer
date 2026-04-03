package handler

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/planner"
)

// GetPlanningStatus reports whether the planning sandbox is running.
func (h *Handler) GetPlanningStatus(w http.ResponseWriter, _ *http.Request) {
	running := false
	if h.planner != nil {
		running = h.planner.IsRunning()
	}
	httpjson.Write(w, http.StatusOK, map[string]any{
		"running": running,
	})
}

// StartPlanning starts the planning sandbox container.
// If already running, returns 200 with running=true (idempotent).
func (h *Handler) StartPlanning(w http.ResponseWriter, r *http.Request) {
	if h.planner == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	if h.planner.IsRunning() {
		httpjson.Write(w, http.StatusOK, map[string]any{"running": true})
		return
	}
	if err := h.planner.Start(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusAccepted, map[string]any{"running": true})
}

// StopPlanning stops the planning sandbox container.
func (h *Handler) StopPlanning(w http.ResponseWriter, _ *http.Request) {
	if h.planner != nil {
		h.planner.Stop()
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"stopped": true})
}

// GetPlanningMessages returns the planning conversation history as a JSON array.
// Supports optional ?before=<RFC3339> for pagination.
func (h *Handler) GetPlanningMessages(w http.ResponseWriter, r *http.Request) {
	if h.planner == nil || h.planner.Conversation() == nil {
		httpjson.Write(w, http.StatusOK, []any{})
		return
	}

	msgs, err := h.planner.Conversation().Messages()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if msgs == nil {
		msgs = []planner.Message{}
	}

	// Optional pagination: filter messages before a given timestamp.
	if before := r.URL.Query().Get("before"); before != "" {
		t, parseErr := time.Parse(time.RFC3339Nano, before)
		if parseErr != nil {
			http.Error(w, "invalid before timestamp", http.StatusBadRequest)
			return
		}
		filtered := make([]planner.Message, 0, len(msgs))
		for _, m := range msgs {
			if m.Timestamp.Before(t) {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}

	httpjson.Write(w, http.StatusOK, msgs)
}

// SendPlanningMessage sends a user message to the planning agent.
// The agent exec runs in a background goroutine; returns 202 immediately.
// Returns 409 if an exec is already in flight.
func (h *Handler) SendPlanningMessage(w http.ResponseWriter, r *http.Request) {
	if h.planner == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	if !h.planner.IsRunning() {
		http.Error(w, "planning sandbox not running", http.StatusConflict)
		return
	}
	if h.planner.IsBusy() {
		httpjson.Write(w, http.StatusConflict, map[string]any{
			"error": "agent is busy",
		})
		return
	}

	req, ok := httpjson.DecodeBody[struct {
		Message     string `json:"message"`
		FocusedSpec string `json:"focused_spec"`
	}](w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	cs := h.planner.Conversation()
	if cs == nil {
		http.Error(w, "conversation store not configured", http.StatusServiceUnavailable)
		return
	}

	// Append user message to conversation store.
	userMsg := planner.Message{
		Role:        "user",
		Content:     req.Message,
		Timestamp:   time.Now().UTC(),
		FocusedSpec: req.FocusedSpec,
	}
	if err := cs.AppendMessage(userMsg); err != nil {
		http.Error(w, "failed to persist message", http.StatusInternalServerError)
		return
	}

	// Build exec args.
	prompt := req.Message
	if req.FocusedSpec != "" {
		prompt = "[Focused spec: " + req.FocusedSpec + "]\n\n" + req.Message
	}
	cmd := []string{"-p", prompt, "--verbose", "--output-format", "stream-json"}

	// Resume existing session if available.
	sess, _ := cs.LoadSession()
	if sess.SessionID != "" {
		cmd = append(cmd, "--resume", sess.SessionID)
	}

	h.planner.SetBusy(true)

	// Run exec in background goroutine.
	go func() {
		defer h.planner.SetBusy(false)

		handle, err := h.planner.Exec(r.Context(), cmd)
		if err != nil {
			slog.Warn("planning exec failed", "error", err)
			return
		}

		rawStdout, _ := io.ReadAll(handle.Stdout())
		_, _ = io.ReadAll(handle.Stderr())
		_, _ = handle.Wait()

		// Extract session ID and save for future --resume calls.
		sessionID := planner.ExtractSessionID(rawStdout)
		if sessionID != "" {
			_ = cs.SaveSession(planner.SessionInfo{
				SessionID:   sessionID,
				LastActive:  time.Now().UTC(),
				FocusedSpec: req.FocusedSpec,
			})
		}

		// Parse response text and append assistant message.
		resultText := planner.ExtractResultText(rawStdout)
		if resultText != "" {
			_ = cs.AppendMessage(planner.Message{
				Role:        "assistant",
				Content:     resultText,
				Timestamp:   time.Now().UTC(),
				FocusedSpec: req.FocusedSpec,
			})
		}
	}()

	httpjson.Write(w, http.StatusAccepted, map[string]any{"status": "accepted"})
}

// ClearPlanningMessages clears the planning conversation history and session.
func (h *Handler) ClearPlanningMessages(w http.ResponseWriter, _ *http.Request) {
	if h.planner != nil && h.planner.Conversation() != nil {
		if err := h.planner.Conversation().Clear(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"status": "cleared"})
}
