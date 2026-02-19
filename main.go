package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/google/uuid"
)

//go:embed ui
var uiFiles embed.FS

func main() {
	addr := envOrDefault("ADDR", ":8080")
	dataDir := envOrDefault("DATA_DIR", "data")

	store, err := NewStore(dataDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer store.Close()
	log.Printf("store loaded from %s/", dataDir)

	// Recover orphaned in_progress tasks from a previous server crash.
	recoverOrphanedTasks(store)

	runner := NewRunner(store, RunnerConfig{
		Command:      envOrDefault("CONTAINER_CMD", "/opt/podman/bin/podman"),
		SandboxImage: envOrDefault("SANDBOX_IMAGE", "wallfacer:latest"),
		EnvFile:      envOrDefault("ENV_FILE", ".env"),
		Workspaces:   envOrDefault("WORKSPACES", ""),
	})

	handler := NewHandler(store, runner)

	mux := http.NewServeMux()

	// Static files (Kanban UI)
	uiFS, _ := fs.Sub(uiFiles, "ui")
	mux.Handle("GET /", http.FileServer(http.FS(uiFS)))

	// API routes
	mux.HandleFunc("GET /api/tasks", handler.ListTasks)
	mux.HandleFunc("POST /api/tasks", handler.CreateTask)

	mux.HandleFunc("PATCH /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.UpdateTask(w, r, id)
	})

	mux.HandleFunc("DELETE /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.DeleteTask(w, r, id)
	})

	mux.HandleFunc("POST /api/tasks/{id}/feedback", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.SubmitFeedback(w, r, id)
	})

	mux.HandleFunc("GET /api/tasks/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.GetEvents(w, r, id)
	})

	mux.HandleFunc("GET /api/tasks/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.StreamLogs(w, r, id)
	})

	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func recoverOrphanedTasks(store *Store) {
	ctx := context.Background()
	tasks, err := store.ListTasks(ctx)
	if err != nil {
		log.Printf("recovery: list tasks: %v", err)
		return
	}
	for _, t := range tasks {
		if t.Status != "in_progress" {
			continue
		}
		log.Printf("recovery: task %s was in_progress at startup, marking as failed", t.ID)
		store.UpdateTaskStatus(ctx, t.ID, "failed")
		store.InsertEvent(ctx, t.ID, "error", map[string]string{
			"error": "server restarted while task was in progress",
		})
		store.InsertEvent(ctx, t.ID, "state_change", map[string]string{
			"from": "in_progress", "to": "failed",
		})
	}
}
