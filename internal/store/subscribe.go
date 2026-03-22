package store

import "changkun.de/x/wallfacer/internal/pkg/pubsub"

// SequencedDelta is a type alias for pubsub.Sequenced[TaskDelta] to minimize
// downstream churn in consumers that reference this type.
type SequencedDelta = pubsub.Sequenced[TaskDelta]

// TaskDelta carries the payload for a single task change notification.
// Deleted is true when the task was removed; Task.ID holds the affected task's ID.
// For non-delete events, Task is a standalone clone of the mutated task.
type TaskDelta struct {
	Task    *Task
	Deleted bool
}

// subscribe registers a channel that receives a SequencedDelta whenever task
// state changes. The caller must call Unsubscribe with the returned ID when done.
func (s *Store) subscribe() (int, <-chan SequencedDelta) {
	return s.hub.Subscribe()
}

// Subscribe is the exported variant of subscribe for use outside the package.
func (s *Store) Subscribe() (int, <-chan SequencedDelta) {
	return s.hub.Subscribe()
}

// SubscriberCount returns the number of currently active SSE subscribers.
func (s *Store) SubscriberCount() int {
	return s.hub.SubscriberCount()
}

// Unsubscribe removes the subscriber and drains any buffered deltas.
func (s *Store) Unsubscribe(id int) {
	s.hub.Unsubscribe(id)
}

// SubscribeWake registers a lightweight wake channel that receives a struct{}
// signal whenever task state changes. The channel has capacity 1 so that rapid
// bursts of notifications coalesce.
func (s *Store) SubscribeWake() (int, <-chan struct{}) {
	return s.hub.SubscribeWake()
}

// UnsubscribeWake removes the wake subscriber.
func (s *Store) UnsubscribeWake(id int) {
	s.hub.UnsubscribeWake(id)
}

// notify stamps a TaskDelta with a sequence number and fans out to all
// subscribers. Must be called with s.mu held (at least read-locked) so
// that the task pointer is stable while we copy it.
func (s *Store) notify(task *Task, deleted bool) {
	var td TaskDelta
	if deleted {
		td = TaskDelta{Task: &Task{ID: task.ID}, Deleted: true}
	} else {
		td = TaskDelta{Task: copyTask(task), Deleted: false}
	}
	s.hub.Publish(td)
}

// LatestDeltaSeq returns the sequence number of the most recently emitted delta.
func (s *Store) LatestDeltaSeq() int64 {
	return s.hub.LatestSeq()
}

// DeltasSince returns all buffered SequencedDeltas with Seq > seq.
// The second return value is true when the requested seq predates the oldest
// entry in the replay buffer (gap-too-old).
func (s *Store) DeltasSince(seq int64) ([]SequencedDelta, bool) {
	return s.hub.Since(seq)
}

// copyTask returns a standalone clone of t.
func copyTask(t *Task) *Task {
	cp := deepCloneTask(t)
	return &cp
}

// cloneTaskDelta deep-copies a TaskDelta for the pub/sub hub's replay buffer
// and subscriber fan-out.
func cloneTaskDelta(td TaskDelta) TaskDelta {
	clone := td
	if td.Task != nil {
		clone.Task = copyTask(td.Task)
	}
	return clone
}
