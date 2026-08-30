package handler

import (
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"latere.ai/x/wallfacer/internal/oteltest"
)

// The outbound clients in this package must carry the otel transport so each
// hop records a client span and sends W3C trace context downstream. These tests
// issue a real request and assert on both, rather than on the transport's type:
// a type assertion passes just as well against an inert transport in a process
// that never registered a TracerProvider or a propagator.

func TestAuthHTTPClientPropagatesTraceContext(t *testing.T) {
	rec := oteltest.Install(t)
	srv := oteltest.NewServer(t, nil)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := httpGet(req)
	if err != nil {
		t.Fatalf("auth client request: %v", err)
	}
	_ = resp.Body.Close()

	srv.RequireTraceparent(t)
	if names := oteltest.SpanNames(rec, trace.SpanKindClient); len(names) == 0 {
		t.Fatal("auth client recorded no client span")
	}
}

func TestSandboxProxyClientPropagatesTraceContext(t *testing.T) {
	rec := oteltest.Install(t)
	srv := oteltest.NewServer(t, nil)

	p := NewSandboxProxy(SandboxProxyConfig{}, nil)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		t.Fatalf("sandbox proxy request: %v", err)
	}
	_ = resp.Body.Close()

	srv.RequireTraceparent(t)
	if names := oteltest.SpanNames(rec, trace.SpanKindClient); len(names) == 0 {
		t.Fatal("sandbox proxy client recorded no client span")
	}
}
