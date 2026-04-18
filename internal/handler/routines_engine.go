package handler

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/watcher"
	"changkun.de/x/wallfacer/internal/routine"
	"changkun.de/x/wallfacer/internal/store"
)

// routineReconcileSettle coalesces bursts of store-change notifications.
// A short settle delay lets a write-and-immediate-read pattern (e.g.
// Register + UpdateRoutineNextRun) collapse into a single reconcile.
const routineReconcileSettle = 250 * time.Millisecond

// StartRoutineEngine initializes the scheduler engine and attaches it to
// the store's change stream. The engine is idempotent under repeated
// Register calls, so running on every store change is cheap.
func (h *Handler) StartRoutineEngine(ctx context.Context) {
	h.routineMu.Lock()
	if h.routineEngine == nil {
		h.routineEngine = routine.NewEngine(ctx, nil, h.fireRoutine)
	}
	h.routineMu.Unlock()

	watcher.Start(ctx, watcher.Config{
		Wake:        h.store,
		Init:        h.reconcileRoutines,
		Action:      h.reconcileRoutines,
		SettleDelay: routineReconcileSettle,
	})
}

// scheduleForTask turns a routine card's persisted schedule fields into
// a concrete routine.Schedule. Disabled or non-positive intervals become
// routine.Disabled() so the engine still tracks the entry (its next-run
// is reported as zero in the UI) but never fires it.
func scheduleForTask(t store.Task) routine.Schedule {
	if !t.RoutineEnabled || t.RoutineIntervalSeconds <= 0 {
		return routine.Disabled()
	}
	return routine.FixedInterval{D: time.Duration(t.RoutineIntervalSeconds) * time.Second}
}

// reconcileRoutines aligns the engine's registry with the current set of
// routine cards. New routines are registered, deleted routines are
// unregistered, and schedule changes propagate via the engine's
// idempotent Register. After reconciliation we write each routine's
// next-run time back to the store so the UI can render a countdown.
// The write is a no-op when the timestamp hasn't drifted meaningfully,
// so the watcher's feedback loop doesn't pulse.
func (h *Handler) reconcileRoutines(ctx context.Context) {
	h.routineMu.Lock()
	eng := h.routineEngine
	h.routineMu.Unlock()
	if eng == nil {
		return
	}

	s, ok := h.currentStore()
	if !ok {
		return
	}

	tasks, err := s.ListTasks(ctx, false)
	if err != nil {
		logger.Handler.Warn("routine: reconcile list tasks", "error", err)
		return
	}

	seen := make(map[uuid.UUID]struct{})
	for _, t := range tasks {
		if !t.IsRoutine() {
			continue
		}
		seen[t.ID] = struct{}{}
		eng.Register(t.ID, scheduleForTask(t))
	}

	nextRuns := eng.NextRuns()
	// Drop engine entries whose underlying routine card no longer exists.
	for id := range nextRuns {
		if _, live := seen[id]; !live {
			eng.Unregister(id)
		}
	}

	// Persist the engine's next-run times back to the store so UI
	// subscribers see a fresh countdown. Skip writes when the drift is
	// below ~500ms to avoid hammering the watcher after every reconcile.
	for _, t := range tasks {
		if !t.IsRoutine() {
			continue
		}
		next := nextRuns[t.ID]
		var desired *time.Time
		if !next.IsZero() {
			tsCopy := next
			desired = &tsCopy
		}
		if routineNextRunEqual(t.RoutineNextRun, desired) {
			continue
		}
		if err := s.UpdateRoutineNextRun(ctx, t.ID, desired); err != nil {
			logger.Handler.Warn("routine: persist next-run", "routine", t.ID, "error", err)
		}
	}
}

// routineNextRunEqual compares two nullable time pointers with a small
// tolerance (500ms) so that clock jitter between engine and store does
// not keep nudging UpdateRoutineNextRun and triggering the watcher.
func routineNextRunEqual(a, b *time.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	d := a.Sub(*b)
	if d < 0 {
		d = -d
	}
	return d < 500*time.Millisecond
}

// fireRoutine is the engine's FireFunc. It runs in a dedicated goroutine
// with no locks held. The flow mirrors createIdeaAgentTask: build the
// instance task via CreateTaskWithOptions, promote to in_progress, emit
// the state-change event, then hand off to the runner.
func (h *Handler) fireRoutine(ctx context.Context, routineID uuid.UUID) {
	s, ok := h.currentStore()
	if !ok {
		logger.Handler.Warn("routine: fire without active store", "routine", routineID)
		return
	}
	routineTask, err := s.GetTask(ctx, routineID)
	if err != nil {
		logger.Handler.Warn("routine: fire task not found", "routine", routineID, "error", err)
		return
	}
	if !routineTask.IsRoutine() {
		return
	}

	now := time.Now()
	// Record last-fired before attempting to spawn so that UI countdowns
	// reflect the attempt even if spawn fails.
	if err := s.UpdateRoutineLastFiredAt(ctx, routineID, &now); err != nil {
		logger.Handler.Warn("routine: record last-fired", "routine", routineID, "error", err)
	}

	prompt := h.buildRoutineInstancePrompt(*routineTask)
	tags := append(slices.Clone(routineTask.Tags), "spawned-by:"+routineID.String())

	instance, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:  prompt,
		Goal:    routineTask.Goal,
		Kind:    routineTask.RoutineSpawnKind,
		Tags:    tags,
		Timeout: routineTask.Timeout,
	})
	if err != nil {
		logger.Handler.Warn("routine: create instance", "routine", routineID, "error", err)
		return
	}

	// Routine instances skip the backlog queue — the user's intent was
	// "run now". Matches the ideation agent's existing behaviour.
	if err := s.UpdateTaskStatus(ctx, instance.ID, store.TaskStatusInProgress); err != nil {
		logger.Handler.Warn("routine: promote instance", "routine", routineID, "instance", instance.ID, "error", err)
		return
	}

	h.insertEventOrLog(ctx, instance.ID, store.EventTypeStateChange,
		store.NewStateChangeData("", store.TaskStatusBacklog, store.TriggerUser, nil))
	h.insertEventOrLog(ctx, routineID, store.EventTypeSystem, map[string]any{
		"kind":     "routine:fired",
		"instance": instance.ID.String(),
	})

	h.runner.RunBackground(instance.ID, prompt, "", false)
	logger.Handler.Info("routine: fired", "routine", routineID, "instance", instance.ID)
}

// buildRoutineInstancePrompt decides what prompt the spawned instance
// runs with. For idea-agent routines we delegate to the existing
// BuildIdeationPrompt so the migrated system:ideation routine behaves
// byte-identically to the old singleton. For plain task routines we use
// the routine card's own prompt verbatim.
func (h *Handler) buildRoutineInstancePrompt(routineTask store.Task) string {
	if routineTask.RoutineSpawnKind == store.TaskKindIdeaAgent {
		tasks, _ := h.store.ListTasks(context.Background(), false)
		active := make([]store.Task, 0, len(tasks))
		for _, t := range tasks {
			if t.IsIdeaAgent() || t.IsRoutine() {
				continue
			}
			switch t.Status {
			case store.TaskStatusBacklog, store.TaskStatusInProgress, store.TaskStatusWaiting:
				active = append(active, t)
			}
		}
		return h.runner.BuildIdeationPrompt(active)
	}
	return routineTask.Prompt
}
