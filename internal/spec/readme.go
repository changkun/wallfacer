package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Meta is the minimum information [EnsureReadme] needs to reference a
// freshly scaffolded spec in `specs/README.md`. Summary is an optional
// one-line "delivers…" description; when empty the rendered row uses a
// placeholder so the table remains valid markdown.
type Meta struct {
	// Path is the workspace-relative spec path, e.g.
	// "specs/local/auth-refactor.md". The track segment (first directory
	// after "specs/") drives which section of the README the entry is
	// placed under.
	Path string

	// Title defaults to a title-cased filename via [TitleFromFilename]
	// when empty, mirroring the Scaffold behaviour.
	Title string

	// Status is the spec's lifecycle state. Empty values render as
	// "(status unset)" so the table stays lint-clean even if the caller
	// forgets to populate the field.
	Status Status

	// Summary is a one-line "delivers…" sentence. When empty, the row
	// uses a placeholder that invites the agent to back-fill later.
	Summary string
}

// trackDisplayNames maps the directory slug used under `specs/<track>/`
// to the human-readable heading rendered in the README. Unknown tracks
// fall through to a title-cased variant of the directory name.
var trackDisplayNames = map[string]string{
	"local":       "Local Product",
	"foundations": "Foundations",
	"cloud":       "Cloud Platform",
	"shared":      "Shared Design",
}

// TrackDisplayName returns the human-readable heading for a given track
// slug. Exposed so other packages can surface consistent labels.
func TrackDisplayName(track string) string {
	if name, ok := trackDisplayNames[track]; ok {
		return name
	}
	return titleCase(strings.ReplaceAll(track, "_", "-"))
}

// titleCase splits on hyphens and upper-cases the first letter of each
// word. Avoids pulling in `golang.org/x/text/cases` for a tiny
// presentation concern.
func titleCase(s string) string {
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		runes := []rune(p)
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

// trackFromSpecPath extracts the track directory from a path like
// "specs/local/foo.md" → "local". Returns "" when the path is
// malformed; the caller is responsible for rejecting that case.
func trackFromSpecPath(specPath string) string {
	clean := filepath.ToSlash(filepath.Clean(specPath))
	parts := strings.Split(clean, "/")
	if len(parts) < 3 || parts[0] != "specs" {
		return ""
	}
	return parts[1]
}

// statusDisplay converts a [Status] into the label used in the README's
// "Status" column. Empty values render as `(status unset)` to keep the
// table visually valid.
func statusDisplay(s Status) string {
	switch s {
	case StatusComplete:
		return "**Complete**"
	case "":
		return "(status unset)"
	default:
		// Title-case the slug: drafted → Drafted, in-progress → In
		// Progress (not a current value but kept forward-compatible).
		return titleCase(string(s))
	}
}

// summaryOrPlaceholder returns a safe, single-line summary suitable for
// the table. Newlines and pipe characters are converted to spaces so
// the caller never produces malformed markdown.
func summaryOrPlaceholder(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return "(agent will fill this in)"
	}
	return s
}

// specTrackRelativePath converts a workspace-relative spec path into the
// track-relative link target used in the README, e.g.
// "specs/local/auth.md" → "local/auth.md". Returns the original path
// unchanged when it does not begin with "specs/".
func specTrackRelativePath(specPath string) string {
	clean := filepath.ToSlash(filepath.Clean(specPath))
	return strings.TrimPrefix(clean, "specs/")
}

// renderRow emits a single markdown-table row for a spec entry. The
// link text is the filename (stable across renames of the display
// title) and the target is track-relative so it resolves correctly
// from the README's location.
func renderRow(meta Meta) string {
	linkText := filepath.Base(meta.Path)
	linkTarget := specTrackRelativePath(meta.Path)
	return fmt.Sprintf(
		"| [%s](%s) | %s | %s |",
		linkText, linkTarget, statusDisplay(meta.Status),
		summaryOrPlaceholder(meta.Summary),
	)
}

// EnsureReadme guarantees specs/README.md exists in `workspace` and
// references `newSpec` under the right track table. Creates the file
// with a minimal template when missing; otherwise appends a row to the
// matching track's table (or a new section when the track is absent).
// User-authored prose outside the track tables is preserved
// byte-for-byte.
//
// Writes are atomic — the updated content is rendered to a sibling
// tempfile, fsynced, and renamed into place. A failure mid-rename
// leaves the existing README untouched.
func EnsureReadme(workspace string, newSpec Meta) error {
	if workspace == "" {
		return fmt.Errorf("workspace is required")
	}
	track := trackFromSpecPath(newSpec.Path)
	if track == "" {
		return fmt.Errorf("invalid spec path %q (must start with specs/<track>/)", newSpec.Path)
	}
	readmePath := filepath.Join(workspace, "specs", "README.md")

	existing, err := os.ReadFile(readmePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", readmePath, err)
	}
	var next []byte
	if os.IsNotExist(err) || len(existing) == 0 {
		next = []byte(renderInitialReadme(track, newSpec))
	} else {
		next = []byte(appendToReadme(string(existing), track, newSpec))
	}

	if err := os.MkdirAll(filepath.Dir(readmePath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(readmePath), err)
	}
	return atomicWriteFile(readmePath, next, 0o644)
}

// renderInitialReadme produces the minimal template used when the
// workspace has no README yet. Includes a short banner noting the
// auto-generated provenance so future manual edits can replace it
// wholesale without fighting the append logic.
func renderInitialReadme(track string, meta Meta) string {
	var b strings.Builder
	b.WriteString("# Specs\n\n")
	b.WriteString("<!-- Auto-generated on first scaffold. Customise the prose freely — future appends only modify the track tables below. -->\n\n")
	b.WriteString("## " + TrackDisplayName(track) + "\n\n")
	b.WriteString("| Spec | Status | Delivers |\n")
	b.WriteString("|------|--------|----------|\n")
	b.WriteString(renderRow(meta))
	b.WriteString("\n")
	return b.String()
}

// appendToReadme inserts a row referencing `meta` into the existing
// README content, returning the new full text. The algorithm operates
// on line ranges so custom prose outside track tables is preserved
// exactly.
//
// Dispatch:
//   - If a `## <TrackDisplay>` heading exists and is followed by a
//     markdown table, append a row to the end of that table.
//   - If the heading exists but the next non-blank block is not a
//     table, insert a fresh table immediately after the heading's
//     description paragraph(s).
//   - If the heading is missing entirely, append a new
//     `## <TrackDisplay>` section to the bottom of the file.
func appendToReadme(content, track string, meta Meta) string {
	display := TrackDisplayName(track)
	heading := "## " + display

	lines := strings.Split(content, "\n")
	headingIdx := -1
	for i, line := range lines {
		if strings.TrimRight(line, " \t\r") == heading {
			headingIdx = i
			break
		}
	}

	if headingIdx == -1 {
		return ensureTrailingNewline(content) + "\n" +
			renderNewTrackSection(track, meta)
	}

	// Find the end of the track's content (next `## ` heading or EOF)
	// and within that range locate the last row of the existing table
	// or the first line that looks like table rule to anchor insertion.
	nextHeadingIdx := len(lines)
	for j := headingIdx + 1; j < len(lines); j++ {
		if strings.HasPrefix(lines[j], "## ") {
			nextHeadingIdx = j
			break
		}
	}

	tableStart := -1
	for j := headingIdx + 1; j < nextHeadingIdx; j++ {
		if strings.HasPrefix(strings.TrimSpace(lines[j]), "|") {
			tableStart = j
			break
		}
	}

	if tableStart == -1 {
		// Heading exists, no table in this section. Insert a table at
		// the top of the section body (right after any description
		// paragraphs — we place it immediately after the first blank
		// line that follows the heading, falling back to directly
		// after the heading when there is no body yet).
		insertAt := headingIdx + 1
		for j := headingIdx + 1; j < nextHeadingIdx; j++ {
			if strings.TrimSpace(lines[j]) == "" {
				insertAt = j
				break
			}
			insertAt = j + 1
		}
		table := []string{
			"",
			"| Spec | Status | Delivers |",
			"|------|--------|----------|",
			renderRow(meta),
			"",
		}
		merged := append([]string{}, lines[:insertAt]...)
		merged = append(merged, table...)
		merged = append(merged, lines[insertAt:]...)
		return strings.Join(merged, "\n")
	}

	// Walk forward from tableStart to the last contiguous row (line
	// that starts with "|" after trimming). Rows end at the first
	// non-row line (blank, prose, EOF, next heading).
	lastRow := tableStart
	for j := tableStart; j < nextHeadingIdx; j++ {
		if strings.HasPrefix(strings.TrimSpace(lines[j]), "|") {
			lastRow = j
			continue
		}
		break
	}
	newRow := renderRow(meta)
	merged := append([]string{}, lines[:lastRow+1]...)
	merged = append(merged, newRow)
	merged = append(merged, lines[lastRow+1:]...)
	return strings.Join(merged, "\n")
}

// renderNewTrackSection emits a full `## <Track>` heading + table
// block with a single row for `meta`. Used when the README lacks a
// heading for the spec's track.
func renderNewTrackSection(track string, meta Meta) string {
	var b strings.Builder
	b.WriteString("## " + TrackDisplayName(track) + "\n\n")
	b.WriteString("| Spec | Status | Delivers |\n")
	b.WriteString("|------|--------|----------|\n")
	b.WriteString(renderRow(meta))
	b.WriteString("\n")
	return b.String()
}

func ensureTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

// atomicWriteFile writes `data` to `path` via a sibling tempfile +
// rename so a failure mid-write never truncates the existing file.
func atomicWriteFile(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, werr := tmp.Write(data); werr != nil {
		_ = tmp.Close()
		err = fmt.Errorf("write temp: %w", werr)
		return err
	}
	if serr := tmp.Sync(); serr != nil {
		_ = tmp.Close()
		err = fmt.Errorf("sync temp: %w", serr)
		return err
	}
	if cerr := tmp.Close(); cerr != nil {
		err = fmt.Errorf("close temp: %w", cerr)
		return err
	}
	if chErr := os.Chmod(tmpPath, perm); chErr != nil {
		err = fmt.Errorf("chmod temp: %w", chErr)
		return err
	}
	if rerr := os.Rename(tmpPath, path); rerr != nil {
		err = fmt.Errorf("rename: %w", rerr)
		return err
	}
	return nil
}
