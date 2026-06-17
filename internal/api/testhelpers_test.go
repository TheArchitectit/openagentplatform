package api

import (
	"io"
	"log/slog"
)

// newDiscardLogger returns a slog.Logger that discards all output.
// Useful in tests where we don't want log noise polluting the test
// output but still need a non-nil logger to pass into constructors.
func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}