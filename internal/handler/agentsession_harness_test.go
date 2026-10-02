package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"latere.ai/x/pkg/httpjson"
)

// The chat runtime starts every turn as a subprocess through the executor
// backend. The tests in this file cover a harness that runs in-process (topos):
// it can be usable for tasks while the chat runtime has no way to launch it, so
// the config response leaves it out of the chat list and a chat turn naming it
// is refused before anything is persisted or launched.

// TestSendAgentMessage_RefusesInProcessHarness asserts a chat turn on the
// in-process topos harness answers 422 with the error envelope, and leaves the
// thread without the user's message and the runtime idle.
func TestSendAgentMessage_RefusesInProcessHarness(t *testing.T) {
	h := newAgentSessionHandlerWithThreads(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/messages",
		strings.NewReader(`{"message":"hello","harness":"topos"}`))
	h.SendAgentMessage(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	var env httpjson.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, rec.Body.String())
	}
	if env.Error.Code != "harness_unavailable_in_chat" {
		t.Errorf("code = %q, want harness_unavailable_in_chat", env.Error.Code)
	}
	if env.Error.Message != messageHarnessUnavailableInChat {
		t.Errorf("message = %q, want %q", env.Error.Message, messageHarnessUnavailableInChat)
	}
	if env.Error.Details["harness"] != "topos" {
		t.Errorf("details.harness = %v, want topos", env.Error.Details["harness"])
	}
	if h.agentSession.IsBusy() {
		t.Error("runtime is busy after a refused turn")
	}
	msgs, err := activeThreadStore(t, h.agentSession).Messages()
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("thread holds %d messages after a refused turn, want 0: %+v", len(msgs), msgs)
	}
}

// TestGetConfig_ChatSandboxesExcludeInProcessHarness asserts the config
// response lists the harnesses the chat runtime can launch: every registered
// subprocess harness, and not topos, whether or not an env file is configured.
func TestGetConfig_ChatSandboxesExcludeInProcessHarness(t *testing.T) {
	withEnv, _ := newTestHandlerWithEnv(t)
	for name, h := range map[string]*Handler{"env file": withEnv, "no env file": newTestHandler(t)} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.GetConfig(w, httptest.NewRequest(http.MethodGet, "/api/config", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
			var resp struct {
				ChatSandboxes []string `json:"chat_sandboxes"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			for _, want := range []string{"claude", "codex", "cursor", "opencode", "pi"} {
				if !slices.Contains(resp.ChatSandboxes, want) {
					t.Errorf("chat_sandboxes = %v, missing %q", resp.ChatSandboxes, want)
				}
			}
			if slices.Contains(resp.ChatSandboxes, "topos") {
				t.Errorf("chat_sandboxes = %v, want no topos", resp.ChatSandboxes)
			}
		})
	}
}
