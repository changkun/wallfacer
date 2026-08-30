package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	otelapi "go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// otlpSink stands in for a collector so otel.Bootstrap takes its live path
// instead of the noop path it uses when OTEL_EXPORTER_OTLP_ENDPOINT is unset.
// Nothing asserts on what is exported; the point is that Bootstrap registers
// real globals.
func otlpSink(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	// Sample every root span so span assertions are not flaky at the default
	// head-sampling ratio.
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "1.0")
}

func newRunServer(t *testing.T) *ServerComponents {
	t.Helper()
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("# empty\n"), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	sc := initServer(configDir, ServerConfig{
		LogFormat: "text",
		Addr:      ":0",
		DataDir:   filepath.Join(configDir, "data"),
		EnvFile:   envFile,
	}, testFS(t), testFS(t))
	t.Cleanup(sc.Shutdown)
	return sc
}

// TestRunBootstrapsTelemetry is the discriminator for the `run` subcommand:
// instrumentation on the run tree only does anything if the run process
// bootstraps telemetry. Without the Bootstrap call, the global TracerProvider
// stays noop and the global propagator injects no traceparent, so every
// otel.Transport-wrapped client in the run tree is silently inert.
func TestRunBootstrapsTelemetry(t *testing.T) {
	otlpSink(t)
	newRunServer(t)

	if _, ok := otelapi.GetTracerProvider().(*sdktrace.TracerProvider); !ok {
		t.Fatalf("run did not register an SDK TracerProvider, got %T", otelapi.GetTracerProvider())
	}
	fields := otelapi.GetTextMapPropagator().Fields()
	if !slices.Contains(fields, "traceparent") {
		t.Fatalf("run did not register a W3C propagator, fields = %v", fields)
	}
}

// installRecorder registers an SDK TracerProvider backed by a span recorder as
// the process-wide provider and returns the recorder. OTEL_EXPORTER_OTLP_ENDPOINT
// is cleared so otel.Bootstrap keeps its noop path and leaves this provider in
// place.
func installRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otelapi.SetTracerProvider(tp)
	return rec
}

// TestRunServerRecordsServerSpan is the discriminator for the run server's
// inbound path: it fails when the run handler chain is not wrapped in
// otel.Handler, which is exactly the pre-fix state. A type assertion on the
// middleware stack could not tell the two apart.
func TestRunServerRecordsServerSpan(t *testing.T) {
	rec := installRecorder(t)
	sc := newRunServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/debug/health", nil)
	rr := httptest.NewRecorder()
	sc.Srv.Handler.ServeHTTP(rr, req)

	var server []string
	for _, span := range rec.Ended() {
		if span.SpanKind() == trace.SpanKindServer {
			server = append(server, span.Name())
		}
	}
	if len(server) == 0 {
		t.Fatalf("run server recorded no server span for %s; the handler chain is not wrapped in otel.Handler", req.URL.Path)
	}

	if got := rr.Header().Get("X-Trace-Id"); got == "" {
		t.Error("run server did not set the X-Trace-Id response header")
	}
}

// TestRunServerSkipsMetricsScrapeSpan verifies the Prometheus scrape produces no
// server span. The scrape runs on a fixed interval and would otherwise dominate
// trace volume.
func TestRunServerSkipsMetricsScrapeSpan(t *testing.T) {
	rec := installRecorder(t)
	sc := newRunServer(t)

	rr := httptest.NewRecorder()
	sc.Srv.Handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200", rr.Code)
	}
	for _, span := range rec.Ended() {
		if span.SpanKind() == trace.SpanKindServer {
			t.Fatalf("metrics scrape recorded a server span %q", span.Name())
		}
	}
}

// TestRunServerPreservesPrometheusSeries pins the wallfacer_* series through the
// otel.Handler wrapper. The Prometheus middleware stays inside otel.Handler
// rather than moving to WithMetricsHook precisely so these label values do not
// change: the hook reports a status class ("2xx"), while this counter carries
// the exact status code.
func TestRunServerPreservesPrometheusSeries(t *testing.T) {
	installRecorder(t)
	sc := newRunServer(t)

	sc.Srv.Handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/debug/health", nil))

	rr := httptest.NewRecorder()
	sc.Srv.Handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rr.Body.String()

	for _, want := range []string{
		`wallfacer_http_requests_total{method="GET",route="GET /api/debug/health",status="200"}`,
		`wallfacer_http_request_duration_seconds_count{method="GET",route="GET /api/debug/health"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing series %s", want)
		}
	}
}
