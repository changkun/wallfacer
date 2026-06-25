package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/spec"
)

func TestFindSpecFile_ResolvesArchived(t *testing.T) {
	ws := t.TempDir()
	// Only the archived (relocated) copy exists on disk.
	archAbs := filepath.Join(ws, "specs/.archive/local/foo.md")
	if err := os.MkdirAll(filepath.Dir(archAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archAbs, []byte("---\nstatus: archived\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Lookup by the LOGICAL path resolves to the archived file.
	got := findSpecFile([]string{ws}, "specs/local/foo.md")
	if got != archAbs {
		t.Errorf("findSpecFile(logical) = %q, want %q", got, archAbs)
	}

	// A live spec still resolves to its live path (preferred over archive).
	liveAbs := filepath.Join(ws, "specs/local/bar.md")
	if err := os.MkdirAll(filepath.Dir(liveAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(liveAbs, []byte("---\nstatus: drafted\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := findSpecFile([]string{ws}, "specs/local/bar.md"); got != liveAbs {
		t.Errorf("findSpecFile(live) = %q, want %q", got, liveAbs)
	}
}

func TestResolveSpecArchiveFallback(t *testing.T) {
	ws := t.TempDir()
	archAbs := filepath.Join(ws, "specs/.archive/local/foo.md")
	if err := os.MkdirAll(filepath.Dir(archAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archAbs, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A request for the logical (live) path rewrites to the archive location.
	logicalAbs := filepath.Join(ws, "specs/local/foo.md")
	if got := resolveSpecArchiveFallback(logicalAbs, ws); got != archAbs {
		t.Errorf("fallback = %q, want %q", got, archAbs)
	}

	// A non-spec missing path is returned unchanged.
	other := filepath.Join(ws, "internal/x/foo.go")
	if got := resolveSpecArchiveFallback(other, ws); got != other {
		t.Errorf("non-spec fallback = %q, want unchanged", got)
	}
}

func TestArchiveSpec_RelocatesToArchiveAndBack(t *testing.T) {
	repo := initPlanningTestRepo(t)
	h, ws := newTestHandlerWithWorkspacesFromRepo(t, repo)
	drafted := strings.Replace(testSpecValidated, "status: validated", "status: drafted", 1)
	writeTestSpec(t, ws, "specs/local/target.md", drafted)
	runGit(t, ws, "add", "specs/")
	runGit(t, ws, "commit", "-m", "seed")

	// Archive: the file physically relocates under .archive/ and leaves the live path.
	w := doTransition(t, h.ArchiveSpec, "specs/local/target.md")
	if w.Code != http.StatusOK {
		t.Fatalf("archive status = %d; body: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(ws, "specs/local/target.md")); !os.IsNotExist(err) {
		t.Errorf("live path should be gone after archive, stat err = %v", err)
	}
	archAbs := filepath.Join(ws, "specs/.archive/local/target.md")
	if _, err := os.Stat(archAbs); err != nil {
		t.Fatalf("archived file missing at %s: %v", archAbs, err)
	}
	if got := readStatus(t, ws, "specs/local/target.md"); got != spec.StatusArchived {
		t.Errorf("status = %q, want archived", got)
	}
	// The tree presents it at the logical path, archived.
	tree, _ := spec.BuildTree(filepath.Join(ws, "specs"))
	if n, ok := tree.All["specs/local/target.md"]; !ok || n.Value.Status != spec.StatusArchived {
		t.Errorf("tree node at logical path = %+v, want archived", n)
	}

	// Unarchive: moves back to the live path.
	w2 := doTransition(t, h.UnarchiveSpec, "specs/local/target.md")
	if w2.Code != http.StatusOK {
		t.Fatalf("unarchive status = %d; body: %s", w2.Code, w2.Body.String())
	}
	if _, err := os.Stat(filepath.Join(ws, "specs/local/target.md")); err != nil {
		t.Errorf("spec should be back at live path: %v", err)
	}
	if _, err := os.Stat(archAbs); !os.IsNotExist(err) {
		t.Errorf(".archive copy should be gone after unarchive, stat err = %v", err)
	}
}
