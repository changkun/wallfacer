package handler

import (
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/store"
)

// allowedRoutineSpawnKinds bounds what a user (or the system-ideation
// bootstrap) may set as a routine's spawn_kind at the API boundary. It
// prevents routines from spawning, say, planning-session tasks that would
// confuse the lifecycle machinery.
var allowedRoutineSpawnKinds = []store.TaskKind{
	store.TaskKindTask,
	store.TaskKindIdeaAgent,
}

// minRoutineIntervalMinutes guards against "fire every second" misuse.
// Engine integration still respects RoutineEnabled=false for a hard pause,
// but the minimum keeps the instance-task churn reasonable.
const minRoutineIntervalMinutes = 1

// RoutineResponse is the JSON shape returned by the routines API. It is
// a deliberate subset of store.Task: only the fields the UI needs to
// render a routine card.
type RoutineResponse struct {
	ID                     uuid.UUID      `json:"id"`
	Prompt                 string         `json:"prompt"`
	Goal                   string         `json:"goal,omitempty"`
	Tags                   []string       `json:"tags,omitempty"`
	Kind                   store.TaskKind `json:"kind"`
	RoutineIntervalSeconds int            `json:"routine_interval_seconds"`
	RoutineEnabled         bool           `json:"routine_enabled"`
	RoutineNextRun         *time.Time     `json:"routine_next_run,omitempty"`
	RoutineLastFiredAt     *time.Time     `json:"routine_last_fired_at,omitempty"`
	RoutineSpawnKind       store.TaskKind `json:"routine_spawn_kind,omitempty"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
}

func toRoutineResponse(t store.Task) RoutineResponse {
	return RoutineResponse{
		ID:                     t.ID,
		Prompt:                 t.Prompt,
		Goal:                   t.Goal,
		Tags:                   slices.Clone(t.Tags),
		Kind:                   t.Kind,
		RoutineIntervalSeconds: t.RoutineIntervalSeconds,
		RoutineEnabled:         t.RoutineEnabled,
		RoutineNextRun:         t.RoutineNextRun,
		RoutineLastFiredAt:     t.RoutineLastFiredAt,
		RoutineSpawnKind:       t.RoutineSpawnKind,
		CreatedAt:              t.CreatedAt,
		UpdatedAt:              t.UpdatedAt,
	}
}

// ListRoutines handles GET /api/routines. Returns every routine card in
// the active workspace store, sorted by creation time so the list order
// is stable across reloads.
func (h *Handler) ListRoutines(w http.ResponseWriter, r *http.Request) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	tasks, err := s.ListTasks(r.Context(), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]RoutineResponse, 0, 4)
	for _, t := range tasks {
		if t.IsRoutine() {
			out = append(out, toRoutineResponse(t))
		}
	}
	slices.SortFunc(out, func(a, b RoutineResponse) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	httpjson.Write(w, http.StatusOK, map[string]any{"routines": out})
}

// CreateRoutine handles POST /api/routines. It wraps the generic store
// creation with routine-specific validation (whitelisted spawn kind,
// minimum interval), then persists a card with Kind=TaskKindRoutine.
func (h *Handler) CreateRoutine(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[struct {
		Prompt          string   `json:"prompt"`
		Goal            string   `json:"goal"`
		IntervalMinutes int      `json:"interval_minutes"`
		SpawnKind       string   `json:"spawn_kind"`
		Enabled         *bool    `json:"enabled"`
		Timeout         int      `json:"timeout"`
		Tags            []string `json:"tags"`
	}](w, r)
	if !ok {
		return
	}

	if req.Prompt == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}
	if req.IntervalMinutes < minRoutineIntervalMinutes {
		http.Error(w, fmt.Sprintf("interval_minutes must be >= %d", minRoutineIntervalMinutes), http.StatusUnprocessableEntity)
		return
	}

	spawnKind := store.TaskKind(req.SpawnKind)
	if !slices.Contains(allowedRoutineSpawnKinds, spawnKind) {
		http.Error(w, fmt.Sprintf("spawn_kind %q is not allowed", req.SpawnKind), http.StatusUnprocessableEntity)
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	s, ok := h.requireStore(w)
	if !ok {
		return
	}

	task, err := s.CreateTaskWithOptions(r.Context(), store.TaskCreateOptions{
		Prompt:                 req.Prompt,
		Goal:                   req.Goal,
		Timeout:                req.Timeout,
		Kind:                   store.TaskKindRoutine,
		Tags:                   req.Tags,
		RoutineIntervalSeconds: req.IntervalMinutes * 60,
		RoutineEnabled:         enabled,
		RoutineSpawnKind:       spawnKind,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.insertEventOrLog(r.Context(), task.ID, store.EventTypeSystem, map[string]any{
		"kind":             "routine:created",
		"interval_seconds": task.RoutineIntervalSeconds,
		"enabled":          task.RoutineEnabled,
		"spawn_kind":       string(task.RoutineSpawnKind),
	})
	httpjson.Write(w, http.StatusCreated, toRoutineResponse(*task))
}

// UpdateRoutineSchedule handles PATCH /api/routines/{id}/schedule. Fields
// omitted from the body are left unchanged so the UI can apply partial
// updates (e.g. toggle enabled without re-sending the interval).
func (h *Handler) UpdateRoutineSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathID(w, r, "id")
	if !ok {
		return
	}

	req, ok := httpjson.DecodeBody[struct {
		IntervalMinutes *int  `json:"interval_minutes"`
		Enabled         *bool `json:"enabled"`
	}](w, r)
	if !ok {
		return
	}

	s, ok := h.requireStore(w)
	if !ok {
		return
	}

	task, err := s.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "routine not found", http.StatusNotFound)
		return
	}
	if !task.IsRoutine() {
		http.Error(w, "task is not a routine", http.StatusUnprocessableEntity)
		return
	}

	if req.IntervalMinutes != nil {
		mins := *req.IntervalMinutes
		if mins < minRoutineIntervalMinutes {
			http.Error(w, fmt.Sprintf("interval_minutes must be >= %d", minRoutineIntervalMinutes), http.StatusUnprocessableEntity)
			return
		}
		if err := s.UpdateRoutineSchedule(r.Context(), id, mins*60); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if req.Enabled != nil {
		if err := s.UpdateRoutineEnabled(r.Context(), id, *req.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	updated, err := s.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.insertEventOrLog(r.Context(), id, store.EventTypeSystem, map[string]any{
		"kind":             "routine:schedule_updated",
		"interval_seconds": updated.RoutineIntervalSeconds,
		"enabled":          updated.RoutineEnabled,
	})
	httpjson.Write(w, http.StatusOK, toRoutineResponse(*updated))
}

// TriggerRoutine handles POST /api/routines/{id}/trigger. For this task it
// only records the triggered event; the scheduler engine (next task) hooks
// in here to actually spawn an instance. Returning 202 keeps the contract
// stable across that transition.
func (h *Handler) TriggerRoutine(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathID(w, r, "id")
	if !ok {
		return
	}

	s, ok := h.requireStore(w)
	if !ok {
		return
	}

	task, err := s.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "routine not found", http.StatusNotFound)
		return
	}
	if !task.IsRoutine() {
		http.Error(w, "task is not a routine", http.StatusUnprocessableEntity)
		return
	}

	h.insertEventOrLog(r.Context(), id, store.EventTypeSystem, map[string]any{
		"kind": "routine:triggered",
	})
	h.routineMu.Lock()
	eng := h.routineEngine
	h.routineMu.Unlock()
	if eng == nil {
		// Engine not yet initialized (pre-boot, or test that opted out).
		// Spawn inline so the caller still sees an instance task created.
		go h.fireRoutine(r.Context(), id)
	} else {
		eng.Trigger(id)
	}
	logger.Handler.Info("routine: trigger requested", "routine", id)
	httpjson.Write(w, http.StatusAccepted, map[string]any{"queued": true})
}

// parsePathID reads a UUID from the named path variable and writes a
// 400 response on parse failure. Used by the routine handlers for the
// {id} segment in PATCH and POST routes.
func parsePathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	raw := r.PathValue(name)
	if raw == "" {
		http.Error(w, "missing "+name, http.StatusBadRequest)
		return uuid.UUID{}, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid %s: %v", name, err), http.StatusBadRequest)
		return uuid.UUID{}, false
	}
	return id, true
}
