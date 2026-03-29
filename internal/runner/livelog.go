package runner

import (
	"context"
	"io"
	"sync"
)

// liveLog is a concurrency-safe, append-only byte buffer that supports
// multiple independent readers. Writers append data via Write; readers
// created via NewReader receive both existing and future data. When the
// writer calls Close the buffer is sealed: subsequent reads drain any
// remaining data and then return io.EOF.
//
// liveLog is used to tee container stdout/stderr during execution so
// that the log-streaming HTTP handler can serve live output without
// relying on `docker logs` (which does not capture `exec` output for
// worker containers).
type liveLog struct {
	mu     sync.Mutex
	buf    []byte
	done   bool
	notify chan struct{} // closed-and-replaced on every mutation to wake readers
}

func newLiveLog() *liveLog {
	return &liveLog{notify: make(chan struct{})}
}

// Write appends p to the buffer and wakes all blocked readers.
// It is safe to call from multiple goroutines.
func (l *liveLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	l.buf = append(l.buf, p...)
	ch := l.notify
	l.notify = make(chan struct{})
	l.mu.Unlock()
	close(ch) // wake all waiting readers
	return len(p), nil
}

// Close seals the buffer. Readers that have consumed all data will
// receive io.EOF on subsequent reads.
func (l *liveLog) Close() {
	l.mu.Lock()
	if l.done {
		l.mu.Unlock()
		return
	}
	l.done = true
	ch := l.notify
	l.mu.Unlock()
	close(ch)
}

// snapshot returns the current buffer contents (as a slice into the
// internal array — caller must copy before releasing), whether the
// writer has closed, and a channel that will be closed when the next
// mutation occurs.
func (l *liveLog) snapshot() (buf []byte, done bool, wake <-chan struct{}) {
	l.mu.Lock()
	buf = l.buf
	done = l.done
	wake = l.notify
	l.mu.Unlock()
	return
}

// NewReader creates an independent reader positioned at the start of
// the buffer. Multiple readers may be active concurrently.
func (l *liveLog) NewReader() *LiveLogReader {
	return &LiveLogReader{log: l}
}

// LiveLogReader reads from a liveLog, blocking when caught up until
// new data arrives or the liveLog is closed.
type LiveLogReader struct {
	log    *liveLog
	offset int
}

// ReadChunk returns new data appended since the last call, blocking
// until data is available, the liveLog is closed (io.EOF), or the
// context is cancelled.
func (r *LiveLogReader) ReadChunk(ctx context.Context) ([]byte, error) {
	for {
		buf, done, wake := r.log.snapshot()
		if r.offset < len(buf) {
			data := make([]byte, len(buf)-r.offset)
			copy(data, buf[r.offset:])
			r.offset = len(buf)
			return data, nil
		}
		if done {
			return nil, io.EOF
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wake:
			// New data or close — re-check.
		}
	}
}
