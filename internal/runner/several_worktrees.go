package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/store"
)

// severalWorktreesEventKind is the kind of the system event that tells a
// task's timeline it has worktrees in several folders.
const severalWorktreesEventKind = "worktrees:several_folders"

// noteSeveralWorktrees writes one system event on the timeline of a task whose
// stored WorktreePaths has entries for more than one folder. Such a task was
// created when a workspace could span several folders; a workspace is now one
// folder and new tasks get one worktree. The task keeps running as it is: the
// agent works in one of its worktrees (see agentWorkDir), and the commit,
// sync and cleanup steps still visit every folder's worktree, so no change is
// dropped. The event says which worktree the agent works in.
//
// The event is written once per task. A resumed or retried run of the same
// task finds the earlier event and writes nothing. When the timeline
// cannot be read the notice is skipped and logged rather than risk writing a
// duplicate.
func (r *Runner) noteSeveralWorktrees(ctx context.Context, task *store.Task) {
	if len(task.WorktreePaths) < 2 {
		return
	}
	s := r.taskStore(task.ID)
	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		logger.Runner.Warn("several worktrees notice: read timeline", "task", task.ID, "error", err)
		return
	}
	for _, ev := range events {
		if ev.EventType != store.EventTypeSystem {
			continue
		}
		var data struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(ev.Data, &data) == nil && data.Kind == severalWorktreesEventKind {
			return
		}
	}
	folders := slices.Sorted(maps.Keys(task.WorktreePaths))
	agentFolder := r.agentWorktreeFolder(task.WorktreePaths)
	if err := s.InsertEvent(ctx, task.ID, store.EventTypeSystem, map[string]string{
		"kind":         severalWorktreesEventKind,
		"agent_folder": agentFolder,
		"result": fmt.Sprintf(
			"This task has worktrees in %d folders (%s) from before a workspace became one folder. "+
				"The agent works in the worktree for %s. Committing the task still merges the changes in every folder.",
			len(folders), strings.Join(folders, ", "), agentFolder),
	}); err != nil {
		logger.Runner.Warn("several worktrees notice: write event", "task", task.ID, "error", err)
	}
}
