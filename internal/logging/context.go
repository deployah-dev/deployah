package logging

import (
	"context"

	"github.com/charmbracelet/log"
)

// loggerKey is a private context key for storing the logger
type loggerKey struct{}

// observableLoggerKey is a private context key for storing the observable logger
type observableLoggerKey struct{}

// WithLogger returns a child context that carries the provided logger
func WithLogger(ctx context.Context, logger *log.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// WithObservableLogger returns a child context that carries the provided observable logger
func WithObservableLogger(ctx context.Context, logger *ObservableLogger) context.Context {
	// Also store the underlying logger for backward compatibility
	ctx = WithLogger(ctx, logger.logger)
	return context.WithValue(ctx, observableLoggerKey{}, logger)
}

// From extracts a logger from the context, or nil if absent
func From(ctx context.Context) *log.Logger {
	if v := ctx.Value(loggerKey{}); v != nil {
		if lgr, ok := v.(*log.Logger); ok {
			return lgr
		}
	}
	return nil
}

// FromObservable extracts an observable logger from the context, or nil if absent
func FromObservable(ctx context.Context) *ObservableLogger {
	if v := ctx.Value(observableLoggerKey{}); v != nil {
		if lgr, ok := v.(*ObservableLogger); ok {
			return lgr
		}
	}
	return nil
}
