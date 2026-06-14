package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateInstanceID_StableAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrCreateInstanceID(dir)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if !strings.HasPrefix(first, "inst_") {
		t.Fatalf("id %q missing inst_ prefix", first)
	}
	// A restart (second call against the same data dir) reuses the same id.
	second, err := LoadOrCreateInstanceID(dir)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first != second {
		t.Fatalf("id not stable: %q != %q", first, second)
	}
}

func TestLoadOrCreateInstanceID_PerDataDir(t *testing.T) {
	a, err := LoadOrCreateInstanceID(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadOrCreateInstanceID(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("distinct data dirs share an id: %q", a)
	}
}

func TestLoadOrCreateInstanceID_RegeneratesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, instanceIDFile)
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := LoadOrCreateInstanceID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !validInstanceID(id) {
		t.Fatalf("corrupt file not regenerated: got %q", id)
	}
}

func TestLoadOrCreateInstanceID_Persisted0600(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreateInstanceID(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, instanceIDFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("instance-id perm = %o, want 600", perm)
	}
}
