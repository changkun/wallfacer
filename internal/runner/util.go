package runner

import "changkun.de/x/wallfacer/internal/pkg/sanitize"

// truncate is a package-level alias for sanitize.Truncate, kept to avoid
// updating every call site in the runner package. It returns s truncated to
// at most n runes, appending "…" when trimmed.
func truncate(s string, n int) string {
	return sanitize.Truncate(s, n)
}

// sanitizeBasename is a package-level alias for sanitize.Basename.
func sanitizeBasename(path string) string {
	return sanitize.Basename(path)
}

// slugifyPrompt is a package-level alias for sanitize.Slug.
func slugifyPrompt(s string, maxLen int) string {
	return sanitize.Slug(s, maxLen)
}
