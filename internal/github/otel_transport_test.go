package github

import (
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"latere.ai/x/wallfacer/internal/oteltest"
)

// The default GitHub clients must record a client span and send W3C trace
// context. These tests issue a real request rather than asserting on the
// transport's type, which cannot tell a working client from an inert one.

// requireTraced issues a request through client against a recording server and
// asserts both halves of a live client hop.
func requireTraced(t *testing.T, name string, client *http.Client) {
	t.Helper()
	rec := oteltest.Install(t)
	srv := oteltest.NewServer(t, nil)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s request: %v", name, err)
	}
	_ = resp.Body.Close()

	srv.RequireTraceparent(t)
	if names := oteltest.SpanNames(rec, trace.SpanKindClient); len(names) == 0 {
		t.Fatalf("%s recorded no client span", name)
	}
}

func TestClientDefaultPropagatesTraceContext(t *testing.T) {
	requireTraced(t, "github client", (&Client{}).httpClient())
}

func TestBrokerDefaultPropagatesTraceContext(t *testing.T) {
	requireTraced(t, "github broker", (&HTTPBroker{}).httpClient())
}

func TestClientKeepsCustomHTTPClient(t *testing.T) {
	custom := &http.Client{}
	c := &Client{HTTP: custom}
	if c.httpClient() != custom {
		t.Fatal("Client must return the configured HTTP client unchanged")
	}
}

func TestBrokerKeepsCustomHTTPClient(t *testing.T) {
	custom := &http.Client{}
	b := &HTTPBroker{HTTP: custom}
	if b.httpClient() != custom {
		t.Fatal("HTTPBroker must return the configured HTTP client unchanged")
	}
}
