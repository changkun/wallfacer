package handler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/planner"
	"changkun.de/x/wallfacer/internal/spec"
)

// Directive is a single /spec-new request extracted from the planning
// agent's output stream. Path is required; Title/Status/Effort default
// in [spec.Scaffold]. Body holds every non-directive line the agent
// emitted after the directive line, up to the next directive or the
// end of the turn — appended to the scaffolded file as its content.
type Directive struct {
	Path   string
	Title  string
	Status spec.Status
	Effort spec.Effort
	Body   string
}

// DirectiveScanner turns an assistant-text stream (already split into
// lines) into a slice of [Directive] values. It tracks fenced-block
// state so code samples that quote the grammar do not trigger spurious
// scaffolds. Call [DirectiveScanner.ScanLine] for every line in the
// order the agent emitted them, then [DirectiveScanner.Directives] to
// obtain the captured list.
//
// The scanner is intentionally line-oriented so the caller can feed it
// incrementally from a stream without buffering the whole response.
type DirectiveScanner struct {
	inFence    bool
	directives []*Directive
	// bodyLines collects the body for the currently active directive.
	bodyLines []string
}

// ScanLine processes one assistant-text line. Call exactly once per
// line, preserving the agent's original order and content (no
// trimming).
func (s *DirectiveScanner) ScanLine(line string) {
	// Fence toggle: a line whose trimmed prefix is ``` flips inFence.
	// The fence line itself is treated as body content of the current
	// directive (if any) so the agent's raw markdown is preserved when
	// a later consumer reads the body back out.
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "```") {
		s.inFence = !s.inFence
		if len(s.directives) > 0 {
			s.bodyLines = append(s.bodyLines, line)
		}
		return
	}

	// Directive recognition: only outside fences, only when the first
	// non-whitespace token is /spec-new.
	if !s.inFence && strings.HasPrefix(trimmed, "/spec-new") {
		// Close out the previous directive's body before starting a new
		// one.
		s.finalizeCurrent()
		d := parseDirective(trimmed)
		if d != nil {
			s.directives = append(s.directives, d)
			s.bodyLines = s.bodyLines[:0]
			return
		}
		// If parsing failed, treat the line as plain body content of
		// whatever directive is active (or drop it if none).
	}

	if len(s.directives) > 0 {
		s.bodyLines = append(s.bodyLines, line)
	}
}

// Directives returns the captured directives, with each directive's
// Body finalised from the accumulated lines. Safe to call multiple
// times; subsequent calls return the same result until [ScanLine] is
// invoked again.
func (s *DirectiveScanner) Directives() []Directive {
	s.finalizeCurrent()
	out := make([]Directive, len(s.directives))
	for i, d := range s.directives {
		out[i] = *d
	}
	return out
}

func (s *DirectiveScanner) finalizeCurrent() {
	if len(s.directives) == 0 {
		return
	}
	last := s.directives[len(s.directives)-1]
	// Trim a single trailing blank line (common before the next
	// directive) but preserve inner blank lines as-is.
	trimmed := s.bodyLines
	for len(trimmed) > 0 && trimmed[len(trimmed)-1] == "" {
		trimmed = trimmed[:len(trimmed)-1]
	}
	last.Body = strings.Join(trimmed, "\n")
	s.bodyLines = s.bodyLines[:0]
}

// parseDirective splits a `/spec-new <path> [k=v ...]` line into a
// [Directive]. Returns nil when the line has no path. Unknown keys
// are silently ignored so the grammar can grow without breaking older
// servers.
func parseDirective(line string) *Directive {
	// Strip the leading /spec-new token and surrounding whitespace.
	rest := strings.TrimSpace(strings.TrimPrefix(line, "/spec-new"))
	if rest == "" {
		return nil
	}
	tokens := tokenize(rest)
	if len(tokens) == 0 {
		return nil
	}
	d := &Directive{Path: tokens[0]}
	for _, tok := range tokens[1:] {
		eq := strings.IndexByte(tok, '=')
		if eq <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(tok[:eq]))
		val := strings.TrimSpace(tok[eq+1:])
		val = strings.TrimPrefix(val, `"`)
		val = strings.TrimSuffix(val, `"`)
		switch key {
		case "title":
			d.Title = val
		case "status":
			d.Status = spec.Status(val)
		case "effort":
			d.Effort = spec.Effort(val)
		}
	}
	return d
}

// tokenize splits a directive argument string on whitespace while
// respecting double-quoted runs so `title="two words"` survives as a
// single token. Escape sequences are not supported — quoted values
// cannot contain a literal double quote.
func tokenize(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case !inQuote && (r == ' ' || r == '\t'):
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// extractAssistantLines walks a stream-json NDJSON payload and returns
// the assistant-authored text as an ordered list of lines. Only
// `type: "assistant"` entries contribute; user messages, tool calls,
// and system metadata are ignored. Each text content block is split on
// newlines and concatenated in the order it appeared in the stream.
func extractAssistantLines(raw []byte) []string {
	var lines []string
	for _, rawLine := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || trimmed[0] != '{' {
			continue
		}
		var obj struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
			continue
		}
		if obj.Type != "assistant" {
			continue
		}
		for _, c := range obj.Message.Content {
			if c.Type != "text" || c.Text == "" {
				continue
			}
			lines = append(lines, strings.Split(c.Text, "\n")...)
		}
	}
	return lines
}

// scaffoldDirective creates one spec file and appends the agent-authored
// body after its frontmatter. `workspace` is the absolute workspace
// root; the directive's Path is relative to it (and must start with
// `specs/<track>/`). Returns the absolute path on success, or an error
// suitable for surfacing as a `system` chat message.
func scaffoldDirective(workspace string, d Directive, now time.Time) (string, error) {
	if workspace == "" {
		return "", fmt.Errorf("no workspace configured")
	}
	// Validate the relative path using the same rules as [spec.Scaffold]
	// so errors surface the familiar message ("must be under a track
	// directory, e.g. specs/local/my-feature.md").
	if err := spec.ValidateSpecPath(d.Path); err != nil {
		return "", err
	}
	abs := filepath.Join(workspace, d.Path)
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("%s already exists", d.Path)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat %s: %w", d.Path, err)
	}

	status := d.Status
	if status == "" {
		status = spec.StatusVague
	}
	if !isValidStatus(status) {
		return "", fmt.Errorf("invalid status %q", d.Status)
	}
	effort := d.Effort
	if effort == "" {
		effort = spec.EffortMedium
	}
	if !isValidEffort(effort) {
		return "", fmt.Errorf("invalid effort %q", d.Effort)
	}
	title := d.Title
	if title == "" {
		title = spec.TitleFromFilename(d.Path)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	skeleton := spec.RenderSkeleton(title, status, effort, spec.ResolveAuthor(), nil, now)

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", filepath.Dir(d.Path), err)
	}
	if err := os.WriteFile(abs, []byte(skeleton), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", d.Path, err)
	}
	if d.Body != "" {
		if appendErr := appendDirectiveBody(abs, d.Body); appendErr != nil {
			return abs, appendErr
		}
	}
	return abs, nil
}

func isValidStatus(s spec.Status) bool {
	for _, v := range spec.ValidStatuses() {
		if v == s {
			return true
		}
	}
	return false
}

func isValidEffort(e spec.Effort) bool {
	for _, v := range spec.ValidEfforts() {
		if v == e {
			return true
		}
	}
	return false
}

// appendDirectiveBody appends the agent's body to the end of a freshly
// scaffolded file. The default skeleton already contains an H1 and
// three placeholder sections; the agent's body is glued on with a
// blank-line separator so it reads as a continuation.
func appendDirectiveBody(path, body string) (err error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if !strings.HasPrefix(body, "\n") {
		if _, err = f.WriteString("\n"); err != nil {
			return err
		}
	}
	if _, err = f.WriteString(body); err != nil {
		return err
	}
	if !strings.HasSuffix(body, "\n") {
		if _, err = f.WriteString("\n"); err != nil {
			return err
		}
	}
	return nil
}

// processDirectives runs each captured [Directive] against a workspace.
// Returns one [planner.Message] per directive that failed so the
// caller can surface the error as a `system`-role entry in the thread
// history; successfully scaffolded directives produce no message.
//
// Errors are reported but never bubbled up — a failed directive must
// not block subsequent directives, nor prevent the agent's raw
// response from being appended to the conversation log.
func processDirectives(workspace string, dirs []Directive, focused string, now time.Time) []planner.Message {
	if len(dirs) == 0 {
		return nil
	}
	var out []planner.Message
	for _, d := range dirs {
		if _, err := scaffoldDirective(workspace, d, now); err != nil {
			out = append(out, planner.Message{
				Role:        "system",
				Content:     fmt.Sprintf("Couldn't create %s: %s", d.Path, err.Error()),
				Timestamp:   now,
				FocusedSpec: focused,
			})
		}
	}
	return out
}
