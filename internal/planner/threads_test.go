package planner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestThreadManager_CreateRenameArchive(t *testing.T) {
	tm, err := NewThreadManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewThreadManager: %v", err)
	}
	// Fresh load seeds a default "Chat 1" thread.
	threads := tm.List(false)
	if len(threads) != 1 || threads[0].Name != "Chat 1" {
		t.Fatalf("expected default Chat 1, got %+v", threads)
	}
	defaultID := threads[0].ID

	meta, err := tm.Create("")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if meta.Name != "Chat 2" {
		t.Errorf("expected auto-incremented name, got %q", meta.Name)
	}

	if err := tm.Rename(meta.ID, "Auth refactor"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	got, _ := tm.Meta(meta.ID)
	if got.Name != "Auth refactor" {
		t.Errorf("after rename: %q, want %q", got.Name, "Auth refactor")
	}

	if err := tm.Archive(meta.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	visible := tm.List(false)
	if len(visible) != 1 || visible[0].ID != defaultID {
		t.Errorf("after archive, visible = %+v; want only default thread", visible)
	}
	all := tm.List(true)
	if len(all) != 2 {
		t.Errorf("List(includeArchived=true) len = %d, want 2", len(all))
	}
	if err := tm.Unarchive(meta.ID); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if len(tm.List(false)) != 2 {
		t.Errorf("after unarchive, visible = %+v; want 2", tm.List(false))
	}
}

func TestThreadManager_SessionIsolation(t *testing.T) {
	tm, err := NewThreadManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewThreadManager: %v", err)
	}
	a := tm.List(false)[0].ID
	b, _ := tm.Create("B")
	sa, err := tm.Store(a)
	if err != nil {
		t.Fatalf("Store(a): %v", err)
	}
	sb, err := tm.Store(b.ID)
	if err != nil {
		t.Fatalf("Store(b): %v", err)
	}
	if err := sa.SaveSession(SessionInfo{SessionID: "sess-a"}); err != nil {
		t.Fatalf("SaveSession a: %v", err)
	}
	if err := sb.SaveSession(SessionInfo{SessionID: "sess-b"}); err != nil {
		t.Fatalf("SaveSession b: %v", err)
	}
	ga, _ := sa.LoadSession()
	gb, _ := sb.LoadSession()
	if ga.SessionID != "sess-a" || gb.SessionID != "sess-b" {
		t.Errorf("session isolation broken: a=%q b=%q", ga.SessionID, gb.SessionID)
	}
}

func TestThreadManager_ActiveFallsBackWhenArchived(t *testing.T) {
	tm, err := NewThreadManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewThreadManager: %v", err)
	}
	first := tm.ActiveID()
	second, _ := tm.Create("Second")
	// Archive the active thread — manager should pick the next available
	// non-archived thread as the new active.
	if err := tm.Archive(first); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if tm.ActiveID() != second.ID {
		t.Errorf("active after archive = %q, want %q", tm.ActiveID(), second.ID)
	}
}

func TestThreadManager_Migration_FromLegacyLayout(t *testing.T) {
	root := t.TempDir()
	// Seed a legacy single-thread layout.
	legacyMsg := filepath.Join(root, messagesFile)
	if err := os.WriteFile(legacyMsg,
		[]byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacySess := filepath.Join(root, sessionFile)
	payload, _ := json.Marshal(SessionInfo{SessionID: "old-sess"})
	if err := os.WriteFile(legacySess, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	tm, err := NewThreadManager(root)
	if err != nil {
		t.Fatalf("NewThreadManager: %v", err)
	}
	threads := tm.List(false)
	if len(threads) != 1 {
		t.Fatalf("expected 1 migrated thread, got %d", len(threads))
	}
	id := threads[0].ID

	// Legacy files should be gone (migrated to threads/<id>/).
	if _, err := os.Stat(legacyMsg); !os.IsNotExist(err) {
		t.Errorf("legacy messages.jsonl should be removed: err=%v", err)
	}
	if _, err := os.Stat(legacySess); !os.IsNotExist(err) {
		t.Errorf("legacy session.json should be removed: err=%v", err)
	}

	// Migrated content should be intact.
	s, err := tm.Store(id)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	msgs, _ := s.Messages()
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Errorf("migrated messages = %+v, want one user/hello", msgs)
	}
	sess, _ := s.LoadSession()
	if sess.SessionID != "old-sess" {
		t.Errorf("migrated session = %q, want old-sess", sess.SessionID)
	}
}

// TestThreadManager_Migration_CrashAfterCopy simulates a crash between
// the copy step and the manifest write step: the copies exist under
// threads/<id>/ but threads.json does not. On next load the manager
// should start fresh (treating the originals as authoritative) rather
// than leave a half-migrated state.
func TestThreadManager_Migration_CrashAfterCopy(t *testing.T) {
	root := t.TempDir()
	// Set up legacy state.
	if err := os.WriteFile(filepath.Join(root, messagesFile),
		[]byte(`{"role":"user","content":"hi"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pre-plant a leftover half-migrated thread dir with a different id.
	// Without threads.json, the manager must ignore it and run a fresh
	// migration (idempotent re-copy).
	leftover := filepath.Join(root, threadsSubdir, "leftover-id")
	if err := os.MkdirAll(leftover, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(leftover, messagesFile),
		[]byte("stale\n"), 0o644)

	tm, err := NewThreadManager(root)
	if err != nil {
		t.Fatalf("NewThreadManager: %v", err)
	}
	threads := tm.List(false)
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread after recovery, got %d", len(threads))
	}
	// The "leftover" id is NOT in the manifest — recovery used a new id.
	if threads[0].ID == "leftover-id" {
		t.Errorf("manager adopted stale leftover-id as the migrated thread")
	}
	// The chosen thread has the migrated message, not the stale one.
	s, _ := tm.Store(threads[0].ID)
	msgs, _ := s.Messages()
	if len(msgs) != 1 || msgs[0].Content != "hi" {
		t.Errorf("migrated content = %+v, want the original hi message", msgs)
	}
}

// TestThreadManager_Migration_CrashAfterManifest simulates a crash
// between the manifest write and the delete step: threads.json already
// points to the migrated copies, but the originals still exist in root.
// Reloading must not duplicate threads; it should silently clean up the
// legacy files.
func TestThreadManager_Migration_CrashAfterManifest(t *testing.T) {
	root := t.TempDir()
	// First pass: full migration, originals removed.
	if err := os.WriteFile(filepath.Join(root, messagesFile),
		[]byte(`{"role":"user","content":"x"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm1, err := NewThreadManager(root)
	if err != nil {
		t.Fatal(err)
	}
	firstID := tm1.List(false)[0].ID

	// Re-plant an original as if the delete step had crashed.
	if err := os.WriteFile(filepath.Join(root, messagesFile),
		[]byte(`{"role":"user","content":"leftover"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reload. Manager should keep the single existing thread and clean
	// up the stray legacy file.
	tm2, err := NewThreadManager(root)
	if err != nil {
		t.Fatal(err)
	}
	threads := tm2.List(false)
	if len(threads) != 1 || threads[0].ID != firstID {
		t.Errorf("expected single preserved thread %q, got %+v", firstID, threads)
	}
	if _, err := os.Stat(filepath.Join(root, messagesFile)); !os.IsNotExist(err) {
		t.Errorf("stale legacy messages.jsonl should have been cleaned: err=%v", err)
	}
}

func TestThreadManager_SetActiveID_RejectsArchived(t *testing.T) {
	tm, err := NewThreadManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := tm.List(false)[0].ID
	b, _ := tm.Create("B")
	if err := tm.Archive(b.ID); err != nil {
		t.Fatal(err)
	}
	if err := tm.SetActiveID(b.ID); err == nil {
		t.Errorf("SetActiveID(archived) should return an error")
	}
	if tm.ActiveID() != a {
		t.Errorf("active should be unchanged: %q", tm.ActiveID())
	}
}

func TestThreadManager_Rename_UnknownID(t *testing.T) {
	tm, _ := NewThreadManager(t.TempDir())
	if err := tm.Rename("unknown-id", "foo"); err == nil {
		t.Error("Rename(unknown) should error")
	}
}

func TestParseChatN(t *testing.T) {
	cases := []struct {
		in string
		n  int
		ok bool
	}{
		{"Chat 1", 1, true},
		{"Chat 42", 42, true},
		{"Chat ", 0, false},
		{"Chat abc", 0, false},
		{"ChatZ", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		n, ok := parseChatN(c.in)
		if n != c.n || ok != c.ok {
			t.Errorf("parseChatN(%q) = (%d, %v); want (%d, %v)", c.in, n, ok, c.n, c.ok)
		}
	}
}
