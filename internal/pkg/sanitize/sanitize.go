// Package sanitize provides string sanitization utilities for filesystem paths,
// container names, and display truncation.
package sanitize

import (
	"path/filepath"
	"strings"
	"unicode"
)

// Basename returns a filesystem- and container-path-safe version of the last
// component of a workspace path. It replaces characters that are problematic
// in container mount paths (colons, control chars, etc.) with underscores and
// preserves unicode letters/digits so paths like "我的项目" remain
// human-readable. Falls back to "workspace" if the result is empty.
func Basename(path string) string {
	base := filepath.Base(path)
	return Base(base)
}

// Base returns a container-path-safe version of a directory basename.
// It replaces characters problematic in mount paths (colons, spaces, shell
// metacharacters) with underscores, preserving unicode letters/digits.
// Falls back to "workspace" if the result is empty.
func Base(name string) string {
	if name == "." || name == "/" || name == `\` || name == "" {
		return "workspace"
	}
	var b strings.Builder
	for _, r := range name {
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
