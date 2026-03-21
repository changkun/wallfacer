package runner

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/store"
	"changkun.de/wallfacer/internal/workspace"
	"github.com/google/uuid"
)

// WebhookEventType identifies the kind of webhook notification.
type WebhookEventType string

// WebhookEventType constants.
const (
	WebhookEventTaskStateChanged WebhookEventType = "task.state_changed"
	maxWebhookPromptLen          int              = 200
	maxWebhookResultLen          int              = 500
	maxWebhookTitleLen           int              = 80
)

// WebhookPayload is the JSON body posted to the configured webhook URL on every
// task state-change event.
type WebhookPayload struct {
	EventType  WebhookEventType `json:"event_type"`
	TaskID     string           `json:"task_id"`
	Status     store.TaskStatus `json:"status"`
	Title      string           `json:"title"`
	Prompt     string           `json:"prompt"`
	Result     string           `json:"result,omitempty"`
	OccurredAt time.Time        `json:"occurred_at"`
}

// WebhookNotifier subscribes to the store's pub/sub channel and delivers an
// HTTP POST to webhookURL whenever a task transitions to a new status.
type WebhookNotifier struct {
	store         *store.Store
	workspace     *workspace.Manager
	webhookURL    string
	webhookSecret string
	client        *http.Client
	backoffs      []time.Duration
	wg            sync.WaitGroup
}

// NewWorkspaceWebhookNotifier constructs a WebhookNotifier that uses a workspace manager.
func NewWorkspaceWebhookNotifier(m *workspace.Manager, cfg envconfig.Config) *WebhookNotifier {
	return &WebhookNotifier{
		workspace:     m,
		webhookURL:    cfg.WebhookURL,
		webhookSecret: cfg.WebhookSecret,
		client:        &http.Client{Timeout: 10 * time.Second},
		backoffs:      []time.Duration{0, 2 * time.Second, 5 * time.Second, 10 * time.Second},
	}
}

// NewWebhookNotifier constructs a WebhookNotifier from runtime config.
func NewWebhookNotifier(s *store.Store, cfg envconfig.Config) *WebhookNotifier {
	return &WebhookNotifier{
		store:         s,
		webhookURL:    cfg.WebhookURL,
		webhookSecret: cfg.WebhookSecret,
		client:        &http.Client{Timeout: 10 * time.Second},
		backoffs:      []time.Duration{0, 2 * time.Second, 5 * time.Second, 10 * time.Second},
	}
}

// SetRetryBackoffs overrides the default retry backoff schedule.
func (wn *WebhookNotifier) SetRetryBackoffs(backoffs []time.Duration) {
	wn.backoffs = append([]time.Duration(nil), backoffs...)
}

// Start subscribes to the store and dispatches webhook deliveries in the
// background until ctx is cancelled.
func (wn *WebhookNotifier) Start(ctx context.Context) {
	lastStatus := make(map[uuid.UUID]store.TaskStatus)
	if wn.workspace == nil {
		id, ch := wn.store.Subscribe()
		defer wn.store.Unsubscribe(id)
		wn.runLoop(ctx, ch, lastStatus)
		return
	}

	wsSubID, wsCh := wn.workspace.Subscribe()
	defer wn.workspace.Unsubscribe(wsSubID)

	var (
		curStore *store.Store
		subID    int
		subCh    <-chan store.SequencedDelta
	)
	subscribeStore := func(s *store.Store) {
		if curStore != nil && subCh != nil {
			curStore.Unsubscribe(subID)
		}
		curStore = s
		subCh = nil
		if s != nil {
			subID, subCh = s.Subscribe()
		}
	}
	subscribeStore(wn.workspace.Snapshot().Store)
	defer func() {
		if curStore != nil && subCh != nil {
			curStore.Unsubscribe(subID)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case snap, ok := <-wsCh:
			if !ok {
				return
			}
			subscribeStore(snap.Store)
		case delta, ok := <-subCh:
			if !ok {
				subCh = nil
				continue
			}
			wn.handleDelta(ctx, delta, lastStatus)
		}
	}
}

func (wn *WebhookNotifier) runLoop(ctx context.Context, ch <-chan store.SequencedDelta, lastStatus map[uuid.UUID]store.TaskStatus) {
	for {
		select {
		case <-ctx.Done():
			return
		case delta, ok := <-ch:
			if !ok {
				return
			}
			wn.handleDelta(ctx, delta, lastStatus)
		}
	}
}

func (wn *WebhookNotifier) handleDelta(ctx context.Context, delta store.SequencedDelta, lastStatus map[uuid.UUID]store.TaskStatus) {
	if delta.Deleted || delta.Task == nil {
		return
	}
	task := delta.Task
	if prev, seen := lastStatus[task.ID]; seen && prev == task.Status {
		return
	}
	lastStatus[task.ID] = task.Status

	result := ""
	if task.Result != nil {
		result = *task.Result
	}
	payload := NewTaskStateChangedPayload(task.ID.String(), task.Status, task.Title, task.Prompt, result, time.Now().UTC())
	wn.wg.Add(1)
	go func() {
		defer wn.wg.Done()
		if err := wn.Send(ctx, payload); err != nil {
			slog.Error("webhook: delivery failed", "event", payload.EventType, "task_id", payload.TaskID, "error", err)
		}
	}()
}

// Wait blocks until all in-flight webhook deliveries have completed.
func (wn *WebhookNotifier) Wait() { wn.wg.Wait() }

// NewTaskStateChangedPayload builds a webhook payload for a task state change event.
func NewTaskStateChangedPayload(taskID string, status store.TaskStatus, title, prompt, result string, occurredAt time.Time) WebhookPayload {
	title = truncateWebhookField(title, maxWebhookTitleLen)
	if title == "" {
		title = truncateWebhookField(prompt, maxWebhookTitleLen)
	}
	return WebhookPayload{
		EventType:  WebhookEventTaskStateChanged,
		TaskID:     taskID,
		Status:     status,
		Title:      title,
		Prompt:     truncateWebhookField(prompt, maxWebhookPromptLen),
		Result:     truncateWebhookField(result, maxWebhookResultLen),
		OccurredAt: occurredAt.UTC(),
	}
}

func truncateWebhookField(s string, limit int) string {
	if len(s) > limit {
		return s[:limit]
	}
	return s
}

// Send POSTs payload to the configured webhook URL. It retries up to 3 times
// on non-2xx responses, sleeping 2 s, 5 s, and 10 s between attempts.
// The context controls cancellation of inter-attempt sleeps; a cancelled
// context causes Send to return early with a wrapped context error.
func (wn *WebhookNotifier) Send(ctx context.Context, payload WebhookPayload) error {
	if wn.webhookURL == "" {
		return fmt.Errorf("webhook URL is not configured")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	sig := ""
	if wn.webhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(wn.webhookSecret))
		mac.Write(data)
		sig = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}

	// Attempt 0 is immediate; attempts 1-3 wait for the respective backoff.
	var lastErr error
	for attempt, delay := range wn.backoffs {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("webhook retry cancelled: %w", ctx.Err())
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, wn.webhookURL, bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Wallfacer-Event", string(payload.EventType))
		if sig != "" {
			req.Header.Set("X-Wallfacer-Signature", sig)
		}

		resp, err := wn.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("attempt %d POST failed: %w", attempt+1, err)
			continue
		}
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("attempt %d returned HTTP %d", attempt+1, resp.StatusCode)
	}
	return lastErr
}
