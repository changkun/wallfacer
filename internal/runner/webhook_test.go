package runner_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
)

func TestWebhookNotifier_DeliverOnStateChange(t *testing.T) {
	// Channel receives each request body as it arrives.
	type received struct {
		header http.Header
		body   []byte
	}
	reqCh := make(chan received, 4)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqCh <- received{header: r.Header.Clone(), body: body}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	s, err := store.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	const secret = "test-secret-key"
	cfg := envconfig.Config{
		WebhookURL:    srv.URL,
		WebhookSecret: secret,
	}

	wn := runner.NewWebhookNotifier(s, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wn.Start(ctx)

	// Give the notifier a moment to subscribe before we fire events.
	time.Sleep(10 * time.Millisecond)

	// Create a task; the store calls notify() with status=backlog.
	longPrompt := strings.Repeat("p", 220)
	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: longPrompt, Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Wait for the webhook POST with a generous timeout.
	var req received
	select {
	case req = <-reqCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for webhook delivery")
	}

	// Verify no extra deliveries for this single state change.
	select {
	case extra := <-reqCh:
		t.Errorf("unexpected extra webhook delivery: %s", extra.body)
	case <-time.After(100 * time.Millisecond):
		// good — only one delivery
	}

	// --- Validate HMAC signature ---
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(req.body)
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	gotSig := req.header.Get("X-Wallfacer-Signature")
	if gotSig != wantSig {
		t.Errorf("X-Wallfacer-Signature mismatch:\n  got  %s\n  want %s", gotSig, wantSig)
	}

	// Validate event header.
	if got := req.header.Get("X-Wallfacer-Event"); got != string(runner.WebhookEventTaskStateChanged) {
		t.Errorf("X-Wallfacer-Event = %q, want %q", got, runner.WebhookEventTaskStateChanged)
	}
	if got := req.header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}

	// --- Validate JSON payload ---
	var payload runner.WebhookPayload
	if err := json.Unmarshal(req.body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.EventType != runner.WebhookEventTaskStateChanged {
		t.Errorf("event_type = %q, want %q", payload.EventType, runner.WebhookEventTaskStateChanged)
	}
	if payload.TaskID != task.ID.String() {
		t.Errorf("task_id = %q, want %q", payload.TaskID, task.ID.String())
	}
	if payload.Status != store.TaskStatusBacklog {
		t.Errorf("status = %q, want %q", payload.Status, store.TaskStatusBacklog)
	}
	if len(payload.Prompt) != 200 {
		t.Errorf("prompt length = %d, want 200", len(payload.Prompt))
	}
	if len(payload.Title) != 80 {
		t.Errorf("title length = %d, want 80", len(payload.Title))
	}
	if payload.OccurredAt.IsZero() {
		t.Error("occurred_at must not be zero")
	}

	// --- Status change triggers a second delivery ---
	longResult := strings.Repeat("r", 520)
	if err := s.UpdateTaskResult(context.Background(), task.ID, longResult, "sess-1", "end_turn", 1); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	if err := s.UpdateTaskStatus(context.Background(), task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	select {
	case req2 := <-reqCh:
		var p2 runner.WebhookPayload
		if err := json.Unmarshal(req2.body, &p2); err != nil {
			t.Fatalf("unmarshal second payload: %v", err)
		}
		if p2.Status != store.TaskStatusInProgress {
			t.Errorf("second delivery status = %q, want %q", p2.Status, store.TaskStatusInProgress)
		}
		if len(p2.Result) != 500 {
			t.Errorf("result length = %d, want 500", len(p2.Result))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for second webhook delivery on status change")
	}
}

func TestWebhookNotifier_NoDeliveryWhenStatusUnchanged(t *testing.T) {
	reqCh := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reqCh <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	s, err := store.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	wn := runner.NewWebhookNotifier(s, envconfig.Config{WebhookURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wn.Start(ctx)
	time.Sleep(10 * time.Millisecond)

	// Create task → one delivery (backlog).
	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "test prompt", Timeout: 5, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	select {
	case <-reqCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first delivery")
	}

	// Update the title — this calls notify() but status is the same → no webhook.
	if err := s.UpdateTaskTitle(context.Background(), task.ID, "new title"); err != nil {
		t.Fatalf("UpdateTaskTitle: %v", err)
	}
	select {
	case <-reqCh:
		t.Error("unexpected webhook delivery when status did not change")
	case <-time.After(150 * time.Millisecond):
		// correct: no extra delivery
	}
}

func TestSend_ContextCancelledDuringRetry(t *testing.T) {
	// Server always returns 503 so Send keeps retrying.
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	wn := runner.NewWebhookNotifier(nil, envconfig.Config{WebhookURL: srv.URL})
	// backoffs: immediate first attempt, then 50 ms, then 200 ms.
	wn.SetRetryBackoffs([]time.Duration{0, 50 * time.Millisecond, 200 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())

	payload := runner.NewTaskStateChangedPayload(
		"test-id",
		store.TaskStatusInProgress,
		"title", "prompt", "result",
		time.Now().UTC(),
	)

	done := make(chan error, 1)
	go func() {
		done <- wn.Send(ctx, payload)
	}()

	// Wait for the first attempt to complete, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	// Send must return well before the full retry sequence (50+200 ms) would finish.
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from cancelled Send, got nil")
		}
		// Should have completed with only the first attempt (possibly two if
		// timing is tight), not all three.
		if attempts >= 3 {
			t.Errorf("expected Send to abort early, but got %d attempts", attempts)
		}
	case <-time.After(150 * time.Millisecond):
		cancel() // clean up
		t.Fatal("Send did not return promptly after context cancellation")
	}
}

func TestWebhookNotifier_RetriesOnFailure(t *testing.T) {
	// First request fails; second succeeds.
	attempts := 0
	reqCh := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		reqCh <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Use a custom notifier with very short timeouts via the exported type.
	dir := t.TempDir()
	s, err := store.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	// Temporarily override the env file path to use a temp file to trigger a
	// notify via the store directly.
	_ = os.MkdirAll(dir, 0o755)
	wn := runner.NewWebhookNotifier(s, envconfig.Config{WebhookURL: srv.URL})
	wn.SetRetryBackoffs([]time.Duration{0, 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wn.Start(ctx)
	time.Sleep(10 * time.Millisecond)

	if _, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "retry test", Timeout: 5, Kind: store.TaskKindTask}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	select {
	case <-reqCh:
		if attempts < 2 {
			t.Errorf("expected at least 2 attempts, got %d", attempts)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for successful retry")
	}
}
