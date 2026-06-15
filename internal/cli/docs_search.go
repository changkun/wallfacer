package cli

import (
	"io/fs"
	"strings"
)

// DocSearchHit is a single docs-search result: the doc slug, its title, and a
// context snippet around the match for display in the command palette.
type DocSearchHit struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// docSearchRoots are the embedded markdown trees the search scans.
var docSearchRoots = []string{"docs/guide", "docs/internals"}

// searchDocs scans the embedded markdown docs for a case-insensitive match of
// query in each doc's title or body and returns up to limit hits. Title matches
// rank ahead of body-only matches so the most relevant page surfaces first.
// Returns nil for queries shorter than two characters.
func searchDocs(docsFS fs.FS, query string, limit int) []DocSearchHit {
	q := strings.TrimSpace(strings.ToLower(query))
	if len(q) < 2 {
		return nil
	}

	var titleHits, bodyHits []DocSearchHit
	for _, root := range docSearchRoots {
		entries, err := fs.ReadDir(docsFS, root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			full := root + "/" + e.Name()
			data, err := fs.ReadFile(docsFS, full)
			if err != nil {
				continue
			}
			body := string(data)
			title := docTitle(body, strings.TrimSuffix(e.Name(), ".md"))
			slug := strings.TrimPrefix(strings.TrimSuffix(full, ".md"), "docs/")

			if strings.Contains(strings.ToLower(title), q) {
				titleHits = append(titleHits, DocSearchHit{Slug: slug, Title: title, Snippet: leadSnippet(body)})
				continue
			}
			if idx := strings.Index(strings.ToLower(body), q); idx >= 0 {
				bodyHits = append(bodyHits, DocSearchHit{Slug: slug, Title: title, Snippet: snippetAround(body, idx, len(q))})
			}
		}
	}

	hits := append(titleHits, bodyHits...)
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// docTitle returns the first "# " heading in body, or fallback if none.
func docTitle(body, fallback string) string {
	for _, line := range strings.SplitN(body, "\n", 30) {
		if t, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(t)
		}
	}
	return fallback
}

// snippetAround returns a whitespace-collapsed context window around the match
// at byte offset idx (qlen bytes long), with ellipses when truncated.
func snippetAround(body string, idx, qlen int) string {
	const radius = 70
	start := max(idx-radius, 0)
	end := min(idx+qlen+radius, len(body))
	frag := strings.Join(strings.Fields(body[start:end]), " ")
	frag = strings.ToValidUTF8(frag, "")
	if start > 0 {
		frag = "… " + frag
	}
	if end < len(body) {
		frag += " …"
	}
	return frag
}

// leadSnippet returns the first paragraph of meaningful text (skipping the
// title heading and blank lines), used for title matches that have no in-body
// match offset to anchor on.
func leadSnippet(body string) string {
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > 160 {
			line = strings.ToValidUTF8(line[:160], "") + " …"
		}
		return line
	}
	return ""
}
