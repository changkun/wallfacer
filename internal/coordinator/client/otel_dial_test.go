package client

import (
	"context"
	"latere.ai/x/pkg/wait/waittest"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"

	"latere.ai/x/pkg/authkit"
	"latere.ai/x/wallfacer/internal/auth"
	"latere.ai/x/wallfacer/internal/coordinator"
	"latere.ai/x/wallfacer/internal/oteltest"
)

// TestDialPropagatesTraceContext exercises the coordination WebSocket dial
// against the real accept handler and asserts on what the dial produced: a
// client span, and a traceparent header on the upgrade request. Before the dial
// client was instrumented, DialOptions.HTTPClient was unset, so the handshake
// used http.DefaultClient and the coordinator started a fresh trace.
func TestDialPropagatesTraceContext(t *testing.T) {
	rec := oteltest.Install(t)

	reg := coordinator.NewRegistry()
	coord := coordinator.NewCoordinator(reg)
	srv := oteltest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithIdentity(r.Context(), &authkit.Identity{Sub: "u1", OrgID: "o1"})
		coord.HandleWS(w, r.WithContext(ctx))
	}))

	c := NewConnector(Config{
		URL:      "ws" + strings.TrimPrefix(srv.URL, "http"),
		Token:    func() (string, bool) { return "token", true },
		OptedIn:  func() bool { return true },
		Manifest: manifestFor("inst-1"),
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() { done <- c.connectOnce(ctx) }()
	waittest.For(t, 3*time.Second, func() bool { return len(reg.Snapshot("o1")) > 0 })
	cancel()
	if !<-done {
		t.Fatal("connectOnce reported no connection")
	}

	srv.RequireTraceparent(t)

	if names := oteltest.SpanNames(rec, trace.SpanKindClient); len(names) == 0 {
		t.Fatalf("dial recorded no client span; DialOptions.HTTPClient is uninstrumented (spans: %v)", rec.Ended())
	}
}
