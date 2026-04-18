// Package routine schedules periodic fire-and-forget callbacks keyed by
// UUID. It generalizes the singleton cronjob-style loop that used to drive
// the ideation agent: each routine owns one [time.AfterFunc] timer, arms it
// according to a [Schedule], and invokes a caller-supplied [FireFunc] when
// the timer elapses.
//
// The package is intentionally stateless from the persistence perspective —
// it does not know about tasks or stores. Callers reconcile the engine with
// external state by issuing [Engine.Register] and [Engine.Unregister] calls.
//
// # Connected packages
//
// No internal dependencies (stdlib + google/uuid only). Consumed by
// [handler] to drive user-defined routine tasks on the board. Adding new
// [Schedule] implementations (cron expressions, time-of-day) does not
// require changes here.
package routine
