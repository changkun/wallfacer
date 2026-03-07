package runner

import "strings"

// truncate returns s truncated to n bytes, with "..." appended if truncation occurred.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// slugifyPrompt creates a container-name-safe slug from s.
// The result is lowercase, contains only [a-z0-9-], is at most maxLen chars,
// and collapses consecutive non-alphanumeric characters into a single dash.
func slugifyPrompt(s string, maxLen int) string {
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
