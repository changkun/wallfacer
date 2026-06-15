package livelog

import (
	"context"
	"io"
	"sync"
	"testing"
)

// TestWriteAfterCloseNoPanic guards against a double-close of the notify
// channel: Close seals the buffer and closes l.notify; a Write arriving
// afterwards must not close that already-closed channel again.
func TestWriteAfterCloseNoPanic(t *testing.T) {
	l := New()
	l.Close()
	// Before the fix this panics with "close of closed channel".
	if n, err := l.Write([]byte("late")); n != 4 || err != nil {
		t.Fatalf("Write after Close = (%d, %v), want (4, nil)", n, err)
	}
}

// TestWriteAfterCloseConcurrent reproduces the live race where a writer is
// still appending (e.g. a tee goroutine draining buffered stdout) while
// another goroutine seals the log.
func TestWriteAfterCloseConcurrent(_ *testing.T) {
	for range 100 {
		l := New()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = l.Write([]byte("data")) }()
		go func() { defer wg.Done(); l.Close() }()
		wg.Wait()
	}
}

// TestPostCloseReaderGetsEOF confirms a write dropped after Close does not
// resurrect the sealed buffer: readers still observe EOF.
func TestPostCloseReaderGetsEOF(t *testing.T) {
	l := New()
	_, _ = l.Write([]byte("hello"))
	l.Close()
	_, _ = l.Write([]byte("dropped"))

	r := l.NewReader()
	got, err := r.ReadChunk(context.Background())
	if err != nil {
		t.Fatalf("first ReadChunk err = %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("ReadChunk = %q, want %q", got, "hello")
	}
	if _, err := r.ReadChunk(context.Background()); err != io.EOF {
		t.Fatalf("second ReadChunk err = %v, want io.EOF", err)
	}
}
