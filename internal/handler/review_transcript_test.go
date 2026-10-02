package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/store"
)

// writeReviewSession lays down a synthetic review session dir under the task's
// worktree .review, mirroring what review writes incrementally during a run.
func writeReviewSession(t *testing.T, worktree, sessionID string, withEnd bool) {
	t.Helper()
	stateDir := reviewStateDir(worktree) // <parent>/.review
	rounds := filepath.Join(stateDir, "sessions", sessionID, "forks", "critic-1", "rounds")
	if err := os.MkdirAll(rounds, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(stateDir, "sessions", sessionID)
	if err := os.WriteFile(filepath.Join(rounds, "r1-critic.md"), []byte("## attack\nnil deref in foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rounds, "r2-proposer.md"), []byte("rebuttal: guarded above"), 0o644); err != nil {
		t.Fatal(err)
	}
	transcript := `{"ts":"2026-06-27T00:00:01Z","fork":1,"round":1,"role":"critic","path":"forks/critic-1/rounds/r1-critic.md","ms":10}
{"ts":"2026-06-27T00:00:02Z","fork":1,"round":2,"role":"proposer","path":"forks/critic-1/rounds/r2-proposer.md","ms":12}
`
	if err := os.WriteFile(filepath.Join(sessionDir, "transcript.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}
	if withEnd {
		end := `{"termination":{"reason":"steady_state"},"stats":{"total_attacks":3,"by_status":{"conceded":2,"open":1},"tokens_used":4200,"wall_seconds":92}}`
		if err := os.WriteFile(filepath.Join(sessionDir, "end.json"), []byte(end), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReviewTranscript_ReturnsForkRounds(t *testing.T) {
	h := newTestHandler(t)
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "p", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	worktree := filepath.Join(t.TempDir(), "wt", "repo")
	if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{filepath.Dir(worktree): worktree}, "branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}
	writeReviewSession(t, worktree, "sess-01", false /* no end.json yet */)

	// While the run is in flight (reserved slot), running=true.
	if !h.beginReview(task.ID) {
		t.Fatal("beginReview should reserve the slot")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/review/transcript", nil)
	w := httptest.NewRecorder()
	h.ReviewTranscript(w, req, task.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp reviewTranscriptResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Running {
		t.Error("expected running=true while the in-flight slot is held")
	}
	if resp.Config == nil || resp.Config.Forks != reviewForkCount || resp.Config.MaxRounds != reviewMaxRounds {
		t.Errorf("config = %+v, want forks=%d rounds=%d", resp.Config, reviewForkCount, reviewMaxRounds)
	}
	if len(resp.Config.CriticModels) == 0 || resp.Config.ProposerModel == "" {
		t.Errorf("config models missing: %+v", resp.Config)
	}
	if len(resp.Forks) != 1 || len(resp.Forks[0].Rounds) != 2 {
		t.Fatalf("forks/rounds = %+v, want 1 fork with 2 rounds", resp.Forks)
	}
	r1, r2 := resp.Forks[0].Rounds[0], resp.Forks[0].Rounds[1]
	if r1.Role != "critic" || r1.Round != 1 || r1.Body == "" {
		t.Errorf("round1 = %+v, want critic R1 with body", r1)
	}
	if r2.Role != "proposer" || r2.Round != 2 || r2.Body != "rebuttal: guarded above" {
		t.Errorf("round2 = %+v, want proposer R2 with body", r2)
	}

	// Run finishes: release the slot and write end.json → running=false + outcome.
	h.endReview(task.ID)
	writeReviewSession(t, worktree, "sess-01", true)
	w2 := httptest.NewRecorder()
	h.ReviewTranscript(w2, httptest.NewRequest(http.MethodGet, "/x", nil), task.ID)
	var resp2 reviewTranscriptResp
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode 2: %v", err)
	}
	if resp2.Running {
		t.Error("expected running=false after the slot is released")
	}
	if resp2.Outcome == nil || resp2.Outcome.Termination != "steady_state" || resp2.Outcome.TotalAttacks != 3 {
		t.Errorf("outcome = %+v, want steady_state with 3 attacks", resp2.Outcome)
	}
	if resp2.Outcome.WallSeconds != 92 || resp2.Outcome.ByStatus["conceded"] != 2 {
		t.Errorf("outcome stats = %+v", resp2.Outcome)
	}
}

func TestReviewTranscript_404WhenNoSession(t *testing.T) {
	h := newTestHandler(t)
	s, _ := h.currentStore()
	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "p", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	h.ReviewTranscript(w, req, task.ID)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 with no review session, got %d", w.Code)
	}
}

// reviewTaskWithSession creates a task with a worktree that holds one review
// session (two rounds in fork 1) and returns the task ID and the session dir.
func reviewTaskWithSession(t *testing.T, h *Handler) (uuid.UUID, string) {
	t.Helper()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "p", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	worktree := filepath.Join(t.TempDir(), "wt", "repo")
	if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{filepath.Dir(worktree): worktree}, "branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}
	writeReviewSession(t, worktree, "sess-01", false)
	return task.ID, filepath.Join(reviewStateDir(worktree), "sessions", "sess-01")
}

// getReviewTranscript serves the transcript for id and returns the decoded
// body. The body is decoded as a map so the test reads the wire field names.
func getReviewTranscript(t *testing.T, h *Handler, id uuid.UUID) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	h.ReviewTranscript(w, httptest.NewRequest(http.MethodGet, "/api/tasks/"+id.String()+"/review/transcript", nil), id)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

// roundCount is the number of rounds across the forks of a transcript body.
func roundCount(t *testing.T, resp map[string]any) int {
	t.Helper()
	n := 0
	forks, _ := resp["forks"].([]any)
	for _, f := range forks {
		fork, ok := f.(map[string]any)
		if !ok {
			t.Fatalf("fork = %v, want an object", f)
		}
		rounds, _ := fork["rounds"].([]any)
		n += len(rounds)
	}
	return n
}

// TestReviewTranscript_LineOverCapIsReportedTruncated covers a transcript with
// a record longer than the scanner's line cap. The scanner stops there, so the
// rounds after it are never read; the response has to say the transcript is
// incomplete instead of presenting the rounds before the line as the whole
// debate.
func TestReviewTranscript_LineOverCapIsReportedTruncated(t *testing.T) {
	h := newTestHandler(t)
	id, sessionDir := reviewTaskWithSession(t, h)

	f, err := os.OpenFile(filepath.Join(sessionDir, "transcript.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	overlong := `{"ts":"2026-06-27T00:00:03Z","fork":1,"round":3,"role":"critic","path":"` +
		strings.Repeat("a", maxReviewTranscriptLineBytes+1) + `"}` + "\n"
	after := `{"ts":"2026-06-27T00:00:04Z","fork":1,"round":4,"role":"proposer","path":"forks/critic-1/rounds/r2-proposer.md","ms":9}` + "\n"
	if _, err := f.WriteString(overlong + after); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	resp := getReviewTranscript(t, h, id)
	if got := roundCount(t, resp); got != 2 {
		t.Errorf("rounds = %d, want the 2 read before the overlong line", got)
	}
	if resp["truncated"] != true {
		t.Errorf("truncated = %v, want true: round 4 follows the overlong line and was not read", resp["truncated"])
	}
}

// TestReviewTranscript_CompleteIsNotTruncated pins the wire shape of the common
// case: a transcript read to its end carries no truncated field.
func TestReviewTranscript_CompleteIsNotTruncated(t *testing.T) {
	h := newTestHandler(t)
	id, _ := reviewTaskWithSession(t, h)

	resp := getReviewTranscript(t, h, id)
	if got := roundCount(t, resp); got != 2 {
		t.Errorf("rounds = %d, want 2", got)
	}
	if v, present := resp["truncated"]; present {
		t.Errorf("truncated = %v, want the field absent", v)
	}
}

// TestReadReviewTranscript_ReadErrorIsReturned covers a read that fails for a
// reason other than the line cap: the error comes back with the path, and a
// transcript that does not exist yet is not an error.
func TestReadReviewTranscript_ReadErrorIsReturned(t *testing.T) {
	missing := t.TempDir()
	if forks, err := readReviewTranscript(missing); err != nil || forks != nil {
		t.Errorf("no transcript yet: forks = %v, err = %v, want nil, nil", forks, err)
	}

	// A directory opens but cannot be read as a file.
	unreadable := t.TempDir()
	if err := os.Mkdir(filepath.Join(unreadable, "transcript.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := readReviewTranscript(unreadable)
	if err == nil {
		t.Fatal("err = nil, want the read error")
	}
	if !strings.Contains(err.Error(), filepath.Join(unreadable, "transcript.jsonl")) {
		t.Errorf("err = %v, want it to name the transcript path", err)
	}
}
