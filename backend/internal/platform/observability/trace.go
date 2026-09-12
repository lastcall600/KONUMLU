package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

type traceCtxKey int

const (
	traceIDKey traceCtxKey = 1
	spanIDKey  traceCtxKey = 2
)

const traceparentHeader = "traceparent"

// TraceID returns the W3C trace-id stored by WithTrace.
func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(traceIDKey).(string)
	return v
}

// SpanID returns the W3C parent-id stored by WithTrace.
func SpanID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(spanIDKey).(string)
	return v
}

// WithTrace records a W3C-compatible trace id without requiring a collector
// or OpenTelemetry SDK. Incoming traceparent is accepted only when well-formed.
func WithTrace(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID, spanID := parseTraceparent(r.Header.Get(traceparentHeader))
		if traceID == "" {
			traceID = newHex(16)
			spanID = newHex(8)
		} else if spanID == "" {
			spanID = newHex(8)
		}
		ctx := context.WithValue(r.Context(), traceIDKey, traceID)
		ctx = context.WithValue(ctx, spanIDKey, spanID)
		w.Header().Set(traceparentHeader, formatTraceparent(traceID, spanID))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func parseTraceparent(raw string) (traceID, spanID string) {
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, "-")
	if len(parts) != 4 || parts[0] != "00" {
		return "", ""
	}
	if !isHex(parts[1], 32) || !isHex(parts[2], 16) || !isHex(parts[3], 2) {
		return "", ""
	}
	if parts[1] == strings.Repeat("0", 32) || parts[2] == strings.Repeat("0", 16) {
		return "", ""
	}
	return parts[1], parts[2]
}

func formatTraceparent(traceID, spanID string) string {
	return "00-" + traceID + "-" + spanID + "-01"
}

func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func newHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("f", n*2)
	}
	return hex.EncodeToString(b)
}
