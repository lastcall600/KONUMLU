package observability

import (
	"context"
	"errors"
	"time"
)

func LogProvider(ctx context.Context, name string, latency time.Duration, err error) {
	if name == "" {
		name = "unknown"
	}
	attrs := []any{
		"provider", name,
		"provider_latency_ms", latency.Milliseconds(),
		"ok", err == nil,
	}
	if err != nil {
		attrs = append(attrs, "error_class", providerErrorClass(err))
	}
	FromContext(ctx).Info("provider_call", attrs...)
}

func providerErrorClass(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "provider_error"
}
