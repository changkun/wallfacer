package oauth

import (
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"latere.ai/x/wallfacer/internal/oteltest"
)

// The default token-exchange client must record a client span and send W3C
// trace context. This test issues a real request rather than asserting on the
// transport's type, which cannot tell a working client from an inert one.

func TestManagerDefaultClientPropagatesTraceContext(t *testing.T) {
	rec := oteltest.Install(t)
	srv := oteltest.NewServer(t, nil)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := NewManager().httpClient().Do(req)
	if err != nil {
		t.Fatalf("token exchange request: %v", err)
	}
	_ = resp.Body.Close()

	srv.RequireTraceparent(t)
	if names := oteltest.SpanNames(rec, trace.SpanKindClient); len(names) == 0 {
		t.Fatal("token exchange client recorded no client span")
	}
}

func TestManagerKeepsCustomHTTPClient(t *testing.T) {
	custom := &http.Client{}
	m := &Manager{HTTPClient: custom}
	if m.httpClient() != custom {
		t.Fatal("Manager must return the configured HTTP client unchanged")
	}
}
