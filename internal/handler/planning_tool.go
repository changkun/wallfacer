package handler

import (
	"net/http"
	"strings"

	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// UpdateTaskPromptTool handles POST /api/planning/tool/update_task_prompt.
// It is the HTTP bridge that lets the planning agent (in task-mode) write
// task.Prompt and append a prompt_round event.
//
// Request body: {task_id: string, prompt: string, thread_id: string}
// Response:     {prev_prompt: string, round: int}
func (h *Handler) UpdateTaskPromptTool(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[struct {
		TaskID   string `json:"task_id"`
		Prompt   string `json:"prompt"`
		ThreadID string `json:"thread_id"`
	}](w, r)
	if !ok {
		return
	}

	taskUUID, err := uuid.Parse(strings.TrimSpace(req.TaskID))
	if err != nil {
		http.Error(w, "task_id: invalid UUID", http.StatusBadRequest)
		return
	}

	threadID := strings.TrimSpace(req.ThreadID)
	cs := h.lookupThreadStore(threadID)
	if cs == nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}
	sess, _ := cs.LoadSession()
	if sess.FocusedTask == "" {
		http.Error(w, "thread is not in task-mode", http.StatusUnprocessableEntity)
		return
	}
	if sess.FocusedTask != taskUUID.String() {
		http.Error(w, "task_id does not match thread's pinned task", http.StatusUnprocessableEntity)
		return
	}

	s, ok := h.currentStore()
	if !ok {
		http.Error(w, "store not configured", http.StatusServiceUnavailable)
		return
	}

	// Count existing prompt_round events to determine the next round number.
	events, err := s.GetEvents(r.Context(), taskUUID)
	if err != nil {
		http.Error(w, "failed to read task events", http.StatusInternalServerError)
		return
	}
	round := 1
	for _, ev := range events {
		if ev.EventType == store.EventTypePromptRound {
			round++
		}
	}

	newPrompt := strings.TrimSpace(req.Prompt)
	prevPrompt, resumeHint, err := s.UpdateTaskPromptDirect(r.Context(), taskUUID, newPrompt)
	if err != nil {
		http.Error(w, "failed to update task prompt: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	payload := store.NewPromptRoundEvent(threadID, round, prevPrompt, newPrompt, resumeHint)
	_ = s.InsertEvent(r.Context(), taskUUID, store.EventTypePromptRound, payload)

	httpjson.Write(w, http.StatusOK, map[string]any{
		"prev_prompt": prevPrompt,
		"round":       round,
	})
}
