package store_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
)

// TestInsertEvent_StampsActorFromContext covers the main contract:
// a ctx decorated with WithActorPrincipal produces an event whose
// ActorSub and ActorType match.
func TestInsertEvent_StampsActorFromContext(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	t.Cleanup(s.WaitCompaction)

	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt: "p", Timeout: 60,
	})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}

	ctx := store.WithActorPrincipal(context.Background(), "user-abc", store.ActorUser)
	if err := s.InsertEvent(ctx, task.ID, store.EventTypeSystem, map[string]string{"msg": "hi"}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	events, err := s.GetEvents(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	// Last event is the one we just inserted; earlier ones come from
	// CreateTaskWithOptions's own state transitions.
	last := events[len(events)-1]
	if last.ActorSub != "user-abc" {
		t.Errorf("ActorSub = %q, want user-abc", last.ActorSub)
	}
	if last.ActorType != "user" {
		t.Errorf("ActorType = %q, want user", last.ActorType)
	}
}

// TestInsertEvent_SystemActor covers background writers: runner
// goroutines and schedulers that decorate ctx with WithSystemActor
// should surface as ActorType="system", empty ActorSub.
func TestInsertEvent_SystemActor(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.WaitCompaction)

	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "p", Timeout: 60})
	if err != nil {
		t.Fatal(err)
	}

	ctx := store.WithSystemActor(context.Background())
	if err := s.InsertEvent(ctx, task.ID, store.EventTypeSystem, map[string]string{"msg": "bg"}); err != nil {
		t.Fatal(err)
	}
	events, _ := s.GetEvents(context.Background(), task.ID)
	last := events[len(events)-1]
	if last.ActorType != "system" || last.ActorSub != "" {
		t.Errorf("got actor=(%q,%q), want (\"\",\"system\")", last.ActorSub, last.ActorType)
	}
}

// TestInsertEvent_NoActorCtx covers legacy / anonymous writes: an
// unannotated ctx produces empty attribution, indistinguishable on
// disk from pre-Phase-2 records.
func TestInsertEvent_NoActorCtx(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.WaitCompaction)

	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "p", Timeout: 60})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertEvent(context.Background(), task.ID, store.EventTypeSystem, map[string]string{"msg": "anon"}); err != nil {
		t.Fatal(err)
	}
	events, _ := s.GetEvents(context.Background(), task.ID)
	last := events[len(events)-1]
	if last.ActorSub != "" || last.ActorType != "" {
		t.Errorf("expected empty attribution, got (%q,%q)", last.ActorSub, last.ActorType)
	}
}

// TestTaskEvent_LegacyRoundTrip confirms events written without the
// new fields deserialize cleanly as empty strings. The fields carry
// `omitempty` so the on-disk JSON stays byte-compatible with old
// trace files.
func TestTaskEvent_LegacyRoundTrip(t *testing.T) {
	legacy := []byte(`{"id":7,"task_id":"00000000-0000-0000-0000-000000000000","event_type":"system","data":null,"created_at":"2026-01-01T00:00:00Z"}`)
	var got store.TaskEvent
	if err := json.Unmarshal(legacy, &got); err != nil {
		t.Fatalf("legacy JSON unmarshal: %v", err)
	}
	if got.ActorSub != "" || got.ActorType != "" {
		t.Errorf("legacy event should round-trip empty, got (%q,%q)", got.ActorSub, got.ActorType)
	}
}
