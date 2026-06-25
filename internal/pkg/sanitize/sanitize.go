// Package sanitize provides string sanitization utilities for display
// truncation and generated slugs.
package sanitize

import "strings"

// Truncate returns s truncated to at most n runes, appending "…" when trimmed.
// It handles multi-byte characters correctly by operating on runes rather than
// bytes.
func Truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// Slug creates a container-name-safe slug from s.
// The result is lowercase, contains only [a-z0-9-], is at most maxLen chars,
// and collapses consecutive non-alphanumeric characters into a single dash.
// Falls back to "task" if the result is empty.
func Slug(s string, maxLen int) string {
	var b []byte
	prevDash := true // suppress leading dashes
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b = append(b, byte(r))
			prevDash = false
		} else if !prevDash {
			b = append(b, '-')
			prevDash = true
		}
		if len(b) >= maxLen {
			break
		}
	}
	slug := strings.TrimRight(string(b), "-")
	if slug == "" {
		return "task"
	}
	return slug
}
