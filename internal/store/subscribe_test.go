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

	s.CreateTask(bg(), "p", 5)

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Error("expected notification after CreateTask, timed out")
	}
}

func TestSubscribe_ReceivesNotificationOnStatusUpdate(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)

	id, ch := s.Subscribe()
	defer s.Unsubscribe(id)

	s.UpdateTaskStatus(bg(), task.ID, "in_progress")

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Error("expected notification after UpdateTaskStatus, timed out")
	}
}

func TestUnsubscribe_StopsNotifications(t *testing.T) {
	s := newTestStore(t)
	id, ch := s.Subscribe()
	s.Unsubscribe(id)

	s.CreateTask(bg(), "p", 5)

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

	s.CreateTask(bg(), "p", 5)

	for i, ch := range []<-chan struct{}{ch1, ch2} {
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

	// Send many notifications without draining — must not block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			s.notify()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("notify blocked unexpectedly")
	}
}

func TestNotify_BufferHoldsOneItem(t *testing.T) {
	s := newTestStore(t)
	_, ch := s.Subscribe()

	// Fire many notifies without consuming.
	for i := 0; i < 10; i++ {
		s.notify()
	}

	// Exactly one item should be buffered.
	select {
	case <-ch:
	default:
		t.Error("expected at least one buffered notification")
	}

	// No further items.
	select {
	case <-ch:
		t.Error("expected at most one buffered notification")
	default:
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
