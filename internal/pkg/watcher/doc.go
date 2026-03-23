// Package watcher provides a reusable event loop for background goroutines
// that react to wake signals and/or periodic tickers.
//
// Wallfacer has several automation watchers (auto-promote, auto-test, auto-submit,
// auto-sync, auto-push) that share the same pattern: wait for a wake signal or
// timer tick, optionally settle before acting, run an action, and clean up on
// cancellation. [Start] encapsulates this pattern with a [Config] struct,
// eliminating duplicated select/timer/context boilerplate across watchers.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed by [handler] for all
// automation watchers (tasks_autopilot.go, ideate.go). The [WakeSource]
// interface is satisfied by [pubsub.Hub] wake subscriptions. Changes to the
// event loop timing (settle delay, ticker behavior) affect all automation
// responsiveness.
//
// # Usage
//
//	watcher.Start(ctx, watcher.Config{
//	    Wake:        hub,
//	    Interval:    5 * time.Minute,
//	    SettleDelay: 2 * time.Second,
//	    Action:      func(ctx context.Context) { promoteNextTask() },
//	})
package watcher
