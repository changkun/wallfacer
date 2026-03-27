// Package watcher provides a reusable event loop for background goroutines
// that react to wake-channel signals and/or periodic tickers. It consolidates
// the repeated subscribe+ticker+select pattern used by autopilot watchers.
package watcher

import (
	"context"
	"time"
)

// WakeSource provides subscribe/unsubscribe for coalescing wake signals.
// Typically satisfied by *pubsub.Hub via its SubscribeWake/UnsubscribeWake methods.
type WakeSource interface {
	SubscribeWake() (id int, ch <-chan struct{})
	UnsubscribeWake(id int)
}

// Config configures a watcher event loop.
type Config struct {
	// Wake is the source of wake signals. If nil, the watcher is ticker-only.
	Wake WakeSource

	// Interval is the periodic ticker interval. Zero means no ticker
	// (wake-only mode).
	Interval time.Duration

	// SettleDelay is an optional pause after receiving a wake signal before
	// calling Action. This gives the UI time to render intermediate states.
	// Zero means no delay.
	SettleDelay time.Duration

	// Action is the function called on each wake or tick event.
	Action func(ctx context.Context)

	// Init is an optional function called once before entering the event loop.
	// Use this for startup recovery scans or initial scheduling.
	Init func(ctx context.Context)

	// Shutdown is an optional function called when the context is cancelled,
	// before the goroutine exits.
	Shutdown func()
}

// Start launches a background goroutine running the configured event loop.
// The goroutine exits when ctx is cancelled.
func Start(ctx context.Context, cfg Config) {
	go run(ctx, cfg)
}

// run implements the event loop. It subscribes to wake signals, sets up a
// ticker, runs the optional Init, then enters a select loop until ctx is done.
func run(ctx context.Context, cfg Config) {
	// Subscribe to wake channel if configured.
	var wakeCh <-chan struct{}
	if cfg.Wake != nil {
		id, ch := cfg.Wake.SubscribeWake()
		wakeCh = ch
		defer cfg.Wake.UnsubscribeWake(id)
	}

	// Set up ticker if configured. A nil tickCh (zero Interval) means the
	// select case is permanently blocked, making this a wake-only watcher.
	var tickCh <-chan time.Time
	if cfg.Interval > 0 {
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()
		tickCh = ticker.C
	}

	// Run optional init function.
	if cfg.Init != nil {
		cfg.Init(ctx)
	}

	// Event loop.
	for {
		select {
		case <-ctx.Done():
			if cfg.Shutdown != nil {
				cfg.Shutdown()
			}
			return
		case <-wakeCh:
			// Optional settle delay: pause briefly after a wake signal to let
			// burst events coalesce before running the action. The inner select
			// ensures cancellation is still respected during the delay.
			if cfg.SettleDelay > 0 {
				select {
				case <-ctx.Done():
					if cfg.Shutdown != nil {
						cfg.Shutdown()
					}
					return
				case <-time.After(cfg.SettleDelay):
				}
			}
			cfg.Action(ctx)
		case <-tickCh:
			cfg.Action(ctx)
		}
	}
}
