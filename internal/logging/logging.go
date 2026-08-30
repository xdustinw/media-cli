// Package logging centralises slog configuration for the CLI.
package logging

import (
	"io"
	"log/slog"
)

// Options controls how the process-wide logger is configured.
type Options struct {
	// Verbose raises the level to Debug for the human-readable handler.
	Verbose bool
	// Debug additionally switches to a JSON handler with source positions.
	Debug bool
}

// Setup builds a slog.Logger writing to w (typically os.Stderr), installs it as
// the default logger and returns it.
func Setup(w io.Writer, opts Options) *slog.Logger {
	level := slog.LevelWarn
	switch {
	case opts.Debug:
		level = slog.LevelDebug
	case opts.Verbose:
		level = slog.LevelInfo
	}

	var handler slog.Handler
	hopts := &slog.HandlerOptions{Level: level, AddSource: opts.Debug}
	if opts.Debug {
		handler = slog.NewJSONHandler(w, hopts)
	} else {
		handler = slog.NewTextHandler(w, hopts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}
