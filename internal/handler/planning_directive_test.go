package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/spec"
)

func scanAll(lines []string) []Directive {
	s := &DirectiveScanner{}
	for _, line := range lines {
		s.ScanLine(line)
	}
	return s.Directives()
}

func TestDirectiveScanner_Simple(t *testing.T) {
	dirs := scanAll([]string{
		`/spec-new specs/local/foo.md title="Foo"`,
		"",
		"## Goal",
		"",
		"Body line.",
	})
	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1", len(dirs))
	}
	d := dirs[0]
	if d.Path != "specs/local/foo.md" {
		t.Errorf("path = %q, want specs/local/foo.md", d.Path)
	}
	if d.Title != "Foo" {
		t.Errorf("title = %q, want Foo", d.Title)
	}
	if !strings.Contains(d.Body, "## Goal") || !strings.Contains(d.Body, "Body line.") {
		t.Errorf("body missing expected content: %q", d.Body)
	}
}

func TestDirectiveScanner_NoDirective(t *testing.T) {
	dirs := scanAll([]string{
		"plain chat about the weather",
		"and some follow-up",
	})
	if len(dirs) != 0 {
		t.Fatalf("got %d directives, want 0", len(dirs))
	}
}

func TestDirectiveScanner_FencedDirectiveIgnored(t *testing.T) {
	dirs := scanAll([]string{
		"Here is the grammar:",
		"```",
		"/spec-new specs/local/bar.md",
		"```",
		"end of sample.",
	})
	if len(dirs) != 0 {
		t.Fatalf("got %d directives, want 0 (directive was inside a fence)", len(dirs))
	}
}

func TestDirectiveScanner_MultipleDirectives(t *testing.T) {
	dirs := scanAll([]string{
		`/spec-new specs/local/a.md title="A"`,
		"body for A",
		`/spec-new specs/local/b.md title="B"`,
		"body for B line 1",
		"body for B line 2",
	})
	if len(dirs) != 2 {
		t.Fatalf("got %d directives, want 2", len(dirs))
	}
	if dirs[0].Path != "specs/local/a.md" || dirs[0].Body != "body for A" {
		t.Errorf("dir 0 = %+v", dirs[0])
	}
	if dirs[1].Path != "specs/local/b.md" {
		t.Errorf("dir 1 path = %q", dirs[1].Path)
	}
	if !strings.Contains(dirs[1].Body, "body for B line 1") ||
		!strings.Contains(dirs[1].Body, "body for B line 2") {
		t.Errorf("dir 1 body missing expected content: %q", dirs[1].Body)
	}
}

func TestDirectiveScanner_ImbalancedFence(t *testing.T) {
	// A fence that never closes — every subsequent line is inside-fence,
	// including any /spec-new that appears. No directives should be
	// recognised.
	dirs := scanAll([]string{
		"opening normal prose",
		"```",
		"/spec-new specs/local/should-not-register.md",
		"more content with no closing fence",
	})
	if len(dirs) != 0 {
		t.Fatalf("got %d directives, want 0 (directive was inside unclosed fence)", len(dirs))
	}
}

func TestDirectiveScanner_ArgParsing(t *testing.T) {
	dirs := scanAll([]string{
		`/spec-new specs/local/auth.md title="quoted with spaces" status=drafted effort=medium unknown=key`,
	})
	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1", len(dirs))
	}
	d := dirs[0]
	if d.Title != "quoted with spaces" {
		t.Errorf("title = %q, want 'quoted with spaces'", d.Title)
	}
	if d.Status != spec.StatusDrafted {
		t.Errorf("status = %q, want drafted", d.Status)
	}
	if d.Effort != spec.EffortMedium {
		t.Errorf("effort = %q, want medium", d.Effort)
	}
}

func TestDirectiveScanner_BodyIncludesMarkdown(t *testing.T) {
	body := []string{
		`/spec-new specs/local/rich.md`,
		"## Background",
		"",
		"A list:",
		"- one",
		"- two",
		"",
		"```go",
		"func Hello() {}",
		"```",
		"",
		"Trailing paragraph.",
	}
	dirs := scanAll(body)
	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1", len(dirs))
	}
	d := dirs[0]
	want := strings.Join(body[1:], "\n")
	// Allow trailing blank stripping.
	want = strings.TrimRight(want, "\n")
	if d.Body != want {
		t.Errorf("body mismatch:\n got: %q\nwant: %q", d.Body, want)
	}
}

func TestDirectiveScanner_NoPathDirectiveDropped(t *testing.T) {
	// `/spec-new` with nothing after it is malformed — dropped silently.
	dirs := scanAll([]string{
		`/spec-new`,
		"follow-up line",
	})
	if len(dirs) != 0 {
		t.Fatalf("got %d directives, want 0", len(dirs))
	}
}

// ---------------------------------------------------------------------------
// extractAssistantLines — NDJSON text extraction.
// ---------------------------------------------------------------------------

func TestExtractAssistantLines_TextBlocksInOrder(t *testing.T) {
	raw := []byte(`{"type":"system","session_id":"s"}
{"type":"assistant","message":{"content":[{"type":"text","text":"hello\nworld"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"more\nstuff"}]}}
{"type":"result","result":"hello\nworld\nmore\nstuff"}
`)
	got := extractAssistantLines(raw)
	want := []string{"hello", "world", "more", "stuff"}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d (%q)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExtractAssistantLines_SkipsNonTextBlocks(t *testing.T) {
	raw := []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"only this"}]}}`)
	got := extractAssistantLines(raw)
	if len(got) != 1 || got[0] != "only this" {
		t.Errorf("got %q, want [\"only this\"]", got)
	}
}

// ---------------------------------------------------------------------------
// scaffoldDirective / processDirectives — file-system integration.
// ---------------------------------------------------------------------------

func TestScaffoldDirective_CreatesFileWithFrontmatter(t *testing.T) {
	ws := t.TempDir()
	d := Directive{
		Path:  "specs/local/demo.md",
		Title: "Demo",
		Body:  "## Problem\n\nHand-written body content.",
	}
	now := time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC)
	abs, err := scaffoldDirective(ws, d, now)
	if err != nil {
		t.Fatalf("scaffoldDirective: %v", err)
	}
	want := filepath.Join(ws, "specs/local/demo.md")
	if abs != want {
		t.Errorf("abs path = %q, want %q", abs, want)
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read scaffolded file: %v", err)
	}
	s := string(content)
	if !strings.HasPrefix(s, "---\n") {
		t.Errorf("expected frontmatter, got: %q", s[:60])
	}
	if !strings.Contains(s, "title: Demo") {
		t.Errorf("missing title in frontmatter: %s", s)
	}
	if !strings.Contains(s, "Hand-written body content.") {
		t.Errorf("body content not appended: %s", s)
	}
	// The body must appear AFTER the closing frontmatter fence.
	idx := strings.Index(s, "---\n")
	idx2 := strings.Index(s[idx+4:], "---\n")
	if idx2 < 0 {
		t.Fatalf("malformed frontmatter (no closing fence)")
	}
	tail := s[idx+4+idx2+4:]
	if !strings.Contains(tail, "Hand-written body content.") {
		t.Errorf("body not in file tail: %q", tail)
	}
}

func TestScaffoldDirective_CollisionReturnsError(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, "specs/local/collide.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := scaffoldDirective(ws, Directive{Path: "specs/local/collide.md"}, time.Now())
	if err == nil {
		t.Fatalf("expected collision error, got nil")
	}
}

func TestScaffoldDirective_InvalidPathReturnsError(t *testing.T) {
	ws := t.TempDir()
	// Path without the "specs/<track>/" prefix is rejected by ValidateSpecPath.
	_, err := scaffoldDirective(ws, Directive{Path: "foo.md"}, time.Now())
	if err == nil {
		t.Fatalf("expected invalid-path error, got nil")
	}
}

func TestProcessDirectives_HappyPath(t *testing.T) {
	ws := t.TempDir()
	dirs := []Directive{
		{Path: "specs/local/one.md", Body: "body one"},
		{Path: "specs/local/two.md", Body: "body two"},
	}
	sys := processDirectives(ws, dirs, "", time.Now())
	if len(sys) != 0 {
		t.Fatalf("expected no system messages on success, got %d: %+v", len(sys), sys)
	}
	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(ws, d.Path)); err != nil {
			t.Errorf("missing scaffolded file %s: %v", d.Path, err)
		}
	}
}

func TestProcessDirectives_ErrorSurfacesSystemMessage(t *testing.T) {
	ws := t.TempDir()
	// Pre-populate a collision.
	path := filepath.Join(ws, "specs/local/collide.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs := []Directive{
		{Path: "specs/local/collide.md"},
		{Path: "specs/local/clean.md"},
	}
	sys := processDirectives(ws, dirs, "focus.md", time.Now())
	if len(sys) != 1 {
		t.Fatalf("expected 1 system message, got %d: %+v", len(sys), sys)
	}
	if sys[0].Role != "system" {
		t.Errorf("role = %q, want system", sys[0].Role)
	}
	if !strings.Contains(sys[0].Content, "specs/local/collide.md") {
		t.Errorf("content missing path: %q", sys[0].Content)
	}
	if sys[0].FocusedSpec != "focus.md" {
		t.Errorf("focused_spec = %q, want focus.md", sys[0].FocusedSpec)
	}
	// The clean directive still scaffolded.
	if _, err := os.Stat(filepath.Join(ws, "specs/local/clean.md")); err != nil {
		t.Errorf("clean spec not scaffolded after a prior failure: %v", err)
	}
}

func TestProcessDirectives_NoWorkspaceReturnsSystemMessage(t *testing.T) {
	sys := processDirectives("", []Directive{{Path: "specs/local/x.md"}}, "", time.Now())
	if len(sys) != 1 || sys[0].Role != "system" {
		t.Fatalf("expected single system message, got %+v", sys)
	}
}

// ---------------------------------------------------------------------------
// README auto-create — readme-autocreate spec.
// ---------------------------------------------------------------------------

func TestScaffoldDirective_FirstScaffoldCreatesReadme(t *testing.T) {
	ws := t.TempDir()
	d := Directive{
		Path:   "specs/local/auth.md",
		Title:  "Auth refactor",
		Status: spec.StatusDrafted,
		Body:   "Rework the session token storage to meet the new compliance rule. Extra paragraph describing approach.",
	}
	if _, err := scaffoldDirective(ws, d, time.Now()); err != nil {
		t.Fatalf("scaffoldDirective: %v", err)
	}
	readme, err := os.ReadFile(filepath.Join(ws, "specs", "README.md"))
	if err != nil {
		t.Fatalf("read readme: %v", err)
	}
	body := string(readme)
	if !strings.Contains(body, "## Local Product") {
		t.Errorf("missing track heading: %s", body)
	}
	if !strings.Contains(body, "| [auth.md](local/auth.md) | Drafted | Rework the session token storage to meet the new compliance rule. |") {
		t.Errorf("row missing or summary wrong: %s", body)
	}
}

func TestScaffoldDirective_SecondScaffoldAppendsRow(t *testing.T) {
	ws := t.TempDir()
	first := Directive{Path: "specs/local/one.md", Body: "Entry one summary."}
	if _, err := scaffoldDirective(ws, first, time.Now()); err != nil {
		t.Fatalf("first scaffold: %v", err)
	}
	second := Directive{Path: "specs/local/two.md", Body: "Entry two summary."}
	if _, err := scaffoldDirective(ws, second, time.Now()); err != nil {
		t.Fatalf("second scaffold: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(ws, "specs", "README.md"))
	if err != nil {
		t.Fatalf("read readme: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, "[one.md](local/one.md)") {
		t.Errorf("first row missing: %s", got)
	}
	if !strings.Contains(got, "[two.md](local/two.md)") {
		t.Errorf("second row missing: %s", got)
	}
	oneIdx := strings.Index(got, "[one.md]")
	twoIdx := strings.Index(got, "[two.md]")
	if oneIdx > twoIdx {
		t.Errorf("row order wrong: one=%d, two=%d", oneIdx, twoIdx)
	}
}

// ---------------------------------------------------------------------------
// applySlashSpecNew + resolveUniqueSpecPath — create-command-expansion spec.
// ---------------------------------------------------------------------------

func TestResolveUniqueSpecPath_NoCollisionPassThrough(t *testing.T) {
	ws := t.TempDir()
	got := resolveUniqueSpecPath(ws, "specs/local/auth.md")
	if got != "specs/local/auth.md" {
		t.Errorf("got %q, want unchanged", got)
	}
}

func TestResolveUniqueSpecPath_CollisionAppendsSuffix(t *testing.T) {
	ws := t.TempDir()
	// Pre-create auth.md so the first candidate collides.
	dir := filepath.Join(ws, "specs/local")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := resolveUniqueSpecPath(ws, "specs/local/auth.md")
	if got != "specs/local/auth-2.md" {
		t.Errorf("got %q, want specs/local/auth-2.md", got)
	}
	// A second collision bumps to -3.
	if err := os.WriteFile(filepath.Join(dir, "auth-2.md"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	got2 := resolveUniqueSpecPath(ws, "specs/local/auth.md")
	if got2 != "specs/local/auth-3.md" {
		t.Errorf("got %q, want specs/local/auth-3.md", got2)
	}
}

func TestApplySlashSpecNew_ScaffoldsAndStripsDirective(t *testing.T) {
	ws := t.TempDir()
	prompt := `/spec-new specs/local/auth.md title="Auth"
User requested a spec with title "Auth". Write a first-draft body for it below.
`
	rest, path, err := applySlashSpecNew(prompt, ws, time.Now())
	if err != nil {
		t.Fatalf("applySlashSpecNew: %v", err)
	}
	if path != "specs/local/auth.md" {
		t.Errorf("created path = %q, want specs/local/auth.md", path)
	}
	// The directive line is removed from the agent-facing prompt.
	if strings.HasPrefix(rest, "/spec-new") {
		t.Errorf("expected directive stripped; got: %q", rest)
	}
	if !strings.Contains(rest, "Write a first-draft body") {
		t.Errorf("body instruction missing: %q", rest)
	}
	// File exists with frontmatter.
	body, err := os.ReadFile(filepath.Join(ws, path))
	if err != nil {
		t.Fatalf("read scaffolded file: %v", err)
	}
	if !strings.Contains(string(body), "title: Auth") {
		t.Errorf("frontmatter missing title: %s", body)
	}
}

func TestApplySlashSpecNew_CollisionBumpsSuffix(t *testing.T) {
	ws := t.TempDir()
	// Pre-create a collision.
	dir := filepath.Join(ws, "specs/local")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt := `/spec-new specs/local/auth.md title="Auth"
Body.
`
	_, path, err := applySlashSpecNew(prompt, ws, time.Now())
	if err != nil {
		t.Fatalf("applySlashSpecNew: %v", err)
	}
	if path != "specs/local/auth-2.md" {
		t.Errorf("path = %q, want specs/local/auth-2.md", path)
	}
	if _, err := os.Stat(filepath.Join(ws, path)); err != nil {
		t.Errorf("spec not created at %s: %v", path, err)
	}
}

func TestApplySlashSpecNew_NoDirectivePassThrough(t *testing.T) {
	prompt := "Just a plain message.\n"
	rest, path, err := applySlashSpecNew(prompt, t.TempDir(), time.Now())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if path != "" {
		t.Errorf("path = %q, want empty", path)
	}
	if rest != prompt {
		t.Errorf("prompt mutated: %q", rest)
	}
}

func TestApplySlashSpecNew_InvalidDirectiveErrors(t *testing.T) {
	ws := t.TempDir()
	// No path after /spec-new — invalid.
	prompt := "/spec-new\nsome body\n"
	_, _, err := applySlashSpecNew(prompt, ws, time.Now())
	if err == nil {
		t.Fatalf("expected error for missing path")
	}
	// Path missing the specs/<track>/ prefix — invalid.
	prompt2 := "/spec-new foo.md\nbody\n"
	_, _, err2 := applySlashSpecNew(prompt2, ws, time.Now())
	if err2 == nil {
		t.Fatalf("expected error for bad path")
	}
}

func TestApplySlashSpecNew_EmptySlugFromBlankCreate(t *testing.T) {
	// Simulates `/create` (no args) after template expansion: the
	// slug helper returns "" so the directive path becomes
	// specs/local/.md — invalid. applySlashSpecNew must surface that.
	ws := t.TempDir()
	prompt := "/spec-new specs/local/.md title=\"\"\nbody\n"
	_, _, err := applySlashSpecNew(prompt, ws, time.Now())
	if err == nil {
		t.Fatalf("expected error for empty slug path")
	}
}

func TestFirstSentence(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"A short one-liner.", "A short one-liner."},
		{"First sentence. Second one.", "First sentence."},
		{"Question? Not this.", "Question?"},
		{"## Heading\n\nBody sentence. Extra.", "Body sentence."},
		{"```\ncode block\n```\nProse sentence. Rest.", "Prose sentence."},
		{"<!-- comment -->\nReal prose here.", "Real prose here."},
		{"No terminator but just text", "No terminator but just text"},
	}
	for _, tc := range cases {
		if got := firstSentence(tc.in); got != tc.want {
			t.Errorf("firstSentence(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
