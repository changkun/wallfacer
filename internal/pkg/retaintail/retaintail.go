// Package retaintail provides a generic function for retaining the tail
// (most recent) elements of a slice.
package retaintail

// Tail returns the last n elements of s. If n <= 0 or len(s) <= n, s is
// returned unchanged.
func Tail[T any](s []T, n int) []T {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
