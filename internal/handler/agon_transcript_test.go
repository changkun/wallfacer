package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"latere.ai/x/wallfacer/internal/store"
)

// writeAgonSession lays down a synthetic agon session dir under the task's
// worktree .agon, mirroring what agon writes incrementally during a run.
func writeAgonSession(t *testing.T, worktree, sessionID string, withEnd bool) {
	t.Helper()
	stateDir := agonStateDir(worktree) // <parent>/.agon
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

func TestAgonTranscript_ReturnsForkRounds(t *testing.T) {
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
	writeAgonSession(t, worktree, "sess-01", false /* no end.json yet */)

	// While the run is in flight (reserved slot), running=true.
	if !h.beginAgon(task.ID) {
		t.Fatal("beginAgon should reserve the slot")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/agon/transcript", nil)
	w := httptest.NewRecorder()
	h.AgonTranscript(w, req, task.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp agonTranscriptResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Running {
		t.Error("expected running=true while the in-flight slot is held")
	}
	if resp.Config == nil || resp.Config.Forks != agonForkCount || resp.Config.MaxRounds != agonMaxRounds {
		t.Errorf("config = %+v, want forks=%d rounds=%d", resp.Config, agonForkCount, agonMaxRounds)
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
	h.endAgon(task.ID)
	writeAgonSession(t, worktree, "sess-01", true)
	w2 := httptest.NewRecorder()
	h.AgonTranscript(w2, httptest.NewRequest(http.MethodGet, "/x", nil), task.ID)
	var resp2 agonTranscriptResp
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

func TestAgonTranscript_404WhenNoSession(t *testing.T) {
	h := newTestHandler(t)
	s, _ := h.currentStore()
	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "p", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	h.AgonTranscript(w, req, task.ID)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 with no agon session, got %d", w.Code)
	}
}
