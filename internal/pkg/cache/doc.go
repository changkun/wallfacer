// Package cache provides a thread-safe in-memory TTL cache with optional
// bounded LRU eviction.
//
// It supports generic key-value storage with per-entry expiration, permanent
// entries that bypass TTL, and an optional maximum size that evicts the oldest
// entries when exceeded. A pluggable clock interface enables deterministic
// testing without real time delays.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed by [handler] for caching
// git diff results, commits-behind counts, and other expensive computations
// that benefit from short-lived memoization. When adjusting cache TTLs, check
// the corresponding constants in [constants].
//
// # Usage
//
//	c := cache.New[string, []byte](5 * time.Minute)
//	c.Set("key", data)
//	if val, ok := c.Get("key"); ok { ... }
//	c.Invalidate("key")
package cache
