package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Package-level loggers; initialized by initLogger (called from init).
var (
	logMain     *slog.Logger
	logRunner   *slog.Logger
	logStore    *slog.Logger
	logGit      *slog.Logger
	logHandler  *slog.Logger
	logRecovery *slog.Logger
)

func init() {
	initLogger("text")
}

// initLogger configures the package-level loggers.
// format is "text" (colored, human-friendly) or "json" (structured JSON).
func initLogger(format string) {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		h = newPrettyHandler(os.Stderr, opts)
	}
	base := slog.New(h)
	logMain = base.With("component", "main")
	logRunner = base.With("component", "runner")
	logStore = base.With("component", "store")
	logGit = base.With("component", "git")
	logHandler = base.With("component", "handler")
	logRecovery = base.With("component", "recovery")
}

// fatal logs at error level and exits with code 1.
func fatal(l *slog.Logger, msg string, args ...any) {
	l.Error(msg, args...)
	os.Exit(1)
}

// ANSI escape codes.
const (
	ansiReset  = "\033[0m"
	ansiDim    = "\033[2m"
	ansiBold   = "\033[1m"
	ansiGray   = "\033[90m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiCyan   = "\033[36m"
)

// prettyHandler formats log records for human consumption with alignment and color.
type prettyHandler struct {
	w        io.Writer
	opts     *slog.HandlerOptions
	mu       sync.Mutex
	preAttrs []slog.Attr
	color    bool
}

func newPrettyHandler(w io.Writer, opts *slog.HandlerOptions) *prettyHandler {
	return &prettyHandler{
		w:     w,
		opts:  opts,
		color: isColorEnabled(w),
	}
}

// isColorEnabled returns true when ANSI colors should be written to w.
// It respects NO_COLOR and TERM=dumb, and only enables colors on real terminals.
func isColorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (h *prettyHandler) clone() *prettyHandler {
	cp := *h
	cp.preAttrs = h.preAttrs[:len(h.preAttrs):len(h.preAttrs)]
	return &cp
}

func (h *prettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cp := h.clone()
	cp.preAttrs = append(cp.preAttrs, attrs...)
	return cp
}

func (h *prettyHandler) WithGroup(_ string) slog.Handler {
	return h // groups are not used in this application
}

func (h *prettyHandler) Handle(_ context.Context, r slog.Record) error {
	// Collect all attributes: pre-set (from With) then record attrs.
	all := make([]slog.Attr, 0, len(h.preAttrs)+r.NumAttrs())
	all = append(all, h.preAttrs...)
	r.Attrs(func(a slog.Attr) bool {
		all = append(all, a)
		return true
	})

	// Separate the "component" attribute from the rest.
	component := ""
	extra := make([]slog.Attr, 0, len(all))
	for _, a := range all {
		if a.Key == "component" {
			component = a.Value.String()
		} else {
			extra = append(extra, a)
		}
	}

	// col wraps s in an ANSI escape sequence when colors are enabled.
	col := func(code, s string) string {
		if h.color {
			return code + s + ansiReset
		}
		return s
	}

	var b strings.Builder

	// Timestamp: HH:MM:SS.mmm  (dim gray)
	b.WriteString(col(ansiDim+ansiGray, r.Time.Format("15:04:05.000")))
	b.WriteString("  ")

	// Level: 3-char abbreviation with color.
	switch r.Level {
	case slog.LevelDebug:
		b.WriteString(col(ansiGray, "DBG"))
	case slog.LevelInfo:
		b.WriteString(col(ansiGreen+ansiBold, "INF"))
	case slog.LevelWarn:
		b.WriteString(col(ansiYellow+ansiBold, "WRN"))
	default: // Error and above
		b.WriteString(col(ansiRed+ansiBold, "ERR"))
	}
	b.WriteString("  ")

	// Component: fixed 8-char column, cyan.
	b.WriteString(col(ansiCyan, fmt.Sprintf("%-8s", component)))
	b.WriteString("  ")

	// Message.
	b.WriteString(r.Message)

	// Additional key=value pairs; error values are highlighted red.
	for _, a := range extra {
		b.WriteString("  ")
		b.WriteString(col(ansiDim, a.Key))
		b.WriteByte('=')
		v := fmt.Sprintf("%v", a.Value.Resolve().Any())
		if a.Key == "error" {
			b.WriteString(col(ansiRed, v))
		} else {
			b.WriteString(v)
		}
	}

	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := fmt.Fprint(h.w, b.String())
	return err
}
