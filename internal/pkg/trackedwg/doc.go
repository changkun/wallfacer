// Package trackedwg provides a sync.WaitGroup wrapper that tracks in-flight
// goroutine labels for observability.
//
// During graceful shutdown, the server needs to know which background operations
// are still running. [WaitGroup] extends sync.WaitGroup with string labels so
// that [WaitGroup.Pending] can report what is blocking shutdown. Labels are
// formatted as "label×N" when multiple goroutines share the same label. The
// [WaitGroup.Go] method provides a convenient shorthand for launching a tracked
// goroutine.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed by [runner] for tracking
// background goroutines (title generation, oversight, sync operations) so the
// server can report pending work during shutdown and the debug/runtime endpoint.
//
// # Usage
//
//	var wg trackedwg.WaitGroup
//	wg.Go("title-gen", func() { generateTitle(taskID) })
//	wg.Go("oversight", func() { generateOversight(taskID) })
//	pending := wg.Pending() // ["oversight", "title-gen"]
//	wg.Wait()
package trackedwg
