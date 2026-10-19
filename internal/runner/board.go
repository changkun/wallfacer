package runner

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/store"
)

// BoardManifest is the JSON structure written to board.json for each task
// run, giving the agent visibility into the other tasks on the board. The
// agent finds the file through WALLFACER_BOARD_JSON.
type BoardManifest struct {
	GeneratedAt time.Time   `json:"generated_at"`
	SelfTaskID  string      `json:"self_task_id"`
	Tasks       []BoardTask `json:"tasks"`
}

// BoardTask is a sanitized view of a single task exposed in board.json.
// SessionID is deliberately absent to prevent session hijacking.
type BoardTask struct {
	ID         string           `json:"id"`
	ShortID    string           `json:"short_id"`
	Title      string           `json:"title,omitempty"`
	Prompt     string           `json:"prompt"`
	Status     store.TaskStatus `json:"status"`
	IsSelf     bool             `json:"is_self"`
	Turns      int              `json:"turns"`
	Result     *string          `json:"result"`
	StopReason *string          `json:"stop_reason"`
	Usage      store.TaskUsage  `json:"usage"`
	BranchName string           `json:"branch_name,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// sharesWorkspace reports whether a task's worktree paths overlap with any of
// the self task's workspace paths. Tasks with no worktrees (e.g. backlog) are
// considered to share all workspaces so they appear in board context.
func sharesWorkspace(worktreePaths map[string]string, selfWorkspaces map[string]struct{}) bool {
	if len(worktreePaths) == 0 {
		return true // backlog tasks have no worktrees; include them
	}
	for repoPath := range worktreePaths {
		if _, ok := selfWorkspaces[repoPath]; ok {
			return true
		}
	}
	return false
}

// generateBoardContext builds the board.json bytes for selfTaskID from one
// store.ListTasks call. Results are cached by (boardChangeSeq, selfTaskID) so
// that per-turn calls cost nearly nothing when no task has changed.
//
// Size-limiting design:
//   - The self-task entry receives its full Prompt and Result.
//   - Other task entries have Prompt truncated to 500 chars and Result to 1000.
//   - After marshaling, if the manifest exceeds 64 KB a warning is logged.
func (r *Runner) generateBoardContext(selfTaskID uuid.UUID) ([]byte, error) {
	// Cache check: if no store mutation has occurred since we last generated
	// the board context for this task, return the cached result.
	currentSeq := r.boardChangeSeq.Load()
	r.boardCache.mu.Lock()
	if r.boardCache.json != nil &&
		r.boardCache.seq == currentSeq &&
		r.boardCache.selfTaskID == selfTaskID {
		jsonCopy := slices.Clone(r.boardCache.json)
		r.boardCache.mu.Unlock()
		return jsonCopy, nil
	}
	r.boardCache.mu.Unlock()

	s := r.currentStore()
	if s == nil {
		return nil, nil
	}
	tasks, err := s.ListTasks(r.shutdownCtx, false)
	if err != nil {
		return nil, err
	}

	// Build the set of workspace repo paths the self task is operating on.
	// Other tasks are only visible if they share at least one workspace.
	selfWorkspaces := map[string]struct{}{}
	for _, t := range tasks {
		if t.ID == selfTaskID {
			for repoPath := range t.WorktreePaths {
				selfWorkspaces[repoPath] = struct{}{}
			}
			break
		}
	}
	// If the self task has no worktrees (e.g. backlog), fall back to the
	// runner's configured workspace list so board context is still useful.
	workspaces := r.currentWorkspaces()
	if len(selfWorkspaces) == 0 && len(workspaces) > 0 {
		for _, ws := range workspaces {
			ws = strings.TrimSpace(ws)
			if ws != "" {
				selfWorkspaces[ws] = struct{}{}
			}
		}
	}

	boardTasks := make([]BoardTask, 0, len(tasks))
	for _, t := range tasks {
		isSelf := t.ID == selfTaskID
		shortID := t.ID.String()[:8]

		// Skip tasks that do not share any workspace with the self task.
		if !isSelf && len(selfWorkspaces) > 0 && !sharesWorkspace(t.WorktreePaths, selfWorkspaces) {
			continue
		}

		prompt := t.Prompt
		result := t.Result
		turns := t.Turns

		if !isSelf {
			// Limit other tasks' text fields to keep board.json compact.
			prompt = truncate(t.Prompt, 500)
			if result != nil {
				s := truncate(*result, 1000)
				result = &s
			}
			// Other tasks' turn counts are not useful for cross-task
			// awareness; omit them to reduce noise.
			turns = 0
		}

		boardTasks = append(boardTasks, BoardTask{
			ID:         t.ID.String(),
			ShortID:    shortID,
			Title:      t.Title,
			Prompt:     prompt,
			Status:     t.Status,
			IsSelf:     isSelf,
			Turns:      turns,
			Result:     result,
			StopReason: t.StopReason,
			Usage:      t.Usage,
			BranchName: t.BranchName,
			CreatedAt:  t.CreatedAt,
			UpdatedAt:  t.UpdatedAt,
		})
	}

	manifest := BoardManifest{
		GeneratedAt: time.Now(),
		SelfTaskID:  selfTaskID.String(),
		Tasks:       boardTasks,
	}

	jsonBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}

	// Manifest size guard: warn when the file would exceed 64 KB so that
	// operators are notified before token costs become significant.
	if len(jsonBytes) > constants.MaxBoardManifestBytes {
		sizes := make([]struct {
			id    string
			bytes int
		}, 0, len(boardTasks))
		for _, bt := range boardTasks {
			if b, merr := json.Marshal(bt); merr == nil {
				sizes = append(sizes, struct {
					id    string
					bytes int
				}{id: bt.ShortID, bytes: len(b)})
			}
		}
		logBoardManifestSizeWarning(sizes, len(jsonBytes))
	}

	// Store in cache.
	r.boardCache.mu.Lock()
	r.boardCache.seq = currentSeq
	r.boardCache.selfTaskID = selfTaskID
	r.boardCache.json = jsonBytes
	r.boardCache.mu.Unlock()

	return jsonBytes, nil
}

// GenerateBoardManifest builds the board manifest for selfTaskID.
// Pass uuid.Nil when there is no self-task (e.g. the debug endpoint).
// Benefits from the same cache as generateBoardContext.
func (r *Runner) GenerateBoardManifest(ctx context.Context, selfTaskID uuid.UUID) (*BoardManifest, error) {
	_ = ctx // context not forwarded; generateBoardContext uses the runner's shutdown context
	jsonBytes, err := r.generateBoardContext(selfTaskID)
	if err != nil {
		return nil, err
	}
	var m BoardManifest
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// logBoardManifestSizeWarning logs a warning that board.json has grown large,
// and lists the top-5 tasks by estimated serialized size to help operators
// pinpoint the source of the bloat. sizes contains pre-computed per-task byte
// counts collected by the caller during serialization.
func logBoardManifestSizeWarning(sizes []struct {
	id    string
	bytes int
}, totalBytes int) {
	slices.SortFunc(sizes, func(a, b struct {
		id    string
		bytes int
	}) int {
		return cmp.Compare(b.bytes, a.bytes)
	})

	top := sizes
	if len(top) > 5 {
		top = top[:5] // utf8-safe: element slice, not a string
	}

	args := []any{"total_bytes", totalBytes}
	for i, ts := range top {
		args = append(args, fmt.Sprintf("task%d", i+1), fmt.Sprintf("%s (%d B)", ts.id, ts.bytes))
	}
	logger.Runner.Warn("board manifest is large", args...)
}

// writeBoardDir writes board.json to a new temp directory under baseDir and
// returns the directory path. The caller must defer os.RemoveAll(dir).
// baseDir must be a path accessible to Docker bind mounts (e.g. under $HOME).
// When baseDir is empty, os.TempDir() is used as the fallback.
func writeBoardDir(data []byte, baseDir string) (string, error) {
	dir, err := os.MkdirTemp(baseDir, "wallfacer-board-*")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "board.json"), data, 0644); err != nil {
		_ = os.RemoveAll(dir)

		return "", err
	}
	return dir, nil
}
