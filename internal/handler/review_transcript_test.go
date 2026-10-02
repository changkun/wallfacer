package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/review"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
)

// getReviewTranscript serves the transcript for id and returns the status and
// the raw body.
func getReviewTranscript(t *testing.T, h *Handler, id uuid.UUID) (int, []byte) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ReviewTranscript(w, httptest.NewRequest(http.MethodGet, "/api/tasks/"+id.String()+"/review/transcript", nil), id)
	return w.Code, w.Body.Bytes()
}

// fetchTranscript serves the transcript for id and decodes the 200 body.
func fetchTranscript(t *testing.T, h *Handler, id uuid.UUID) reviewTranscriptResp {
	t.Helper()
	code, body := getReviewTranscript(t, h, id)
	return decodeTranscript(t, code, body)
}

// decodeTranscript decodes a 200 transcript body.
func decodeTranscript(t *testing.T, code int, body []byte) reviewTranscriptResp {
	t.Helper()
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", code, body)
	}
	var resp reviewTranscriptResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

// TestReviewTranscript_ReturnsRounds runs a two-round session through the real
// review loop and reads it back: each round carries the reviewer's answer, the
// feedback the findings went out in and the task's reply, and the ended
// session carries its outcome and the configuration it ran under.
func TestReviewTranscript_ReturnsRounds(t *testing.T) {
	answers := []*runner.ReviewerResult{changes(400, findingHigh, findingLow), approve()}
	h, s, _ := reviewLoopHandler(t, "WALLFACER_REVIEW_MODEL=reviewer-model\n", func(in runner.ReviewerInput) (*runner.ReviewerResult, error) {
		return answers[in.Round-1], nil
	})
	ctx := context.Background()
	task := waitingTaskForReview(t, s, "")

	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("round 1: %v", err)
	}
	// Mid-session, while a round runs: running, one round, no outcome yet.
	if !h.beginReview(task.ID) {
		t.Fatal("beginReview should reserve the slot")
	}
	mid := fetchTranscript(t, h, task.ID)
	if !mid.Running || len(mid.Rounds) != 1 || mid.Outcome != nil {
		t.Errorf("mid-session = running %v, %d rounds, outcome %+v; want running, 1 round, no outcome", mid.Running, len(mid.Rounds), mid.Outcome)
	}
	h.endReview(task.ID)

	task = finishTurn(t, s, task.ID, "fixed the nil map")
	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("round 2: %v", err)
	}

	resp := fetchTranscript(t, h, task.ID)
	if resp.Running || resp.Legacy || resp.Truncated {
		t.Errorf("flags = running %v legacy %v truncated %v, want all false", resp.Running, resp.Legacy, resp.Truncated)
	}
	if c := resp.Config; c == nil || c.MaxRounds != 3 || c.CostCap != 50000 || c.ReviewerModel != "reviewer-model" {
		t.Errorf("config = %+v, want 3 rounds, 50000 tokens, reviewer-model", resp.Config)
	}
	if len(resp.Rounds) != 2 {
		t.Fatalf("rounds = %d, want 2: %+v", len(resp.Rounds), resp.Rounds)
	}
	r1, r2 := resp.Rounds[0], resp.Rounds[1]
	if r1.Round != 1 || r1.Reviewer == nil || r1.Reviewer.Verdict != runner.ReviewChangesRequested || len(r1.Reviewer.Findings) != 2 {
		t.Errorf("round 1 reviewer = %+v", r1.Reviewer)
	}
	if !strings.Contains(r1.Feedback, findingHigh.Claim) || r1.Reply != "fixed the nil map" {
		t.Errorf("round 1 feedback %q, reply %q", r1.Feedback, r1.Reply)
	}
	if r2.Round != 2 || r2.Reviewer == nil || r2.Reviewer.Verdict != runner.ReviewApprove || r2.Feedback != "" {
		t.Errorf("round 2 = %+v", r2)
	}
	if o := resp.Outcome; o == nil || o.Termination != review.TerminationApproved || o.Rounds != 2 || o.Unresolved != 0 || o.Tokens != 400 {
		t.Errorf("outcome = %+v, want approved after 2 rounds, 400 tokens", resp.Outcome)
	}
}

func TestReviewTranscript_404WhenNoSession(t *testing.T) {
	h := newTestHandler(t)
	s, _ := h.currentStore()
	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "p", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if code, _ := getReviewTranscript(t, h, task.ID); code != http.StatusNotFound {
		t.Errorf("expected 404 with no review session, got %d", code)
	}
}

// writeLegacySession lays down a session the earlier debate engine wrote:
// forks of critic and proposer rounds in markdown files, and an end.json of
// its own shape.
func writeLegacySession(t *testing.T, stateDir, id string) {
	t.Helper()
	sessionDir := filepath.Join(stateDir, "sessions", id)
	rounds := filepath.Join(sessionDir, "forks", "critic-1", "rounds")
	if err := os.MkdirAll(rounds, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(rounds, "r1-critic.md"):         "## attack\nnil deref in foo",
		filepath.Join(sessionDir, "transcript.jsonl"): `{"ts":"2026-06-27T00:00:01Z","fork":1,"round":1,"role":"critic","path":"forks/critic-1/rounds/r1-critic.md","ms":10}` + "\n",
		filepath.Join(sessionDir, "end.json"):         `{"termination":{"reason":"steady_state"},"stats":{"total_attacks":3,"tokens_used":4200}}`,
	}
	for path, body := range files {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestReviewTranscript_LegacySessionDegrades proves a session the earlier
// engine left on disk is served as legacy with no rounds, rather than failing
// the route or being misread as rounds of the current review.
func TestReviewTranscript_LegacySessionDegrades(t *testing.T) {
	h, s, _ := reviewLoopHandler(t, "", func(runner.ReviewerInput) (*runner.ReviewerResult, error) { return approve(), nil })
	task := waitingTaskForReview(t, s, "")
	writeLegacySession(t, reviewStateDir(primaryWorktree(task.WorktreePaths)), "sess-01")

	code, body := getReviewTranscript(t, h, task.ID)
	resp := decodeTranscript(t, code, body)
	if !resp.Legacy || resp.SessionID != "sess-01" || len(resp.Rounds) != 0 || resp.Outcome != nil {
		t.Errorf("legacy session = %+v, want legacy, its id, no rounds, no outcome", resp)
	}
	if !strings.Contains(string(body), `"rounds":[]`) {
		t.Errorf("body %s: rounds must be an empty array, not null", body)
	}

	// The next review starts its own session, and the route reads that one.
	if err := h.runReview(context.Background(), s, task); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	resp = fetchTranscript(t, h, task.ID)
	if resp.Legacy || len(resp.Rounds) != 1 {
		t.Errorf("after a new review = %+v, want the new session's round", resp)
	}
}

// TestReviewTranscript_SkipAndFailedAttempts proves a skipped review reports
// its reason as the outcome, and a failed attempt shows in its round with the
// fixed sentence and the reviewer's raw answer.
func TestReviewTranscript_SkipAndFailedAttempts(t *testing.T) {
	t.Run("skip", func(t *testing.T) {
		h, s, _ := reviewLoopHandler(t, "", func(runner.ReviewerInput) (*runner.ReviewerResult, error) {
			return nil, &runner.ReviewRefusal{Code: runner.ReviewRefusedModelUnset, Detail: "WALLFACER_REVIEW_MODEL is empty"}
		})
		task := waitingTaskForReview(t, s, "")
		if err := h.runReview(context.Background(), s, task); err != nil {
			t.Fatalf("runReview: %v", err)
		}
		resp := fetchTranscript(t, h, task.ID)
		o := resp.Outcome
		if o == nil || o.Termination != review.TerminationSkipped || o.Skip == nil || o.Skip.Code != runner.ReviewRefusedModelUnset || o.Skip.Message == "" {
			t.Errorf("outcome = %+v, want the skip with its code and sentence", o)
		}
	})

	t.Run("failed attempt", func(t *testing.T) {
		h, s, _ := reviewLoopHandler(t, "", func(runner.ReviewerInput) (*runner.ReviewerResult, error) {
			return nil, fmt.Errorf("review: %w", &runner.ReviewOutputError{Raw: "it looks fine", Reason: "no JSON object"})
		})
		task := waitingTaskForReview(t, s, "")
		if err := h.runReview(context.Background(), s, task); err == nil {
			t.Fatal("runReview returned nil for a failed round")
		}
		resp := fetchTranscript(t, h, task.ID)
		if len(resp.Rounds) != 1 || len(resp.Rounds[0].FailedAttempts) != 1 || resp.Rounds[0].Reviewer != nil {
			t.Fatalf("rounds = %+v, want round 1 with one failed attempt and no answer", resp.Rounds)
		}
		a := resp.Rounds[0].FailedAttempts[0]
		if a.Code != review.CodeOutputInvalid || a.Raw != "it looks fine" || !strings.Contains(a.Message, "could not be read") {
			t.Errorf("failed attempt = %+v", a)
		}
		if resp.Outcome != nil {
			t.Error("a failed attempt leaves the session open; there is no outcome yet")
		}
	})
}

// TestReviewTranscript_TruncatedIsReported covers a transcript with a record
// longer than the read cap: the rounds before it are served and the response
// says the transcript is incomplete.
func TestReviewTranscript_TruncatedIsReported(t *testing.T) {
	h, s, _ := reviewLoopHandler(t, "", func(runner.ReviewerInput) (*runner.ReviewerResult, error) {
		return changes(10, findingHigh), nil
	})
	task := waitingTaskForReview(t, s, "")
	if err := h.runReview(context.Background(), s, task); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	sess, found, err := review.Newest(reviewStateDir(primaryWorktree(task.WorktreePaths)))
	if err != nil || !found {
		t.Fatalf("Newest: %v %v", found, err)
	}
	f, err := os.OpenFile(filepath.Join(sess.Dir, "transcript.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	overlong := `{"round":1,"role":"implementer","body":"` + strings.Repeat("a", 9<<20) + "\"}\n"
	if _, err := f.WriteString(overlong + `{"round":2,"role":"reviewer","verdict":"approve"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	code, body := getReviewTranscript(t, h, task.ID)
	resp := decodeTranscript(t, code, body)
	if !resp.Truncated {
		t.Error("truncated = false, want true: the round 2 record follows the overlong line")
	}
	if len(resp.Rounds) != 1 || resp.Rounds[0].Reviewer == nil {
		t.Errorf("rounds = %+v, want the round read before the overlong line", resp.Rounds)
	}

	// A transcript read to its end carries no truncated field.
	h2, s2, _ := reviewLoopHandler(t, "", func(runner.ReviewerInput) (*runner.ReviewerResult, error) { return approve(), nil })
	task2 := waitingTaskForReview(t, s2, "")
	if err := h2.runReview(context.Background(), s2, task2); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if _, body := getReviewTranscript(t, h2, task2.ID); strings.Contains(string(body), `"truncated"`) {
		t.Errorf("complete transcript body %s carries a truncated field", body)
	}
}
