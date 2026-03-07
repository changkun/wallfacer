package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// BoardManifest is the JSON structure written to board.json inside each
// task container, giving Claude visibility into sibling tasks on the board.
type BoardManifest struct {
	GeneratedAt time.Time   `json:"generated_at"`
	SelfTaskID  string      `json:"self_task_id"`
	Tasks       []BoardTask `json:"tasks"`
}

// BoardTask is a sanitized view of a single task exposed in board.json.
// SessionID is deliberately absent to prevent session hijacking.
type BoardTask struct {
	ID            string           `json:"id"`
	ShortID       string           `json:"short_id"`
	Title         string           `json:"title,omitempty"`
	Prompt        string           `json:"prompt"`
	Status        store.TaskStatus `json:"status"`
	IsSelf        bool             `json:"is_self"`
	Turns         int              `json:"turns"`
	Result        *string          `json:"result"`
	StopReason    *string          `json:"stop_reason"`
	Usage         store.TaskUsage  `json:"usage"`
	BranchName    string           `json:"branch_name,omitempty"`
	WorktreeMount *string          `json:"worktree_mount"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

// canMountWorktree reports whether a sibling task's worktrees are eligible
// for read-only mounting based on its status.
func canMountWorktree(status store.TaskStatus, worktreePaths map[string]string) bool {
	switch status {
	case store.TaskStatusWaiting, store.TaskStatusFailed:
		return true
	case store.TaskStatusDone:
		// Only if at least one worktree directory still exists on disk.
		for _, wt := range worktreePaths {
			if info, err := os.Stat(wt); err == nil && info.IsDir() {
				return true
			}
		}
		return false
	default:
		// backlog (no worktree), in_progress (actively modified),
		// cancelled/archived (worktrees cleaned up).
		return false
	}
}

// generateBoardContext serializes all non-archived tasks into board.json bytes.
//
// Size-limiting design (see WriteBoardManifest for rationale):
//   - The self-task entry receives its full Prompt and Result so the running
//     agent always has its own complete context.
//   - Sibling task entries have Prompt truncated to 500 characters and Result
//     truncated to 1000 characters (with a trailing "..." when cut), and their
//     Turns counter is reset to 0 because only summary fields (status, result,
//     branch) matter for cross-task awareness.
//   - After marshalling, if the manifest exceeds 64 KB a warning is logged
//     listing the five largest contributors so operators can investigate.
//
// It strips SessionID, marks is_self, and computes worktree_mount paths.
func (r *Runner) generateBoardContext(selfTaskID uuid.UUID, mountWorktrees bool) ([]byte, error) {
	tasks, err := r.store.ListTasks(context.TODO(), false)
	if err != nil {
		return nil, err
	}

	boardTasks := make([]BoardTask, 0, len(tasks))
	for _, t := range tasks {
		isSelf := t.ID == selfTaskID
		shortID := t.ID.String()[:8]

		var worktreeMount *string
		if mountWorktrees && !isSelf && canMountWorktree(t.Status, t.WorktreePaths) && len(t.WorktreePaths) > 0 {
			// Compute the container mount path for the first workspace.
			// All sibling worktrees are mounted under /workspace/.tasks/worktrees/<short-id>/.
			for repoPath := range t.WorktreePaths {
				basename := filepath.Base(repoPath)
				p := "/workspace/.tasks/worktrees/" + shortID + "/" + basename
				worktreeMount = &p
				break // just indicate the mount root; multiple repos follow the same pattern
			}
		}

		prompt := t.Prompt
		result := t.Result
		turns := t.Turns

		if !isSelf {
			// Limit sibling task text fields to keep board.json compact.
			prompt = truncate(t.Prompt, 500)
			if result != nil {
				s := truncate(*result, 1000)
				result = &s
			}
			// Sibling turn counts are not useful for cross-task awareness;
			// omit them to reduce noise.
			turns = 0
		}

		boardTasks = append(boardTasks, BoardTask{
			ID:            t.ID.String(),
			ShortID:       shortID,
			Title:         t.Title,
			Prompt:        prompt,
			Status:        t.Status,
			IsSelf:        isSelf,
			Turns:         turns,
			Result:        result,
			StopReason:    t.StopReason,
			Usage:         t.Usage,
			BranchName:    t.BranchName,
			WorktreeMount: worktreeMount,
			CreatedAt:     t.CreatedAt,
			UpdatedAt:     t.UpdatedAt,
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
	const maxManifestBytes = 64 * 1024
	if len(jsonBytes) > maxManifestBytes {
		logBoardManifestSizeWarning(boardTasks, len(jsonBytes))
	}

	return jsonBytes, nil
}

// logBoardManifestSizeWarning logs a warning that board.json has grown large,
// and lists the top-5 tasks by estimated serialized size to help operators
// pinpoint the source of the bloat.
func logBoardManifestSizeWarning(tasks []BoardTask, totalBytes int) {
	type taskSize struct {
		id    string
		bytes int
	}
	sizes := make([]taskSize, 0, len(tasks))
	for _, bt := range tasks {
		b, err := json.Marshal(bt)
		if err == nil {
			sizes = append(sizes, taskSize{id: bt.ShortID, bytes: len(b)})
		}
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i].bytes > sizes[j].bytes })

	top := sizes
	if len(top) > 5 {
		top = top[:5]
	}

	args := []any{"total_bytes", totalBytes}
	for i, ts := range top {
		args = append(args, fmt.Sprintf("task%d", i+1), fmt.Sprintf("%s (%d B)", ts.id, ts.bytes))
	}
	logger.Runner.Warn("board manifest is large", args...)
}

// prepareBoardContext writes board.json to a temp directory and returns the
// directory path. The caller must defer os.RemoveAll(dir).
func (r *Runner) prepareBoardContext(selfTaskID uuid.UUID, mountWorktrees bool) (string, error) {
	data, err := r.generateBoardContext(selfTaskID, mountWorktrees)
	if err != nil {
		return "", err
	}

	dir, err := os.MkdirTemp("", "wallfacer-board-*")
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(filepath.Join(dir, "board.json"), data, 0644); err != nil {
		os.RemoveAll(dir)
		return "", err
	}

	return dir, nil
}

// buildSiblingMounts returns shortID → (repoPath → worktreePath) for
// eligible sibling tasks. Only tasks whose worktrees can be safely mounted
// read-only are included.
func (r *Runner) buildSiblingMounts(selfTaskID uuid.UUID) map[string]map[string]string {
	tasks, err := r.store.ListTasks(context.TODO(), false)
	if err != nil {
		logger.Runner.Warn("buildSiblingMounts: list tasks", "error", err)
		return nil
	}

	mounts := make(map[string]map[string]string)
	for _, t := range tasks {
		if t.ID == selfTaskID {
			continue
		}
		if !canMountWorktree(t.Status, t.WorktreePaths) || len(t.WorktreePaths) == 0 {
			continue
		}
		shortID := t.ID.String()[:8]
		mounts[shortID] = make(map[string]string, len(t.WorktreePaths))
		maps.Copy(mounts[shortID], t.WorktreePaths)
	}

	if len(mounts) == 0 {
		return nil
	}
	return mounts
}
