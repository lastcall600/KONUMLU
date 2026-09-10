package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"unicode"
)

type ctxKey int

const requestIDKey ctxKey = 1

const requestIDHeader = "X-Request-Id"

// RequestID returns the correlation id stored by WithRequestID.
func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// WithRequestID assigns a request id and echoes it on the response.
// Incoming X-Request-Id is accepted only when it is a short token of safe characters.
func WithRequestID(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := incomingRequestID(r.Header.Get(requestIDHeader))
		if id == "" {
			id = newRequestID()
		}
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func incomingRequestID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 128 {
		return ""
	}
	for _, r := range raw {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			return ""
		}
	}
	return raw
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "missing"
	}
	return hex.EncodeToString(b[:])
}
