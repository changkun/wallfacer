package handler

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// undoResult is the JSON payload returned by UndoPlanningRound.
type undoResult struct {
	Round         int      `json:"round"`
	Summary       string   `json:"summary"`
	FilesReverted []string `json:"files_reverted"`
	Workspace     string   `json:"workspace"`
}

// planRoundSubject parses "plan: round N — <summary>" as written by
// commitPlanningRound. The em-dash (U+2014) must match exactly; surrounding
// whitespace is flexible because git's default --cleanup=strip removes
// trailing spaces from commit subjects (so "plan: round 42 — " becomes
// "plan: round 42 —" on disk). The summary group may be empty or missing.
var planRoundSubject = regexp.MustCompile(`^plan: round (\d+)(?:\s+—\s*(.*))?$`)

// addedDispatchLine matches unified-diff added lines that set a UUID
// dispatched_task_id. Excludes diff headers like "+++ b/specs/..." since
// those don't begin with "+dispatched_task_id:".
var addedDispatchLine = regexp.MustCompile(
	`(?m)^\+dispatched_task_id:\s*([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`)

// UndoPlanningRound resets the first workspace with a planning commit back to
// the state before the last `plan: round N` commit. Stashes dirty user edits
// across the reset, pops the stash after (returning 409 on conflict), and
// cancels any kanban tasks that were dispatched by the reverted commit.
//
// Responds 409 if no planning commits exist in any workspace, or if the
// latest planning commit is not at HEAD (manual commits made since would be
// destroyed by a HEAD~1 reset — the user must resolve manually).
func (h *Handler) UndoPlanningRound(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	for _, ws := range h.currentWorkspaces() {
		out, err := cmdexec.Git(ws, "log", "--format=%H %s", "--grep=^plan: round", "-1").WithContext(ctx).Output()
		if err != nil || out == "" {
			continue
		}
		hash, subject, ok := strings.Cut(out, " ")
		if !ok {
			continue
		}

		head, err := cmdexec.Git(ws, "rev-parse", "HEAD").WithContext(ctx).Output()
		if err != nil {
			http.Error(w, "rev-parse HEAD: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if head != hash {
			httpjson.Write(w, http.StatusConflict, map[string]any{
				"error":     "latest planning commit is not at HEAD; new commits have been added since — resolve manually",
				"workspace": ws,
			})
			return
		}

		diff, _ := cmdexec.Git(ws, "diff", "HEAD~1", "HEAD", "--", "specs/").WithContext(ctx).Output()
		files, _ := cmdexec.Git(ws, "diff", "--name-only", "HEAD~1", "HEAD").WithContext(ctx).Output()

		stashed := gitutil.StashIfDirty(ws)

		if err := cmdexec.Git(ws, "reset", "--hard", "HEAD~1").WithContext(ctx).Run(); err != nil {
			if stashed {
				_ = gitutil.StashPop(ws)
			}
			http.Error(w, "git reset --hard HEAD~1: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if stashed {
			if err := gitutil.StashPop(ws); err != nil {
				httpjson.Write(w, http.StatusConflict, map[string]any{
					"error":     "stash pop conflict after undo; stash retained for manual resolution",
					"detail":    err.Error(),
					"workspace": ws,
				})
				return
			}
		}

		for _, id := range extractDispatchedTaskIDs(diff) {
			h.cancelDispatchedTask(ctx, id)
		}

		round, summary := parsePlanSubject(subject)
		var filesReverted []string
		if files != "" {
			filesReverted = strings.Split(files, "\n")
		}
		httpjson.Write(w, http.StatusOK, undoResult{
			Round:         round,
			Summary:       summary,
			FilesReverted: filesReverted,
			Workspace:     ws,
		})
		return
	}

	httpjson.Write(w, http.StatusConflict, map[string]any{
		"error": "no planning commits to undo",
	})
}

// parsePlanSubject extracts the round number and summary from a commit
// subject line. Returns (0, "") if the subject doesn't match the expected
// format.
func parsePlanSubject(subject string) (int, string) {
	m := planRoundSubject.FindStringSubmatch(strings.TrimSpace(subject))
	if m == nil {
		return 0, ""
	}
	n, _ := strconv.Atoi(m[1])
	return n, m[2]
}

// extractDispatchedTaskIDs scans a unified diff for lines that add a
// dispatched_task_id and returns the UUIDs, de-duplicated, in order of
// first appearance.
func extractDispatchedTaskIDs(diff string) []uuid.UUID {
	matches := addedDispatchLine.FindAllStringSubmatch(diff, -1)
	ids := make([]uuid.UUID, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		raw := m[1]
		if seen[raw] {
			continue
		}
		seen[raw] = true
		id, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// cancelDispatchedTask cancels the kanban task linked to a dispatched spec
// when that dispatch is being reverted by undo. Best-effort: logs and
// returns without error on any failure (missing task, already terminal,
// cancel error). The spec file's dispatched_task_id is not touched here —
// the caller has already reverted the frontmatter via git.
func (h *Handler) cancelDispatchedTask(ctx context.Context, taskID uuid.UUID) {
	task, err := h.store.GetTask(ctx, taskID)
	if err != nil {
		slog.Warn("undo: dispatched task not found", "taskID", taskID, "err", err)
		return
	}
	switch task.Status {
	case store.TaskStatusDone, store.TaskStatusCancelled:
		return
	}
	if err := h.store.CancelTask(ctx, taskID); err != nil {
		slog.Warn("undo: cancel dispatched task failed", "taskID", taskID, "err", err)
		return
	}
	h.insertEventOrLog(ctx, taskID, store.EventTypeStateChange,
		store.NewStateChangeData(task.Status, store.TaskStatusCancelled, store.TriggerUser, nil))
}
