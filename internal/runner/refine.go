package runner

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"changkun.de/x/wallfacer/internal/agents"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
)

// roleRefinement binds to the agents.Refinement descriptor; the
// runner's dispatch plumbing lives in agent_bindings.go.
var roleRefinement = agents.Refinement

// RunRefinement runs the sandbox agent in read-only mode to produce a
// detailed implementation spec for the task's current prompt. The task
// stays in backlog; only CurrentRefinement is updated to track state.
// userInstructions is an optional hint from the user that narrows the
// agent's focus (e.g. "keep backward compatibility").
func (r *Runner) RunRefinement(taskID uuid.UUID, userInstructions string) {
	bgCtx := r.shutdownCtx

	task, err := r.taskStore(taskID).GetTask(bgCtx, taskID)
	if err != nil {
		logger.Runner.Error("refinement: get task", "task", taskID, "error", err)
		return
	}

	prompt := r.buildRefinementPrompt(task, userInstructions, time.Now())

	// Preserve the legacy slugged container name so in-flight log
	// consumers that filter by prefix still match.
	slug := slugifyPrompt(prompt, 20)
	containerName := "wallfacer-refine-" + slug + "-" + taskID.String()[:8]

	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanStart,
		store.SpanData{Phase: "refinement", Label: "refinement"})
	res, err := r.runAgent(bgCtx, roleRefinement, task, prompt, runAgentOpts{
		ContainerName: containerName,
		TrackUsage:    true,
		Turn:          1,
		OnLaunch: func(name string, handle sandbox.Handle) {
			r.refineContainers.Set(taskID, name)
			r.refineContainers.SetHandle(taskID, handle, nil)
		},
	})
	r.refineContainers.Delete(taskID)
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd,
		store.SpanData{Phase: "refinement", Label: "refinement"})

	if err != nil {
		logger.Runner.Error("refinement container error", "task", taskID, "error", err)
		// Don't overwrite a cleared job (task may have been reset).
		cur, getErr := r.taskStore(taskID).GetTask(bgCtx, taskID)
		if getErr != nil || cur.CurrentRefinement == nil {
			return
		}
		cur.CurrentRefinement.Status = store.RefinementJobStatusFailed
		cur.CurrentRefinement.Error = err.Error()
		_ = r.taskStore(taskID).UpdateRefinementJob(bgCtx, taskID, cur.CurrentRefinement)
		return
	}

	cur, getErr := r.taskStore(taskID).GetTask(bgCtx, taskID)
	if getErr != nil || cur.CurrentRefinement == nil {
		return
	}
	resultStr, _ := res.Parsed.(string)
	cleaned := cleanRefinementResult(resultStr)
	_, spec := extractGoalFromRefinement(cleaned)
	cur.CurrentRefinement.Status = store.RefinementJobStatusDone
	cur.CurrentRefinement.Result = spec
	_ = r.taskStore(taskID).UpdateRefinementJob(bgCtx, taskID, cur.CurrentRefinement)

	logger.Runner.Info("refinement complete", "task", taskID)
}

// buildRefinementPrompt constructs the refinement agent prompt from the task's
// metadata, user instructions, and the current date. The task's age in days is
// included so the agent can gauge urgency.
func (r *Runner) buildRefinementPrompt(task *store.Task, userInstructions string, now time.Time) string {
	const dateLayout = "2006-01-02"
	ageDays := int(now.Sub(task.CreatedAt).Hours() / 24)
	if ageDays < 0 {
		ageDays = 0
	}
	return r.promptsMgr.Refinement(prompts.RefinementData{
		CreatedAt:        task.CreatedAt.Format(dateLayout),
		Today:            now.Format(dateLayout),
		AgeDays:          ageDays,
		Status:           string(task.Status),
		Prompt:           task.Prompt,
		UserInstructions: strings.TrimSpace(userInstructions),
	})
}

// cleanRefinementResult strips any agent preamble (internal monologue,
// separator lines) that appears before the actual spec content.
// It looks for the first top-level markdown heading ("# ") and returns
// everything from that point; if none is found, the original text is returned.
func cleanRefinementResult(result string) string {
	// Check if the result starts directly with a heading.
	if strings.HasPrefix(result, "# ") {
		return result
	}
	// Find the first occurrence of a top-level heading on its own line.
	if idx := strings.Index(result, "\n# "); idx != -1 {
		return strings.TrimSpace(result[idx:])
	}
	return result
}

// extractGoalFromRefinement splits a cleaned refinement result into a goal
// summary and the remaining implementation spec. It looks for a "# Goal"
// section at the start; everything between that heading and the next top-level
// heading is the goal. If no "# Goal" section is found, goal is empty and
// spec is the full text.
func extractGoalFromRefinement(result string) (goal, spec string) {
	const goalHeading = "# Goal"
	if !strings.HasPrefix(result, goalHeading) {
		return "", result
	}
	rest := result[len(goalHeading):]
	// Find the next heading (H1 or H2) after "# Goal".
	// The spec may start with "## Objective" (no wrapping H1).
	nextHeading := -1
	for _, marker := range []string{"\n# ", "\n## "} {
		if idx := strings.Index(rest, marker); idx != -1 && (nextHeading == -1 || idx < nextHeading) {
			nextHeading = idx
		}
	}
	if nextHeading == -1 {
		// No further heading — entire text is goal (unusual but safe).
		return strings.TrimSpace(rest), ""
	}
	goal = strings.TrimSpace(rest[:nextHeading])
	spec = strings.TrimSpace(rest[nextHeading+1:]) // skip the leading newline
	return goal, spec
}
