// Package constants provides consolidated tunable system parameters used
// throughout wallfacer.
//
// Rather than scattering magic numbers across packages, all timeouts, polling
// intervals, retry counts, size limits, concurrency caps, cache TTLs, and
// pagination defaults are defined here. This makes it easy to audit and adjust
// system behavior from a single location. Some values are declared as variables
// (not constants) to allow test overrides.
//
// # Connected packages
//
// Consumed by nearly every internal package: [runner], [handler], [store], [cli],
// and [logger]. Changing a constant here has system-wide impact — verify all
// call sites before modifying a value. grep for the constant name to find all
// consumers.
//
// # Usage
//
//	timeout := constants.DefaultTaskTimeout
//	ticker := time.NewTicker(constants.AutoPromoteInterval)
package constants
