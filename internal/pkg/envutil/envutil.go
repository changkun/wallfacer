// Package envutil provides helpers for reading typed values from
// environment variables with defaults and optional minimum bounds.
package envutil

import (
	"os"
	"strconv"
	"time"
)

// Int reads an integer from environment variable key.
// Returns defaultVal if absent, empty, or unparseable.
func Int(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

// IntMin reads an integer from environment variable key with a minimum bound.
// Returns defaultVal if absent, empty, unparseable, or below min.
func IntMin(key string, defaultVal, min int) int {
	n := Int(key, defaultVal)
	if n < min {
		return defaultVal
	}
	return n
}

// Duration reads a time.Duration from environment variable key.
// Returns defaultVal if absent, empty, or unparseable.
func Duration(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultVal
}

// DurationMin reads a time.Duration with a minimum bound.
// Returns defaultVal if absent, empty, unparseable, or below min.
func DurationMin(key string, defaultVal, min time.Duration) time.Duration {
	d := Duration(key, defaultVal)
	if d < min {
		return defaultVal
	}
	return d
}
