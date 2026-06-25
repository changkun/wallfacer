package handler

import (
	"os"
	"path/filepath"
	"testing"
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
