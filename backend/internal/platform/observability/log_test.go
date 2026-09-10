package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/internal/platform/config"
)

func TestConfigureJSONAndAccessLogOmitSecrets(t *testing.T) {
	var buf bytes.Buffer
	logger := ConfigureJSON(config.Config{Environment: config.EnvTest, LogLevel: "info"}, &buf)
	secret := "super-secret-token"
	h := Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		FromContext(r.Context()).Info("ok")
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Cookie", "session="+secret)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	_ = logger
	out := buf.String()
	if strings.Contains(out, secret) || strings.Contains(out, "Bearer") || strings.Contains(out, "session=") {
		t.Fatalf("log leaked secrets: %s", out)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing request id")
	}
}

func TestNopTelemetry(t *testing.T) {
	tr := NopTracer()
	ctx, end := tr.Start(nil, "work")
	end()
	NopMetrics().Inc("x", nil)
	NopMetrics().Observe("y", 1, nil)
	_ = ctx
}

func TestJSONLogLines(t *testing.T) {
	var buf bytes.Buffer
	ConfigureJSON(config.Config{Environment: config.EnvProduction, LogLevel: "info"}, &buf)
	slog.Info("boot")
	line, _, _ := bytes.Cut(buf.Bytes(), []byte("\n"))
	var row map[string]any
	if err := json.Unmarshal(line, &row); err != nil {
		t.Fatalf("json: %v body=%s", err, buf.String())
	}
	if row["env"] != "production" {
		t.Fatalf("row = %#v", row)
	}
}
