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
// For non-delete events, Task is a standalone clone of the mutated task.
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
	ch := make(chan SequencedDelta, 256)
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
// The channel is NOT closed here: StreamTasks is always the one calling Unsubscribe
// (via defer) after its own goroutine exits, so there is no blocked receiver to wake.
//
// Safety note: notify() may have already evicted and closed this subscriber's channel
// due to overflow. In that case, notify() deletes the ID from s.subscribers under
// s.subMu before releasing the lock. When Unsubscribe subsequently acquires s.subMu,
// s.subscribers[id] returns ok=false, so the drain loop is skipped entirely — no
// double-close and no spin-read on a closed channel.
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

// SubscribeWake registers a lightweight wake channel that receives a struct{}
// signal whenever task state changes. The channel has capacity 1 so that rapid
// bursts of notifications coalesce: once the channel is full, subsequent sends
// are dropped (the pending signal is already sufficient). The caller must call
// UnsubscribeWake with the returned ID when done.
func (s *Store) SubscribeWake() (int, <-chan struct{}) {
	s.wakeSubMu.Lock()
	defer s.wakeSubMu.Unlock()
	id := s.nextWakeSubID
	s.nextWakeSubID++
	ch := make(chan struct{}, 1)
	s.wakeSubscribers[id] = ch
	return id, ch
}

// UnsubscribeWake removes the wake subscriber and drains any buffered signal.
func (s *Store) UnsubscribeWake(id int) {
	s.wakeSubMu.Lock()
	ch, ok := s.wakeSubscribers[id]
	delete(s.wakeSubscribers, id)
	s.wakeSubMu.Unlock()
	if ok {
		select {
		case <-ch:
		default:
		}
	}
}

// notify stamps a TaskDelta with a sequence number, appends it to the bounded
// replay buffer, and pushes it to all SSE subscribers. Non-blocking: if a
// subscriber's buffer is already full the channel is closed and the subscriber
// is evicted so its SSE handler goroutine unblocks and returns, causing the
// browser's EventSource to reconnect and receive a full snapshot.
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
	s.replayBuf = append(s.replayBuf, cloneSequencedDelta(sd))
	if len(s.replayBuf) > replayBufMax {
		s.replayBuf = s.replayBuf[len(s.replayBuf)-replayBufMax:]
	}
	s.replayMu.Unlock()

	// Fan out to live subscribers. If a subscriber's buffer is full, close and
	// evict it so the SSE handler goroutine (which does `sd, ok := <-ch`) exits
	// cleanly and the browser's EventSource reconnects for a fresh snapshot.
	// Overflowed IDs are collected first, then deleted after the loop — all
	// under the same lock — to prevent concurrent subscribe/unsubscribe races.
	var overflowed []int
	s.subMu.Lock()
	for id, ch := range s.subscribers {
		select {
		case ch <- cloneSequencedDelta(sd):
		default:
			close(ch)
			overflowed = append(overflowed, id)
		}
	}
	for _, id := range overflowed {
		delete(s.subscribers, id)
	}
	s.subMu.Unlock()

	// Fan out wake signal to wake-only subscribers. The capacity-1 channel
	// coalesces bursts: if a signal is already pending, the send is a no-op.
	s.wakeSubMu.Lock()
	for _, ch := range s.wakeSubscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	s.wakeSubMu.Unlock()
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

	// Return a deep copy so callers can safely mutate replayed payloads.
	result := make([]SequencedDelta, len(s.replayBuf)-lo)
	for i := range result {
		result[i] = cloneSequencedDelta(s.replayBuf[lo+i])
	}
	return result, false
}

// copyTask returns a standalone clone of t.
func copyTask(t *Task) *Task {
	cp := deepCloneTask(t)
	return &cp
}

func cloneSequencedDelta(sd SequencedDelta) SequencedDelta {
	clone := sd
	if sd.Task != nil {
		clone.Task = copyTask(sd.Task)
	}
	return clone
}
