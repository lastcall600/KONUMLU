package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/platform/config"
)

const (
	synthBearer     = "synth-staff-token-aaaa"
	synthSession    = "synth-session-bbbb"
	synthCSRF       = "synth-csrf-cccc"
	synthOTP        = "246801"
	synthQR         = "synth-qr-dddddddd"
	synthPhone      = "+905551112233"
	synthEmail      = "synth.user@example.test"
	synthProvider   = "synth-webhook-eeee"
	synthTCKN       = "10000000146"
)

func TestAccessLogKeepsSafeMetadataAndRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	ConfigureJSON(config.Config{Environment: config.EnvTest, LogLevel: "info"}, &buf)
	h := Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if TraceID(r.Context()) == "" {
			t.Fatal("missing trace id")
		}
		FromContext(r.Context()).Info("ok")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/staff/moderation/reports", nil)
	req.Header.Set("Authorization", "Bearer "+synthBearer)
	req.Header.Set("X-CSRF-Token", synthCSRF)
	req.Header.Set("Cookie", "__Host-konumlu_session="+synthSession+"; __Host-konumlu_csrf="+synthCSRF)
	req.AddCookie(&http.Cookie{Name: "__Host-konumlu_session", Value: synthSession})
	req.AddCookie(&http.Cookie{Name: "__Host-konumlu_csrf", Value: synthCSRF})
	req.Header.Set("X-Request-Id", "req-safe-1")
	req.Header.Set("traceparent", "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	out := buf.String()
	assertNoLeak(t, out)
	if rec.Header().Get("X-Request-Id") != "req-safe-1" {
		t.Fatalf("request id = %q", rec.Header().Get("X-Request-Id"))
	}
	if !strings.Contains(rec.Header().Get("traceparent"), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatalf("traceparent = %q", rec.Header().Get("traceparent"))
	}

	var sawHTTP bool
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("json: %v body=%s", err, line)
		}
		if row["msg"] != "http_request" {
			continue
		}
		sawHTTP = true
		if row["request_id"] != "req-safe-1" {
			t.Fatalf("request_id = %#v", row["request_id"])
		}
		if row["trace_id"] != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
			t.Fatalf("trace_id = %#v", row["trace_id"])
		}
		if row["method"] != "GET" {
			t.Fatalf("method = %#v", row["method"])
		}
		if row["error_class"] != "unauthenticated" {
			t.Fatalf("error_class = %#v", row["error_class"])
		}
		if row["actor_kind"] != "staff_route" {
			t.Fatalf("actor_kind = %#v", row["actor_kind"])
		}
		if row["authorization_present"] != true {
			t.Fatalf("authorization_present = %#v", row["authorization_present"])
		}
		if row["session_present"] != true {
			t.Fatalf("session_present = %#v", row["session_present"])
		}
		if _, ok := row["duration_ms"]; !ok {
			t.Fatal("missing duration_ms")
		}
	}
	if !sawHTTP {
		t.Fatalf("missing http_request log: %s", out)
	}
}

func TestStructuredLoggerRedactsSensitiveKeysWithoutDroppingIDs(t *testing.T) {
	var buf bytes.Buffer
	logger := ConfigureJSON(config.Config{Environment: config.EnvProduction, LogLevel: "info"}, &buf)
	logger.Error("failed",
		"request_id", "req-keep-me",
		"trace_id", "cccccccccccccccccccccccccccccccc",
		"error_class", "provider_error",
		"upload_url", "https://127.0.0.1:9000/b/k?X-Amz-Signature=deadbeefsignature",
		"authorization", "Bearer "+synthBearer,
		"cookie", "__Host-konumlu_session="+synthSession,
		"x-csrf-token", synthCSRF,
		"otp", synthOTP,
		"signupProof", "raw-signup-proof-zzzz",
		"resetProof", "raw-reset-proof-yyyy",
		"challengeToken", "raw-challenge-token-xxxx",
		"ceremonyToken", "raw-ceremony-secret-wwww",
		"step_up", "raw-step-up-vvvv",
		"webauthn_challenge", "raw-webauthn-uuuu",
		"credentialRawId", "raw-credential-id-tttt",
		"passkeyCredentialId", "raw-passkey-id-ssss",
		"provider_secret", synthProvider,
		"qr_token", synthQR,
		"phone", synthPhone,
		"email", synthEmail,
		"webhook_secret", synthProvider,
		"tckn", synthTCKN,
	)
	out := buf.String()
	assertNoLeak(t, out)
	var row map[string]any
	line, _, _ := bytes.Cut(buf.Bytes(), []byte("\n"))
	if err := json.Unmarshal(line, &row); err != nil {
		t.Fatalf("json: %v", err)
	}
	if row["request_id"] != "req-keep-me" || row["trace_id"] != "cccccccccccccccccccccccccccccccc" {
		t.Fatalf("ids dropped: %#v", row)
	}
	if row["error_class"] != "provider_error" {
		t.Fatalf("error_class = %#v", row["error_class"])
	}
	if row["authorization"] != Redacted || row["otp"] != Redacted || row["email"] != Redacted {
		t.Fatalf("expected redacted keys: %#v", row)
	}
	if row["signupProof"] != Redacted || row["resetProof"] != Redacted || row["challengeToken"] != Redacted || row["provider_secret"] != Redacted {
		t.Fatalf("expected redacted identity proofs: %#v", row)
	}
	if row["ceremonyToken"] != Redacted || row["step_up"] != Redacted || row["webauthn_challenge"] != Redacted {
		t.Fatalf("expected redacted step-up fields: %#v", row)
	}
	if row["credentialRawId"] != Redacted || row["passkeyCredentialId"] != Redacted {
		t.Fatalf("expected redacted credential ids: %#v", row)
	}
	if row["upload_url"] != Redacted {
		t.Fatalf("upload_url = %#v", row["upload_url"])
	}
}

func TestRedactTextCoversRepresentativeSecrets(t *testing.T) {
	dump := strings.Join([]string{
		"Authorization: Bearer " + synthBearer,
		"Cookie: __Host-konumlu_session=" + synthSession + "; __Host-konumlu_csrf=" + synthCSRF,
		"X-CSRF-Token: " + synthCSRF,
		"otp=" + synthOTP,
		"qr=" + synthQR,
		synthPhone,
		synthEmail,
		"secret=" + synthProvider,
		synthTCKN,
		"https://127.0.0.1:9000/konumlu-media/media/listing-images/x?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAFAKE&X-Amz-Signature=deadbeefsignature",
	}, " ")
	got := RedactText(dump)
	assertNoLeak(t, got)
	if !strings.Contains(got, "req-") && !strings.Contains(dump, "req-") {
		// IDs were never present; still ensure useful tokens like Bearer marker remain classified.
	}
	if !strings.Contains(got, "Bearer "+Redacted) {
		t.Fatalf("bearer not classified: %s", got)
	}
	if strings.Contains(got, "deadbeefsignature") || strings.Contains(got, "AKIAFAKE") {
		t.Fatalf("signed storage query leaked: %s", got)
	}
	if RedactHeader("Authorization", "Bearer "+synthBearer) != Redacted {
		t.Fatal("authorization header")
	}
	if len(RedactHeaders(http.Header{"Authorization": []string{"Bearer " + synthBearer}})) != 0 {
		t.Fatal("headers must not be dumped")
	}
}

func TestLogProviderOmitsErrorText(t *testing.T) {
	var buf bytes.Buffer
	ConfigureJSON(config.Config{Environment: config.EnvTest, LogLevel: "info"}, &buf)
	LogProvider(nil, "email", 12*time.Millisecond, errWithSecret{})
	out := buf.String()
	assertNoLeak(t, out)
	if !strings.Contains(out, `"provider":"email"`) || !strings.Contains(out, `"error_class":"provider_error"`) {
		t.Fatalf("provider log = %s", out)
	}
	if strings.Contains(out, "synth") {
		t.Fatalf("provider error leaked: %s", out)
	}
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

func assertNoLeak(t *testing.T, out string) {
	t.Helper()
	leaks := []string{
		synthBearer, synthSession, synthCSRF, synthOTP, synthQR,
		synthPhone, synthEmail, synthProvider, synthTCKN,
		"raw-signup-proof-zzzz", "raw-reset-proof-yyyy", "raw-challenge-token-xxxx",
		"raw-ceremony-secret-wwww", "raw-step-up-vvvv", "raw-webauthn-uuuu",
		"Bearer " + synthBearer,
	}
	for _, leak := range leaks {
		if strings.Contains(out, leak) {
			t.Fatalf("log leaked %q: %s", leak, out)
		}
	}
}

type errWithSecret struct{}

func (errWithSecret) Error() string {
	return "provider failed for " + synthEmail + " secret=" + synthProvider
}
