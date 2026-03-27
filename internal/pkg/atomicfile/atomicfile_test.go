package atomicfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

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
