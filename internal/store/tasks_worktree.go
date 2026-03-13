package store

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ErrRefinementAlreadyRunning is returned by StartRefinementJobIfIdle when a
// refinement job is already in "running" state for the given task.
var ErrRefinementAlreadyRunning = errors.New("refinement already running")

const refinementRecentCompleteWindow = 500 * time.Millisecond

// ListTasks returns all tasks sorted by position then creation time.

func (s *Store) UpdateTaskWorktrees(_ context.Context, id uuid.UUID, worktreePaths map[string]string, branchName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.WorktreePaths = worktreePaths
	t.BranchName = branchName
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: worktree paths and branch name are not search-indexed fields.
	s.notify(t, false)
	return nil
}

// UpdateTaskCommitHashes stores the post-merge commit hash per repo path.
func (s *Store) UpdateTaskCommitHashes(_ context.Context, id uuid.UUID, hashes map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.CommitHashes = hashes
	t.UpdatedAt = time.Now()
	// Search index not updated: commit hashes are not search-indexed fields.
	return s.saveTask(id, t)
}

// UpdateTaskTestRun sets the IsTestRun flag and LastTestResult on a task atomically.
// Call with isTestRun=true and empty lastTestResult to mark the start of a test run;
// call with isTestRun=false and a verdict ("pass"/"fail"/"") when the test completes.
func (s *Store) UpdateTaskTestRun(_ context.Context, id uuid.UUID, isTestRun bool, lastTestResult string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.IsTestRun = isTestRun
	t.LastTestResult = lastTestResult
	if isTestRun || lastTestResult != "fail" {
		t.PendingTestFeedback = ""
	}
	if isTestRun {
		// Record the current turn count so we know which turn files belong to
		// the implementation phase vs the test phase.
		t.TestRunStartTurn = t.Turns
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: test-run state and result are not search-indexed fields.
	s.notify(t, false)
	return nil
}

// UpdateTaskPendingTestFeedback stores or clears the pending feedback message
// generated from the latest failed test run.
func (s *Store) UpdateTaskPendingTestFeedback(_ context.Context, id uuid.UUID, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.PendingTestFeedback = message
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify(t, false)
	return nil
}

// SetTaskFailureCategory sets the failure_category field on a task.
// It is called immediately after a TaskStatusFailed transition to record
// the machine-readable root cause. The field is persisted atomically so
// the UI can display and filter by it.
func (s *Store) SetTaskFailureCategory(_ context.Context, id uuid.UUID, cat FailureCategory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.FailureCategory = cat
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: failure category is not a search-indexed field.
	s.notify(t, false)
	return nil
}

// UpdateTaskBaseCommitHashes stores the default-branch HEAD captured before merge.
func (s *Store) UpdateTaskBaseCommitHashes(_ context.Context, id uuid.UUID, hashes map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.BaseCommitHashes = hashes
	t.UpdatedAt = time.Now()
	// Search index not updated: base commit hashes are not search-indexed fields.
	return s.saveTask(id, t)
}

// UpdateRefinementJob persists the current refinement job state.
// Pass nil to clear the active refinement job.
func (s *Store) UpdateRefinementJob(_ context.Context, id uuid.UUID, job *RefinementJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if job != nil {
		jobCopy := *job
		t.CurrentRefinement = &jobCopy
	} else {
		t.CurrentRefinement = nil
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: refinement job state is not a search-indexed field.
	s.notify(t, false)
	return nil
}

// StartRefinementJobIfIdle atomically checks that no refinement is currently
// running for the task and, if so, persists the new job. Returns
// ErrRefinementAlreadyRunning without modifying the store when the existing
// CurrentRefinement.Status == "running". If the existing job completed very
// recently and recorded an error or output, it is also treated as still
// in-flight to avoid concurrent duplicate starts during fast failure races.
// The guard uses task.UpdatedAt so a just-completed runner job does not
// immediately become eligible for a second start in a tight race.
func (s *Store) StartRefinementJobIfIdle(_ context.Context, id uuid.UUID, job *RefinementJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if t.CurrentRefinement != nil {
		status := t.CurrentRefinement.Status
		if status == "running" {
			return ErrRefinementAlreadyRunning
		}
		if t.CurrentRefinement.Source == "runner" && (status == "failed" || status == "done") {
			elapsed := time.Since(t.UpdatedAt)
			if elapsed >= 0 && elapsed < refinementRecentCompleteWindow && (t.CurrentRefinement.Error != "" || t.CurrentRefinement.Result != "") {
				return ErrRefinementAlreadyRunning
			}
		}
	}
	jobCopy := *job
	t.CurrentRefinement = &jobCopy
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: refinement job state is not a search-indexed field.
	s.notify(t, false)
	return nil
}

// ApplyRefinement saves a refinement session and updates the task prompt.
// The current prompt is pushed into PromptHistory before being replaced.
// The CurrentRefinement job is cleared after applying.
func (s *Store) ApplyRefinement(_ context.Context, id uuid.UUID, newPrompt string, session RefinementSession) error {
	// Compute the lowercased prompt before acquiring the lock so that
	// strings.ToLower does not extend the critical section.
	loweredPrompt := strings.ToLower(newPrompt)

	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	session.ResultPrompt = newPrompt
	t.PromptHistory = append(t.PromptHistory, t.Prompt)
	t.RefineSessions = append(t.RefineSessions, session)
	t.Prompt = newPrompt
	t.CurrentRefinement = nil
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	if entry, ok := s.searchIndex[id]; ok {
		entry.prompt = loweredPrompt
		s.searchIndex[id] = entry
	}
	s.notify(t, false)
	return nil
}

// DismissRefinement clears the current refinement job without changing the prompt.
// Used when the user chooses not to apply the refined prompt.
func (s *Store) DismissRefinement(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.CurrentRefinement = nil
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: clearing the refinement job does not affect
	// the prompt or any other search-indexed field.
	s.notify(t, false)
	return nil
}

const maxSearchResults = 50
const snippetPadding = 60

// SearchTasks performs a case-insensitive substring search across title, prompt,
// tags (joined), and oversight summary text. Search order favours the cheapest
// fields first. Each task produces at most one result (first matching field).
// Results are capped at maxSearchResults. Archived tasks are included.
//
// All matching is done against the in-memory search index (pre-lowercased text
// built at startup and kept in sync with mutations), so no per-query disk I/O
// is required.
func (s *Store) SearchTasks(_ context.Context, query string) ([]TaskSearchResult, error) {
	q := strings.ToLower(query)

	// Match against the in-memory index under a single RLock, then clone only
	// the matched tasks after releasing the lock.
	type matchResult struct {
		id      uuid.UUID
		field   string
		snippet string
	}

	s.mu.RLock()
	matches := make([]matchResult, 0)
	for id, t := range s.tasks {
		if len(matches) >= maxSearchResults {
			break
		}
		if field, snippet, ok := matchTask(t, s.searchIndex[id], q); ok {
			matches = append(matches, matchResult{id: id, field: field, snippet: snippet})
		}
	}
	s.mu.RUnlock()

	results := make([]TaskSearchResult, 0, len(matches))
	for _, m := range matches {
		t, err := s.GetTask(context.Background(), m.id)
		if err != nil {
			continue
		}
		results = append(results, TaskSearchResult{
			Task:         t,
			MatchedField: m.field,
			Snippet:      m.snippet,
		})
	}
	return results, nil
}

// matchTask checks each field in cheapest-first order using pre-lowercased index
// entries. Returns (field, snippet, true) on the first match, or ("", "", false).
// Snippet text is taken from the original (non-lowercased) task fields so that
// the UI output is unchanged.
func matchTask(t *Task, entry indexedTaskText, q string) (field, snippet string, ok bool) {
	if idx := strings.Index(entry.title, q); idx != -1 {
		return "title", buildSnippet(t.Title, idx, len(q)), true
	}
	if idx := strings.Index(entry.prompt, q); idx != -1 {
		return "prompt", buildSnippet(t.Prompt, idx, len(q)), true
	}
	if idx := strings.Index(entry.tags, q); idx != -1 {
		return "tags", buildSnippet(strings.Join(t.Tags, " "), idx, len(q)), true
	}
	if entry.oversight != "" {
		if idx := strings.Index(entry.oversight, q); idx != -1 {
			return "oversight", buildSnippet(entry.oversightRaw, idx, len(q)), true
		}
	}
	return "", "", false
}

// buildSnippet returns an HTML-escaped substring of src centred on the match at
// [idx, idx+matchLen) with up to snippetPadding bytes of context on each side.
// Truncation points are adjusted to UTF-8 rune boundaries, and ellipsis markers
// are prepended/appended when the window is shorter than src.
func buildSnippet(src string, idx, matchLen int) string {
	start := idx - snippetPadding
	prefix := "…"
	if start <= 0 {
		start = 0
		prefix = ""
	}
	end := idx + matchLen + snippetPadding
	suffix := "…"
	if end >= len(src) {
		end = len(src)
		suffix = ""
	}
	// Align to UTF-8 rune boundaries.
	for start > 0 && !utf8.RuneStart(src[start]) {
		start--
	}
	for end < len(src) && !utf8.RuneStart(src[end]) {
		end++
	}
	return html.EscapeString(prefix + src[start:end] + suffix)
}

// MarkTurnTruncated appends turn to the task's TruncatedTurns list, recording
// that the output file for that turn was truncated by the server-side size
// budget. It is called by SaveTurnOutput when truncation occurs.
func (s *Store) MarkTurnTruncated(_ context.Context, taskID uuid.UUID, turn int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}

	task.TruncatedTurns = append(task.TruncatedTurns, turn)
	return s.saveTask(taskID, task)
}

// IncrementAutoRetryCount records one auto-retry attempt for the given
// FailureCategory: it increments AutoRetryCount and decrements the per-category
// budget in AutoRetryBudget (flooring at zero).
func (s *Store) IncrementAutoRetryCount(_ context.Context, id uuid.UUID, category FailureCategory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.AutoRetryCount++
	if t.AutoRetryBudget == nil {
		t.AutoRetryBudget = make(map[FailureCategory]int)
	}
	if t.AutoRetryBudget[category] > 0 {
		t.AutoRetryBudget[category]--
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify(t, false)
	return nil
}

// clampTimeout ensures timeout stays in [1, 1440] minutes with a default of 60.
func clampTimeout(v int) int {
	if v <= 0 {
		return 60
	}
	if v > 1440 {
		return 1440
	}
	return v
}
