package ndjson

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type record struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestReadFile_HappyPath(t *testing.T) {
	path := writeTempFile(t, `{"name":"a","value":1}
{"name":"b","value":2}
`)
	got, err := ReadFile[record](path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	if got[0].Name != "a" || got[1].Value != 2 {
		t.Fatalf("unexpected records: %+v", got)
	}
}

func TestReadFile_MissingFile(t *testing.T) {
	got, err := ReadFile[record](filepath.Join(t.TempDir(), "nonexistent.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d", len(got))
	}
}

func TestReadFile_SkipsEmptyAndBlankLines(t *testing.T) {
	path := writeTempFile(t, `{"name":"a","value":1}


{"name":"b","value":2}
`)
	got, err := ReadFile[record](path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
}

func TestReadFile_SkipsMalformedLines(t *testing.T) {
	path := writeTempFile(t, `{"name":"a","value":1}
NOT JSON
{"name":"b","value":2}
`)
	got, err := ReadFile[record](path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
}

func TestReadFile_OnErrorCallback(t *testing.T) {
	path := writeTempFile(t, `{"name":"a","value":1}
BAD LINE
{"name":"b","value":2}
`)
	var errLines []int
	got, err := ReadFile[record](path, WithOnError(func(lineNum int, _ error) {
		errLines = append(errLines, lineNum)
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	if len(errLines) != 1 || errLines[0] != 2 {
		t.Fatalf("expected error on line 2, got %v", errLines)
	}
}

func TestReadFile_CustomBufferSize(t *testing.T) {
	// Create a line longer than the default 64KB scanner buffer.
	longVal := strings.Repeat("x", 100_000)
	path := writeTempFile(t, `{"name":"`+longVal+`","value":1}`+"\n")

	got, err := ReadFile[record](path, WithBufferSize(64*1024, 1024*1024))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	if got[0].Name != longVal {
		t.Fatal("long value mismatch")
	}
}

func TestReadFileFunc_Filter(t *testing.T) {
	path := writeTempFile(t, `{"name":"a","value":1}
{"name":"b","value":2}
{"name":"c","value":3}
`)
	var filtered []record
	err := ReadFileFunc[record](path, func(r record) bool {
		if r.Value >= 2 {
			filtered = append(filtered, r)
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 2 {
		t.Fatalf("got %d records, want 2", len(filtered))
	}
}

func TestReadFileFunc_EarlyStop(t *testing.T) {
	path := writeTempFile(t, `{"name":"a","value":1}
{"name":"b","value":2}
{"name":"c","value":3}
`)
	var collected []record
	err := ReadFileFunc[record](path, func(r record) bool {
		collected = append(collected, r)
		return r.Name != "b" // stop after "b"
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(collected) != 2 {
		t.Fatalf("got %d records, want 2 (a and b)", len(collected))
	}
}

func TestReadFileFunc_MissingFile(t *testing.T) {
	called := false
	err := ReadFileFunc[record](filepath.Join(t.TempDir(), "nope.jsonl"), func(_ record) bool {
		called = true
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("callback should not have been called for missing file")
	}
}

func TestAppendFile_CreatesAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")

	if err := AppendFile(path, record{Name: "a", Value: 1}); err != nil {
		t.Fatal(err)
	}
	if err := AppendFile(path, record{Name: "b", Value: 2}); err != nil {
		t.Fatal(err)
	}

	got, err := ReadFile[record](path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("unexpected records: %+v", got)
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.jsonl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
