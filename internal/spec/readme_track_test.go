package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTrackDisplayNamesMatchReadmeHeadings pins TrackDisplayName to the
// headings in specs/README.md. EnsureReadme appends a new spec's row
// under `## <TrackDisplayName(track)>`; when the map has no entry the
// fallback title-cases the directory name, which silently appends a
// duplicate section instead of finding the real one (a spec in the
// `intent` track landing under a fresh `## Intent` rather than the
// existing `## Git Workflow`).
func TestTrackDisplayNamesMatchReadmeHeadings(t *testing.T) {
	root := filepath.Join(repoRootDir(t), "specs")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read specs/: %v", err)
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read specs/README.md: %v", err)
	}

	var checked int
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		checked++
		heading := "## " + TrackDisplayName(e.Name())
		if !strings.Contains(string(readme), heading+"\n") {
			t.Errorf("track %q maps to %q, but specs/README.md has no %q heading; "+
				"EnsureReadme would append a duplicate section",
				e.Name(), TrackDisplayName(e.Name()), heading)
		}
	}
	if checked == 0 {
		t.Fatal("no track directories under specs/")
	}
}
