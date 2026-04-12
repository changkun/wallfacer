package spec

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseFile reads a spec file from disk and parses its YAML frontmatter
// and markdown body. The path is stored on the returned Spec as-is.
func ParseFile(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read spec %s: %w", path, err)
	}
	return ParseBytes(data, path)
}

// ParseBytes parses a spec document from raw bytes. The path argument is
// stored on the returned Spec for identification (it is not accessed on disk).
func ParseBytes(data []byte, path string) (*Spec, error) {
	if len(data) == 0 {
		return nil, errors.New("empty spec file")
	}

	// Normalize CRLF to LF so files written on Windows (or round-tripped
	// through git with core.autocrlf=true) parse the same as LF-only files.
	content := strings.ReplaceAll(string(data), "\r\n", "\n")

	// Frontmatter must start with "---\n".
	if !strings.HasPrefix(content, "---\n") {
		return nil, errors.New("missing frontmatter: file must start with ---")
	}

	// Find the closing "---\n" delimiter after the opening one.
	rest := content[4:] // skip opening "---\n"
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		// Also accept "---\n" at the very end (no trailing newline after closing delimiter).
		if strings.HasSuffix(rest, "\n---") {
			end = len(rest) - 3
		} else {
			return nil, errors.New("missing frontmatter: no closing --- delimiter")
		}
	}

	frontmatter := rest[:end]
	body := ""
	if end+4 < len(rest) {
		body = strings.TrimLeft(rest[end+4:], "\n")
	}

	var s Spec
	if err := yaml.Unmarshal([]byte(frontmatter), &s); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	s.Path = path
	s.Body = body

	// Ensure slices are non-nil for consistent downstream handling.
	if s.DependsOn == nil {
		s.DependsOn = []string{}
	}
	if s.Affects == nil {
		s.Affects = []string{}
	}

	return &s, nil
}

// trackFromPath extracts the track (second directory component) from a
// spec's relative path. E.g., "specs/local/foo/bar.md" → "local".
// The leading "specs/" prefix is stripped before extracting the track.
func trackFromPath(path string) string {
	p := strings.TrimPrefix(path, "specs/")
	parts := strings.SplitN(p, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}
