package store

// TaskDelta carries the payload for a single task change notification.
// Deleted is true when the task was removed; Task.ID holds the affected task's ID.
// For non-delete events, Task is a deep copy of the mutated task.
type TaskDelta struct {
	Task    *Task
	Deleted bool
}

// subscribe registers a channel that receives a TaskDelta whenever task state changes.
// The caller must call Unsubscribe with the returned ID when done.
func (s *Store) subscribe() (int, <-chan TaskDelta) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	id := s.nextSubID
	s.nextSubID++
	ch := make(chan TaskDelta, 64)
	s.subscribers[id] = ch
	return id, ch
}

// Subscribe is the exported variant of subscribe for use outside the package.
func (s *Store) Subscribe() (int, <-chan TaskDelta) {
	return s.subscribe()
}

// SubscriberCount returns the number of currently active SSE subscribers.
// It is safe to call concurrently.
func (s *Store) SubscriberCount() int {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	return len(s.subscribers)
}

// Unsubscribe removes the subscriber and drains any buffered deltas to free memory.
// The channel is NOT closed: StreamTasks is always the one calling Unsubscribe (via
// defer) after its own goroutine exits, so there is no blocked receiver to wake.
func (s *Store) Unsubscribe(id int) {
	s.subMu.Lock()
	ch, ok := s.subscribers[id]
	delete(s.subscribers, id)
	s.subMu.Unlock()
	if ok {
		// After removal from the map no new sends will reach ch.
		// Drain any items that were buffered before removal.
		for {
			select {
			case <-ch:
			default:
				return
			}
		}
	}
}

// notify pushes a TaskDelta to all SSE subscribers. Non-blocking: if a subscriber's
// buffer is already full, the delta is dropped for that subscriber.
// Must be called with s.mu held (at least read-locked) so that the task pointer
// is stable while we copy it.
func (s *Store) notify(task *Task, deleted bool) {
	var delta TaskDelta
	if deleted {
		// For deletes, only the ID is needed by the handler.
		delta = TaskDelta{Task: &Task{ID: task.ID}, Deleted: true}
	} else {
		delta = TaskDelta{Task: copyTask(task), Deleted: false}
	}
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, ch := range s.subscribers {
		select {
		case ch <- delta:
		default:
		}
	}
}

// copyTask returns a shallow copy of t with pointer fields deep-copied.
func copyTask(t *Task) *Task {
	cp := *t
	if t.CurrentRefinement != nil {
		jobCopy := *t.CurrentRefinement
		cp.CurrentRefinement = &jobCopy
	}
	return &cp
}
