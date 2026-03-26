// Tests for subscribe.go: Subscribe, Unsubscribe, and notify.
package store

import (
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/pubsub"
)

func TestSubscribe_ReceivesNotificationOnCreate(t *testing.T) {
	s := newTestStore(t)
	id, ch := s.Subscribe()
	defer s.Unsubscribe(id)

	_, _ = s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	select {
	case delta := <-ch:
		if delta.Value.Task == nil {
			t.Error("expected non-nil task in delta")
		}
		if delta.Value.Deleted {
			t.Error("expected Deleted=false for CreateTask")
		}
	case <-time.After(time.Second):
		t.Error("expected notification after CreateTask, timed out")
	}
}

func TestSubscribe_ReceivesNotificationOnStatusUpdate(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	id, ch := s.Subscribe()
	defer s.Unsubscribe(id)

	_ = s.UpdateTaskStatus(bg(), task.ID, "in_progress")

	select {
	case delta := <-ch:
		if delta.Value.Task == nil || delta.Value.Task.ID != task.ID {
			t.Errorf("expected delta for task %s, got %v", task.ID, delta.Value.Task)
		}
	case <-time.After(time.Second):
		t.Error("expected notification after UpdateTaskStatus, timed out")
	}
}

func TestSubscribe_DeleteSendsDeletedDelta(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	id, ch := s.Subscribe()
	defer s.Unsubscribe(id)

	_ = s.DeleteTask(bg(), task.ID, "")

	select {
	case delta := <-ch:
		if delta.Value.Task == nil || delta.Value.Task.ID != task.ID {
			t.Errorf("expected delete delta for task %s, got %v", task.ID, delta.Value.Task)
		}
		if !delta.Value.Deleted {
			t.Error("expected Deleted=true for DeleteTask")
		}
	case <-time.After(time.Second):
		t.Error("expected notification after DeleteTask, timed out")
	}
}

func TestUnsubscribe_StopsNotifications(t *testing.T) {
	s := newTestStore(t)
	id, ch := s.Subscribe()
	s.Unsubscribe(id)

	_, _ = s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	select {
	case <-ch:
		t.Error("should not receive notification after unsubscribe")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestSubscribe_MultipleSubscribersAllNotified(t *testing.T) {
	s := newTestStore(t)
	id1, ch1 := s.Subscribe()
	id2, ch2 := s.Subscribe()
	defer s.Unsubscribe(id1)
	defer s.Unsubscribe(id2)

	_, _ = s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	for i, ch := range []<-chan pubsub.Sequenced[TaskDelta]{ch1, ch2} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Errorf("subscriber %d did not receive notification", i+1)
		}
	}
}

func TestNotify_NonBlocking(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.Subscribe()
	dummy := &Task{}

	done := make(chan struct{})
	go func() {
		for range 100 {
			s.notify(dummy, false)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("notify blocked unexpectedly")
	}
}

func TestNotify_BufferHoldsMultipleItems(t *testing.T) {
	s := newTestStore(t)
	_, ch := s.Subscribe()
	dummy := &Task{}

	const n = 10
	for range n {
		s.notify(dummy, false)
	}

	received := 0
	for {
		select {
		case <-ch:
			received++
		default:
			goto done
		}
	}
done:
	if received != n {
		t.Errorf("expected %d buffered notifications, got %d", n, received)
	}
}

func TestSubscribe_IDsAreUnique(t *testing.T) {
	s := newTestStore(t)
	seen := make(map[int]bool)
	for range 10 {
		id, ch := s.Subscribe()
		_ = ch
		s.Unsubscribe(id)
		if seen[id] {
			t.Errorf("duplicate subscriber ID: %d", id)
		}
		seen[id] = true
	}
}

func TestNotify_DeltaContainsCorrectTask(t *testing.T) {
	s := newTestStore(t)
	_, ch := s.Subscribe()

	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "hello", Timeout: 5})

	select {
	case delta := <-ch:
		if delta.Value.Deleted {
			t.Error("expected Deleted=false")
		}
		if delta.Value.Task == nil {
			t.Fatal("expected non-nil Task")
		}
		if delta.Value.Task.ID != task.ID {
			t.Errorf("delta task ID mismatch: got %s want %s", delta.Value.Task.ID, task.ID)
		}
		if delta.Value.Task.Prompt != "hello" {
			t.Errorf("expected prompt 'hello', got %q", delta.Value.Task.Prompt)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for delta")
	}
}

func TestSubscribe_DeltaPayloadIsIsolatedFromStoreAndReplay(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "hello", Timeout: 5})

	s.mu.Lock()
	want := setTaskCloneFixture(t, s.tasks[task.ID])
	s.mu.Unlock()

	id, ch := s.Subscribe()
	defer s.Unsubscribe(id)

	s.mu.RLock()
	s.notify(s.tasks[task.ID], false)
	s.mu.RUnlock()

	var first pubsub.Sequenced[TaskDelta]
	select {
	case first = <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first delta")
	}
	if first.Value.Task == nil {
		t.Fatal("expected task payload in first delta")
	}

	mutateTaskCloneForIsolation(first.Value.Task)

	got, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after delta mutation: %v", err)
	}
	assertTaskMatchesSnapshot(t, got, want)

	replayed, tooOld := s.DeltasSince(first.Seq - 1)
	if tooOld {
		t.Fatal("unexpected replay gap")
	}
	if len(replayed) != 1 {
		t.Fatalf("expected 1 replayed delta, got %d", len(replayed))
	}
	assertTaskMatchesSnapshot(t, replayed[0].Value.Task, want)

	if err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	var second pubsub.Sequenced[TaskDelta]
	select {
	case second = <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second delta")
	}
	if second.Value.Task == nil {
		t.Fatal("expected task payload in second delta")
	}
	if second.Value.Task.Status != TaskStatusInProgress {
		t.Fatalf("second delta status = %q, want %q", second.Value.Task.Status, TaskStatusInProgress)
	}
	if second.Value.Task.PromptHistory[0] != want.PromptHistory[0] {
		t.Fatalf("second delta prompt history = %q, want %q", second.Value.Task.PromptHistory[0], want.PromptHistory[0])
	}
	if second.Value.Task.RefineSessions[0].Messages[0].Content != want.RefineSessions[0].Messages[0].Content {
		t.Fatalf("second delta refinement message = %q, want %q", second.Value.Task.RefineSessions[0].Messages[0].Content, want.RefineSessions[0].Messages[0].Content)
	}
	if second.Value.Task.WorktreePaths["/repo"] != want.WorktreePaths["/repo"] {
		t.Fatalf("second delta worktree path = %q, want %q", second.Value.Task.WorktreePaths["/repo"], want.WorktreePaths["/repo"])
	}
}

// --- SubscribeWake / UnsubscribeWake tests ---

func TestSubscribeWake_ReceivesSignal(t *testing.T) {
	s := newTestStore(t)
	id, ch := s.SubscribeWake()
	defer s.UnsubscribeWake(id)

	dummy := &Task{}
	s.notify(dummy, false)

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Error("expected wake signal after notify, timed out")
	}
}

func TestSubscribeWake_BurstCoalescing(t *testing.T) {
	s := newTestStore(t)
	id, ch := s.SubscribeWake()
	defer s.UnsubscribeWake(id)

	dummy := &Task{}

	done := make(chan struct{})
	go func() {
		for range 100 {
			s.notify(dummy, false)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notify calls blocked unexpectedly")
	}

	received := 0
	for {
		select {
		case <-ch:
			received++
		default:
			goto drained
		}
	}
drained:
	if received < 1 || received > 2 {
		t.Errorf("expected 1 or 2 wake receives after 100 notifies (burst coalescing), got %d", received)
	}
}

func TestUnsubscribeWake_StopsSignals(t *testing.T) {
	s := newTestStore(t)
	id, ch := s.SubscribeWake()
	s.UnsubscribeWake(id)

	dummy := &Task{}
	s.notify(dummy, false)

	select {
	case <-ch:
		t.Error("should not receive wake signal after UnsubscribeWake")
	case <-time.After(20 * time.Millisecond):
	}
}

// --- Replay buffer and sequence ID tests ---

func TestNotify_StampsMonotonicSeq(t *testing.T) {
	s := newTestStore(t)
	_, ch := s.Subscribe()
	dummy := &Task{}

	const n = 5
	for range n {
		s.notify(dummy, false)
	}

	prev := int64(0)
	for range n {
		select {
		case sd := <-ch:
			if sd.Seq <= prev {
				t.Errorf("seq %d is not strictly greater than previous %d", sd.Seq, prev)
			}
			prev = sd.Seq
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for delta")
		}
	}
}

func TestLatestDeltaSeq_StartsAtZero(t *testing.T) {
	s := newTestStore(t)
	if got := s.LatestDeltaSeq(); got != 0 {
		t.Errorf("expected initial LatestDeltaSeq=0, got %d", got)
	}
}

func TestLatestDeltaSeq_IncreasesWithNotify(t *testing.T) {
	s := newTestStore(t)
	dummy := &Task{}
	s.notify(dummy, false)
	if got := s.LatestDeltaSeq(); got != 1 {
		t.Errorf("expected LatestDeltaSeq=1 after one notify, got %d", got)
	}
	s.notify(dummy, false)
	if got := s.LatestDeltaSeq(); got != 2 {
		t.Errorf("expected LatestDeltaSeq=2 after two notifies, got %d", got)
	}
}

func TestDeltasSince_EmptyBuffer(t *testing.T) {
	s := newTestStore(t)
	deltas, tooOld := s.DeltasSince(0)
	if tooOld {
		t.Error("expected tooOld=false for empty buffer")
	}
	if len(deltas) != 0 {
		t.Errorf("expected empty deltas for empty buffer, got %d", len(deltas))
	}
}

func TestDeltasSince_ReturnsAllWhenSeqIsZero(t *testing.T) {
	s := newTestStore(t)
	dummy := &Task{}
	s.notify(dummy, false) // seq=1
	s.notify(dummy, false) // seq=2
	s.notify(dummy, false) // seq=3

	deltas, tooOld := s.DeltasSince(0)
	if tooOld {
		t.Error("expected tooOld=false")
	}
	if len(deltas) != 3 {
		t.Errorf("expected 3 deltas, got %d", len(deltas))
	}
	if deltas[0].Seq != 1 || deltas[2].Seq != 3 {
		t.Errorf("unexpected seq values: %d, %d", deltas[0].Seq, deltas[2].Seq)
	}
}

func TestDeltasSince_ReturnsMissedDeltas(t *testing.T) {
	s := newTestStore(t)
	dummy := &Task{}
	s.notify(dummy, false) // seq=1
	s.notify(dummy, false) // seq=2
	s.notify(dummy, false) // seq=3
	s.notify(dummy, false) // seq=4

	deltas, tooOld := s.DeltasSince(2)
	if tooOld {
		t.Error("expected tooOld=false")
	}
	if len(deltas) != 2 {
		t.Fatalf("expected 2 deltas, got %d", len(deltas))
	}
	if deltas[0].Seq != 3 || deltas[1].Seq != 4 {
		t.Errorf("unexpected seq values: %d, %d", deltas[0].Seq, deltas[1].Seq)
	}
}

func TestDeltasSince_NothingNewWhenUpToDate(t *testing.T) {
	s := newTestStore(t)
	dummy := &Task{}
	s.notify(dummy, false) // seq=1
	s.notify(dummy, false) // seq=2

	deltas, tooOld := s.DeltasSince(2)
	if tooOld {
		t.Error("expected tooOld=false when up to date")
	}
	if len(deltas) != 0 {
		t.Errorf("expected no new deltas, got %d", len(deltas))
	}
}

func TestDeltasSince_GapTooOld(t *testing.T) {
	// Use a hub with small replay capacity to test gap detection.
	s := newTestStore(t)
	s.hub = pubsub.NewHub[TaskDelta](
		pubsub.WithReplayCapacity[TaskDelta](3),
		pubsub.WithClone(cloneTaskDelta),
	)
	dummy := &Task{}

	// Publish enough to fill and overflow the small buffer.
	for range 10 {
		s.notify(dummy, false)
	}

	// Requesting seq=1 means we need deltas 2..10, but only 3 are buffered.
	deltas, tooOld := s.DeltasSince(1)
	if !tooOld {
		t.Error("expected tooOld=true when gap predates oldest buffer entry")
	}
	if len(deltas) != 0 {
		t.Errorf("expected no deltas on gap-too-old, got %d", len(deltas))
	}
}

func TestDeltasSince_NoGapWhenOldestIsSeqPlusOne(t *testing.T) {
	// Use a hub with small replay capacity.
	s := newTestStore(t)
	s.hub = pubsub.NewHub[TaskDelta](
		pubsub.WithReplayCapacity[TaskDelta](2),
		pubsub.WithClone(cloneTaskDelta),
	)
	dummy := &Task{}

	// Publish exactly 2 items to fill the buffer.
	s.notify(dummy, false) // seq=1
	s.notify(dummy, false) // seq=2

	// oldest=1, seq=0 → oldest (1) > seq+1 (1) is false → no gap.
	deltas, tooOld := s.DeltasSince(0)
	if tooOld {
		t.Error("expected tooOld=false when oldest == seq+1")
	}
	if len(deltas) != 2 {
		t.Errorf("expected 2 deltas, got %d", len(deltas))
	}
}

func TestReplayBuf_BoundedToMax(t *testing.T) {
	// Use a hub with known small capacity to test bounding.
	const testCap = 50
	s := newTestStore(t)
	s.hub = pubsub.NewHub[TaskDelta](
		pubsub.WithReplayCapacity[TaskDelta](testCap),
		pubsub.WithClone(cloneTaskDelta),
	)
	dummy := &Task{}

	for range testCap + 10 {
		s.notify(dummy, false)
	}

	// Request from seq=0 — should get at most testCap.
	deltas, _ := s.DeltasSince(0)
	if len(deltas) > testCap {
		t.Errorf("replay returned %d deltas, expected at most %d", len(deltas), testCap)
	}
}

func TestNotify_OverflowClosesChannel(t *testing.T) {
	s := newTestStore(t)
	_, ch := s.Subscribe()
	dummy := &Task{}

	const total = 257
	done := make(chan struct{})
	go func() {
		for range total {
			s.notify(dummy, false)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notify calls blocked unexpectedly on overflow")
	}

	closed := false
	timeout := time.After(time.Second)
	for !closed {
		select {
		case _, ok := <-ch:
			if !ok {
				closed = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for channel to be closed after overflow")
		}
	}

	if count := s.SubscriberCount(); count != 0 {
		t.Errorf("expected SubscriberCount=0 after eviction, got %d", count)
	}

	id2, ch2 := s.Subscribe()
	defer s.Unsubscribe(id2)
	s.notify(dummy, false)
	select {
	case sd, ok := <-ch2:
		if !ok {
			t.Error("fresh subscriber channel closed unexpectedly")
		}
		if sd.Value.Task == nil {
			t.Error("expected non-nil task in delta on fresh subscriber")
		}
	case <-time.After(time.Second):
		t.Error("fresh subscriber did not receive notification after overflow eviction")
	}
}

func TestNotify_OverflowUnsubscribeIsNoop(t *testing.T) {
	s := newTestStore(t)
	id, ch := s.Subscribe()
	dummy := &Task{}

	for range 257 {
		s.notify(dummy, false)
	}

	timeout := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				goto evicted
			}
		case <-timeout:
			t.Fatal("timed out waiting for eviction")
		}
	}
evicted:
	done := make(chan struct{})
	go func() {
		s.Unsubscribe(id)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("Unsubscribe blocked on already-evicted subscriber")
	}
}

func TestListTasksAndSeq_ConsistentView(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "hi", Timeout: 5})

	tasks, seq, err := s.ListTasksAndSeq(bg(), false)
	if err != nil {
		t.Fatalf("ListTasksAndSeq: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("expected at least one task")
	}
	if seq < 1 {
		t.Errorf("expected seq >= 1, got %d", seq)
	}

	_ = s.UpdateTaskStatus(bg(), task.ID, "in_progress")
	_, seq2, _ := s.ListTasksAndSeq(bg(), false)
	if seq2 <= seq {
		t.Errorf("expected seq2 (%d) > seq (%d) after status update", seq2, seq)
	}
}
