package runner

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
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
	IsSelf        bool            `json:"is_self"`
	Turns         int             `json:"turns"`
	Result        *string         `json:"result"`
	StopReason    *string         `json:"stop_reason"`
	Usage         store.TaskUsage `json:"usage"`
	BranchName    string          `json:"branch_name,omitempty"`
	WorktreeMount *string         `json:"worktree_mount"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
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

		boardTasks = append(boardTasks, BoardTask{
			ID:            t.ID.String(),
			ShortID:       shortID,
			Title:         t.Title,
			Prompt:        t.Prompt,
			Status:        t.Status,
			IsSelf:        isSelf,
			Turns:         t.Turns,
			Result:        t.Result,
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

	return json.MarshalIndent(manifest, "", "  ")
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
