// Package logger provides structured logging utilities for wallfacer using
// the stdlib log/slog package.
//
// It defines pre-configured, named loggers for each subsystem (Main, Runner,
// Store, Git, Handler, Recovery, Prompts) so that log output can be filtered
// and correlated by component. The text format uses a custom pretty handler
// with color support for human readability; the JSON format uses the standard
// slog JSON handler for structured log aggregation.
//
// # Connected packages
//
// Depends on [latere.ai/x/wallfacer/internal/constants] for configuration.
// Consumed by virtually every internal package that produces log output:
// [runner], [handler], [store], [cli], [envconfig], [prompts], and others.
// Changing logger names or initialization affects all log consumers.
//
// # Usage
//
//	logger.Init("text") // or "json"
//	logger.Runner.Info("task started", "task_id", id)
//	logger.Handler.Error("request failed", "err", err)
//
// A server process instead hands [NewFormatHandler] to otel.Bootstrap as
// Config.Stdout and passes the logger it returns to [Adopt]. Terminal output is
// unchanged and the same records also reach the OTLP log bridge:
//
//	base, shutdown, err := otel.Bootstrap(ctx, otel.Config{
//		ServiceName: "wallfacer",
//		Stdout:      logger.NewFormatHandler(format),
//	})
//	logger.Adopt(base)
package logger
