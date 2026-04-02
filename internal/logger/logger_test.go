package logger

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestInit verifies that Init populates all named loggers for both formats.
func TestInit(t *testing.T) {
	t.Run("text format", func(t *testing.T) {
		Init("text")
		loggers := map[string]*slog.Logger{
			"Main":     Main,
			"Runner":   Runner,
			"Store":    Store,
			"Git":      Git,
			"Handler":  Handler,
			"Recovery": Recovery,
		}
		for name, l := range loggers {
			if l == nil {
				t.Errorf("%s logger should not be nil after Init(text)", name)
			}
		}
	})

	t.Run("json format", func(t *testing.T) {
		Init("json")
		if Main == nil {
			t.Error("Main logger should not be nil after Init(json)")
		}
		if Runner == nil {
			t.Error("Runner logger should not be nil after Init(json)")
		}
	})

	// Restore text format after test.
	t.Cleanup(func() { Init("text") })
}

// TestInit_UnknownFormat verifies that an unknown format falls back to text (pretty) handler.
func TestInit_UnknownFormat(t *testing.T) {
	Init("unknown")
	if Main == nil {
		t.Error("Main logger should not be nil after Init with unknown format")
	}
	t.Cleanup(func() { Init("text") })
}

// TestFatal verifies that Fatal causes exit code 1.
// It uses a subprocess technique to avoid terminating the test process.
func TestFatal(t *testing.T) {
	if os.Getenv("TEST_LOGGER_FATAL") == "1" {
		// Running inside the subprocess: call Fatal and expect os.Exit(1).
		Fatal("fatal test message", "key", "value")
		return // unreachable
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestFatal")
	cmd.Env = append(os.Environ(), "TEST_LOGGER_FATAL=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected Fatal to exit with non-zero code")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("expected exit code 1, got %d", exitErr.ExitCode())
	}
}

// TestIsColorEnabled covers the false-returning branches.
// The true branch requires a real TTY and cannot be tested in a unit test.
func TestIsColorEnabled(t *testing.T) {
	t.Run("non-file writer returns false", func(t *testing.T) {
		var buf bytes.Buffer
		if isColorEnabled(&buf) {
			t.Error("expected false for bytes.Buffer (not an *os.File)")
		}
	})

	t.Run("NO_COLOR env disables color", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		f, err := os.CreateTemp(t.TempDir(), "logger-color-*")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close() //nolint:errcheck
		if isColorEnabled(f) {
			t.Error("expected false when NO_COLOR is set")
		}
	})

	t.Run("TERM=dumb disables color", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		t.Setenv("TERM", "dumb")
		f, err := os.CreateTemp(t.TempDir(), "logger-color-*")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close() //nolint:errcheck
		if isColorEnabled(f) {
			t.Error("expected false when TERM=dumb")
		}
	})

	t.Run("regular file is not a terminal", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		t.Setenv("TERM", "xterm-256color")
		f, err := os.CreateTemp(t.TempDir(), "logger-color-*")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close() //nolint:errcheck
		// A regular temp file is not a char device, so should return false.
		if isColorEnabled(f) {
			t.Error("expected false for regular file (not a terminal)")
		}
	})
}

// TestPrettyHandlerClone verifies that clone produces an independent copy.
func TestPrettyHandlerClone(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	h := newPrettyHandler(&buf, opts)
	h.preAttrs = []slog.Attr{slog.String("k1", "v1")}

	cp := h.clone()

	if cp.w != h.w {
		t.Error("clone should share the same writer")
	}
	if cp.opts != h.opts {
		t.Error("clone should share the same opts")
	}
	if len(cp.preAttrs) != 1 {
		t.Errorf("expected 1 preAttr in clone, got %d", len(cp.preAttrs))
	}

	// Appending to clone must not affect the original.
	cp.preAttrs = append(cp.preAttrs, slog.String("k2", "v2"))
	if len(h.preAttrs) != 1 {
		t.Errorf("original preAttrs unexpectedly modified: got %d attrs", len(h.preAttrs))
	}
}

// TestPrettyHandlerEnabled checks level filtering.
func TestPrettyHandlerEnabled(t *testing.T) {
	tests := []struct {
		minLevel   slog.Level
		queryLevel slog.Level
		want       bool
	}{
		{slog.LevelInfo, slog.LevelDebug, false},
		{slog.LevelInfo, slog.LevelInfo, true},
		{slog.LevelInfo, slog.LevelWarn, true},
		{slog.LevelInfo, slog.LevelError, true},
		{slog.LevelDebug, slog.LevelDebug, true},
		{slog.LevelError, slog.LevelWarn, false},
	}

	for _, tc := range tests {
		var buf bytes.Buffer
		opts := &slog.HandlerOptions{Level: tc.minLevel}
		h := newPrettyHandler(&buf, opts)
		got := h.Enabled(context.Background(), tc.queryLevel)
		if got != tc.want {
			t.Errorf("Enabled(minLevel=%v, query=%v) = %v, want %v",
				tc.minLevel, tc.queryLevel, got, tc.want)
		}
	}
}

// TestPrettyHandlerWithAttrs verifies attribute accumulation and copy-on-write.
func TestPrettyHandlerWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	h := newPrettyHandler(&buf, opts)

	h2 := h.WithAttrs([]slog.Attr{slog.String("k1", "v1")}).(*prettyHandler)
	if len(h2.preAttrs) != 1 {
		t.Errorf("expected 1 preAttr, got %d", len(h2.preAttrs))
	}

	// Chaining should accumulate.
	h3 := h2.WithAttrs([]slog.Attr{slog.String("k2", "v2")}).(*prettyHandler)
	if len(h3.preAttrs) != 2 {
		t.Errorf("expected 2 preAttrs after chaining, got %d", len(h3.preAttrs))
	}

	// Original must be unmodified.
	if len(h.preAttrs) != 0 {
		t.Errorf("original preAttrs modified: expected 0, got %d", len(h.preAttrs))
	}
}

// TestPrettyHandlerWithGroup verifies that WithGroup returns the same handler.
func TestPrettyHandlerWithGroup(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	h := newPrettyHandler(&buf, opts)

	h2 := h.WithGroup("mygroup")
	if h2 != h {
		t.Error("WithGroup should return the same handler (groups are unsupported)")
	}
}

// TestPrettyHandlerHandle_Levels tests each log level badge without color.
func TestPrettyHandlerHandle_Levels(t *testing.T) {
	tests := []struct {
		level slog.Level
		badge string
	}{
		{slog.LevelDebug, "DBG"},
		{slog.LevelInfo, "INF"},
		{slog.LevelWarn, "WRN"},
		{slog.LevelError, "ERR"},
	}

	for _, tc := range tests {
		t.Run(tc.level.String(), func(t *testing.T) {
			var buf bytes.Buffer
			opts := &slog.HandlerOptions{Level: slog.LevelDebug}
			h := newPrettyHandler(&buf, opts)
			h.color = false

			r := slog.NewRecord(time.Now(), tc.level, "test message", 0)
			if err := h.Handle(context.Background(), r); err != nil {
				t.Fatalf("Handle returned unexpected error: %v", err)
			}

			out := buf.String()
			if !strings.Contains(out, tc.badge) {
				t.Errorf("expected badge %q in output, got: %q", tc.badge, out)
			}
			if !strings.Contains(out, "test message") {
				t.Errorf("expected message in output, got: %q", out)
			}
		})
	}
}

// TestPrettyHandlerHandle_AboveError exercises the default case (level > Error).
func TestPrettyHandlerHandle_AboveError(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	h := newPrettyHandler(&buf, opts)
	h.color = false

	// LevelError+4 is above Error; should still render "ERR".
	r := slog.NewRecord(time.Now(), slog.LevelError+4, "critical", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "ERR") {
		t.Errorf("expected ERR badge for level above Error, got: %q", buf.String())
	}
}

// TestPrettyHandlerHandle_WithComponent checks component extraction from preAttrs.
func TestPrettyHandlerHandle_WithComponent(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	h := newPrettyHandler(&buf, opts)
	h.color = false
	h = h.WithAttrs([]slog.Attr{slog.String("component", "runner")}).(*prettyHandler)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "runner") {
		t.Errorf("expected component name in output, got: %q", buf.String())
	}
}

// TestPrettyHandlerHandle_ExtraAttrs checks key=value rendering and pipe separator.
func TestPrettyHandlerHandle_ExtraAttrs(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	h := newPrettyHandler(&buf, opts)
	h.color = false

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	r.AddAttrs(slog.String("key", "value"))
	r.AddAttrs(slog.String("error", "something broke"))

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "key=") {
		t.Errorf("expected key= in output, got: %q", out)
	}
	if !strings.Contains(out, "│") {
		t.Errorf("expected pipe separator │ in output, got: %q", out)
	}
	if !strings.Contains(out, "error=") {
		t.Errorf("expected error= in output, got: %q", out)
	}
}

// TestPrettyHandlerHandle_Color verifies ANSI codes appear when color is enabled.
func TestPrettyHandlerHandle_Color(t *testing.T) {
	levels := []slog.Level{
		slog.LevelDebug,
		slog.LevelInfo,
		slog.LevelWarn,
		slog.LevelError,
	}

	for _, lvl := range levels {
		t.Run(lvl.String(), func(t *testing.T) {
			var buf bytes.Buffer
			opts := &slog.HandlerOptions{Level: slog.LevelDebug}
			h := newPrettyHandler(&buf, opts)
			h.color = true // force color regardless of terminal

			r := slog.NewRecord(time.Now(), lvl, "colored message", 0)
			r.AddAttrs(slog.String("error", "err detail"))
			r.AddAttrs(slog.String("key", "val"))
			if err := h.Handle(context.Background(), r); err != nil {
				t.Fatalf("Handle returned error: %v", err)
			}
			if !strings.Contains(buf.String(), ansiReset) {
				t.Errorf("expected ANSI reset code in colored output, got: %q", buf.String())
			}
		})
	}
}

// TestPrettyHandlerHandle_NoExtraAttrs verifies no pipe is written when there are no extra attrs.
func TestPrettyHandlerHandle_NoExtraAttrs(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	h := newPrettyHandler(&buf, opts)
	h.color = false

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "just a message", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if strings.Contains(buf.String(), "│") {
		t.Errorf("expected no pipe when no extra attrs, got: %q", buf.String())
	}
}

// TestNeedsQuoting covers all triggering characters and clean strings.
func TestNeedsQuoting(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"", true},
		{"simple", false},
		{"normal123", false},
		{"CamelCase", false},
		{"has space", true},
		{"has\ttab", true},
		{"has\nnewline", true},
		{"has\rcarriage", true},
		{`has"quote`, true},
		{"has=equals", true},
	}

	for _, tc := range tests {
		if got := needsQuoting(tc.s); got != tc.want {
			t.Errorf("needsQuoting(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

// TestPrettyValue exercises all branches of prettyValue.
func TestPrettyValue(t *testing.T) {
	t.Run("UUID is truncated to 8 chars", func(t *testing.T) {
		uuid := "550e8400-e29b-41d4-a716-446655440000"
		got := prettyValue(slog.StringValue(uuid))
		if got != uuid[:8] {
			t.Errorf("prettyValue(uuid) = %q, want %q", got, uuid[:8])
		}
	})

	t.Run("long string is truncated with ellipsis", func(t *testing.T) {
		long := strings.Repeat("a", 201)
		got := prettyValue(slog.StringValue(long))
		if !strings.Contains(got, "…") {
			t.Errorf("expected ellipsis in truncated output, got: %q", got)
		}
	})

	t.Run("exactly 200-char string is not truncated", func(t *testing.T) {
		s := strings.Repeat("a", 200)
		got := prettyValue(slog.StringValue(s))
		if strings.Contains(got, "…") {
			t.Errorf("200-char string should not be truncated, got: %q", got)
		}
	})

	t.Run("string needing quoting is quoted", func(t *testing.T) {
		got := prettyValue(slog.StringValue("has space"))
		if got != `"has space"` {
			t.Errorf("prettyValue(\"has space\") = %q, want %q", got, `"has space"`)
		}
	})

	t.Run("empty string is quoted", func(t *testing.T) {
		got := prettyValue(slog.StringValue(""))
		if got != `""` {
			t.Errorf("prettyValue(\"\") = %q, want %q", got, `""`)
		}
	})

	t.Run("plain string returned as-is", func(t *testing.T) {
		got := prettyValue(slog.StringValue("normal"))
		if got != "normal" {
			t.Errorf("prettyValue(\"normal\") = %q, want %q", got, "normal")
		}
	})

	t.Run("non-string value formatted via Sprintf", func(t *testing.T) {
		got := prettyValue(slog.IntValue(42))
		if got != "42" {
			t.Errorf("prettyValue(42) = %q, want %q", got, "42")
		}
	})

	t.Run("boolean value", func(t *testing.T) {
		got := prettyValue(slog.BoolValue(true))
		if got != "true" {
			t.Errorf("prettyValue(true) = %q, want %q", got, "true")
		}
	})
}
