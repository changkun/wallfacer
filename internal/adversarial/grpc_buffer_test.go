package adversarial

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc"
	"google.golang.org/grpc/experimental"
	"google.golang.org/grpc/mem"
)

type fragmentBufferPool struct {
	mem.BufferPool
	compacted atomic.Int32
}

func (p *fragmentBufferPool) Get(size int) *[]byte {
	// Individual DATA frames below are one byte, so a pooled buffer this
	// large proves that queued fragments were coalesced before application reads.
	if size > 1 && size <= 4096 {
		p.compacted.Add(1)
	}
	return p.BufferPool.Get(size)
}

// TestGRPCReceiveBufferCompactsFragments guards CVE-2026-84304. The application
// deliberately does not consume the stream. Only 4 KiB is sent, split into
// one-byte DATA frames, to exercise compaction without exhausting memory.
func TestGRPCReceiveBufferCompactsFragments(t *testing.T) {
	pool := &fragmentBufferPool{BufferPool: mem.DefaultBufferPool()}
	started := make(chan struct{})
	server := grpc.NewServer(experimental.BufferPool(pool), grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		close(started)
		<-stream.Context().Done()
		return stream.Context().Err()
	}))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	defer func() { server.Stop(); <-done }()

	conn, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(conn, http2.ClientPreface); err != nil {
		t.Fatal(err)
	}
	framer := http2.NewFramer(conn, conn)
	var writeMu sync.Mutex
	if err := framer.WriteSettings(); err != nil {
		t.Fatal(err)
	}
	var headers bytes.Buffer
	encoder := hpack.NewEncoder(&headers)
	for _, field := range []hpack.HeaderField{
		{Name: ":method", Value: "POST"},
		{Name: ":scheme", Value: "http"},
		{Name: ":path", Value: "/regression.Service/Hold"},
		{Name: ":authority", Value: "localhost"},
		{Name: "content-type", Value: "application/grpc"},
		{Name: "te", Value: "trailers"},
	} {
		if err := encoder.WriteField(field); err != nil {
			t.Fatal(err)
		}
	}
	if err := framer.WriteHeaders(http2.HeadersFrameParam{StreamID: 1, BlockFragment: headers.Bytes(), EndHeaders: true}); err != nil {
		t.Fatal(err)
	}

	// A PING ACK after all DATA frames is a transport barrier: it avoids
	// timing assertions and proves the server processed every fragment.
	ping := [8]byte{'c', 'o', 'm', 'p', 'a', 'c', 't', '!'}
	processed := make(chan error, 1)
	go func() {
		for {
			frame, err := framer.ReadFrame()
			if err != nil {
				processed <- err
				return
			}
			switch f := frame.(type) {
			case *http2.SettingsFrame:
				if !f.IsAck() {
					writeMu.Lock()
					err = framer.WriteSettingsAck()
					writeMu.Unlock()
					if err != nil {
						processed <- err
						return
					}
				}
			case *http2.PingFrame:
				if f.IsAck() && f.Data == ping {
					processed <- nil
					return
				}
				if !f.IsAck() {
					writeMu.Lock()
					err = framer.WritePing(true, f.Data)
					writeMu.Unlock()
					if err != nil {
						processed <- err
						return
					}
				}
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("gRPC stream did not start")
	}
	pool.compacted.Store(0)
	writeMu.Lock()
	for range 4096 {
		if err = framer.WriteData(1, false, []byte{0}); err != nil {
			break
		}
	}
	if err == nil {
		err = framer.WritePing(false, ping)
	}
	writeMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-processed; err != nil {
		t.Fatal(err)
	}
	if pool.compacted.Load() == 0 {
		t.Fatal("queued one-byte DATA frames were never compacted into pooled buffers")
	}
}
