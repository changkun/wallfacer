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

// planCommitSubject parses a scope-prefixed planning commit subject like
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

// planThreadTrailer parses the `Plan-Thread: <id>` git trailer that
// identifies the chat thread that wrote the commit.
var planThreadTrailer = regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(planThreadTrailerPrefix) + `(\S+)\s*$`)

// addedDispatchLine matches unified-diff added lines that set a UUID
// dispatched_task_id. Excludes diff headers like "+++ b/specs/..." since
// those don't begin with "+dispatched_task_id:".
var addedDispatchLine = regexp.MustCompile(
	`(?m)^\+dispatched_task_id:\s*([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`)

// UndoPlanningRound creates a forward `git revert` commit that undoes the
// caller's most recent planning round (commit carrying a matching
// Plan-Thread trailer). Using `git revert` rather than `git reset --hard
// HEAD~1` lets undo succeed even when another thread has committed
// afterwards — each thread's rounds are independently revertible in
// their own time.
//
// The revert commit itself carries `Plan-Thread: <id>` and
// `Plan-Round: N+1` trailers so it's attributable to the same thread and
// keeps round numbering monotonic.
//
// The `?thread=<id>` query parameter selects the caller's thread; when
// omitted, the active thread is used. Dispatched board tasks referenced
// by the reverted commit are cancelled (same behaviour as the pre-revert
// design). Dirty user edits are stashed across the revert.
//
// Responds 409 if the thread has no planning commits to undo, or if the
// revert produces a merge conflict. In the conflict case the revert is
// aborted so the working tree is left clean.
func (h *Handler) UndoPlanningRound(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	threadID := h.threadIDFromRequest(r)

	for _, ws := range h.currentWorkspaces() {
		hash, subject, body := findLatestThreadPlanCommit(ctx, ws, threadID)
		if hash == "" {
			continue
		}

		diff, _ := cmdexec.Git(ws, "show", hash, "--", "specs/").WithContext(ctx).Output()
		files, _ := cmdexec.Git(ws, "show", "--name-only", "--format=", hash).WithContext(ctx).Output()

		stashed := gitutil.StashIfDirty(ws)

		// Apply the revert without committing so we can attach our own
		// thread/round trailers via a hand-built commit message.
		revertErr := cmdexec.Git(ws, "revert", "--no-commit", hash).WithContext(ctx).Run()
		if revertErr != nil {
			// Conflict (or any other revert failure) leaves the index in
			// a mid-revert state. Abort so the working tree is clean and
			// let the stash pop restore the user's edits.
			_ = cmdexec.Git(ws, "revert", "--abort").WithContext(ctx).Run()
			if stashed {
				_ = gitutil.StashPop(ws)
			}
			httpjson.Write(w, http.StatusConflict, map[string]any{
				"error":     "revert conflict; working tree left unchanged",
				"detail":    revertErr.Error(),
				"workspace": ws,
			})
			return
		}

		priorRound, summary := parsePlanMessage(subject, body)
		nextRound := nextPlanRound(ctx, ws)
		revertSubject := buildRevertSubject(summary, priorRound)
		commitMsg := revertSubject +
			"\n\n" + planRoundTrailerPrefix + strconv.Itoa(nextRound) +
			"\n" + planThreadTrailerPrefix + threadID

		args := []string{"-C", ws}
		args = append(args, hostGitIdentityOverrides(ctx)...)
		args = append(args, "commit", "-m", commitMsg)
		if err := cmdexec.New("git", args...).WithContext(ctx).Run(); err != nil {
			_ = cmdexec.Git(ws, "revert", "--abort").WithContext(ctx).Run()
			if stashed {
				_ = gitutil.StashPop(ws)
			}
			http.Error(w, "git commit (revert): "+err.Error(), http.StatusInternalServerError)
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

		var filesReverted []string
		if files != "" {
			filesReverted = strings.Split(strings.TrimSpace(files), "\n")
		}
		httpjson.Write(w, http.StatusOK, undoResult{
			Round:         priorRound,
			Summary:       summary,
			FilesReverted: filesReverted,
			Workspace:     ws,
		})
		return
	}

	httpjson.Write(w, http.StatusConflict, map[string]any{
		"error": "no planning commits to undo for this thread",
	})
}

// findLatestThreadPlanCommit returns (hash, subject, body) for the most
// recent commit in ws that matches both the Plan-Round trailer and (when
// threadID is non-empty) a Plan-Thread trailer with the given ID. Revert
// commits already carry a thread trailer for the same thread, so this
// would re-match them; to avoid picking a commit that's already been
// reverted, we walk the log newest-first and skip any commit whose diff
// has been net-reverted by a later commit (git represents that naturally:
// when pair (C, revert(C)) sits in history, the files match neither C
// nor the tree at the point before C — we detect by checking if `git
// cherry` would consider it cherry-picked).
//
// Simplification: because every round commit and every revert carries a
// distinct Plan-Round number, walking newest-first and picking the first
// round whose net effect is not already reverted suffices. We implement
// this by walking commits in log order and consulting the "applied
// rounds" set (rounds introduced by a round commit, removed by a revert
// of that round). The most recent round in the set is the target.
func findLatestThreadPlanCommit(ctx context.Context, ws, threadID string) (hash, subject, body string) {
	// %x00 separates fields; %x1E terminates records.
	out, err := cmdexec.Git(ws, "log",
		"--grep=^"+planRoundTrailerPrefix,
		"--format=%H%x00%s%x00%B%x1E").WithContext(ctx).Output()
	if err != nil || out == "" {
		return "", "", ""
	}

	type entry struct {
		hash, subject, body string
		round               int
	}
	var entries []entry
	for _, rec := range strings.Split(out, "\x1E") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, "\x00", 3)
		if len(parts) < 3 {
			continue
		}
		// Filter by Plan-Thread when threadID is supplied.
		if threadID != "" {
			m := planThreadTrailer.FindStringSubmatch(parts[2])
			if m == nil || m[1] != threadID {
				continue
			}
		}
		roundMatch := planRoundTrailer.FindStringSubmatch(parts[2])
		if roundMatch == nil {
			continue
		}
		r, _ := strconv.Atoi(roundMatch[1])
		entries = append(entries, entry{
			hash: parts[0], subject: parts[1], body: parts[2], round: r,
		})
	}
	if len(entries) == 0 {
		return "", "", ""
	}

	// Walk newest-first. A revert commit has a `Revert "…"` subject
	// that the buildRevertSubject helper does NOT produce (we strip
	// that and use a scope-prefixed subject), but we DO write the word
	// "Revert" into our subject. Reverts carry `revertRoundPrefix` in
	// their subject for easy detection. See buildRevertSubject.
	applied := map[int]bool{}
	reverted := map[int]bool{}
	// Build the set newest -> oldest? Easier: process oldest -> newest
	// so state converges as the log is replayed.
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if r, ok := revertedRoundFromSubject(e.subject); ok {
			reverted[r] = true
			continue
		}
		if !reverted[e.round] {
			applied[e.round] = true
		}
	}
	// Now pick the highest-round entry whose round is still applied.
	var best entry
	bestFound := false
	for _, e := range entries {
		if _, isRevert := revertedRoundFromSubject(e.subject); isRevert {
			continue
		}
		if !applied[e.round] {
			continue
		}
		if !bestFound || e.round > best.round {
			best = e
			bestFound = true
		}
	}
	if !bestFound {
		return "", "", ""
	}
	return best.hash, best.subject, best.body
}

// nextPlanRound returns the next round number to use for a planning
// commit in ws. Counts the number of `Plan-Round:` trailers reachable
// from HEAD and adds one. Mirrors the logic in commitPlanningRound.
func nextPlanRound(ctx context.Context, ws string) int {
	out, err := cmdexec.Git(ws, "log", "--format=%H",
		"--grep=^"+planRoundTrailerPrefix).WithContext(ctx).Output()
	if err != nil || out == "" {
		return 1
	}
	return len(strings.Split(strings.TrimSpace(out), "\n")) + 1
}

// revertSubjectPrefix prefixes the subject line of a revert commit so
// findLatestThreadPlanCommit can tell reverts from forward rounds. The
// "(plan)" scope is retained so the commit still threads through
// planCommitSubject-aware tooling.
const revertSubjectPrefix = "planning(plan): revert round "

// buildRevertSubject builds a commit subject for a `git revert` of a
// planning round. The subject carries the original round number so
// findLatestThreadPlanCommit can mark the round as un-applied when
// scanning the log.
func buildRevertSubject(originalSummary string, originalRound int) string {
	s := revertSubjectPrefix + strconv.Itoa(originalRound)
	if originalSummary != "" {
		s += " (" + originalSummary + ")"
	}
	// Keep subject lines from growing absurdly long.
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// revertedRoundFromSubject parses a revert commit's subject and returns
// the original round number it reverts, or (0, false) when the subject
// isn't a planning-revert.
func revertedRoundFromSubject(subject string) (int, bool) {
	subject = strings.TrimSpace(subject)
	if !strings.HasPrefix(subject, revertSubjectPrefix) {
		return 0, false
	}
	rest := subject[len(revertSubjectPrefix):]
	// Round number runs until end, space, or '('.
	end := len(rest)
	for i, r := range rest {
		if r == ' ' || r == '(' {
			end = i
			break
		}
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0, false
	}
	return n, true
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

// cancelDispatchedTask cancels the board task linked to a dispatched spec
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
