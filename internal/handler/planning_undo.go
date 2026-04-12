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

// planCommitSubject parses a kanban-style planning commit subject like
// "specs/local/auth(plan): add OAuth breakdown" as written by
// commitPlanningRound. Group 1 is the primary-path scope, group 2 is the
// imperative summary. A missing summary yields an empty group rather than
// a match failure, so `<path>(plan):` alone still parses (git's default
// --cleanup=strip drops the trailing space).
var planCommitSubject = regexp.MustCompile(`^(\S+)\(plan\):\s*(.*)$`)

// planRoundTrailer parses the `Plan-Round: N` git trailer that
// commitPlanningRound writes at the bottom of the commit body. This is
// what `git log --grep="^Plan-Round: "` anchors on.
var planRoundTrailer = regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(planRoundTrailerPrefix) + `(\d+)\s*$`)

// addedDispatchLine matches unified-diff added lines that set a UUID
// dispatched_task_id. Excludes diff headers like "+++ b/specs/..." since
// those don't begin with "+dispatched_task_id:".
var addedDispatchLine = regexp.MustCompile(
	`(?m)^\+dispatched_task_id:\s*([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`)

// UndoPlanningRound resets the first workspace with a planning commit back to
// the state before the last commit carrying the Plan-Round trailer. Stashes
// dirty user edits across the reset, pops the stash after (returning 409 on
// conflict), and cancels any kanban tasks that were dispatched by the
// reverted commit.
//
// Responds 409 if no planning commits exist in any workspace, or if the
// latest planning commit is not at HEAD (manual commits made since would be
// destroyed by a HEAD~1 reset — the user must resolve manually).
func (h *Handler) UndoPlanningRound(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	for _, ws := range h.currentWorkspaces() {
		// %x00 separates the three fields so subject or body text with
		// spaces and newlines round-trips cleanly. We need the body in
		// addition to the subject to read the Plan-Round trailer.
		out, err := cmdexec.Git(ws, "log", "-1",
			"--grep=^"+planRoundTrailerPrefix,
			"--format=%H%x00%s%x00%B").WithContext(ctx).Output()
		if err != nil || out == "" {
			continue
		}
		parts := strings.SplitN(out, "\x00", 3)
		if len(parts) < 3 {
			continue
		}
		hash, subject, body := parts[0], parts[1], parts[2]

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

		round, summary := parsePlanMessage(subject, body)
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

// parsePlanMessage extracts the round number (from the Plan-Round trailer
// in body) and the imperative summary (from the `<path>(plan): <subject>`
// subject line) of a planning commit. Either value is zero/empty when the
// commit doesn't follow the expected shape, so callers can still surface
// whatever the log gave them.
func parsePlanMessage(subject, body string) (int, string) {
	round := 0
	if m := planRoundTrailer.FindStringSubmatch(body); m != nil {
		round, _ = strconv.Atoi(m[1])
	}
	summary := ""
	if m := planCommitSubject.FindStringSubmatch(strings.TrimSpace(subject)); m != nil {
		summary = strings.TrimSpace(m[2])
	}
	return round, summary
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
