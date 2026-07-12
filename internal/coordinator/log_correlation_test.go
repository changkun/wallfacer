package coordinator

import (
	"context"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// spanCtxCapture records whether the ctx passed to Handle carried a valid span
// context, which is what the otelslog bridge needs to stamp TraceId/SpanId onto
// exported records (observability spec 02). A plain slog call hands the bridge
// context.Background(), so the record loses its trace correlation.
type spanCtxCapture struct {
	sawSpan bool
}

func (h *spanCtxCapture) Enabled(context.Context, slog.Level) bool { return true }

func (h *spanCtxCapture) Handle(ctx context.Context, _ slog.Record) error {
	h.sawSpan = h.sawSpan || trace.SpanContextFromContext(ctx).IsValid()
	return nil
}

func (h *spanCtxCapture) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *spanCtxCapture) WithGroup(string) slog.Handler { return h }

func TestDispatchLogsCarryRequestSpanContext(t *testing.T) {
	capture := &spanCtxCapture{}
	c := NewCoordinator(NewRegistry())
	c.log = slog.New(capture)

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01},
		SpanID:     trace.SpanID{0x02},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	// A malformed frame logs a warning on the request path.
	c.dispatch(ctx, Principal{Sub: "u"}, "inst", []byte("not json"))

	if !capture.sawSpan {
		t.Fatal("request-path log record lost the request span context; log with the *Context slog variants")
	}
}
