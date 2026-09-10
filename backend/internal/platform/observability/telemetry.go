package observability

import (
	"context"
	"net/http"
	"time"

	"backend/internal/platform/httpx"
)

// Tracer is a provider-neutral tracing hook. The default is a no-op.
type Tracer interface {
	Start(ctx context.Context, name string) (context.Context, func())
}

// Metrics is a provider-neutral metrics hook. The default is a no-op.
type Metrics interface {
	Inc(name string, labels map[string]string)
	Observe(name string, value float64, labels map[string]string)
}

type noopTelemetry struct{}

func (noopTelemetry) Start(ctx context.Context, name string) (context.Context, func()) {
	_ = name
	if ctx == nil {
		ctx = context.Background()
	}
	return ctx, func() {}
}

func (noopTelemetry) Inc(string, map[string]string) {}

func (noopTelemetry) Observe(string, float64, map[string]string) {}

func NopTracer() Tracer   { return noopTelemetry{} }
func NopMetrics() Metrics { return noopTelemetry{} }

// AccessLog logs method, path, status, and duration. It does not log cookies,
// authorization headers, or bodies.
func AccessLog(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		FromContext(r.Context()).Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", httpx.RequestID(r.Context()),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Wrap applies request-id then access logging.
func Wrap(h http.Handler) http.Handler {
	return httpx.WithRequestID(AccessLog(h))
}
