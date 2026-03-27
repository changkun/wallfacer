// Package pubsub provides a generic fan-out pub/sub hub with bounded replay
// for reconnecting subscribers.
package pubsub

import (
	"sync"
	"sync/atomic"
)

const (
	// DefaultReplayCapacity is the replay buffer size for the pub/sub hub.
	DefaultReplayCapacity = 512

	// DefaultChannelSize is the per-subscriber channel buffer size.
	DefaultChannelSize = 256
)

// Sequenced wraps a value with a monotonic sequence number assigned by the hub.
type Sequenced[T any] struct {
	Seq   int64
	Value T
}

// Hub is a fan-out pub/sub hub. Published values are sent to all active
// subscribers. A bounded replay buffer allows reconnecting clients to catch up
// on missed events via [Since].
type Hub[T any] struct {
	deltaSeq atomic.Int64 // monotonically increasing sequence counter

	// Replay buffer: bounded ring of recent messages for reconnecting clients.
	// Protected by replayMu (RWMutex: writers append, readers call Since).
	replayMu  sync.RWMutex
	replayBuf []Sequenced[T]
	replayCap int

	// Full subscribers: each gets a buffered channel receiving every published message.
	subMu       sync.Mutex
	subscribers map[int]chan Sequenced[T]
	nextSubID   int
	channelSize int

	// Wake subscribers: lightweight capacity-1 channels that coalesce bursts
	// into a single signal, useful for polling-style consumers.
	wakeSubMu       sync.Mutex
	wakeSubscribers map[int]chan struct{}
	nextWakeSubID   int

	clone func(T) T // optional deep-copy function for value isolation
}

// Option configures a [Hub].
type Option[T any] func(*Hub[T])

// WithReplayCapacity sets the replay buffer size (default: 512).
func WithReplayCapacity[T any](n int) Option[T] {
	return func(h *Hub[T]) { h.replayCap = n }
}

// WithChannelSize sets the per-subscriber channel buffer (default: 256).
func WithChannelSize[T any](n int) Option[T] {
	return func(h *Hub[T]) { h.channelSize = n }
}

// WithClone sets a function to deep-copy values before sending to subscribers
// and before storing in the replay buffer. If nil (default), values are shared.
func WithClone[T any](fn func(T) T) Option[T] {
	return func(h *Hub[T]) { h.clone = fn }
}

// NewHub creates a Hub with the given options.
func NewHub[T any](opts ...Option[T]) *Hub[T] {
	h := &Hub[T]{
		replayCap:       DefaultReplayCapacity,
		channelSize:     DefaultChannelSize,
		subscribers:     make(map[int]chan Sequenced[T]),
		wakeSubscribers: make(map[int]chan struct{}),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// cloneValue returns a deep copy of v if a clone function was configured,
// otherwise returns v as-is (shared reference).
func (h *Hub[T]) cloneValue(v T) T {
	if h.clone != nil {
		return h.clone(v)
	}
	return v
}

// Publish assigns a monotonic sequence number to value, appends it to the
// replay buffer, and fans out to all subscribers. Overflowed subscribers have
// their channel closed and are evicted.
func (h *Hub[T]) Publish(value T) {
	seq := h.deltaSeq.Add(1)
	sd := Sequenced[T]{Seq: seq, Value: h.cloneValue(value)}

	// Append to bounded replay buffer.
	h.replayMu.Lock()
	h.replayBuf = append(h.replayBuf, Sequenced[T]{Seq: seq, Value: h.cloneValue(value)})
	if len(h.replayBuf) > h.replayCap {
		h.replayBuf = h.replayBuf[len(h.replayBuf)-h.replayCap:]
	}
	h.replayMu.Unlock()

	// Fan out to live subscribers.
	// Fan out to live subscribers. Non-blocking send: if a subscriber's
	// channel is full, close it and mark for eviction to prevent a slow
	// consumer from blocking all publishers.
	var overflowed []int
	h.subMu.Lock()
	for id, ch := range h.subscribers {
		select {
		case ch <- Sequenced[T]{Seq: sd.Seq, Value: h.cloneValue(sd.Value)}:
		default:
			close(ch)
			overflowed = append(overflowed, id)
		}
	}
	// Remove evicted subscribers in a second pass to avoid mutating the map
	// during iteration.
	for _, id := range overflowed {
		delete(h.subscribers, id)
	}
	h.subMu.Unlock()

	// Fan out wake signal. Non-blocking send into capacity-1 channels
	// naturally coalesces burst notifications: if a signal is already
	// pending the new one is dropped, which is the desired behavior.
	h.wakeSubMu.Lock()
	for _, ch := range h.wakeSubscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	h.wakeSubMu.Unlock()
}

// Subscribe registers a channel that receives a [Sequenced] value on each
// [Publish]. The caller must call [Unsubscribe] with the returned ID when done.
func (h *Hub[T]) Subscribe() (int, <-chan Sequenced[T]) {
	h.subMu.Lock()
	defer h.subMu.Unlock()
	id := h.nextSubID
	h.nextSubID++
	ch := make(chan Sequenced[T], h.channelSize)
	h.subscribers[id] = ch
	return id, ch
}

// SubscribeWake registers a lightweight capacity-1 wake channel that coalesces
// burst notifications. The caller must call [UnsubscribeWake] when done.
func (h *Hub[T]) SubscribeWake() (int, <-chan struct{}) {
	h.wakeSubMu.Lock()
	defer h.wakeSubMu.Unlock()
	id := h.nextWakeSubID
	h.nextWakeSubID++
	ch := make(chan struct{}, 1)
	h.wakeSubscribers[id] = ch
	return id, ch
}

// Unsubscribe removes a subscriber and drains any buffered items.
func (h *Hub[T]) Unsubscribe(id int) {
	h.subMu.Lock()
	ch, ok := h.subscribers[id]
	delete(h.subscribers, id)
	h.subMu.Unlock()
	if ok {
		for {
			select {
			case <-ch:
			default:
				return
			}
		}
	}
}

// UnsubscribeWake removes a wake subscriber and drains any buffered signal.
func (h *Hub[T]) UnsubscribeWake(id int) {
	h.wakeSubMu.Lock()
	ch, ok := h.wakeSubscribers[id]
	delete(h.wakeSubscribers, id)
	h.wakeSubMu.Unlock()
	if ok {
		select {
		case <-ch:
		default:
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (h *Hub[T]) SubscriberCount() int {
	h.subMu.Lock()
	defer h.subMu.Unlock()
	return len(h.subscribers)
}

// LatestSeq returns the most recent sequence number (0 if nothing published).
func (h *Hub[T]) LatestSeq() int64 {
	return h.deltaSeq.Load()
}

// Since returns all buffered entries with Seq > seq. The bool is true when
// there is a gap (caller must fall back to a full snapshot).
func (h *Hub[T]) Since(seq int64) ([]Sequenced[T], bool) {
	h.replayMu.RLock()
	defer h.replayMu.RUnlock()

	if len(h.replayBuf) == 0 {
		return nil, false
	}

	oldest := h.replayBuf[0].Seq
	if oldest > seq+1 {
		return nil, true // gap too old
	}

	// Binary search for first entry with Seq > seq.
	lo, hi := 0, len(h.replayBuf)
	for lo < hi {
		mid := (lo + hi) / 2
		if h.replayBuf[mid].Seq <= seq {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	if lo >= len(h.replayBuf) {
		return nil, false
	}

	result := make([]Sequenced[T], len(h.replayBuf)-lo)
	for i := range result {
		src := h.replayBuf[lo+i]
		result[i] = Sequenced[T]{Seq: src.Seq, Value: h.cloneValue(src.Value)}
	}
	return result, false
}
