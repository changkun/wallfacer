package handler

import (
	"context"
	"testing"
	"time"
)

// endlessReader always yields a chunk and never observes ctx, so the only
// place cancellation can be honored is the channel send inside pumpChunks.
type endlessReader struct{}

func (endlessReader) ReadChunk(context.Context) ([]byte, error) {
	return []byte("x"), nil
}

// TestPumpChunks_ExitsOnCancelWithFullBuffer reproduces the live-log relay
// goroutine leak: with a disconnected consumer (cancelled ctx) and a full
// channel buffer, the producer must still exit. Before the fix the unguarded
// `ch <- data` blocked forever; the test would hang and fail.
func TestPumpChunks_ExitsOnCancelWithFullBuffer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan []byte, 4) // matches the production buffer size
	done := make(chan struct{})
	go func() {
		pumpChunks(ctx, endlessReader{}, ch)
		close(done)
	}()

	// Give the producer time to fill the buffer and block on the next send.
	// No one drains ch, simulating a client that has gone away.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// pumpChunks returned: the goroutine did not leak.
	case <-time.After(2 * time.Second):
		t.Fatal("pumpChunks did not exit after context cancel with a full buffer (goroutine leak)")
	}
}
