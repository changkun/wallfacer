package store

// replayBufMax is the maximum number of SequencedDeltas kept in the replay buffer.
// Clients that reconnect after missing more than this many events fall back to a
// full snapshot instead of a delta replay.
const replayBufMax = 512

// SequencedDelta is a TaskDelta stamped with a monotonic sequence number.
// The Seq field is assigned by Store.notify and increases strictly with each call.
type SequencedDelta struct {
	Seq int64
	TaskDelta
}

// TaskDelta carries the payload for a single task change notification.
// Deleted is true when the task was removed; Task.ID holds the affected task's ID.
// For non-delete events, Task is a deep copy of the mutated task.
type TaskDelta struct {
	Task    *Task
	Deleted bool
}

// subscribe registers a channel that receives a SequencedDelta whenever task
// state changes. The caller must call Unsubscribe with the returned ID when done.
func (s *Store) subscribe() (int, <-chan SequencedDelta) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	id := s.nextSubID
	s.nextSubID++
	ch := make(chan SequencedDelta, 64)
	s.subscribers[id] = ch
	return id, ch
}

// Subscribe is the exported variant of subscribe for use outside the package.
func (s *Store) Subscribe() (int, <-chan SequencedDelta) {
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

// notify stamps a TaskDelta with a sequence number, appends it to the bounded
// replay buffer, and pushes it to all SSE subscribers. Non-blocking: if a
// subscriber's buffer is already full, the delta is dropped for that subscriber.
// Must be called with s.mu held (at least read-locked) so that the task pointer
// is stable while we copy it.
func (s *Store) notify(task *Task, deleted bool) {
	var td TaskDelta
	if deleted {
		// For deletes, only the ID is needed by the handler.
		td = TaskDelta{Task: &Task{ID: task.ID}, Deleted: true}
	} else {
		td = TaskDelta{Task: copyTask(task), Deleted: false}
	}

	seq := s.deltaSeq.Add(1)
	sd := SequencedDelta{Seq: seq, TaskDelta: td}

	// Append to bounded replay buffer; trim oldest entries when over capacity.
	s.replayMu.Lock()
	s.replayBuf = append(s.replayBuf, sd)
	if len(s.replayBuf) > replayBufMax {
		s.replayBuf = s.replayBuf[len(s.replayBuf)-replayBufMax:]
	}
	s.replayMu.Unlock()

	// Fan out to live subscribers.
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, ch := range s.subscribers {
		select {
		case ch <- sd:
		default:
		}
	}
}

// LatestDeltaSeq returns the sequence number of the most recently emitted delta.
// A return value of 0 means no deltas have been emitted since the store started.
func (s *Store) LatestDeltaSeq() int64 {
	return s.deltaSeq.Load()
}

// DeltasSince returns all buffered SequencedDeltas with Seq > seq.
//
// The second return value is true when the requested seq predates the oldest
// entry in the replay buffer (gap-too-old), meaning the caller must fall back
// to a full snapshot. It is false when replay is possible (including when there
// are simply no new deltas to send).
func (s *Store) DeltasSince(seq int64) ([]SequencedDelta, bool) {
	s.replayMu.RLock()
	defer s.replayMu.RUnlock()

	if len(s.replayBuf) == 0 {
		// Nothing buffered — no gap, just nothing to replay.
		return nil, false
	}

	oldest := s.replayBuf[0].Seq
	// If the oldest buffered delta is strictly newer than seq+1, then we are
	// missing at least one delta the client has not yet seen.
	if oldest > seq+1 {
		return nil, true // gap too old — caller must send a full snapshot
	}

	// Binary-search for the first entry with Seq > seq.
	lo, hi := 0, len(s.replayBuf)
	for lo < hi {
		mid := (lo + hi) / 2
		if s.replayBuf[mid].Seq <= seq {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	if lo >= len(s.replayBuf) {
		// All buffered deltas are at or before seq — nothing new.
		return nil, false
	}

	// Return a copy so the caller can use it after unlocking.
	result := make([]SequencedDelta, len(s.replayBuf)-lo)
	copy(result, s.replayBuf[lo:])
	return result, false
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
