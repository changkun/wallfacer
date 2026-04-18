package handler

import (
	"context"
	"net/http"
	"slices"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/store"
)

// systemIdeationTag identifies the routine card that stands in for the
// legacy ideation singleton. Exactly one such card should exist per
// workspace store; ensureSystemIdeationRoutine is idempotent.
const systemIdeationTag = "system:ideation"

// ideationSeed is the per-startup configuration snapshot used to
// materialize the system:ideation routine on first boot in a store
// that does not already have one. Subsequent config writes (via
// SetIdeation / SetIdeationInterval) route through the routine's
// own fields, so legacy envconfig becomes read-only for ideation.
type ideationSeed struct {
	enabled  bool
	interval time.Duration
}

// ensureSystemIdeationRoutine is invoked from the engine's
// reconcileRoutines loop. It creates the system:ideation routine
// card in the supplied store when none exists yet, seeding it from
// legacyIdeationSeed. On subsequent reconciles (or after restarts)
// the existing routine is found and left alone, so the bootstrap is
// safe to call repeatedly.
func (h *Handler) ensureSystemIdeationRoutine(ctx context.Context, s *store.Store) {
	if s == nil {
		return
	}
	tasks, err := s.ListTasks(ctx, true) // include archived to avoid duplicates
	if err != nil {
		return
	}
	for _, t := range tasks {
		if t.IsRoutine() && slices.Contains(t.Tags, systemIdeationTag) {
			return // already exists
		}
	}

	seed := h.legacyIdeationSeed
	intervalSeconds := int(seed.interval / time.Second)
	if intervalSeconds <= 0 {
		intervalSeconds = int(constants.DefaultIdeationInterval / time.Second)
	}

	_, err = s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:                 "Ideation routine",
		Goal:                   "Brainstorm new task ideas at a scheduled cadence.",
		Kind:                   store.TaskKindRoutine,
		Tags:                   []string{systemIdeationTag},
		Timeout:                constants.IdeaAgentDefaultTimeout,
		RoutineIntervalSeconds: intervalSeconds,
		RoutineEnabled:         seed.enabled,
		RoutineSpawnKind:       store.TaskKindIdeaAgent,
	})
	if err != nil {
		return
	}
}

// findSystemIdeationRoutine returns the system:ideation routine card from
// the active workspace store, or nil if none exists yet (e.g. before the
// first reconcile tick). Callers treat nil as "use defaults".
func (h *Handler) findSystemIdeationRoutine(ctx context.Context) *store.Task {
	s, ok := h.currentStore()
	if !ok {
		return nil
	}
	tasks, err := s.ListTasks(ctx, true)
	if err != nil {
		return nil
	}
	for _, t := range tasks {
		if t.IsRoutine() && slices.Contains(t.Tags, systemIdeationTag) {
			tCopy := t
			return &tCopy
		}
	}
	return nil
}

// TriggerIdeation handles POST /api/ideate.
// Fires the system:ideation routine immediately. The routine card is
// created on demand here if the engine has not yet reconciled it, so
// the legacy shim never returns 503 under normal boot timing.
func (h *Handler) TriggerIdeation(w http.ResponseWriter, r *http.Request) {
	routineTask := h.findSystemIdeationRoutine(r.Context())
	if routineTask == nil {
		if s, ok := h.currentStore(); ok {
			h.ensureSystemIdeationRoutine(r.Context(), s)
			routineTask = h.findSystemIdeationRoutine(r.Context())
		}
	}
	if routineTask == nil {
		http.Error(w, "ideation routine not initialized yet", http.StatusServiceUnavailable)
		return
	}

	h.routineMu.Lock()
	eng := h.routineEngine
	h.routineMu.Unlock()

	// If the engine exists and has already registered this routine, let
	// it drive the fire (so the scheduled cycle re-arms). Otherwise fall
	// back to firing inline — this covers the window between bootstrap
	// and the first reconcile tick.
	if eng != nil {
		if _, registered := eng.NextRuns()[routineTask.ID]; registered {
			eng.Trigger(routineTask.ID)
		} else {
			go h.fireRoutine(r.Context(), routineTask.ID)
		}
	} else {
		go h.fireRoutine(r.Context(), routineTask.ID)
	}

	httpjson.Write(w, http.StatusAccepted, map[string]any{
		"queued":     true,
		"routine_id": routineTask.ID.String(),
	})
}

// CancelIdeation handles DELETE /api/ideate.
// Cancels any currently running or backlogged idea-agent instance task.
// The routine card itself is unaffected — the next scheduled fire will
// spawn a new instance.
func (h *Handler) CancelIdeation(w http.ResponseWriter, r *http.Request) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	tasks, err := s.ListTasks(r.Context(), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cancelled := false
	for _, t := range tasks {
		if !t.IsIdeaAgent() {
			continue
		}
		switch t.Status {
		case store.TaskStatusInProgress:
			h.runner.KillContainer(t.ID)
			_ = s.UpdateTaskStatus(r.Context(), t.ID, store.TaskStatusCancelled)
			h.insertEventOrLog(r.Context(), t.ID, store.EventTypeStateChange,
				store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusCancelled, "", nil))
			cancelled = true
		case store.TaskStatusBacklog:
			_ = s.UpdateTaskStatus(r.Context(), t.ID, store.TaskStatusCancelled)
			h.insertEventOrLog(r.Context(), t.ID, store.EventTypeStateChange,
				store.NewStateChangeData(store.TaskStatusBacklog, store.TaskStatusCancelled, "", nil))
			cancelled = true
		}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}

// GetIdeationStatus handles GET /api/ideate.
// Returns the system routine's enabled flag, whether an instance task is
// currently running, and the next-run timestamp. The shape mirrors the
// legacy endpoint so existing clients keep working.
func (h *Handler) GetIdeationStatus(w http.ResponseWriter, r *http.Request) {
	routineTask := h.findSystemIdeationRoutine(r.Context())
	running := false
	if s, ok := h.currentStore(); ok {
		tasks, _ := s.ListTasks(r.Context(), false)
		for _, t := range tasks {
			if t.IsIdeaAgent() && t.Status == store.TaskStatusInProgress {
				running = true
				break
			}
		}
	}

	resp := map[string]any{
		"enabled": routineTask != nil && routineTask.RoutineEnabled,
		"running": running,
	}
	if routineTask != nil && routineTask.RoutineNextRun != nil {
		resp["next_run_at"] = *routineTask.RoutineNextRun
	}
	httpjson.Write(w, http.StatusOK, resp)
}
