package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/spec"
	"latere.ai/x/wallfacer/internal/speccomment"
)

// TestSpecFilePath verifies the path resolution tolerates both conventions: the
// frontend's focusedSpecPath carries the leading "specs/" while the spec-tree
// node path omits it. A mismatch here is what made every comment POST 400.
func TestSpecFilePath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs", "cloud"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "specs", "cloud", "x.md")
	if err := os.WriteFile(want, []byte("# X\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The frontend sends the path WITH the specs/ prefix.
	if got, ok := specFilePath(root, "specs/cloud/x.md"); !ok || got != want {
		t.Fatalf("prefixed path: got %q ok=%v, want %q true", got, ok, want)
	}
	// The spec-tree node path omits it.
	if got, ok := specFilePath(root, "cloud/x.md"); !ok || got != want {
		t.Fatalf("bare path: got %q ok=%v, want %q true", got, ok, want)
	}
	// A path that does not exist resolves to nothing (not a false 400/match).
	if _, ok := specFilePath(root, "cloud/missing.md"); ok {
		t.Fatal("nonexistent spec should not resolve")
	}
	// A directory is not a spec file.
	if _, ok := specFilePath(root, "specs/cloud"); ok {
		t.Fatal("a directory should not resolve as a spec file")
	}
}

// TestSpecFilePathRejectsWorkspaceEscape verifies that a browser-supplied spec
// path cannot traverse out of the workspace. SubmitSpecComment reads the
// resolved file (os.ReadFile) and resolveSpecRepo stats it, so an unguarded
// "../" would let a comment op read or anchor against files outside the tree.
// findSpecFile already rejects such escapes; specFilePath must match.
func TestSpecFilePathRejectsWorkspaceEscape(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(ws, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A real, readable file outside the workspace that a traversal would target.
	outside := filepath.Join(root, "secret.md")
	if err := os.WriteFile(outside, []byte("# secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	escapes := []string{
		"../secret.md",
		"../../secret.md",
		"specs/../../secret.md",
	}
	for _, p := range escapes {
		if got, ok := specFilePath(ws, p); ok {
			t.Errorf("escape %q resolved to %q, want rejected", p, got)
		}
	}
}

// TestRepositionThreadMultiLineNotOrphaned reproduces the user-facing bug: a
// multi-line comment created on a real spec must reattach INLINE on the next
// load, not land in triage as orphaned. It drives the exact instance-side path
// (specFilePath -> ParseBytes -> ComputeAnchor on create, then repositionThread
// on GET) with the "specs/"-prefixed path the frontend actually sends.
func TestRepositionThreadMultiLineNotOrphaned(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ntitle: T\n---\n\n# Heading\n\nFirst line of a paragraph.\nSecond line continues.\nThird line ends it.\n"
	if err := os.WriteFile(filepath.Join(root, "specs", "x.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create: compute a multi-line anchor exactly as SubmitSpecComment does.
	parsed, err := spec.ParseBytes([]byte(content), "x.md")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bodyLines := strings.Split(parsed.Body, "\n")
	start, end := 0, 0
	for i, l := range bodyLines {
		if strings.HasPrefix(l, "First line") {
			start = i + 1
		}
		if strings.HasPrefix(l, "Third line") {
			end = i + 1
		}
	}
	if start == 0 || end <= start {
		t.Fatalf("fixture lines not found: start=%d end=%d", start, end)
	}
	anchor := spec.ComputeAnchor(parsed.Body, start, end)

	// GET: the frontend sends spec_path WITH the leading specs/ prefix.
	thread := speccomment.Thread{SpecPath: "specs/x.md", Anchor: anchor, Status: speccomment.StatusActive}
	got := repositionThread(thread, root)
	if got.Orphaned {
		t.Fatal("multi-line comment orphaned on display (the triage bug)")
	}
	if got.Line != start {
		t.Fatalf("reattached to line %d, want the range start %d", got.Line, start)
	}
}

// TestSubmitSpecComment_FreeFormSpec reproduces the user-facing 422: creating a
// comment on a free-form spec (no YAML frontmatter, rendered as a read-only doc
// node) returned "spec parse failed" because the create path treated a missing
// frontmatter as a fatal parse error. For such files the whole document is the
// anchoring body. The created anchor must also round-trip through
// repositionThread without orphaning, the property the shared body helper
// guarantees by computing the same body on both paths.
func TestSubmitSpecComment_FreeFormSpec(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	specsDir := filepath.Join(root, "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A free-form spec: no "---" frontmatter, just markdown. Mirrors the user's
	// specs/00-overview.md. Line 3 is the paragraph text.
	content := "# Overview\n\nThis is a free-form spec.\nNo frontmatter here.\n"
	if err := os.WriteFile(filepath.Join(specsDir, "00-overview.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// resolveSpecRepo needs a git remote, else it 400s before the parse runs.
	for _, args := range [][]string{
		{"init"}, {"remote", "add", "origin", "https://github.com/acme/widgets.git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	var captured speccomment.Event
	relay := NewCommentRelay()
	relay.SetSendUp(func(ev speccomment.Event) error { captured = ev; return nil })
	h := &Handler{workspaces: []string{root}}
	h.SetCommentRelay(relay)

	// The exact POST the browser sent: select line 3, comment "ok".
	body := `{"op":"create","spec":"specs/00-overview.md","body":"ok","start_line":3,"end_line":3}`
	req := httptest.NewRequest(http.MethodPost, "/api/spec-comments", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.SubmitSpecComment(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s), want 202; a free-form spec must not parse-fail",
			w.Code, strings.TrimSpace(w.Body.String()))
	}
	if captured.Thread == nil {
		t.Fatal("no event forwarded to the coordinator")
	}

	// Round-trip: the anchor created above must reattach inline on the next load,
	// not orphan, because reposition reads the same free-form body.
	thread := speccomment.Thread{
		SpecPath: "specs/00-overview.md",
		Anchor:   captured.Thread.Anchor,
		Status:   speccomment.StatusActive,
	}
	got := repositionThread(thread, root)
	if got.Orphaned {
		t.Fatal("free-form comment orphaned on reload (anchor body mismatch)")
	}
	if got.Line != 3 {
		t.Fatalf("reattached to line %d, want 3 (the selected line)", got.Line)
	}
}

// TestGitObjectSHAs verifies the advisory anchor metadata: a committed spec
// yields a non-empty commit and blob, and the blob changes after an edit (the
// signal the outdated/out-of-sync banner is built on).
func TestGitObjectSHAs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	specsDir := filepath.Join(root, "specs", "cloud")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specRel := "cloud/x.md"
	specPath := filepath.Join(root, "specs", specRel)
	if err := os.WriteFile(specPath, []byte("# X\n\nfirst line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"},
		{"add", "."}, {"commit", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	commit, blob := gitObjectSHAs(root, specRel)
	if commit == "" || blob == "" {
		t.Fatalf("expected non-empty commit and blob, got commit=%q blob=%q", commit, blob)
	}

	// Editing the file changes the blob hash (the outdated signal), commit stays.
	if err := os.WriteFile(specPath, []byte("# X\n\nfirst line edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit2, blob2 := gitObjectSHAs(root, specRel)
	if blob2 == blob {
		t.Fatal("blob hash should change after an edit (outdated signal)")
	}
	if commit2 != commit {
		t.Fatal("HEAD commit should not change on an uncommitted edit")
	}
}

// stubToggle is a CoordinationToggle whose state the test sets directly.
type stubToggle struct {
	optedIn, signedIn, connected, authRejected bool
}

func (s *stubToggle) OptedIn() bool      { return s.optedIn }
func (s *stubToggle) SetOptedIn(v bool)  { s.optedIn = v }
func (s *stubToggle) Connected() bool    { return s.connected }
func (s *stubToggle) SignedIn() bool     { return s.signedIn }
func (s *stubToggle) AuthRejected() bool { return s.authRejected }

// TestCoordinationStatusUnauthorized covers the surfaced auth-rejection state:
// signed in and opted in, but the coordinator refuses the token, must report
// state "unauthorized" (not an endless "connecting") so the failure is visible.
func TestCoordinationStatusUnauthorized(t *testing.T) {
	cases := []struct {
		name      string
		toggle    *stubToggle
		wantState string
	}{
		{"unauthorized", &stubToggle{optedIn: true, signedIn: true, authRejected: true}, "unauthorized"},
		{"connecting", &stubToggle{optedIn: true, signedIn: true}, "connecting"},
		{"connected wins over auth flag", &stubToggle{optedIn: true, signedIn: true, connected: true, authRejected: true}, "connected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{}
			h.SetCoordinationToggle(tc.toggle)
			w := httptest.NewRecorder()
			h.GetCoordinationStatus(w, httptest.NewRequest(http.MethodGet, "/api/coordination/status", nil))

			var got struct {
				State        string `json:"state"`
				AuthRejected bool   `json:"auth_rejected"`
			}
			if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.State != tc.wantState {
				t.Fatalf("state = %q, want %q", got.State, tc.wantState)
			}
			if got.AuthRejected != tc.toggle.authRejected {
				t.Fatalf("auth_rejected = %v, want %v", got.AuthRejected, tc.toggle.authRejected)
			}
		})
	}
}
