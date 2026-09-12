package observability

import (
	"context"
	"net/http"
	"time"

	"backend/internal/platform/httpx"
)

const sessionCookieName = "__Host-konumlu_session"

// Tracer is an OpenTelemetry-compatible tracing hook. The default is a no-op
// and does not require a vendor or collector.
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

func (noopTelemetry) Inc(_ string, _ map[string]string) {}

func (noopTelemetry) Observe(_ string, _ float64, _ map[string]string) {}

func NopTracer() Tracer   { return noopTelemetry{} }
func NopMetrics() Metrics { return noopTelemetry{} }

// AccessLog logs allowlisted request metadata. It does not log bodies, headers,
// cookies, tokens, or query strings.
func AccessLog(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		logger := FromContext(r.Context()).With(
			"request_id", httpx.RequestID(r.Context()),
			"trace_id", TraceID(r.Context()),
		)
		r = r.WithContext(WithLogger(r.Context(), logger))
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		route := r.Pattern
		if route == "" {
			route = r.URL.Path
		}
		attrs := []any{
			"method", r.Method,
			"route", route,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"actor_kind", actorKind(r.URL.Path),
			"session_present", hasCookie(r, sessionCookieName),
			"authorization_present", r.Header.Get("Authorization") != "",
		}
		if class := HTTPErrorClass(rec.status); class != "" {
			attrs = append(attrs, "error_class", class)
		}
		logger.Info("http_request", attrs...)
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

func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

// Wrap applies request-id, W3C trace id, then access logging.
func Wrap(h http.Handler) http.Handler {
	return httpx.WithRequestID(WithTrace(AccessLog(h)))
}

func hasCookie(r *http.Request, name string) bool {
	if r == nil {
		return false
	}
	_, err := r.Cookie(name)
	return err == nil
}