package observability

import (
	"log/slog"
	"strings"
)

func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		attrs := a.Value.Group()
		out := make([]slog.Attr, len(attrs))
		for i, child := range attrs {
			out[i] = replaceAttr(nil, child)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	}
	if IsSafeLogKey(a.Key) {
		if a.Value.Kind() == slog.KindString {
			a.Value = slog.StringValue(RedactText(a.Value.String()))
		}
		return a
	}
	if IsSensitiveKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	if a.Value.Kind() == slog.KindString {
		a.Value = slog.StringValue(RedactText(a.Value.String()))
	}
	return a
}

func HTTPErrorClass(status int) string {
	switch {
	case status >= 200 && status < 400:
		return ""
	case status == 401:
		return "unauthenticated"
	case status == 403:
		return "forbidden"
	case status == 404:
		return "not_found"
	case status == 409:
		return "conflict"
	case status == 429:
		return "rate_limited"
	case status >= 400 && status < 500:
		return "client_error"
	default:
		return "server_error"
	}
}

func actorKind(path string) string {
	if strings.HasPrefix(path, "/v1/staff") {
		return "staff_route"
	}
	return "http"
}
