// Tests for subscribe.go: Subscribe, Unsubscribe, and notify.
package store

import (
	"testing"
	"time"
)

func TestSubscribe_ReceivesNotificationOnCreate(t *testing.T) {
	s := newTestStore(t)
	id, ch := s.Subscribe()
	defer s.Unsubscribe(id)

	_, _ = s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	select {
	case delta := <-ch:
		if delta.Task == nil {
			t.Error("expected non-nil task in delta")
		}
		if delta.Deleted {
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
		if delta.Task == nil || delta.Task.ID != task.ID {
			t.Errorf("expected delta for task %s, got %v", task.ID, delta.Task)
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
		if delta.Task == nil || delta.Task.ID != task.ID {
			t.Errorf("expected delete delta for task %s, got %v", task.ID, delta.Task)
		}
		if !delta.Deleted {
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
		// correct: no notification received
	}
}

func TestSubscribe_MultipleSubscribersAllNotified(t *testing.T) {
	s := newTestStore(t)
	id1, ch1 := s.Subscribe()
	id2, ch2 := s.Subscribe()
	defer s.Unsubscribe(id1)
	defer s.Unsubscribe(id2)

	_, _ = s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	for i, ch := range []<-chan SequencedDelta{ch1, ch2} {
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

	// Send many notifications without draining — must not block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
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

	// The channel buffer is 256; fire fewer than that so all are delivered.
	const n = 10
	for i := 0; i < n; i++ {
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
	for i := 0; i < 10; i++ {
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
		if delta.Deleted {
			t.Error("expected Deleted=false")
		}
		if delta.Task == nil {
			t.Fatal("expected non-nil Task")
		}
		if delta.Task.ID != task.ID {
			t.Errorf("delta task ID mismatch: got %s want %s", delta.Task.ID, task.ID)
		}
		if delta.Task.Prompt != "hello" {
			t.Errorf("expected prompt 'hello', got %q", delta.Task.Prompt)
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

	var first SequencedDelta
	select {
	case first = <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first delta")
	}
	if first.Task == nil {
		t.Fatal("expected task payload in first delta")
	}

	mutateTaskCloneForIsolation(first.Task)

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
	assertTaskMatchesSnapshot(t, replayed[0].Task, want)

	if err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	var second SequencedDelta
	select {
	case second = <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second delta")
	}
	if second.Task == nil {
		t.Fatal("expected task payload in second delta")
	}
	if second.Task.Status != TaskStatusInProgress {
		t.Fatalf("second delta status = %q, want %q", second.Task.Status, TaskStatusInProgress)
	}
	if second.Task.PromptHistory[0] != want.PromptHistory[0] {
		t.Fatalf("second delta prompt history = %q, want %q", second.Task.PromptHistory[0], want.PromptHistory[0])
	}
	if second.Task.RefineSessions[0].Messages[0].Content != want.RefineSessions[0].Messages[0].Content {
		t.Fatalf("second delta refinement message = %q, want %q", second.Task.RefineSessions[0].Messages[0].Content, want.RefineSessions[0].Messages[0].Content)
	}
	if second.Task.WorktreePaths["/repo"] != want.WorktreePaths["/repo"] {
		t.Fatalf("second delta worktree path = %q, want %q", second.Task.WorktreePaths["/repo"], want.WorktreePaths["/repo"])
	}
}

// --- SubscribeWake / UnsubscribeWake tests ---

// TestSubscribeWake_ReceivesSignal verifies that a wake subscriber receives a
// signal after a notify call.
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

// TestSubscribeWake_BurstCoalescing verifies that 100 rapid notify calls result
// in exactly 1 or 2 receives on the wake channel (burst coalescing), and that
// no notify call blocks.
func TestSubscribeWake_BurstCoalescing(t *testing.T) {
	s := newTestStore(t)
	id, ch := s.SubscribeWake()
	defer s.UnsubscribeWake(id)

	dummy := &Task{}

	// Fire 100 rapid notify calls without draining — must not block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			s.notify(dummy, false)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notify calls blocked unexpectedly")
	}

	// Drain all available signals. With a capacity-1 channel and non-blocking
	// sends, at most 1 signal should be buffered after the burst. A count of 2
	// is also acceptable in case a receive races with a concurrent send.
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

// TestUnsubscribeWake_StopsSignals verifies that no signal is received after
// UnsubscribeWake is called.
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
		// correct: no signal received
	}
}

// --- Replay buffer and sequence ID tests ---

// TestNotify_StampsMonotonicSeq verifies that each delta emitted by notify gets
// a strictly increasing Seq field.
func TestNotify_StampsMonotonicSeq(t *testing.T) {
	s := newTestStore(t)
	_, ch := s.Subscribe()
	dummy := &Task{}

	const n = 5
	for i := 0; i < n; i++ {
		s.notify(dummy, false)
	}

	prev := int64(0)
	for i := 0; i < n; i++ {
		select {
		case sd := <-ch:
			if sd.Seq <= prev {
				t.Errorf("seq %d is not strictly greater than previous %d", sd.Seq, prev)
			}
			prev = sd.Seq
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for delta %d", i)
		}
	}
}

// TestLatestDeltaSeq_StartsAtZero verifies the initial sequence is 0.
func TestLatestDeltaSeq_StartsAtZero(t *testing.T) {
	s := newTestStore(t)
	if got := s.LatestDeltaSeq(); got != 0 {
		t.Errorf("expected initial LatestDeltaSeq=0, got %d", got)
	}
}

// TestLatestDeltaSeq_IncreasesWithNotify verifies LatestDeltaSeq tracks notify calls.
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

// TestDeltasSince_EmptyBuffer returns no gap and empty slice when buffer is empty.
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

// TestDeltasSince_ReturnsAllWhenSeqIsZero verifies that DeltasSince(0) returns
// all buffered deltas when seq=0 and the buffer starts at seq=1.
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

// TestDeltasSince_ReturnsMissedDeltas verifies partial replay when seq is mid-range.
func TestDeltasSince_ReturnsMissedDeltas(t *testing.T) {
	s := newTestStore(t)
	dummy := &Task{}
	s.notify(dummy, false) // seq=1
	s.notify(dummy, false) // seq=2
	s.notify(dummy, false) // seq=3
	s.notify(dummy, false) // seq=4

	// Client has seq=2; should receive seq=3 and seq=4.
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

// TestDeltasSince_NothingNewWhenUpToDate returns empty and no gap.
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

// TestDeltasSince_GapTooOld verifies tooOld=true when the requested seq predates
// the oldest buffered delta.
func TestDeltasSince_GapTooOld(t *testing.T) {
	s := newTestStore(t)
	dummy := &Task{}

	// Simulate a very old seq by directly manipulating the replay buffer.
	// We inject entries with seq=10 and seq=11 into the buffer, leaving a gap
	// for seq=1..9 that no longer exists.
	s.replayMu.Lock()
	s.replayBuf = []SequencedDelta{
		{Seq: 10, TaskDelta: TaskDelta{Task: dummy}},
		{Seq: 11, TaskDelta: TaskDelta{Task: dummy}},
	}
	s.replayMu.Unlock()

	// Requesting seq=5 means we need deltas 6..11, but oldest=10 > 5+1=6.
	deltas, tooOld := s.DeltasSince(5)
	if !tooOld {
		t.Error("expected tooOld=true when gap predates oldest buffer entry")
	}
	if len(deltas) != 0 {
		t.Errorf("expected no deltas on gap-too-old, got %d", len(deltas))
	}
}

// TestDeltasSince_NoGapWhenOldestIsSeqPlusOne verifies the boundary: oldest == seq+1
// is NOT a gap.
func TestDeltasSince_NoGapWhenOldestIsSeqPlusOne(t *testing.T) {
	s := newTestStore(t)
	dummy := &Task{}

	// oldest=6, seq=5 → oldest (6) > seq+1 (6) is false → no gap.
	s.replayMu.Lock()
	s.replayBuf = []SequencedDelta{
		{Seq: 6, TaskDelta: TaskDelta{Task: dummy}},
		{Seq: 7, TaskDelta: TaskDelta{Task: dummy}},
	}
	s.replayMu.Unlock()

	deltas, tooOld := s.DeltasSince(5)
	if tooOld {
		t.Error("expected tooOld=false when oldest == seq+1")
	}
	if len(deltas) != 2 {
		t.Errorf("expected 2 deltas, got %d", len(deltas))
	}
}

// TestReplayBuf_BoundedToMax verifies that the replay buffer never exceeds replayBufMax.
func TestReplayBuf_BoundedToMax(t *testing.T) {
	s := newTestStore(t)
	dummy := &Task{}

	for i := 0; i < replayBufMax+10; i++ {
		s.notify(dummy, false)
	}

	s.replayMu.RLock()
	n := len(s.replayBuf)
	s.replayMu.RUnlock()

	if n > replayBufMax {
		t.Errorf("replay buffer length %d exceeds max %d", n, replayBufMax)
	}
}

// TestNotify_OverflowClosesChannel verifies that when a subscriber's buffer is
// exhausted, the 257th notify call closes the channel (ok=false on receive),
// decrements SubscriberCount to 0, and a fresh Subscribe still works.
func TestNotify_OverflowClosesChannel(t *testing.T) {
	s := newTestStore(t)
	_, ch := s.Subscribe()
	dummy := &Task{}

	// Fill the buffer (256 items) and trigger one overflow — must not block.
	const total = 257 // one beyond the 256-item buffer
	done := make(chan struct{})
	go func() {
		for i := 0; i < total; i++ {
			s.notify(dummy, false)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notify calls blocked unexpectedly on overflow")
	}

	// The channel must have been closed by the overflow eviction.
	// Drain buffered items first, then confirm the close.
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

	// The subscriber must have been removed from the store.
	if count := s.SubscriberCount(); count != 0 {
		t.Errorf("expected SubscriberCount=0 after eviction, got %d", count)
	}

	// A brand-new Subscribe must return a working open channel.
	id2, ch2 := s.Subscribe()
	defer s.Unsubscribe(id2)
	s.notify(dummy, false)
	select {
	case sd, ok := <-ch2:
		if !ok {
			t.Error("fresh subscriber channel closed unexpectedly")
		}
		if sd.Task == nil {
			t.Error("expected non-nil task in delta on fresh subscriber")
		}
	case <-time.After(time.Second):
		t.Error("fresh subscriber did not receive notification after overflow eviction")
	}
}

// TestNotify_OverflowUnsubscribeIsNoop verifies that calling Unsubscribe on an
// already-evicted subscriber ID is safe and does not panic or block.
func TestNotify_OverflowUnsubscribeIsNoop(t *testing.T) {
	s := newTestStore(t)
	id, ch := s.Subscribe()
	dummy := &Task{}

	// Overflow the channel to trigger eviction.
	for i := 0; i < 257; i++ {
		s.notify(dummy, false)
	}

	// Wait for the channel to be closed.
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
	// Calling Unsubscribe on the already-evicted ID must not panic or spin.
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

// TestListTasksAndSeq_ConsistentView verifies that the sequence number returned
// by ListTasksAndSeq matches the task state: if a task has been mutated, the seq
// returned must be >= the seq of that mutation.
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
	// seq must reflect the CreateTask notification (seq >= 1).
	if seq < 1 {
		t.Errorf("expected seq >= 1, got %d", seq)
	}

	// Update the task; the new seq must be higher.
	_ = s.UpdateTaskStatus(bg(), task.ID, "in_progress")
	_, seq2, _ := s.ListTasksAndSeq(bg(), false)
	if seq2 <= seq {
		t.Errorf("expected seq2 (%d) > seq (%d) after status update", seq2, seq)
	}
}
