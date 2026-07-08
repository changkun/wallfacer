package handler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/topos/adversarial"
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

// mockVerifier records Verify calls.
type mockVerifier struct {
	called int
	lastIn adversarial.VerifyInput
	result *adversarial.VerifyResult
	err    error
}

func (v *mockVerifier) Verify(_ context.Context, in adversarial.VerifyInput) (*adversarial.VerifyResult, error) {
	v.called++
	v.lastIn = in
	return v.result, v.err
}

func TestTryAutoReview_SkipsWhenDisabled(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: 0}}
	h.verifier = v
	// ReviewEnabled defaults to false — tryAutoReview must not call verifier.
	h.tryAutoReview(context.Background())
	if v.called != 0 {
		t.Errorf("verifier called %d times when review disabled, want 0", v.called)
	}
}

func TestTryAutoReview_SkipsTaskWithoutSession(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: 0}}
	h.verifier = v
	h.SetReview(true)

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "no-session", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	// No session → ListWaitingTasksWithSession returns nothing → verifier not called.
	h.tryAutoReview(ctx)
	if v.called != 0 {
		t.Errorf("verifier called %d times for task without session, want 0", v.called)
	}
}

func TestTryAutoReview_SkipsTaskWithReviewAlreadyRun(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: 0}}
	h.verifier = v
	h.SetReview(true)

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "already-run", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := s.UpdateTaskResult(ctx, task.ID, "done", "session-xyz", "end_turn", 1); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	if err := s.UpdateTaskReview(ctx, task.ID, 0, "", ""); err != nil {
		t.Fatalf("UpdateTaskReview: %v", err)
	}

	h.tryAutoReview(ctx)
	if v.called != 0 {
		t.Errorf("verifier called %d times for already-run task, want 0", v.called)
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

// waitingTaskWithSession creates a waiting task that has a session ID and a
// (non-git) worktree path, the minimum for runReview to reach the verifier.
func waitingTaskWithSession(t *testing.T, s *store.Store) store.Task {
	t.Helper()
	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "verify", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := s.UpdateTaskResult(ctx, task.ID, "done", "sess-1", "end_turn", 1); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{t.TempDir(): t.TempDir()}, "branch"); err != nil {
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
	unresolved := 3
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: unresolved, Headline: "boom"}}
	h.verifier = v

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskWithSession(t, s)

	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ReviewUnresolved == nil || *got.ReviewUnresolved != unresolved {
		t.Errorf("ReviewUnresolved = %v, want %d", got.ReviewUnresolved, unresolved)
	}
	if got.ReviewHeadline != "boom" {
		t.Errorf("ReviewHeadline = %q, want %q", got.ReviewHeadline, "boom")
	}
}

// TestRunReview_AttributesCost proves the review run's USD is added to the task's
// usage total and recorded under the "review" sub-agent breakdown.
func TestRunReview_AttributesCost(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: 0, USD: 0.42}}
	h.verifier = v

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskWithSession(t, s)

	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Usage.CostUSD != 0.42 {
		t.Errorf("task total CostUSD = %v, want 0.42", got.Usage.CostUSD)
	}
	if bd := got.UsageBreakdown[store.SandboxActivityReview]; bd.CostUSD != 0.42 {
		t.Errorf("review breakdown CostUSD = %v, want 0.42", bd.CostUSD)
	}
}

// TestRunReview_AttributesTokensFromEndJson proves the complete token breakdown
// is read from review's session end.json and attributed to the task, alongside
// the USD cost.
func TestRunReview_AttributesTokensFromEndJson(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	sessionDir := t.TempDir()
	endJSON := `{"stats":{"token_usage":{"input_tokens":1500,"output_tokens":300,"cache_read_input_tokens":900,"cache_creation_input_tokens":40}}}`
	if err := os.WriteFile(filepath.Join(sessionDir, "end.json"), []byte(endJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: 0, USD: 0.91, SessionDir: sessionDir}}
	h.verifier = v

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskWithSession(t, s)

	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	bd := got.UsageBreakdown[store.SandboxActivityReview]
	if bd.InputTokens != 1500 || bd.OutputTokens != 300 || bd.CacheReadInputTokens != 900 || bd.CacheCreationTokens != 40 {
		t.Errorf("review token breakdown = %+v, want input=1500 output=300 cacheRead=900 cacheCreate=40", bd)
	}
	if bd.CostUSD != 0.91 {
		t.Errorf("review CostUSD = %v, want 0.91", bd.CostUSD)
	}
}

// TestRunReview_EmitsTimelineEvents proves a run surfaces start + completion
// events on the task timeline, so a manual or auto trigger is visible rather
// than silently running in the background.
func TestRunReview_EmitsTimelineEvents(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: 2, Headline: "nil deref in foo"}}
	h.verifier = v

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskWithSession(t, s)

	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	var started, finished bool
	for _, e := range events {
		if e.EventType != store.EventTypeSystem {
			continue
		}
		d := string(e.Data)
		if strings.Contains(d, "verification started") {
			started = true
		}
		if strings.Contains(d, "2 unresolved") {
			finished = true
		}
	}
	if !started {
		t.Error("expected an 'Review: ... started' timeline event")
	}
	if !finished {
		t.Error("expected an 'Review: 2 unresolved' completion event")
	}
}

func TestReviewSupersedesTest_Gate(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	sid := "sess-1"
	withSession := &store.Task{SessionID: &sid}
	noSession := &store.Task{}

	// Review off: never supersedes.
	if h.reviewSupersedesTest(withSession) {
		t.Error("review off should not supersede the test agent")
	}
	// Review on + session: supersedes.
	h.SetReview(true)
	if !h.reviewSupersedesTest(withSession) {
		t.Error("review on + session should supersede the test agent")
	}
	// Review on, no session: falls back to the test agent.
	if h.reviewSupersedesTest(noSession) {
		t.Error("a non-session task should fall back to the test agent")
	}
}

// TestRunReview_BlocksOnUnresolved proves an unresolved verdict is a hard barrier:
// the verdict is persisted, the task stays parked in waiting, autoimplement does not
// auto-resume it, and a clean verdict clears the barrier. This is the
// block-on-first-failure behavior that replaced the old auto-feedback loop.
func TestRunReview_BlocksOnUnresolved(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.SetAutoimplement(true)
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: 2, Headline: "nil deref"}}
	h.verifier = v

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskWithSession(t, s)

	// Unresolved attacks: verdict persisted, task stays waiting (the barrier).
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

	// Autoimplement must not auto-resume a task halted by unresolved attacks: it stays
	// waiting until a human confirms or resumes it with steering.
	h.tryAutoPromote(ctx)
	got, _ = s.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("status = %s, want waiting (no auto-resume on unresolved attacks)", got.Status)
	}

	// A clean verdict clears the barrier.
	v.result = &adversarial.VerifyResult{Unresolved: 0}
	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("runReview clean: %v", err)
	}
	got, _ = s.GetTask(ctx, task.ID)
	if got.ReviewUnresolved == nil || *got.ReviewUnresolved != 0 {
		t.Errorf("after clean: ReviewUnresolved = %v, want 0", got.ReviewUnresolved)
	}
}

// TestRunReview_ThreadsCriteria proves the task's persisted Criteria reaches the
// verifier input, so review critics are anchored to the same acceptance bar as
// the test agent (the previously-blocked goal #7).
func TestRunReview_ThreadsCriteria(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: 0}}
	h.verifier = v

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskWithSession(t, s)
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
	if v.lastIn.Criteria != "the /health endpoint returns 200" {
		t.Errorf("verifier received Criteria %q, want the task's criteria", v.lastIn.Criteria)
	}
}

// TestRunReview_SkipsPersistWhenNotWaiting proves a run that finishes after the
// task already left waiting (resumed, submitted, failed) does not stamp a stale
// result onto it.
func TestRunReview_SkipsPersistWhenNotWaiting(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	v := &mockVerifier{result: &adversarial.VerifyResult{Unresolved: 5, Headline: "stale"}}
	h.verifier = v

	ctx := context.Background()
	s, ok := h.currentStore()
	if !ok {
		t.Fatal("no current store")
	}
	task := waitingTaskWithSession(t, s)

	// Task leaves waiting before the (mock, instantaneous) run completes.
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	if err := h.runReview(ctx, s, task); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if v.called != 1 {
		t.Fatalf("verifier called %d times, want 1", v.called)
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
// so a test can observe a run in flight and assert no duplicate is launched.
type blockingVerifier struct {
	mu      sync.Mutex
	called  int
	entered chan struct{}
	release chan struct{}
}

func (v *blockingVerifier) Verify(_ context.Context, _ adversarial.VerifyInput) (*adversarial.VerifyResult, error) {
	v.mu.Lock()
	v.called++
	v.mu.Unlock()
	v.entered <- struct{}{}
	<-v.release
	return &adversarial.VerifyResult{Unresolved: 0}, nil
}

// TestTryAutoReview_DedupesConcurrentTicks proves a waiting task whose review run is
// still in flight is not re-launched on the next watcher tick. Without the
// beginReview guard, ReviewUnresolved stays nil for the whole multi-minute run, so
// every tick fires another duplicate run for the same task.
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
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "dedup", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := s.UpdateTaskResult(ctx, task.ID, "done", "sess-1", "end_turn", 1); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	// A non-git worktree path is enough: generateWorktreeDiff skips it and
	// returns "", and runReview still reaches the verifier.
	if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{t.TempDir(): t.TempDir()}, "branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}

	// First tick: launch a run and wait until it is parked inside Verify so the
	// in-flight slot is held.
	h.tryAutoReview(ctx)
	select {
	case <-v.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first review run never reached the verifier")
	}

	// Second tick while the first run is still in flight: must dedup.
	h.tryAutoReview(ctx)
	select {
	case <-v.entered:
		t.Fatal("second review run started for an in-flight task; dedup failed")
	case <-time.After(200 * time.Millisecond):
	}

	close(v.release) // let the first run finish

	v.mu.Lock()
	got := v.called
	v.mu.Unlock()
	if got != 1 {
		t.Errorf("verifier called %d times, want 1", got)
	}
}

// TestReviewTuning_MinimalDefaultsAndOverride proves the default review depth is the
// minimum floor (1 fork, 3 rounds — one full attack/rebuttal/re-assess cycle) and
// that env overrides expand it. Guards the cost-minimizing default and the dial.
func TestReviewTuning_MinimalDefaultsAndOverride(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)

	// No env override: the conservative floor.
	forks, rounds, costCap := h.reviewTuning()
	if forks != 1 || rounds != 3 {
		t.Errorf("default tuning = forks %d, rounds %d; want 1 fork, 3 rounds", forks, rounds)
	}
	if costCap != 50000 {
		t.Errorf("default cost cap = %d, want 50000", costCap)
	}

	// Env overrides expand verification depth.
	envBody := "WALLFACER_REVIEW_FORKS=2\nWALLFACER_REVIEW_ROUNDS=6\nWALLFACER_REVIEW_COST_CAP=120000\n"
	if err := os.WriteFile(envPath, []byte(envBody), 0o644); err != nil {
		t.Fatal(err)
	}
	forks, rounds, costCap = h.reviewTuning()
	if forks != 2 || rounds != 6 || costCap != 120000 {
		t.Errorf("override tuning = forks %d, rounds %d, cap %d; want 2, 6, 120000", forks, rounds, costCap)
	}
}
