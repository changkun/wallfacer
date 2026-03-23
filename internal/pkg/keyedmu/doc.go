// Package keyedmu provides a per-key mutex map for fine-grained locking.
//
// When multiple goroutines operate on different tasks concurrently, a single
// global mutex creates unnecessary contention. This package provides [Map] which
// maintains a separate sync.Mutex per key, allowing operations on different keys
// to proceed in parallel while serializing operations on the same key. The
// zero value is ready to use.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed by [runner] for per-task
// locking during execution, commit, and worktree operations. This ensures that
// concurrent operations on different tasks do not interfere while preventing
// races on the same task.
//
// # Usage
//
//	var mu keyedmu.Map[string]
//	mu.Lock(taskID)
//	defer mu.Unlock(taskID)
//	// exclusive access for this taskID
package keyedmu
