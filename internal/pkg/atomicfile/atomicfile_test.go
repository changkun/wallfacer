package atomicfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// swapHook replaces a package-level function variable for the duration of a
// test and restores it on cleanup. Returns a pointer so the caller can
// replace the value inline.
func swapHook[T any](t *testing.T, ptr *T, val T) {
	t.Helper()
	orig := *ptr
	*ptr = val
	t.Cleanup(func() { *ptr = orig })
}

// TestWrite_Success validates the happy path: data is written to the target
// path and the temporary file is cleaned up afterward.
func TestWrite_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	data := []byte("hello world")

	if err := Write(path, data, 0644); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}

	// Temp file must not remain.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file should not exist after successful write")
	}
}

// TestWrite_DirNotExist verifies that Write returns an error when the
// parent directory does not exist.
func TestWrite_DirNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "file.txt")
	if err := Write(path, []byte("x"), 0644); err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

// TestWriteJSON_Success verifies that WriteJSON marshals a value as indented
// JSON and the result can be read back and unmarshaled correctly.
func TestWriteJSON_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	v := map[string]int{"a": 1, "b": 2}

	if err := WriteJSON(path, v, 0644); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var result map[string]int
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if result["a"] != 1 || result["b"] != 2 {
		t.Fatalf("unexpected result: %v", result)
	}
}

// TestWriteJSON_MarshalError verifies that WriteJSON returns an error for
// unmarshalable types (channels) and does not leave a file on disk.
func TestWriteJSON_MarshalError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	// Channels cannot be marshaled to JSON.
	if err := WriteJSON(path, make(chan int), 0644); err == nil {
		t.Fatal("expected marshal error")
	}
	// File must not exist.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file should not exist after marshal error")
	}
}

// TestWrite_Overwrite verifies that a second Write to the same path
// atomically replaces the file content.
func TestWrite_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	if err := Write(path, []byte("first"), 0644); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if err := Write(path, []byte("second"), 0644); err != nil {
		t.Fatalf("second Write: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "second" {
		t.Fatalf("got %q, want %q", got, "second")
	}
}

// TestWrite_WriteError verifies that a write error is returned and the
// temp file is cleaned up.
func TestWrite_WriteError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	injected := errors.New("injected write error")
	swapHook(t, &writeFile, func(f *os.File, data []byte) (int, error) {
		return 0, injected
	})

	err := Write(path, []byte("data"), 0644)
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected write error, got %v", err)
	}
	// Target must not exist.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("target file should not exist after write error")
	}
	// No leftover temp files.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("temp file %q was not cleaned up", e.Name())
		}
	}
}

// TestWrite_CloseError verifies that a close error is returned and the
// temp file is cleaned up.
func TestWrite_CloseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	injected := errors.New("injected close error")
	swapHook(t, &closeFile, func(f *os.File) error {
		_ = f.Close() // actually close the fd to avoid leaks
		return injected
	})

	err := Write(path, []byte("data"), 0644)
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected close error, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("target file should not exist after close error")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("temp file %q was not cleaned up", e.Name())
		}
	}
}

// TestWrite_ChmodError verifies that a chmod error is returned and the
// temp file is cleaned up.
func TestWrite_ChmodError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	injected := errors.New("injected chmod error")
	swapHook(t, &chmodPath, func(string, os.FileMode) error { return injected })

	err := Write(path, []byte("data"), 0644)
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected chmod error, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("target file should not exist after chmod error")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("temp file %q was not cleaned up", e.Name())
		}
	}
}

// TestWrite_RenameError verifies that a rename error is returned and the
// temp file is cleaned up.
func TestWrite_RenameError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	injected := errors.New("injected rename error")
	swapHook(t, &renamePath, func(string, string) error { return injected })

	err := Write(path, []byte("data"), 0644)
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected rename error, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("target file should not exist after rename error")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("temp file %q was not cleaned up", e.Name())
		}
	}
}

// TestWrite_Concurrent verifies that concurrent writes to the same path
// do not corrupt the file -- exactly one writer's content should survive.
func TestWrite_Concurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.txt")
	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			data := []byte{byte('A' + i)}
			_ = Write(path, data, 0644)
		}(i)
	}
	wg.Wait()

	// File must contain exactly one byte (one of the writers won).
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 byte, got %d", len(got))
	}
}
