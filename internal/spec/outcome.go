package spec

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"latere.ai/x/wallfacer/internal/pkg/atomicfile"
)

// outcomeHeading is the body section the drift pipeline writes its verdict to.
const outcomeHeading = "## Outcome"

// SetOutcome writes or replaces the "## Outcome" section in a spec's body,
// preserving the frontmatter byte-for-byte. An existing Outcome section (up to
// the next "## " heading or end of file) is replaced; otherwise the section is
// appended. The write is atomic.
func SetOutcome(path, outcomeBody string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read spec %s: %w", path, err)
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return errors.New("missing frontmatter: file must start with ---")
	}
	rest := content[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return errors.New("missing frontmatter: no closing --- delimiter")
	}
	head := content[:4+end+5] // through the closing "---\n"
	body := content[4+end+5:]

	section := outcomeHeading + "\n\n" + strings.TrimSpace(outcomeBody) + "\n"
	newBody := replaceOrAppendSection(body, section)

	perm := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		perm = info.Mode()
	}
	if err := atomicfile.Write(path, []byte(head+newBody), perm); err != nil {
		return fmt.Errorf("write spec file: %w", err)
	}
	return nil
}

// replaceOrAppendSection replaces an existing "## Outcome" section (spanning to
// the next "## " heading or end) with section, or appends it when absent.
func replaceOrAppendSection(body, section string) string {
	idx := indexOfHeading(body, outcomeHeading)
	if idx < 0 {
		trimmed := strings.TrimRight(body, "\n")
		if trimmed == "" {
			return section
		}
		return trimmed + "\n\n" + section
	}

	// Find the end of the existing section: the next "## " heading at line start.
	afterStart := idx + len(outcomeHeading)
	rest := body[afterStart:]
	next := indexOfNextH2(rest)
	before := strings.TrimRight(body[:idx], "\n")
	var tail string
	if next >= 0 {
		tail = rest[next:]
	}
	out := section
	if before != "" {
		out = before + "\n\n" + section
	}
	if tail != "" {
		out = strings.TrimRight(out, "\n") + "\n\n" + tail
	}
	return out
}

// indexOfHeading returns the byte index of a heading that sits at the start of
// a line, or -1.
func indexOfHeading(body, heading string) int {
	if strings.HasPrefix(body, heading) {
		return 0
	}
	if i := strings.Index(body, "\n"+heading); i >= 0 {
		return i + 1
	}
	return -1
}

// indexOfNextH2 returns the index (within s) of the next "## " heading at a line
// start, or -1.
func indexOfNextH2(s string) int {
	if i := strings.Index(s, "\n## "); i >= 0 {
		return i + 1
	}
	return -1
}
