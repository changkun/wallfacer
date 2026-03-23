package watcher

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockWakeSource is a test WakeSource backed by a channel.
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
