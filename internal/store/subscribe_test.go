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

	s.CreateTask(bg(), "p", 5, false, "", "")

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
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")

	id, ch := s.Subscribe()
	defer s.Unsubscribe(id)

	s.UpdateTaskStatus(bg(), task.ID, "in_progress")

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
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")

	id, ch := s.Subscribe()
	defer s.Unsubscribe(id)

	s.DeleteTask(bg(), task.ID)

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

	s.CreateTask(bg(), "p", 5, false, "", "")

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

	s.CreateTask(bg(), "p", 5, false, "", "")

	for i, ch := range []<-chan TaskDelta{ch1, ch2} {
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

	// The channel buffer is 64; fire fewer than that so all are delivered.
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

	task, _ := s.CreateTask(bg(), "hello", 5, false, "", "")

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
