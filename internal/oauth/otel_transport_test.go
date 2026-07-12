package oauth

import (
	"net/http"
	"reflect"
	"testing"

	"latere.ai/x/pkg/otel"
)

// The default token-exchange client must carry the otel transport so the
// outbound hop records a client span (observability spec 01). This test fails
// on a bare client (nil Transport).

func TestManagerDefaultClientUsesOtelTransport(t *testing.T) {
	m := NewManager()
	want := reflect.TypeOf(otel.Transport(nil))
	if got := reflect.TypeOf(m.httpClient().Transport); got != want {
		t.Fatalf("Manager default transport = %v, want %v", got, want)
	}
}

func TestManagerKeepsCustomHTTPClient(t *testing.T) {
	custom := &http.Client{}
	m := &Manager{HTTPClient: custom}
	if m.httpClient() != custom {
		t.Fatal("Manager must return the configured HTTP client unchanged")
	}
}
