package handler

import (
	"reflect"
	"testing"

	"latere.ai/x/pkg/otel"
)

// The outbound clients must carry the otel transport so W3C trace context
// propagates across service hops (observability spec 01). These tests fail on
// a bare client (nil Transport).

func TestAuthHTTPClientUsesOtelTransport(t *testing.T) {
	want := reflect.TypeOf(otel.Transport(nil))
	if got := reflect.TypeOf(authHTTPClient.Transport); got != want {
		t.Fatalf("authHTTPClient.Transport = %v, want %v", got, want)
	}
}

func TestNewSandboxProxyClientUsesOtelTransport(t *testing.T) {
	p := NewSandboxProxy(SandboxProxyConfig{}, nil)
	want := reflect.TypeOf(otel.Transport(nil))
	if got := reflect.TypeOf(p.Client.Transport); got != want {
		t.Fatalf("SandboxProxy client transport = %v, want %v", got, want)
	}
}
