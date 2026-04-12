package spec

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Index describes a workspace's top-level spec roadmap file
// (`specs/README.md`). Distinct from [Spec] because the roadmap has no
// frontmatter, no lifecycle, and no dependency edges — it's a plain
// markdown document pinned as a first-class entry in the spec explorer.
type Index struct {
	// Path is the roadmap file's location relative to the repo root,
	// e.g. "specs/README.md". Always ends in "README.md".
	Path string `json:"path"`

	// Workspace is the absolute workspace root that owns this index.
	// Exposed so the frontend can route /api/explorer/file reads to the
	// right mount when multiple workspaces are active.
	Workspace string `json:"workspace"`

	// Title is the text of the first-level Markdown heading in the
	// file ("# <Title>") with leading/trailing whitespace trimmed.
	// Falls back to "Roadmap" when the file has no H1 or is empty.
	Title string `json:"title"`

	// Modified is the file's filesystem mtime. Used by the SSE stream
	// to detect roadmap changes without re-reading the whole file.
	Modified time.Time `json:"modified"`
}

// indexFallbackTitle is the display title used when the README has no
// top-level heading — keeps the pinned explorer entry labelled even for
// stub files.
const indexFallbackTitle = "Roadmap"

// indexTitleScanMax caps how far ResolveIndex will read into a README
// while hunting for the first H1. Large READMEs with very late headings
// are pathological; prefer the fallback over an unbounded read.
const indexTitleScanMax = 200

// ResolveIndex returns the first workspace in `workspaces` that has a
// `specs/README.md` file, with its title and mtime extracted. Returns
// (nil, nil) when no workspace contains a roadmap — the explorer hides
// the pinned entry in that case.
//
// Resolution is deterministic: iteration follows the workspace slice
// order. When multiple workspaces have a README, the first one wins.
// A follow-up spec may introduce a workspace-picker for users who want
// to choose which roadmap surfaces; for now this keeps the frontend
// consumer simple.
//
// IO errors other than "file does not exist" propagate to the caller
// so a misconfigured mount (permission denied, etc.) is visible rather
// than silently dropping the index.
func ResolveIndex(workspaces []string) (*Index, error) {
	for _, ws := range workspaces {
		path := filepath.Join(ws, "specs", "README.md")
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if info.IsDir() {
			// Treat a directory named README.md as "no roadmap" —
			// unusual but worth being defensive about.
			continue
		}
		title, err := readFirstH1(path, indexFallbackTitle)
		if err != nil {
			return nil, err
		}
		return &Index{
			Path:      "specs/README.md",
			Workspace: ws,
			Title:     title,
			Modified:  info.ModTime(),
		}, nil
	}
	return nil, nil
}

// readFirstH1 scans up to indexTitleScanMax lines of path and returns
// the text of the first "# heading" line (trimmed). Returns fallback
// when the file has no H1 within the scan window.
func readFirstH1(path, fallback string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for i := 0; sc.Scan() && i < indexTitleScanMax; i++ {
		line := strings.TrimRight(sc.Text(), "\r\n")
		if title, ok := strings.CutPrefix(line, "# "); ok {
			if trimmed := strings.TrimSpace(title); trimmed != "" {
				return trimmed, nil
			}
		}
	}
	// Scanner errors on oversized lines etc. — not fatal for title
	// extraction; fall back so the explorer still pins the entry.
	return fallback, nil
}
