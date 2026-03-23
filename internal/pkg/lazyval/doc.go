// Package lazyval provides an atomic cached value with lazy loading and
// explicit invalidation.
//
// Some configuration values are expensive to compute but rarely change. [Value]
// loads the value on first access via a user-supplied function, caches it, and
// serves subsequent reads without recomputation. Calling [Value.Invalidate]
// clears the cache so the next [Value.Get] recomputes. This replaces scattered
// atomic.Int32 + parse patterns with a type-safe generic wrapper.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed by [handler] for caching
// parsed configuration values that are read on every request but updated
// infrequently.
//
// # Usage
//
//	v := lazyval.New(func() int { return expensiveCompute() })
//	val := v.Get()       // loads on first call, cached after
//	v.Invalidate()       // next Get() will recompute
package lazyval
