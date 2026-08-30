package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	otelapi "go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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
