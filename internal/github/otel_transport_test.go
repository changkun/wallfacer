package github

import (
	"net/http"
	"reflect"
	"testing"

	"latere.ai/x/pkg/otel"
)

// The default GitHub clients must carry the otel transport so outbound calls
// record client spans and propagate trace context (observability spec 01).
// These tests fail on a bare client (nil Transport).

func TestClientDefaultUsesOtelTransport(t *testing.T) {
	c := &Client{}
	want := reflect.TypeOf(otel.Transport(nil))
	if got := reflect.TypeOf(c.httpClient().Transport); got != want {
		t.Fatalf("Client default transport = %v, want %v", got, want)
	}
}

func TestClientKeepsCustomHTTPClient(t *testing.T) {
	custom := &http.Client{}
	c := &Client{HTTP: custom}
	if c.httpClient() != custom {
		t.Fatal("Client must return the configured HTTP client unchanged")
	}
}

func TestBrokerDefaultUsesOtelTransport(t *testing.T) {
	b := &HTTPBroker{}
	want := reflect.TypeOf(otel.Transport(nil))
	if got := reflect.TypeOf(b.httpClient().Transport); got != want {
		t.Fatalf("HTTPBroker default transport = %v, want %v", got, want)
	}
}

func TestBrokerKeepsCustomHTTPClient(t *testing.T) {
	custom := &http.Client{}
	b := &HTTPBroker{HTTP: custom}
	if b.httpClient() != custom {
		t.Fatal("HTTPBroker must return the configured HTTP client unchanged")
	}
}
