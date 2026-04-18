package routine

import "time"

// SystemClock is the production [Clock] implementation. It delegates
// directly to the stdlib [time] package.
type SystemClock struct{}

// Now returns time.Now.
func (SystemClock) Now() time.Time { return time.Now() }

// AfterFunc wraps [time.AfterFunc] so the returned *time.Timer satisfies
// the [Timer] interface.
func (SystemClock) AfterFunc(d time.Duration, f func()) Timer {
	return time.AfterFunc(d, f)
}
