package runner

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

func TestLiveLogWriteAndRead(t *testing.T) {
	ll := newLiveLog()

	// Write some data before creating a reader.
	if _, err := ll.Write([]byte("hello ")); err != nil {
		t.Fatal(err)
	}

	r := ll.NewReader()

	// Reader should see the data written before it was created.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	chunk, err := r.ReadChunk(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(chunk) != "hello " {
		t.Fatalf("got %q, want %q", chunk, "hello ")
	}

	// Write more data and read it.
	_, _ = ll.Write([]byte("world"))
	chunk, err = r.ReadChunk(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(chunk) != "world" {
		t.Fatalf("got %q, want %q", chunk, "world")
	}

	// Close and expect EOF.
	ll.Close()
	_, err = r.ReadChunk(ctx)
	if err != io.EOF {
		t.Fatalf("got err=%v, want io.EOF", err)
	}
}

func TestLiveLogMultipleReaders(t *testing.T) {
	ll := newLiveLog()
	_, _ = ll.Write([]byte("data"))

	r1 := ll.NewReader()
	r2 := ll.NewReader()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	c1, _ := r1.ReadChunk(ctx)
	c2, _ := r2.ReadChunk(ctx)
	if string(c1) != "data" || string(c2) != "data" {
		t.Fatalf("readers got %q and %q, both should be %q", c1, c2, "data")
	}
}

func TestLiveLogContextCancellation(t *testing.T) {
	ll := newLiveLog()
	r := ll.NewReader()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := r.ReadChunk(ctx)
	if !isContextErr(err) {
		t.Fatalf("expected context error, got %v", err)
	}
}

func isContextErr(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}

func TestLiveLogConcurrentWriteAndRead(t *testing.T) {
	ll := newLiveLog()
	r := ll.NewReader()
	ctx := context.Background()

	const N = 100
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range N {
			_, _ = ll.Write([]byte{byte(i)})
		}
		ll.Close()
	}()

	var got []byte
	for {
		chunk, err := r.ReadChunk(ctx)
		got = append(got, chunk...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()

	if len(got) != N {
		t.Fatalf("got %d bytes, want %d", len(got), N)
	}
	for i, b := range got {
		if b != byte(i) {
			t.Fatalf("byte %d: got %d, want %d", i, b, i)
		}
	}
}

func TestLiveLogDoubleClose(_ *testing.T) {
	ll := newLiveLog()
	ll.Close()
	ll.Close() // should not panic
}
