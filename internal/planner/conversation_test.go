package planner

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *ConversationStore {
	t.Helper()
	cs, err := NewConversationStore(t.TempDir(), "test-fp")
	if err != nil {
		t.Fatalf("NewConversationStore: %v", err)
	}
	return cs
}

func TestConversationStore_AppendAndRead(t *testing.T) {
	cs := newTestStore(t)

	msgs := []Message{
		{Role: "user", Content: "hello", Timestamp: time.Now().Truncate(time.Millisecond)},
		{Role: "assistant", Content: "hi there", Timestamp: time.Now().Add(time.Second).Truncate(time.Millisecond)},
		{Role: "user", Content: "break down", Timestamp: time.Now().Add(2 * time.Second).Truncate(time.Millisecond), FocusedSpec: "specs/foo.md"},
	}

	for _, m := range msgs {
		if err := cs.AppendMessage(m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	got, err := cs.Messages()
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(got))
	}

	for i, want := range msgs {
		if got[i].Role != want.Role {
			t.Errorf("msg[%d].Role = %q, want %q", i, got[i].Role, want.Role)
		}
		if got[i].Content != want.Content {
			t.Errorf("msg[%d].Content = %q, want %q", i, got[i].Content, want.Content)
		}
		if !got[i].Timestamp.Equal(want.Timestamp) {
			t.Errorf("msg[%d].Timestamp = %v, want %v", i, got[i].Timestamp, want.Timestamp)
		}
		if got[i].FocusedSpec != want.FocusedSpec {
			t.Errorf("msg[%d].FocusedSpec = %q, want %q", i, got[i].FocusedSpec, want.FocusedSpec)
		}
	}
}

func TestConversationStore_AppendConcurrent(t *testing.T) {
	cs := newTestStore(t)

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_ = cs.AppendMessage(Message{
				Role:      "user",
				Content:   "msg",
				Timestamp: time.Now(),
			})
		}()
	}
	wg.Wait()

	got, err := cs.Messages()
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(got) != n {
		t.Errorf("len(Messages) = %d, want %d", len(got), n)
	}
}

func TestConversationStore_Clear(t *testing.T) {
	cs := newTestStore(t)

	_ = cs.AppendMessage(Message{Role: "user", Content: "a", Timestamp: time.Now()})
	_ = cs.SaveSession(SessionInfo{SessionID: "s1", LastActive: time.Now()})

	if err := cs.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	got, err := cs.Messages()
	if err != nil {
		t.Fatalf("Messages after clear: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(Messages) after clear = %d, want 0", len(got))
	}

	sess, err := cs.LoadSession()
	if err != nil {
		t.Fatalf("LoadSession after clear: %v", err)
	}
	if sess.SessionID != "" {
		t.Errorf("SessionID after clear = %q, want empty", sess.SessionID)
	}
}

func TestConversationStore_SessionRoundTrip(t *testing.T) {
	cs := newTestStore(t)

	want := SessionInfo{
		SessionID:   "sess-abc123",
		LastActive:  time.Now().Truncate(time.Millisecond),
		FocusedSpec: "specs/local/foo.md",
	}

	if err := cs.SaveSession(want); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	got, err := cs.LoadSession()
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	if got.SessionID != want.SessionID {
		t.Errorf("SessionID = %q, want %q", got.SessionID, want.SessionID)
	}
	if !got.LastActive.Equal(want.LastActive) {
		t.Errorf("LastActive = %v, want %v", got.LastActive, want.LastActive)
	}
	if got.FocusedSpec != want.FocusedSpec {
		t.Errorf("FocusedSpec = %q, want %q", got.FocusedSpec, want.FocusedSpec)
	}
}

func TestConversationStore_LoadSession_Missing(t *testing.T) {
	cs := newTestStore(t)

	got, err := cs.LoadSession()
	if err != nil {
		t.Fatalf("LoadSession from empty dir: %v", err)
	}
	if got.SessionID != "" {
		t.Errorf("SessionID = %q, want empty", got.SessionID)
	}
	if !got.LastActive.IsZero() {
		t.Errorf("LastActive = %v, want zero", got.LastActive)
	}
}

func TestConversationStore_MalformedLines(t *testing.T) {
	cs := newTestStore(t)

	// Write a file with a mix of valid and invalid lines.
	content := `{"role":"user","content":"good1","timestamp":"2026-04-03T10:00:00Z"}
not valid json
{"role":"assistant","content":"good2","timestamp":"2026-04-03T10:01:00Z"}
{broken
`
	path := filepath.Join(cs.dir, messagesFile)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := cs.Messages()
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(Messages) = %d, want 2 (skipping malformed lines)", len(got))
	}
	if got[0].Content != "good1" {
		t.Errorf("msg[0].Content = %q, want %q", got[0].Content, "good1")
	}
	if got[1].Content != "good2" {
		t.Errorf("msg[1].Content = %q, want %q", got[1].Content, "good2")
	}
}

func TestConversationStore_MessagesEmpty(t *testing.T) {
	cs := newTestStore(t)

	got, err := cs.Messages()
	if err != nil {
		t.Fatalf("Messages from empty store: %v", err)
	}
	if got != nil {
		t.Errorf("Messages = %v, want nil for empty store", got)
	}
}

func TestConversationStore_ClearEmpty(t *testing.T) {
	cs := newTestStore(t)
	// Clearing an empty store should not error.
	if err := cs.Clear(); err != nil {
		t.Fatalf("Clear empty store: %v", err)
	}
}

func TestPlannerConversation(t *testing.T) {
	p := New(Config{
		Command:     "podman",
		Fingerprint: "test123",
		ConfigDir:   t.TempDir(),
	})
	if p.Conversation() == nil {
		t.Error("Conversation() = nil, want non-nil when ConfigDir is set")
	}
}

func TestPlannerConversation_NoConfigDir(t *testing.T) {
	p := New(Config{Command: "podman", Fingerprint: "test123"})
	if p.Conversation() != nil {
		t.Error("Conversation() should be nil when ConfigDir is empty")
	}
}
