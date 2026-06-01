package yamldir

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestReadAll_EmptyDir(t *testing.T) {
	files, err := ReadAll("test", "")
	if err != nil {
		t.Fatalf("ReadAll empty: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil files for empty dir, got %d", len(files))
	}
}

func TestReadAll_MissingDir(t *testing.T) {
	files, err := ReadAll("test", filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("ReadAll missing: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil files for missing dir, got %d", len(files))
	}
}

func TestReadAll_PicksOnlyYAML(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("a.yaml", "slug: a\n")
	write("b.yml", "slug: b\n")
	write("README.md", "skip me")
	write("notes.txt", "skip me too")
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	files, err := ReadAll("test", dir)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}

	got := make([]string, len(files))
	for i, f := range files {
		got[i] = filepath.Base(f.Path)
	}
	sort.Strings(got)
	want := []string{"a.yaml", "b.yml"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("file[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReadAll_BodiesIntact(t *testing.T) {
	dir := t.TempDir()
	content := "slug: x\ntitle: Test\n"
	if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	files, err := ReadAll("test", dir)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if string(files[0].Body) != content {
		t.Errorf("body = %q, want %q", files[0].Body, content)
	}
	if !strings.HasSuffix(files[0].Path, "x.yaml") {
		t.Errorf("path = %q, want suffix x.yaml", files[0].Path)
	}
}
