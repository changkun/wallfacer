package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/set"
	"changkun.de/x/wallfacer/internal/store"
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

// sharesWorkspace reports whether a task's worktree paths overlap with any of
// the self task's workspace paths. Tasks with no worktrees (e.g. backlog) are
// considered to share all workspaces so they appear in board context.
func sharesWorkspace(worktreePaths map[string]string, selfWorkspaces set.Set[string]) bool {
	if len(worktreePaths) == 0 {
		return true // backlog tasks have no worktrees; include them
	}
	for repoPath := range worktreePaths {
		if selfWorkspaces.Has(repoPath) {
			return true
		}
	}
	return false
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

// countWriter wraps an io.Writer and counts the total bytes written.
type countWriter struct {
	w io.Writer
	n int64
}

func (cw *countWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	return n, err
}

// generateBoardContextAndMounts is the fused board-context generator.
// It produces both the board.json bytes and the sibling-mount map in a single
// store.ListTasks call. Results are cached by (boardChangeSeq, selfTaskID) so
// that per-turn calls cost nearly nothing when no task has changed.
//
// Size-limiting design:
//   - The self-task entry receives its full Prompt and Result.
//   - Sibling task entries have Prompt truncated to 500 chars and Result to 1000.
//   - After marshalling, if the manifest exceeds 64 KB a warning is logged.
func (r *Runner) generateBoardContextAndMounts(selfTaskID uuid.UUID, mountWorktrees bool) ([]byte, map[string]map[string]string, error) {
	// Cache check: if no store mutation has occurred since we last generated
	// the board context for this task, return the cached result.
	currentSeq := r.boardChangeSeq.Load()
	r.boardCache.mu.Lock()
	if r.boardCache.json != nil &&
		r.boardCache.seq == currentSeq &&
		r.boardCache.selfTaskID == selfTaskID {
		jsonCopy := make([]byte, len(r.boardCache.json))
		copy(jsonCopy, r.boardCache.json)
		mountsCopy := deepCopyMounts(r.boardCache.mounts)
		r.boardCache.mu.Unlock()
		return jsonCopy, mountsCopy, nil
	}
	r.boardCache.mu.Unlock()

	tasks, err := r.store.ListTasks(r.shutdownCtx, false)
	if err != nil {
		return nil, nil, err
	}

	// Build the set of workspace repo paths the self task is operating on.
	// Siblings are only visible if they share at least one workspace.
	selfWorkspaces := set.New[string]()
	for _, t := range tasks {
		if t.ID == selfTaskID {
			for repoPath := range t.WorktreePaths {
				selfWorkspaces.Add(repoPath)
			}
			break
		}
	}
	// If the self task has no worktrees (e.g. backlog), fall back to the
	// runner's configured workspace list so board context is still useful.
	if selfWorkspaces.Len() == 0 && len(r.workspaces) > 0 {
		for _, ws := range r.workspaces {
			ws = strings.TrimSpace(ws)
			if ws != "" {
				selfWorkspaces.Add(ws)
			}
		}
	}

	boardTasks := make([]BoardTask, 0, len(tasks))
	mounts := make(map[string]map[string]string)
	for _, t := range tasks {
		isSelf := t.ID == selfTaskID
		shortID := t.ID.String()[:8]

		// Skip siblings that do not share any workspace with the self task.
		if !isSelf && selfWorkspaces.Len() > 0 && !sharesWorkspace(t.WorktreePaths, selfWorkspaces) {
			continue
		}

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
			// Record host-side mount paths for sibling worktrees.
			mounts[shortID] = make(map[string]string, len(t.WorktreePaths))
			maps.Copy(mounts[shortID], t.WorktreePaths)
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

	if len(mounts) == 0 {
		mounts = nil
	}

	manifest := BoardManifest{
		GeneratedAt: time.Now(),
		SelfTaskID:  selfTaskID.String(),
		Tasks:       boardTasks,
	}

	jsonBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, nil, err
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

	// Store in cache (caller gets a deep copy of mounts).
	r.boardCache.mu.Lock()
	r.boardCache.seq = currentSeq
	r.boardCache.selfTaskID = selfTaskID
	r.boardCache.json = jsonBytes
	r.boardCache.mounts = mounts
	r.boardCache.mu.Unlock()

	return jsonBytes, deepCopyMounts(mounts), nil
}

// deepCopyMounts returns a deep copy of a shortID → (repoPath → worktreePath) map.
func deepCopyMounts(m map[string]map[string]string) map[string]map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]map[string]string, len(m))
	for k, v := range m {
		inner := make(map[string]string, len(v))
		maps.Copy(inner, v)
		out[k] = inner
	}
	return out
}

// GenerateBoardManifest builds the board manifest for selfTaskID.
// Pass uuid.Nil when there is no self-task (e.g. the debug endpoint).
// Pass mountWorktrees=false when worktree paths are not needed.
// Benefits from the same cache as generateBoardContextAndMounts.
func (r *Runner) GenerateBoardManifest(ctx context.Context, selfTaskID uuid.UUID, mountWorktrees bool) (*BoardManifest, error) {
	_ = ctx // context not forwarded; generateBoardContextAndMounts uses background context internally
	jsonBytes, _, err := r.generateBoardContextAndMounts(selfTaskID, mountWorktrees)
	if err != nil {
		return nil, err
	}
	var m BoardManifest
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// generateBoardContext serializes all non-archived tasks into board.json bytes.
// It is a thin wrapper around generateBoardContextAndMounts that discards the
// sibling-mount map. Kept for backward compatibility with tests.
func (r *Runner) generateBoardContext(ctx context.Context, selfTaskID uuid.UUID, mountWorktrees bool) ([]byte, error) {
	_ = ctx // context not forwarded; generateBoardContextAndMounts uses background context internally
	data, _, err := r.generateBoardContextAndMounts(selfTaskID, mountWorktrees)
	return data, err
}

// logBoardManifestSizeWarning logs a warning that board.json has grown large,
// and lists the top-5 tasks by estimated serialized size to help operators
// pinpoint the source of the bloat. sizes contains pre-computed per-task byte
// counts collected by the caller during serialisation.
func logBoardManifestSizeWarning(sizes []struct {
	id    string
	bytes int
}, totalBytes int) {
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

// writeBoardDir writes board.json to a new temp directory and returns the
// directory path. The caller must defer os.RemoveAll(dir).
func writeBoardDir(data []byte) (string, error) {
	dir, err := os.MkdirTemp("", "wallfacer-board-*")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "board.json"), data, 0644); err != nil {
		_ = os.RemoveAll(dir)

		return "", err
	}
	return dir, nil
}

// streamBoardJSON creates a temp directory, opens board.json inside it, and
// writes the board manifest in a single streaming pass without constructing an
// intermediate BoardManifest value. It returns the directory path and the
// number of bytes written. The caller must defer os.RemoveAll(dir) on success.
func streamBoardJSON(ctx context.Context, st *store.Store, selfTaskID uuid.UUID, mountWorktrees bool) (dir string, written int64, err error) {
	dir, err = os.MkdirTemp("", "wallfacer-board-*")
	if err != nil {
		return "", 0, err
	}

	f, ferr := os.Create(filepath.Join(dir, "board.json"))
	if ferr != nil {
		_ = os.RemoveAll(dir)

		return "", 0, ferr
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close board.json: %w", closeErr)
			_ = os.RemoveAll(dir)

			dir = ""
			written = 0
		}
	}()

	cw := &countWriter{w: f}

	if _, err = fmt.Fprintf(cw, "{\"generated_at\":%q,\"self_task_id\":%q,\"tasks\":[\n",
		time.Now().UTC().Format(time.RFC3339Nano), selfTaskID.String()); err != nil {
		_ = os.RemoveAll(dir)

		return "", 0, err
	}

	tasks, err := st.ListTasks(ctx, false)
	if err != nil {
		_ = os.RemoveAll(dir)

		return "", 0, err
	}

	taskSizes := make([]struct {
		id    string
		bytes int
	}, 0, len(tasks))

	for i, t := range tasks {
		isSelf := t.ID == selfTaskID
		shortID := t.ID.String()[:8]

		var worktreeMount *string
		if mountWorktrees && !isSelf && canMountWorktree(t.Status, t.WorktreePaths) && len(t.WorktreePaths) > 0 {
			for repoPath := range t.WorktreePaths {
				basename := filepath.Base(repoPath)
				p := "/workspace/.tasks/worktrees/" + shortID + "/" + basename
				worktreeMount = &p
				break
			}
		}

		prompt := t.Prompt
		result := t.Result
		turns := t.Turns

		if !isSelf {
			prompt = truncate(t.Prompt, 500)
			if result != nil {
				s := truncate(*result, 1000)
				result = &s
			}
			turns = 0
		}

		bt := BoardTask{
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
		}

		b, merr := json.Marshal(bt)
		if merr != nil {
			_ = os.RemoveAll(dir)

			return "", 0, merr
		}

		taskSizes = append(taskSizes, struct {
			id    string
			bytes int
		}{id: shortID, bytes: len(b)})

		if i > 0 {
			if _, werr := fmt.Fprint(cw, ",\n"); werr != nil {
				_ = os.RemoveAll(dir)

				return "", 0, werr
			}
		}
		if _, werr := cw.Write(b); werr != nil {
			_ = os.RemoveAll(dir)

			return "", 0, werr
		}
	}

	if _, werr := fmt.Fprint(cw, "]\n}"); werr != nil {
		_ = os.RemoveAll(dir)

		return "", 0, werr
	}

	if cw.n > int64(constants.MaxBoardManifestBytes) {
		logBoardManifestSizeWarning(taskSizes, int(cw.n))
	}

	return dir, cw.n, nil
}

// prepareBoardContext writes board.json to a temp directory and returns the
// directory path. The caller must defer os.RemoveAll(dir).
func (r *Runner) prepareBoardContext(ctx context.Context, selfTaskID uuid.UUID, mountWorktrees bool) (string, error) {
	dir, _, err := streamBoardJSON(ctx, r.store, selfTaskID, mountWorktrees)
	return dir, err
}

// buildSiblingMounts returns shortID → (repoPath → worktreePath) for
// eligible sibling tasks. Only tasks whose worktrees can be safely mounted
// read-only are included.
// It is a thin wrapper around generateBoardContextAndMounts that discards the
// board JSON. Kept for backward compatibility with tests.
func (r *Runner) buildSiblingMounts(ctx context.Context, selfTaskID uuid.UUID) map[string]map[string]string {
	_ = ctx // context not forwarded; generateBoardContextAndMounts uses background context internally
	_, mounts, err := r.generateBoardContextAndMounts(selfTaskID, true)
	if err != nil {
		logger.Runner.Warn("buildSiblingMounts: list tasks", "error", err)
		return nil
	}
	return mounts
}
