// Package pubsub provides a generic fan-out pub/sub hub with bounded replay
// buffer for reconnecting subscribers.
//
// The task board UI uses Server-Sent Events to receive live updates. When the
// store mutates a task, it publishes a delta through the [Hub], which fans out
// to all connected SSE subscribers. A bounded replay buffer allows reconnecting
// clients to catch up on missed events via [Hub.Since] using monotonic sequence
// numbers. Lightweight wake-only subscribers ([Hub.SubscribeWake]) receive a
// signal without the full payload, useful for polling-style consumers.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed by [store] for task change
// notifications — the store publishes [store.TaskDelta] values, and [handler]
// subscribes to stream them as SSE. Changes to the hub's delivery guarantees
// (buffering, drop policy) directly affect UI responsiveness.
//
// # Usage
//
//	hub := pubsub.NewHub[Delta](pubsub.WithReplayCapacity[Delta](100))
//	hub.Publish(delta)
//	id, ch := hub.Subscribe()
//	defer hub.Unsubscribe(id)
//	for msg := range ch { ... }
package pubsub
