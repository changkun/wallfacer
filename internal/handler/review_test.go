package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/review"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
)

// ─────────────────────────────────────────────────────────────────────────────
// ReviewEnabled / SetReview toggle
// ─────────────────────────────────────────────────────────────────────────────

func TestReviewEnabled_DefaultsFalse(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	if h.ReviewEnabled() {
		t.Error("ReviewEnabled() should default to false")
	}
}

func TestSetReview_EnablesAndDisables(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.SetReview(true)
	if !h.ReviewEnabled() {
		t.Error("ReviewEnabled() should be true after SetReview(true)")
	}
	h.SetReview(false)
	if h.ReviewEnabled() {
		t.Error("ReviewEnabled() should be false after SetReview(false)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// tryAutoReview short-circuit paths
// ─────────────────────────────────────────────────────────────────────────────

// mockVerifier records Verify calls and returns a fixed result.
type mockVerifier struct {
	mu     sync.Mutex
	called int
	lastIn review.Input
	result *review.Result
	err    error
}

func (v *mockVerifier) Verify(_ context.Context, in review.Input) (*review.Result, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.called++
	v.lastIn = in
	if in.OnStart != nil && v.err == nil && v.result != nil && v.result.Outcome != review.OutcomeSkipped {
		in.OnStart(1, max(in.MaxRounds, 1))
	}
	return v.result, v.err
}

func (v *mockVerifier) calls() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.called
}

// finished is a mock result for a session that ended with unresolved open
// findings (0 means approved).
func finished(unresolved int, headline string) *review.Result {
	termination := review.TerminationMaxRounds
	if unresolved == 0 {
		termination = review.TerminationApproved
	}
	return &review.Result{Outcome: review.OutcomeFinished, Termination: termination, Unresolved: unresolved, Headline: headline, SessionDir: "/sessions/1"}
}

func TestTryAutoReview_SkipsWhenDisabled(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: finished(0, "")}
	h.verifier = v
	// ReviewEnabled defaults to false — tryAutoReview must not call verifier.
	h.tryAutoReview(context.Background())
	if v.calls() != 0 {
		t.Errorf("verifier called %d times when review disabled, want 0", v.calls())
	}
}

func TestTryAutoReview_SkipsTaskWithoutWorktree(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: finished(0, "")}
	h.verifier = v
	h.SetReview(true)

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "no-worktree", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	// No worktree → nothing to review → verifier not called.
	h.tryAutoReview(ctx)
	time.Sleep(50 * time.Millisecond)
	if v.calls() != 0 {
		t.Errorf("verifier called %d times for task without worktree, want 0", v.calls())
	}
}

func TestTryAutoReview_SkipsTaskWithReviewAlreadyRun(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: finished(0, "")}
	h.verifier = v
	h.SetReview(true)

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskForReview(t, s, "")
	if err := s.UpdateTaskReview(ctx, task.ID, 0, "", ""); err != nil {
		t.Fatalf("UpdateTaskReview: %v", err)
	}

	h.tryAutoReview(ctx)
	time.Sleep(50 * time.Millisecond)
	if v.calls() != 0 {
		t.Errorf("verifier called %d times for already-run task, want 0", v.calls())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// State dir placement + deterministic cwd
// ─────────────────────────────────────────────────────────────────────────────

func TestReviewStateDir_OutsideWorktree(t *testing.T) {
	// Build paths with filepath.Join so the expectations use the OS separator
	// (reviewStateDir goes through filepath, so a hardcoded "/" fails on Windows).
	wt := filepath.Join("/data", "worktrees", "abc123", "myrepo")
	got := reviewStateDir(wt)
	want := filepath.Join("/data", "worktrees", "abc123", ".review")
	if got != want {
		t.Errorf("reviewStateDir = %q, want %q", got, want)
	}
	// The state dir must not live inside the worktree, or git add -A would
	// stage it and generateWorktreeDiff would surface it as task changes.
	if strings.HasPrefix(got, wt+string(filepath.Separator)) {
		t.Errorf("reviewStateDir %q is inside the worktree %q", got, wt)
	}
	if reviewStateDir("") != "" {
		t.Error("reviewStateDir(\"\") should return \"\"")
	}
}

func TestPrimaryWorktree_Deterministic(t *testing.T) {
	m := map[string]string{
		"repoB": "/wt/zeta",
		"repoA": "/wt/alpha",
		"repoC": "/wt/mid",
	}
	// Run several times: map iteration is randomized, the result must not be.
	for range 8 {
		if got := primaryWorktree(m); got != "/wt/alpha" {
			t.Fatalf("primaryWorktree = %q, want /wt/alpha (deterministic)", got)
		}
	}
	if primaryWorktree(map[string]string{}) != "" {
		t.Error("primaryWorktree of empty map should return \"\"")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// In-flight dedup + concurrency cap (beginReview / endReview)
// ─────────────────────────────────────────────────────────────────────────────

func TestBeginReview_DedupAndCap(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	id1, id2, id3 := uuid.New(), uuid.New(), uuid.New()

	if !h.beginReview(id1) {
		t.Fatal("first reservation should succeed")
	}
	if h.beginReview(id1) {
		t.Fatal("duplicate reservation for the same task must fail")
	}
	if !h.beginReview(id2) {
		t.Fatal("second distinct task within cap should succeed")
	}
	if h.beginReview(id3) {
		t.Fatal("third task exceeds maxConcurrentReview, reservation must fail")
	}
	h.endReview(id1)
	if !h.beginReview(id3) {
		t.Fatal("after a slot is released, reservation should succeed")
	}
}

// waitingTaskForReview creates a waiting task with a (non-git) worktree path,
// the minimum for runReview to reach the verifier. sessionID is optional: a
// review does not need one.
func waitingTaskForReview(t *testing.T, s *store.Store, sessionID string) store.Task {
	t.Helper()
	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "verify", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := s.UpdateTaskResult(ctx, task.ID, "done", sessionID, "end_turn", 1); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	// The worktree sits in a directory of its own, as <worktreesDir>/<taskID>/
	// does in production, so each task's review state directory (the
	// worktree's sibling) is its own.
	worktree := filepath.Join(t.TempDir(), "repo")
	if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{t.TempDir(): worktree}, "branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}
	fresh, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return *fresh
}

func TestRunReview_PersistsWhenWaiting(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.verifier = &mockVerifier{result: finished(3, "boom")}

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskForReview(t, s, "sess-1")

	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ReviewUnresolved == nil || *got.ReviewUnresolved != 3 {
		t.Errorf("ReviewUnresolved = %v, want 3", got.ReviewUnresolved)
	}
	if got.ReviewHeadline != "boom" {
		t.Errorf("ReviewHeadline = %q, want %q", got.ReviewHeadline, "boom")
	}
	if got.ReviewSessionDir != "/sessions/1" {
		t.Errorf("ReviewSessionDir = %q, want the session dir", got.ReviewSessionDir)
	}
}

// taskEvents returns the task's event payloads of one type as strings.
func taskEvents(t *testing.T, s *store.Store, id uuid.UUID, typ store.EventType) []string {
	t.Helper()
	events, err := s.GetEvents(context.Background(), id)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	var out []string
	for _, e := range events {
		if e.EventType == typ {
			out = append(out, string(e.Data))
		}
	}
	return out
}

// countContaining counts the strings in list that contain sub.
func countContaining(list []string, sub string) int {
	n := 0
	for _, s := range list {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

// TestRunReview_EmitsTimelineEvents proves a round surfaces its start and the
// session's outcome on the task timeline, so a manual or auto trigger is
// visible rather than silently running in the background.
func TestRunReview_EmitsTimelineEvents(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.verifier = &mockVerifier{result: finished(2, "nil deref in foo")}

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskForReview(t, s, "")

	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	system := taskEvents(t, s, task.ID, store.EventTypeSystem)
	if countContaining(system, "round 1 of at most 3 started") != 1 {
		t.Errorf("expected one round-start event, got %v", system)
	}
	if countContaining(system, "2 open finding(s)") != 1 {
		t.Errorf("expected one '2 open finding(s)' completion event, got %v", system)
	}
}

func TestReviewSupersedesTest_Gate(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	withWorktree := &store.Task{WorktreePaths: map[string]string{"/repo": "/wt/repo"}}
	noWorktree := &store.Task{}

	// Review off: never supersedes.
	if h.reviewSupersedesTest(withWorktree) {
		t.Error("review off should not supersede the test agent")
	}
	// Review on + worktree: supersedes, with or without a session.
	h.SetReview(true)
	if !h.reviewSupersedesTest(withWorktree) {
		t.Error("review on + worktree should supersede the test agent")
	}
	// Review on, no worktree: nothing to review; falls back to the test agent.
	if h.reviewSupersedesTest(noWorktree) {
		t.Error("a task without a worktree should fall back to the test agent")
	}
}

// TestRunReview_BlocksOnUnresolved proves open findings at the end of a session
// are a hard barrier: the verdict is persisted, the task stays parked in
// waiting, autoimplement does not auto-resume it, and an approval clears the
// barrier.
func TestRunReview_BlocksOnUnresolved(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.SetAutoimplement(true)
	v := &mockVerifier{result: finished(2, "nil deref")}
	h.verifier = v

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskForReview(t, s, "sess-1")

	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	got, _ := s.GetTask(ctx, task.ID)
	if got.ReviewUnresolved == nil || *got.ReviewUnresolved != 2 {
		t.Fatalf("ReviewUnresolved = %v, want 2", got.ReviewUnresolved)
	}
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("status = %s, want waiting (task halted for review)", got.Status)
	}

	h.tryAutoPromote(ctx)
	got, _ = s.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("status = %s, want waiting (no auto-resume on open findings)", got.Status)
	}

	v.result = finished(0, "")
	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("runReview approve: %v", err)
	}
	got, _ = s.GetTask(ctx, task.ID)
	if got.ReviewUnresolved == nil || *got.ReviewUnresolved != 0 {
		t.Errorf("after approve: ReviewUnresolved = %v, want 0", got.ReviewUnresolved)
	}
}

// TestRunReview_ThreadsInputs proves the round reads the task (with its
// criteria), the state directory beside the worktree, and the configured
// round limit and token budget.
func TestRunReview_ThreadsInputs(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	if err := os.WriteFile(envPath, []byte("WALLFACER_REVIEW_ROUNDS=5\nWALLFACER_REVIEW_COST_CAP=70000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := &mockVerifier{result: finished(0, "")}
	h.verifier = v

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskForReview(t, s, "")
	if err := s.UpdateTaskCriteria(ctx, task.ID, "the /health endpoint returns 200"); err != nil {
		t.Fatalf("UpdateTaskCriteria: %v", err)
	}
	fresh, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	if err := h.runReview(ctx, s, *fresh); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	in := v.lastIn
	if in.Task == nil || in.Task.Criteria != "the /health endpoint returns 200" {
		t.Errorf("verifier task criteria = %v, want the task's criteria", in.Task)
	}
	if want := reviewStateDir(primaryWorktree(fresh.WorktreePaths)); in.StateDir != want {
		t.Errorf("StateDir = %q, want %q", in.StateDir, want)
	}
	if in.MaxRounds != 5 || in.CostCapTokens != 70000 {
		t.Errorf("bounds = %d rounds, %d tokens; want 5, 70000", in.MaxRounds, in.CostCapTokens)
	}
}

// TestRunReview_SkipsPersistWhenNotWaiting proves a session that finishes after
// the task already left waiting (resumed, submitted, failed) does not stamp a
// stale result onto it.
func TestRunReview_SkipsPersistWhenNotWaiting(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: finished(5, "stale")}
	h.verifier = v

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskForReview(t, s, "sess-1")

	// Task leaves waiting before the (mock, instantaneous) round completes.
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if v.calls() != 1 {
		t.Fatalf("verifier called %d times, want 1", v.calls())
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ReviewUnresolved != nil {
		t.Errorf("ReviewUnresolved = %v, want nil (no stale write)", *got.ReviewUnresolved)
	}
}

// blockingVerifier counts Verify calls and parks inside Verify until released,
// so a test can observe a round in flight and assert no duplicate is launched.
type blockingVerifier struct {
	mu      sync.Mutex
	called  int
	entered chan struct{}
	release chan struct{}
}

func (v *blockingVerifier) Verify(_ context.Context, _ review.Input) (*review.Result, error) {
	v.mu.Lock()
	v.called++
	v.mu.Unlock()
	v.entered <- struct{}{}
	<-v.release
	return finished(0, ""), nil
}

// TestTryAutoReview_DedupesConcurrentTicks proves a waiting task whose review
// round is still in flight is not re-launched on the next watcher tick: one
// run at a time per task. Without the beginReview guard, ReviewUnresolved stays
// nil for the whole round, so every tick fires another duplicate.
func TestTryAutoReview_DedupesConcurrentTicks(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &blockingVerifier{entered: make(chan struct{}, 4), release: make(chan struct{})}
	h.verifier = v
	h.SetReview(true)

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	waitingTaskForReview(t, s, "")

	h.tryAutoReview(ctx)
	select {
	case <-v.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first review round never reached the verifier")
	}

	// Second tick while the first round is still in flight: must dedup.
	h.tryAutoReview(ctx)
	select {
	case <-v.entered:
		t.Fatal("second review round started for an in-flight task; dedup failed")
	case <-time.After(200 * time.Millisecond):
	}

	close(v.release)

	v.mu.Lock()
	got := v.called
	v.mu.Unlock()
	if got != 1 {
		t.Errorf("verifier called %d times, want 1", got)
	}
}

// TestRunReview_FailedRoundOpensBreaker proves the retry bound: a failed round
// leaves the verdict unset, says so on the timeline in a fixed sentence, and
// opens the auto-review breaker, so the next tick does not retry until the
// breaker's backoff has passed.
func TestRunReview_FailedRoundOpensBreaker(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{err: &review.Failure{Code: review.CodeOutputInvalid, Err: errors.New("no JSON object")}}
	h.verifier = v
	h.SetReview(true)

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskForReview(t, s, "")

	if err := h.runReview(ctx, s, task); err == nil {
		t.Fatal("runReview returned nil for a failed round")
	}
	got, _ := s.GetTask(ctx, task.ID)
	if got.ReviewUnresolved != nil {
		t.Errorf("ReviewUnresolved = %v, want nil after a failed round", *got.ReviewUnresolved)
	}
	system := taskEvents(t, s, task.ID, store.EventTypeSystem)
	if countContaining(system, "answer could not be read") != 1 || countContaining(system, "no JSON object") != 0 {
		t.Errorf("timeline = %v, want the fixed sentence without the developer detail", system)
	}
	if !h.breakers["auto-review"].isOpen() {
		t.Fatal("auto-review breaker should be open after a failed round")
	}

	h.tryAutoReview(ctx)
	time.Sleep(50 * time.Millisecond)
	if v.calls() != 1 {
		t.Errorf("verifier called %d times, want 1: the open breaker holds the retry", v.calls())
	}
}

// TestReviewTuning_MinimalDefaultsAndOverride proves the default review depth
// and that env overrides change it.
func TestReviewTuning_MinimalDefaultsAndOverride(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)

	rounds, costCap := h.reviewTuning()
	if rounds != 3 || costCap != 50000 {
		t.Errorf("default tuning = %d rounds, cap %d; want 3 rounds, 50000", rounds, costCap)
	}

	envBody := "WALLFACER_REVIEW_ROUNDS=6\nWALLFACER_REVIEW_COST_CAP=120000\n"
	if err := os.WriteFile(envPath, []byte(envBody), 0o644); err != nil {
		t.Fatal(err)
	}
	rounds, costCap = h.reviewTuning()
	if rounds != 6 || costCap != 120000 {
		t.Errorf("override tuning = %d rounds, cap %d; want 6, 120000", rounds, costCap)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The review loop end to end: the real review.Reviewer on a mock runner
// ─────────────────────────────────────────────────────────────────────────────

// reviewLoopHandler returns a handler whose runner is a mock (so feedback
// delivery is observable as RunBackground calls) and whose verifier is the real
// review loop on that mock. reviewer answers each round.
func reviewLoopHandler(t *testing.T, env string, reviewer func(in runner.ReviewerInput) (*runner.ReviewerResult, error)) (*Handler, *store.Store, *runner.MockRunner) {
	t.Helper()
	h, envPath := newTestHandlerWithEnv(t)
	if err := os.WriteFile(envPath, []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	mock := &runner.MockRunner{
		RunReviewerFn: func(_ context.Context, _ *store.Task, in runner.ReviewerInput) (*runner.ReviewerResult, error) {
			res, err := reviewer(in)
			if err == nil && in.OnStart != nil {
				in.OnStart()
			}
			return res, err
		},
	}
	h.runner = mock
	h.verifier = review.New(mock)
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	return h, s, mock
}

// changes is a reviewer answer requesting changes, with the given usage.
func changes(tokens int, findings ...runner.ReviewFinding) *runner.ReviewerResult {
	return &runner.ReviewerResult{
		Answer: runner.ReviewAnswer{Verdict: runner.ReviewChangesRequested, Findings: findings},
		Model:  "reviewer-model",
		Usage:  store.TaskUsage{InputTokens: tokens},
	}
}

// approve is a reviewer answer approving the change.
func approve() *runner.ReviewerResult {
	return &runner.ReviewerResult{Answer: runner.ReviewAnswer{Verdict: runner.ReviewApprove}, Model: "reviewer-model"}
}

// finishTurn moves a task the review resumed back to waiting with the given
// result, as the runner does when the task's turn ends.
func finishTurn(t *testing.T, s *store.Store, id uuid.UUID, result string) store.Task {
	t.Helper()
	ctx := context.Background()
	if err := s.ForceUpdateTaskStatus(ctx, id, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := s.UpdateTaskResult(ctx, id, result, "", "end_turn", 2); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	fresh, err := s.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return *fresh
}

var (
	findingHigh = runner.ReviewFinding{Severity: runner.ReviewSeverityHigh, Claim: "Add writes to a nil map", Location: "store.go:10"}
	findingLow  = runner.ReviewFinding{Severity: runner.ReviewSeverityLow, Claim: "the doc comment names the wrong field"}
)

// TestReviewLoop_ChangesRequestedRoundsAreBounded walks a full session that
// never approves: each round's findings go to the task through the feedback
// path, the next round reads them and the task's reply, and the last allowed
// round ends the session with its findings open and the most severe one as
// the headline.
func TestReviewLoop_ChangesRequestedRoundsAreBounded(t *testing.T) {
	var seen []runner.ReviewerInput
	h, s, mock := reviewLoopHandler(t, "WALLFACER_REVIEW_ROUNDS=3\n", func(in runner.ReviewerInput) (*runner.ReviewerResult, error) {
		seen = append(seen, in)
		return changes(100, findingHigh, findingLow), nil
	})
	ctx := context.Background()
	task := waitingTaskForReview(t, s, "sess-1")

	for round := 1; round <= 2; round++ {
		if err := h.runReview(ctx, s, task); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		got, _ := s.GetTask(ctx, task.ID)
		if got.Status != store.TaskStatusInProgress {
			t.Fatalf("round %d: status = %s, want in_progress (findings sent as feedback)", round, got.Status)
		}
		if got.ReviewUnresolved != nil {
			t.Fatalf("round %d: verdict set mid-session", round)
		}
		task = finishTurn(t, s, task.ID, fmt.Sprintf("reply %d", round))
	}
	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("round 3: %v", err)
	}

	if n := len(mock.RunCalls()); n != 2 {
		t.Errorf("feedback deliveries = %d, want 2 (rounds - 1)", n)
	}
	if len(seen) != 3 {
		t.Fatalf("reviewer runs = %d, want 3", len(seen))
	}
	for i, in := range seen {
		if in.Round != i+1 || in.MaxRounds != 3 {
			t.Errorf("run %d: round %d of %d, want %d of 3", i+1, in.Round, in.MaxRounds, i+1)
		}
	}
	if len(seen[1].PriorFindings) != 2 || seen[1].ImplementerReply != "reply 1" {
		t.Errorf("round 2 input = %+v, want the round 1 findings and the task's reply", seen[1])
	}

	got, _ := s.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("status = %s, want waiting after the last round", got.Status)
	}
	if got.ReviewUnresolved == nil || *got.ReviewUnresolved != 2 {
		t.Fatalf("ReviewUnresolved = %v, want 2 open findings", got.ReviewUnresolved)
	}
	if got.ReviewHeadline != findingHigh.Claim {
		t.Errorf("ReviewHeadline = %q, want the most severe claim %q", got.ReviewHeadline, findingHigh.Claim)
	}
	if got.ReviewSessionDir == "" {
		t.Error("ReviewSessionDir is empty")
	}

	feedback := taskEvents(t, s, task.ID, store.EventTypeFeedback)
	if len(feedback) != 2 || !strings.Contains(feedback[0], findingHigh.Claim) {
		t.Errorf("feedback events = %v, want two carrying the findings", feedback)
	}
	changesFromReview := countContaining(taskEvents(t, s, task.ID, store.EventTypeStateChange), `"trigger":"auto_review"`)
	if changesFromReview != 2 {
		t.Errorf("state changes triggered by the review = %d, want 2", changesFromReview)
	}
}

// TestReviewLoop_ApproveEndsSession proves an approval after a feedback round
// ends the session with no open findings.
func TestReviewLoop_ApproveEndsSession(t *testing.T) {
	answers := []*runner.ReviewerResult{changes(100, findingHigh), approve()}
	h, s, mock := reviewLoopHandler(t, "", func(in runner.ReviewerInput) (*runner.ReviewerResult, error) {
		return answers[in.Round-1], nil
	})
	ctx := context.Background()
	task := waitingTaskForReview(t, s, "")

	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("round 1: %v", err)
	}
	task = finishTurn(t, s, task.ID, "fixed the nil map")
	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("round 2: %v", err)
	}

	got, _ := s.GetTask(ctx, task.ID)
	if got.ReviewUnresolved == nil || *got.ReviewUnresolved != 0 || got.ReviewHeadline != "" {
		t.Errorf("verdict = %v %q, want 0 open findings and no headline", got.ReviewUnresolved, got.ReviewHeadline)
	}
	if n := len(mock.RunCalls()); n != 1 {
		t.Errorf("feedback deliveries = %d, want 1", n)
	}
}

// TestReviewLoop_CostCapEndsSession proves the token budget ends the session
// instead of sending the findings back once the reviewer has spent it.
func TestReviewLoop_CostCapEndsSession(t *testing.T) {
	h, s, mock := reviewLoopHandler(t, "WALLFACER_REVIEW_COST_CAP=1000\n", func(runner.ReviewerInput) (*runner.ReviewerResult, error) {
		return changes(1500, findingHigh), nil
	})
	ctx := context.Background()
	task := waitingTaskForReview(t, s, "")

	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	got, _ := s.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("status = %s, want waiting: no feedback past the budget", got.Status)
	}
	if got.ReviewUnresolved == nil || *got.ReviewUnresolved != 1 {
		t.Errorf("ReviewUnresolved = %v, want 1", got.ReviewUnresolved)
	}
	if n := len(mock.RunCalls()); n != 0 {
		t.Errorf("feedback deliveries = %d, want 0", n)
	}
	if countContaining(taskEvents(t, s, task.ID, store.EventTypeSystem), "token budget is spent") != 1 {
		t.Error("expected a timeline event naming the spent budget")
	}
}

// TestReviewLoop_SameModelSkipsWithReason proves a reviewer model equal to the
// task's does not run the review: the reason is on the timeline once, however
// many ticks follow, the verdict stays unset, and nothing is sent to the task.
func TestReviewLoop_SameModelSkipsWithReason(t *testing.T) {
	h, s, mock := reviewLoopHandler(t, "", func(runner.ReviewerInput) (*runner.ReviewerResult, error) {
		return nil, &runner.ReviewRefusal{Code: runner.ReviewRefusedSameModel, Detail: `reviewer model "m" is the task's model`}
	})
	ctx := context.Background()
	task := waitingTaskForReview(t, s, "")

	for range 3 {
		if err := h.runReview(ctx, s, task); err != nil {
			t.Fatalf("runReview: %v", err)
		}
	}
	system := taskEvents(t, s, task.ID, store.EventTypeSystem)
	if n := countContaining(system, "the reviewer model is the model the task runs on"); n != 1 {
		t.Errorf("skip events = %d, want 1: %v", n, system)
	}
	if countContaining(system, "started") != 0 {
		t.Errorf("a skipped review must not report a round start: %v", system)
	}
	got, _ := s.GetTask(ctx, task.ID)
	if got.ReviewUnresolved != nil || got.Status != store.TaskStatusWaiting {
		t.Errorf("task = %s with verdict %v, want waiting with no verdict", got.Status, got.ReviewUnresolved)
	}
	if n := len(mock.RunCalls()); n != 0 {
		t.Errorf("feedback deliveries = %d, want 0", n)
	}
	if h.breakers["auto-review"].isOpen() {
		t.Error("a skip is not a failure; the breaker should stay closed")
	}

	// The skip is in the record the transcript route reads.
	sess, found, err := review.Newest(reviewStateDir(primaryWorktree(task.WorktreePaths)))
	if err != nil || !found || sess.End == nil || sess.End.Skip == nil || sess.End.Skip.Code != runner.ReviewRefusedSameModel {
		t.Fatalf("newest session = %+v (found %v, err %v), want the recorded skip", sess, found, err)
	}
	b, err := json.Marshal(sess.End.Skip)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"detail"`) {
		t.Errorf("skip record %s lacks the developer detail", b)
	}
}

// TestReviewLoop_FeedbackNotSentWhenTaskMovedOn proves findings for a task that
// left waiting while its reviewer ran are not delivered, and the session ends
// as superseded so the next review starts afresh.
func TestReviewLoop_FeedbackNotSentWhenTaskMovedOn(t *testing.T) {
	var s *store.Store
	var taskID uuid.UUID
	h, s, mock := reviewLoopHandler(t, "", func(runner.ReviewerInput) (*runner.ReviewerResult, error) {
		// The task is cancelled while the reviewer runs.
		if err := s.ForceUpdateTaskStatus(context.Background(), taskID, store.TaskStatusCancelled); err != nil {
			return nil, err
		}
		return changes(100, findingHigh), nil
	})
	ctx := context.Background()
	task := waitingTaskForReview(t, s, "")
	taskID = task.ID

	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if n := len(mock.RunCalls()); n != 0 {
		t.Errorf("feedback deliveries = %d, want 0", n)
	}
	sess, found, err := review.Newest(reviewStateDir(primaryWorktree(task.WorktreePaths)))
	if err != nil || !found || sess.End == nil || sess.End.Termination != review.TerminationSuperseded {
		t.Fatalf("newest session = %+v (found %v, err %v), want it ended as superseded", sess, found, err)
	}
}
