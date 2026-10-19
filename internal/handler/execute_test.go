package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/store"
)

// createWaitingTask creates a task in the store and moves it to waiting status.
func createWaitingTask(t *testing.T, h *Handler, prompt string) uuid.UUID {
	t.Helper()
	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: prompt, Timeout: 15})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(context.Background(), task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("set waiting: %v", err)
	}
	return task.ID
}
