package runner

import (
	"path/filepath"
	"strings"
	"unicode"
)

// truncate returns s truncated to n bytes, with "..." appended if truncation occurred.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// sanitizeBasename returns a filesystem- and container-path-safe version of
// the last component of a workspace path. It replaces characters that are
// problematic in container mount paths (colons, control chars, etc.) with
// underscores and preserves unicode letters/digits so paths like "我的项目"
// remain human-readable. Falls back to "workspace" if the result is empty.
func sanitizeBasename(path string) string {
	base := filepath.Base(path)
	if base == "." || base == "/" || base == "" {
		return "workspace"
	}
	var b strings.Builder
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	result := b.String()
	if result == "" {
		return "workspace"
	}
	return result
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
