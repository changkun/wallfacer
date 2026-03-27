package ndjson

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// record is a minimal struct used across all NDJSON tests.
type record struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// TestReadFile_HappyPath validates that a well-formed NDJSON file is decoded
// into the expected slice of records.
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

// TestReadFile_MissingFile verifies that a nonexistent file returns a
// non-nil empty slice and no error (missing = empty, not an error).
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

// TestReadFile_SkipsEmptyAndBlankLines verifies that blank lines between
// records are silently ignored.
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

// TestReadFile_SkipsMalformedLines verifies that lines containing invalid
// JSON are skipped without returning an error.
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

// TestReadFile_OnErrorCallback verifies that WithOnError receives the
// correct 1-based line number for malformed JSON lines.
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

// TestReadFile_CustomBufferSize verifies that WithBufferSize allows reading
// lines that exceed the default 64 KB scanner buffer.
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

// TestReadFileFunc_Filter verifies that ReadFileFunc iterates all records and
// the callback can selectively collect a subset.
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

// TestReadFileFunc_EarlyStop verifies that returning false from the callback
// stops iteration -- only records up to and including the stop are seen.
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

// TestReadFileFunc_MissingFile verifies that a nonexistent file returns
// nil error and the callback is never invoked.
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

// TestAppendFile_CreatesAndAppends verifies that AppendFile creates the file
// on first call and subsequent calls append records that can be read back.
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

// TestReadFile_OpenError verifies that ReadFile returns an error when
// the file exists but is not readable (permission denied).
func TestReadFile_OpenError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0000 does not restrict reads on Windows")
	}
	// A path that exists but is a directory, not a file — os.Open succeeds
	// but scanning will yield zero records. Instead, use a permission error.
	dir := t.TempDir()
	path := filepath.Join(dir, "noread.jsonl")
	if err := os.WriteFile(path, []byte(`{"name":"a","value":1}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0644) })

	_, err := ReadFile[record](path)
	if err == nil {
		t.Fatal("expected permission error")
	}
}

// TestReadFileFunc_OpenError verifies that ReadFileFunc returns an error
// when the file exists but is not readable (permission denied).
func TestReadFileFunc_OpenError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0000 does not restrict reads on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "noread.jsonl")
	if err := os.WriteFile(path, []byte(`{"name":"a","value":1}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0644) })

	err := ReadFileFunc[record](path, func(_ record) bool { return true })
	if err == nil {
		t.Fatal("expected permission error")
	}
}

// TestReadFileFunc_SkipsEmptyLines verifies that blank lines between
// records are silently ignored when using the callback-based reader.
func TestReadFileFunc_SkipsEmptyLines(t *testing.T) {
	path := writeTempFile(t, `{"name":"a","value":1}

{"name":"b","value":2}
`)
	var got []record
	err := ReadFileFunc[record](path, func(r record) bool {
		got = append(got, r)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
}

// TestReadFileFunc_OnErrorCallback verifies that WithOnError receives the
// correct line number for malformed lines when using ReadFileFunc.
func TestReadFileFunc_OnErrorCallback(t *testing.T) {
	path := writeTempFile(t, `{"name":"a","value":1}
BAD LINE
{"name":"b","value":2}
`)
	var errLines []int
	err := ReadFileFunc[record](path, func(_ record) bool {
		return true
	}, WithOnError(func(lineNum int, _ error) {
		errLines = append(errLines, lineNum)
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(errLines) != 1 || errLines[0] != 2 {
		t.Fatalf("expected error on line 2, got %v", errLines)
	}
}

// TestReadFileFunc_CustomBufferSize verifies that WithBufferSize allows
// ReadFileFunc to handle lines exceeding the default scanner buffer.
func TestReadFileFunc_CustomBufferSize(t *testing.T) {
	longVal := strings.Repeat("x", 100_000)
	path := writeTempFile(t, `{"name":"`+longVal+`","value":1}`+"\n")

	var got []record
	err := ReadFileFunc[record](path, func(r record) bool {
		got = append(got, r)
		return true
	}, WithBufferSize(64*1024, 1024*1024))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != longVal {
		t.Fatal("expected long record")
	}
}

// TestAppendFile_MarshalError verifies that AppendFile returns an error
// for unmarshalable types (channels) without creating a file.
func TestAppendFile_MarshalError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	// channels cannot be marshaled to JSON.
	err := AppendFile(path, make(chan int))
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

// TestAppendFile_OpenError verifies that AppendFile returns an error when
// the parent directory does not exist.
func TestAppendFile_OpenError(t *testing.T) {
	// Try to write to a path inside a non-existent directory.
	err := AppendFile(filepath.Join(t.TempDir(), "nodir", "sub", "out.jsonl"), record{Name: "a"})
	if err == nil {
		t.Fatal("expected open error")
	}
}

// TestReadFile_ScannerError verifies that ReadFile surfaces a scanner error
// when a line exceeds the default buffer size and no custom buffer is set.
func TestReadFile_ScannerError(t *testing.T) {
	// Create a file with a line longer than the default scanner buffer
	// (64KB) WITHOUT using WithBufferSize, so the scanner hits its limit.
	longLine := `{"name":"` + strings.Repeat("x", 70_000) + `","value":1}`
	path := writeTempFile(t, longLine+"\n")

	_, err := ReadFile[record](path)
	if err == nil {
		t.Fatal("expected scanner error for line exceeding buffer")
	}
}

// TestReadFileFunc_ScannerError verifies that ReadFileFunc surfaces a scanner
// error when a line exceeds the default buffer size.
func TestReadFileFunc_ScannerError(t *testing.T) {
	longLine := `{"name":"` + strings.Repeat("x", 70_000) + `","value":1}`
	path := writeTempFile(t, longLine+"\n")

	err := ReadFileFunc[record](path, func(_ record) bool { return true })
	if err == nil {
		t.Fatal("expected scanner error for line exceeding buffer")
	}
}

// errCloser wraps an io.Reader and injects a configurable error on Close,
// used to test error propagation from the underlying ReadCloser.
type errCloser struct {
	io.Reader
	closeErr error
}

func (e *errCloser) Close() error { return e.closeErr }

// errWriteCloser injects configurable errors on Write and/or Close,
// used to test error propagation in appendTo.
type errWriteCloser struct {
	writeErr error
	closeErr error
	closed   bool
}

func (e *errWriteCloser) Write([]byte) (int, error) {
	if e.writeErr != nil {
		return 0, e.writeErr
	}
	return 0, nil
}

func (e *errWriteCloser) Close() error {
	e.closed = true
	return e.closeErr
}

// TestReadAll_CloseError verifies that readAll propagates errors from
// the underlying ReadCloser's Close method.
func TestReadAll_CloseError(t *testing.T) {
	data := `{"name":"a","value":1}` + "\n"
	rc := &errCloser{
		Reader:   strings.NewReader(data),
		closeErr: errors.New("close failed"),
	}
	_, err := readAll[record](rc, &config{})
	if err == nil || err.Error() != "close failed" {
		t.Fatalf("expected close error, got %v", err)
	}
}

// TestReadFunc_CloseError verifies that readFunc propagates errors from
// the underlying ReadCloser's Close method.
func TestReadFunc_CloseError(t *testing.T) {
	data := `{"name":"a","value":1}` + "\n"
	rc := &errCloser{
		Reader:   strings.NewReader(data),
		closeErr: errors.New("close failed"),
	}
	err := readFunc[record](rc, func(_ record) bool { return true }, config{})
	if err == nil || err.Error() != "close failed" {
		t.Fatalf("expected close error, got %v", err)
	}
}

// TestAppendTo_WriteError verifies that appendTo returns the write error
// and still calls Close on the WriteCloser.
func TestAppendTo_WriteError(t *testing.T) {
	wc := &errWriteCloser{writeErr: errors.New("write failed")}
	err := appendTo(wc, []byte(`{"name":"a"}`))
	if err == nil || err.Error() != "write failed" {
		t.Fatalf("expected write error, got %v", err)
	}
	if !wc.closed {
		t.Fatal("expected Close to be called on write error")
	}
}

// TestAppendTo_CloseError verifies that appendTo propagates errors from
// the WriteCloser's Close method when the write itself succeeds.
func TestAppendTo_CloseError(t *testing.T) {
	wc := &errWriteCloser{closeErr: errors.New("close failed")}
	err := appendTo(wc, []byte(`{"name":"a"}`))
	if err == nil || err.Error() != "close failed" {
		t.Fatalf("expected close error, got %v", err)
	}
}

// TestReadFile_EmptyFile verifies that an empty file returns a non-nil
// empty slice (consistent with the missing-file behavior).
func TestReadFile_EmptyFile(t *testing.T) {
	path := writeTempFile(t, "")
	got, err := ReadFile[record](path)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 records, got %d", len(got))
	}
}

// writeTempFile is a test helper that writes content to a temporary JSONL
// file and returns its path.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.jsonl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
