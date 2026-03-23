// Package circuitbreaker provides two fault isolation implementations for
// protecting against cascading failures.
//
// [Breaker] is a lock-free, three-state (closed/open/half-open) circuit breaker
// using atomic operations for high-throughput paths like container launches.
// [BackoffBreaker] is a simpler mutex-based implementation with exponential
// backoff for cases where per-watcher isolation is needed. Both track consecutive
// failures and temporarily block operations when the failure threshold is exceeded.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed by [runner] (container launch
// circuit breaker to prevent rapid container crash loops) and [handler] (per-watcher
// circuit breakers for automation goroutines). When tuning thresholds, check
// WALLFACER_CONTAINER_CB_THRESHOLD in [envconfig] and related constants.
//
// # Usage
//
//	cb := circuitbreaker.New(5, 30*time.Second)
//	if cb.Allow() {
//	    err := launchContainer()
//	    if err != nil { cb.RecordFailure() } else { cb.RecordSuccess() }
//	}
package circuitbreaker
