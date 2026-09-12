package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNopTelemetry(t *testing.T) {
	tr := NopTracer()
	ctx, end := tr.Start(nil, "work")
	end()
	NopMetrics().Inc("x", nil)
	NopMetrics().Observe("y", 1, nil)
	_ = ctx
}

func TestWithTraceRejectsUnsafeIncoming(t *testing.T) {
	h := WithTrace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if TraceID(r.Context()) == "not a trace" {
			t.Fatal("accepted unsafe trace")
		}
		if TraceID(r.Context()) == "" {
			t.Fatal("missing generated trace")
		}
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("traceparent", "not a trace")
	h.ServeHTTP(httptest.NewRecorder(), req)
}
