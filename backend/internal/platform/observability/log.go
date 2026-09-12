package observability

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"backend/internal/platform/config"
)

// ConfigureJSON installs the process-wide JSON slog handler with the central
// redaction policy. No collector is required.
func ConfigureJSON(cfg config.Config, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	level := slog.LevelInfo
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replaceAttr,
	})
	logger := slog.New(h).With("env", string(cfg.Environment))
	slog.SetDefault(logger)
	return logger
}

type ctxKey int

const loggerKey ctxKey = 1

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, loggerKey, logger)
}

func FromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
			return l
		}
	}
	return slog.Default()
}
