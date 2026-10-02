package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"latere.ai/x/pkg/httpjson"

	"latere.ai/x/wallfacer/internal/agentsession"
	"latere.ai/x/wallfacer/internal/store"
)

// chatThreadFixture is a handler whose chat runtime keeps its threads under a
// known directory, so a test can damage a thread's files on disk.
type chatThreadFixture struct {
	h         *Handler
	cs        *agentsession.ConversationStore
	threadDir string
}

func newChatThreadFixture(t *testing.T) chatThreadFixture {
	t.Helper()
	h := newTestHandler(t)
	configDir := t.TempDir()
	p := agentsession.New(agentsession.Config{Fingerprint: "test-fp", ConfigDir: configDir})
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.agentSession = p
	tm := p.Sessions()
	if tm == nil {
		t.Fatal("Sessions() = nil, want a thread manager")
	}
	id := tm.ActiveID()
	cs, err := tm.Store(id)
	if err != nil {
		t.Fatalf("Store(%s): %v", id, err)
	}
	return chatThreadFixture{
		h:         h,
		cs:        cs,
		threadDir: filepath.Join(configDir, "agent-sessions", "test-fp", "threads", id),
	}
}

// assertChatSessionError checks that rec carries the 500 envelope a failed
// read or write of the thread's session state answers with.
func assertChatSessionError(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body %q", rec.Code, rec.Body.String())
	}
	var env httpjson.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (%q)", err, rec.Body.String())
	}
	if env.Error.Code != codeChatSessionUnavailable || env.Error.Message != messageChatSessionUnavailable {
		t.Fatalf("envelope = %+v, want code %q with its message", env.Error, codeChatSessionUnavailable)
	}
	if env.Error.Details["error"] == nil || env.Error.Details["thread"] == nil {
		t.Fatalf("details = %v, want the thread and the underlying error", env.Error.Details)
	}
}

// TestSendAgentMessage_UnreadableSessionIsSurfaced verifies that a thread
// whose session state cannot be read refuses the message with a 500 before
// anything is stored, instead of treating the thread as unpinned and running
// the turn.
func TestSendAgentMessage_UnreadableSessionIsSurfaced(t *testing.T) {
	f := newChatThreadFixture(t)
	if err := os.WriteFile(filepath.Join(f.threadDir, "session.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/messages", strings.NewReader(`{"message":"hello"}`))
	f.h.SendAgentMessage(rec, req)

	assertChatSessionError(t, rec)
	msgs, err := f.cs.Messages()
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("messages = %+v, want none stored for a refused turn", msgs)
	}
}

// TestSendAgentMessage_UnwritableModePinIsSurfaced verifies that a failed
// write of the thread's mode pin refuses the message with a 500 before the
// message is stored, instead of running the turn with the pin lost.
func TestSendAgentMessage_UnwritableModePinIsSurfaced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not stop file creation on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f := newChatThreadFixture(t)
	task, err := f.h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "refine me", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}
	// An earlier message leaves messages.jsonl in place, so only the pin's
	// new file needs the directory to be writable.
	if err := f.cs.AppendMessage(agentsession.Message{Role: "user", Content: "earlier", Timestamp: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.threadDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(f.threadDir, 0o755); err != nil {
			t.Errorf("restore thread dir mode: %v", err)
		}
	})

	rec := httptest.NewRecorder()
	body := `{"message":"tighten the prompt","focused_task":"` + task.ID.String() + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/agent/messages", strings.NewReader(body))
	f.h.SendAgentMessage(rec, req)

	assertChatSessionError(t, rec)
	msgs, err := f.cs.Messages()
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "earlier" {
		t.Fatalf("messages = %+v, want only the earlier message", msgs)
	}
}
