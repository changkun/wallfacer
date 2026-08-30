// Package oteltest installs a real OpenTelemetry SDK in a test process so
// instrumentation can be checked by what it produces rather than by the type of
// the round tripper it is built from.
//
// A reflect.TypeOf assertion on an http.Client's Transport cannot distinguish a
// working client from an inert one: otelhttp resolves to noop when no
// TracerProvider and no propagator are registered, which is exactly the failure
// this package exists to catch. Tests using Install assert on recorded spans and
// on the traceparent header that reaches the server.
package oteltest

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	otelapi "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// Install registers an SDK TracerProvider backed by a span recorder and the
// composite W3C TraceContext/Baggage propagator as the process globals, then
// returns the recorder. It clears OTEL_EXPORTER_OTLP_ENDPOINT so a service
// bootstrap running later in the test keeps its noop path and leaves these
// globals alone.
//
// The provider is shut down at test cleanup, which flushes every span into the
// recorder. Call Spans after the work under test has finished.
func Install(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(rec),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	otelapi.SetTracerProvider(tp)
	otelapi.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return rec
}

// SpanNames returns the names of the recorded spans of the given kind, in the
// order they ended.
func SpanNames(rec *tracetest.SpanRecorder, kind trace.SpanKind) []string {
	var names []string
	for _, span := range rec.Ended() {
		if span.SpanKind() == kind {
			names = append(names, span.Name())
		}
	}
	return names
}

// Server is an httptest server that records the W3C trace headers of every
// request it serves, so a test can assert that trace context actually crossed
// the wire.
type Server struct {
	*httptest.Server

	mu      sync.Mutex
	headers []http.Header
}

// NewServer starts a Server that serves h, recording each request's headers
// first. A nil h replies 200 with an empty body. The server is closed at test
// cleanup.
func NewServer(t *testing.T, h http.Handler) *Server {
	t.Helper()
	s := &Server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.headers = append(s.headers, r.Header.Clone())
		s.mu.Unlock()
		if h == nil {
			w.WriteHeader(http.StatusOK)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// Traceparents returns the traceparent header of every request served, in
// order. An entry is empty when the request carried none.
func (s *Server) Traceparents() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.headers))
	for _, h := range s.headers {
		out = append(out, h.Get("traceparent"))
	}
	return out
}

// RequireTraceparent fails the test unless at least one served request carried
// a non-empty traceparent header. This is the assertion that catches an inert
// otelhttp transport: without a registered propagator the header is never
// injected, so the receiving service starts a fresh trace.
func (s *Server) RequireTraceparent(t *testing.T) {
	t.Helper()
	got := s.Traceparents()
	if len(got) == 0 {
		t.Fatal("no request reached the server")
	}
	for _, tp := range got {
		if tp != "" {
			return
		}
	}
	t.Fatalf("no request carried a traceparent header; the client transport is inert (got %d requests)", len(got))
}
