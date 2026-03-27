package watcher

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockWakeSource is a test WakeSource backed by a buffered channel.
// It tracks subscribe/unsubscribe call counts for assertions.
type mockWakeSource struct {
	ch           chan struct{}
	mu           sync.Mutex
	subscribed   int
	unsubscribed int
}

func newMockWakeSource() *mockWakeSource {
	return &mockWakeSource{ch: make(chan struct{}, 1)}
}

func (m *mockWakeSource) SubscribeWake() (int, <-chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribed++
	return m.subscribed, m.ch
}

func (m *mockWakeSource) UnsubscribeWake(_ int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unsubscribed++
}

func (m *mockWakeSource) wake() {
	select {
	case m.ch <- struct{}{}:
	default:
	}
}

// TestWatcher_WakeOnly verifies that a wake signal triggers the Action callback
// and that cancellation causes the watcher to unsubscribe.
func TestWatcher_WakeOnly(t *testing.T) {
	ws := newMockWakeSource()
	var calls atomic.Int32

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	Start(ctx, Config{
		Wake: ws,
		Action: func(_ context.Context) {
			calls.Add(1)
		},
	})

	ws.wake()
	waitFor(t, func() bool { return calls.Load() >= 1 })

	cancel()
	waitFor(t, func() bool {
		ws.mu.Lock()
		defer ws.mu.Unlock()
		return ws.unsubscribed > 0
	})
}

// TestWatcher_TickerOnly verifies that the periodic ticker fires the Action
// callback without any wake source configured.
func TestWatcher_TickerOnly(t *testing.T) {
	var calls atomic.Int32

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	Start(ctx, Config{
		Interval: 5 * time.Millisecond,
		Action: func(_ context.Context) {
			calls.Add(1)
		},
	})

	waitFor(t, func() bool { return calls.Load() >= 2 })
	cancel()
}

// TestWatcher_WakeAndTicker verifies that both wake signals and ticker ticks
// independently trigger the Action callback.
func TestWatcher_WakeAndTicker(t *testing.T) {
	ws := newMockWakeSource()
	var calls atomic.Int32

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	Start(ctx, Config{
		Wake:     ws,
		Interval: 5 * time.Millisecond,
		Action: func(_ context.Context) {
			calls.Add(1)
		},
	})

	// Wake triggers action.
	ws.wake()
	waitFor(t, func() bool { return calls.Load() >= 1 })

	// Ticker also triggers action.
	waitFor(t, func() bool { return calls.Load() >= 3 })
	cancel()
}

// TestWatcher_SettleDelay verifies that the Action is not called until the
// settle delay has elapsed after a wake signal.
func TestWatcher_SettleDelay(t *testing.T) {
	ws := newMockWakeSource()
	var actionTime atomic.Int64

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	wakeTime := time.Now()
	Start(ctx, Config{
		Wake:        ws,
		SettleDelay: 20 * time.Millisecond,
		Action: func(_ context.Context) {
			actionTime.Store(time.Now().UnixNano())
		},
	})

	ws.wake()
	waitFor(t, func() bool { return actionTime.Load() > 0 })

	elapsed := time.Duration(actionTime.Load() - wakeTime.UnixNano())
	if elapsed < 15*time.Millisecond {
		t.Errorf("action fired too early: %v after wake, expected >= 20ms", elapsed)
	}
	cancel()
}

// TestWatcher_Init verifies that the Init callback runs before any Action
// callback fires.
func TestWatcher_Init(t *testing.T) {
	var initCalled atomic.Bool
	var actionCalled atomic.Bool

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	Start(ctx, Config{
		Interval: 5 * time.Millisecond,
		Init: func(_ context.Context) {
			if actionCalled.Load() {
				t.Error("Init called after Action")
			}
			initCalled.Store(true)
		},
		Action: func(_ context.Context) {
			actionCalled.Store(true)
		},
	})

	waitFor(t, actionCalled.Load)
	if !initCalled.Load() {
		t.Error("Init was never called")
	}
	cancel()
}

// TestWatcher_Shutdown verifies that the Shutdown callback is invoked when the
// context is cancelled.
func TestWatcher_Shutdown(t *testing.T) {
	var shutdownCalled atomic.Bool

	ctx, cancel := context.WithCancel(t.Context())

	Start(ctx, Config{
		Interval: time.Hour, // won't fire
		Action:   func(_ context.Context) {},
		Shutdown: func() {
			shutdownCalled.Store(true)
		},
	})

	cancel()
	waitFor(t, shutdownCalled.Load)
}

// TestWatcher_ContextCancel verifies that starting with an already-cancelled
// context exits promptly and unsubscribes from the wake source.
func TestWatcher_ContextCancel(t *testing.T) {
	ws := newMockWakeSource()

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	Start(ctx, Config{
		Wake:     ws,
		Interval: time.Hour,
		Action:   func(_ context.Context) {},
	})

	// Should unsubscribe promptly.
	waitFor(t, func() bool {
		ws.mu.Lock()
		defer ws.mu.Unlock()
		return ws.unsubscribed > 0
	})
}

// TestWatcher_NilWakeSource verifies that a nil Wake source (ticker-only mode)
// works correctly without panicking.
func TestWatcher_NilWakeSource(t *testing.T) {
	var calls atomic.Int32

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	Start(ctx, Config{
		Interval: 5 * time.Millisecond,
		Action: func(_ context.Context) {
			calls.Add(1)
		},
	})

	waitFor(t, func() bool { return calls.Load() >= 2 })
	cancel()
}

// TestWatcher_SettleDelayCancelledDuringSettle verifies that cancelling the
// context during the settle delay prevents the Action from firing and triggers
// the Shutdown callback instead.
func TestWatcher_SettleDelayCancelledDuringSettle(t *testing.T) {
	ws := newMockWakeSource()
	var actionCalled atomic.Bool
	var shutdownCalled atomic.Bool

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	Start(ctx, Config{
		Wake:        ws,
		SettleDelay: 500 * time.Millisecond, // long settle
		Action: func(_ context.Context) {
			actionCalled.Store(true)
		},
		Shutdown: func() {
			shutdownCalled.Store(true)
		},
	})

	// Send wake then cancel during settle delay.
	ws.wake()
	time.Sleep(10 * time.Millisecond)
	cancel()

	waitFor(t, shutdownCalled.Load)
	// Action should NOT have been called since we cancelled during settle.
	if actionCalled.Load() {
		t.Error("action should not fire when cancelled during settle delay")
	}
}

// waitFor polls pred at short intervals, failing the test after a timeout.
func waitFor(t *testing.T, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}
