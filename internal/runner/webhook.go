package runner

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// WebhookPayload is the JSON body posted to the configured webhook URL on every
// task state-change event.
type WebhookPayload struct {
	EventType  string           `json:"event_type"`
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
	webhookURL    string
	webhookSecret string
	client        *http.Client
}

// NewWebhookNotifier constructs a WebhookNotifier from runtime config.
func NewWebhookNotifier(s *store.Store, cfg envconfig.Config) *WebhookNotifier {
	return &WebhookNotifier{
		store:         s,
		webhookURL:    cfg.WebhookURL,
		webhookSecret: cfg.WebhookSecret,
		client:        &http.Client{Timeout: 10 * time.Second},
	}
}

// Start subscribes to the store and dispatches webhook deliveries in the
// background until ctx is cancelled.
func (wn *WebhookNotifier) Start(ctx context.Context) {
	id, ch := wn.store.Subscribe()
	defer wn.store.Unsubscribe(id)

	lastStatus := make(map[uuid.UUID]store.TaskStatus)

	for {
		select {
		case <-ctx.Done():
			return
		case delta, ok := <-ch:
			if !ok {
				return
			}
			// Skip deletes — there is no meaningful status to report.
			if delta.Deleted || delta.Task == nil {
				continue
			}
			task := delta.Task

			// Only deliver when the status actually changes.
			if prev, seen := lastStatus[task.ID]; seen && prev == task.Status {
				continue
			}
			lastStatus[task.ID] = task.Status

			prompt := task.Prompt
			if len(prompt) > 200 {
				prompt = prompt[:200]
			}

			result := ""
			if task.Result != nil {
				result = *task.Result
				if len(result) > 500 {
					result = result[:500]
				}
			}

			title := task.Title
			if title == "" {
				title = task.Prompt
				if len(title) > 80 {
					title = title[:80]
				}
			}

			payload := WebhookPayload{
				EventType:  "task.state_changed",
				TaskID:     task.ID.String(),
				Status:     task.Status,
				Title:      title,
				Prompt:     prompt,
				Result:     result,
				OccurredAt: time.Now().UTC(),
			}
			go wn.deliver(payload)
		}
	}
}

// deliver POSTs payload to the webhook URL. It retries up to 3 times on
// non-2xx responses, sleeping 2 s, 5 s, and 10 s between attempts. Errors
// are logged but never propagate (the caller must not block).
func (wn *WebhookNotifier) deliver(payload WebhookPayload) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("webhook: marshal payload", "error", err)
		return
	}

	sig := ""
	if wn.webhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(wn.webhookSecret))
		mac.Write(data)
		sig = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}

	// Attempt 0 is immediate; attempts 1-3 wait for the respective backoff.
	backoffs := []time.Duration{0, 2 * time.Second, 5 * time.Second, 10 * time.Second}
	for attempt, delay := range backoffs {
		if delay > 0 {
			time.Sleep(delay)
		}

		req, err := http.NewRequest(http.MethodPost, wn.webhookURL, bytes.NewReader(data))
		if err != nil {
			slog.Error("webhook: create request", "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Wallfacer-Event", "task.state_changed")
		if sig != "" {
			req.Header.Set("X-Wallfacer-Signature", sig)
		}

		resp, err := wn.client.Do(req)
		if err != nil {
			slog.Error("webhook: POST failed", "attempt", attempt+1, "error", err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return
		}
		slog.Error("webhook: non-2xx response", "attempt", attempt+1, "status", resp.StatusCode)
	}
}
