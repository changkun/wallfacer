package spec

import (
	"os"
	"strings"
)

// IsLeafPath reports whether the spec file at absPath is a leaf spec
// (has no child specs in a corresponding subdirectory). A spec at
// "specs/local/foo.md" is non-leaf if "specs/local/foo/" exists and
// contains at least one .md file.
func IsLeafPath(absPath string) bool {
	dir := strings.TrimSuffix(absPath, ".md")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true // no subdirectory → leaf
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			return false // has child spec files → non-leaf
		}
	}
	return true // subdirectory exists but has no .md files → leaf
}
