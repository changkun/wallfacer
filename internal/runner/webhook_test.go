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
	"testing"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/store"
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
	task, err := s.CreateTask(context.Background(), "implement feature X", 30, false, "", store.TaskKindTask)
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
	if got := req.header.Get("X-Wallfacer-Event"); got != "task.state_changed" {
		t.Errorf("X-Wallfacer-Event = %q, want %q", got, "task.state_changed")
	}
	if got := req.header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}

	// --- Validate JSON payload ---
	var payload runner.WebhookPayload
	if err := json.Unmarshal(req.body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.EventType != "task.state_changed" {
		t.Errorf("event_type = %q, want %q", payload.EventType, "task.state_changed")
	}
	if payload.TaskID != task.ID.String() {
		t.Errorf("task_id = %q, want %q", payload.TaskID, task.ID.String())
	}
	if payload.Status != store.TaskStatusBacklog {
		t.Errorf("status = %q, want %q", payload.Status, store.TaskStatusBacklog)
	}
	if payload.Prompt == "" {
		t.Error("prompt must not be empty")
	}
	if payload.OccurredAt.IsZero() {
		t.Error("occurred_at must not be zero")
	}

	// --- Status change triggers a second delivery ---
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
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for second webhook delivery on status change")
	}
}

func TestWebhookNotifier_NoDeliveryWhenStatusUnchanged(t *testing.T) {
	reqCh := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	task, err := s.CreateTask(context.Background(), "test prompt", 5, false, "", store.TaskKindTask)
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

func TestWebhookNotifier_RetriesOnFailure(t *testing.T) {
	// First request fails; second succeeds.
	attempts := 0
	reqCh := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wn.Start(ctx)
	time.Sleep(10 * time.Millisecond)

	if _, err := s.CreateTask(context.Background(), "retry test", 5, false, "", store.TaskKindTask); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	select {
	case <-reqCh:
		if attempts < 2 {
			t.Errorf("expected at least 2 attempts, got %d", attempts)
		}
	case <-time.After(15 * time.Second): // 2s backoff before retry
		t.Fatal("timed out waiting for successful retry")
	}
}
