package netgsm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domain "backend/internal/notifications"
	"backend/internal/notifications/contracts"
	"backend/internal/notifications/policy"
	"backend/internal/platform/config"
	"backend/internal/platform/observability"
)

type captured struct {
	n         atomic.Int32
	path      string
	auth      string
	body      []byte
	status    int
	response  string
	delay     time.Duration
	hang      bool
}

func (c *captured) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.n.Add(1)
		c.path = r.URL.Path
		c.auth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		c.body = body
		if c.hang {
			<-r.Context().Done()
			return
		}
		if c.delay > 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(c.delay):
			}
		}
		status := c.status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if c.response != "" {
			_, _ = w.Write([]byte(c.response))
		} else {
			_, _ = w.Write([]byte(`{"code":"00","jobid":"17377215342605050417149344"}`))
		}
	})
}

func mustTransport(t *testing.T, srv *httptest.Server, timeout time.Duration) *Transport {
	t.Helper()
	if timeout <= 0 {
		timeout = time.Second
	}
	tr, err := NewWithHTTP(Config{
		Username:  "netgsm-user",
		Password:  "netgsm-secret-password",
		MsgHeader: "KONUMLUTEST",
		Timeout:   timeout,
		BaseURL:   srv.URL,
	}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestTransactionalValidSendAccepted(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	ch, err := NewChannelClient(mustTransport(t, srv, 0))
	if err != nil {
		t.Fatal(err)
	}
	res, err := ch.Send(context.Background(), validChannelReq())
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderRef != "17377215342605050417149344" {
		t.Fatalf("jobid=%q", res.ProviderRef)
	}
	if cap.path != pathSend {
		t.Fatalf("path=%s", cap.path)
	}
	if cap.n.Load() != 1 {
		t.Fatalf("calls=%d", cap.n.Load())
	}
}

func TestOTPValidSendAccepted(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	otp, err := NewOTPClient(mustTransport(t, srv, 0))
	if err != nil {
		t.Fatal(err)
	}
	res, err := otp.Send(context.Background(), validOTPReq())
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderRef == "" {
		t.Fatal("accepted OTP should keep opaque jobid on ProviderRef")
	}
	if cap.path != pathOTP {
		t.Fatalf("path=%s want %s", cap.path, pathOTP)
	}
}

func TestHTTPBasicAuthUsedAndCredentialsAbsentFromBody(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	ch, _ := NewChannelClient(mustTransport(t, srv, 0))
	if _, err := ch.Send(context.Background(), validChannelReq()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cap.auth, "Basic ") {
		t.Fatalf("auth=%q", cap.auth)
	}
	user, pass, ok := parseBasic(cap.auth)
	if !ok || user != "netgsm-user" || pass != "netgsm-secret-password" {
		t.Fatal("basic auth must carry server credentials")
	}
	var payload map[string]any
	if err := json.Unmarshal(cap.body, &payload); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"username", "password", "usercode", "user", "pass"} {
		if _, exists := payload[k]; exists {
			t.Fatalf("credential field %s in body", k)
		}
	}
	if strings.Contains(string(cap.body), "netgsm-secret-password") || strings.Contains(string(cap.body), "netgsm-user") {
		t.Fatal("credentials in JSON body")
	}
}

func TestServerOwnedMsgHeader(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	ch, _ := NewChannelClient(mustTransport(t, srv, 0))
	req := validChannelReq()
	req.Variables = map[string]string{"msgheader": "ATTACKER"}
	if _, err := ch.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var payload sendRequest
	if err := json.Unmarshal(cap.body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.MsgHeader != "KONUMLUTEST" {
		t.Fatalf("msgheader=%q", payload.MsgHeader)
	}
}

func TestJobIDRemainsOpaqueStringIncludingLongIDs(t *testing.T) {
	longID := "1737721534260505041714934417377215342605050417149344"
	cap := &captured{response: `{"code":"00","jobid":"` + longID + `"}`}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	ch, _ := NewChannelClient(mustTransport(t, srv, 0))
	res, err := ch.Send(context.Background(), validChannelReq())
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderRef != longID {
		t.Fatalf("jobid=%q", res.ProviderRef)
	}
	numeric := &captured{response: `{"code":"00","jobid":17377215342605050417149344}`}
	srv2 := httptest.NewServer(numeric.handler())
	t.Cleanup(srv2.Close)
	ch2, _ := NewChannelClient(mustTransport(t, srv2, 0))
	res, err = ch2.Send(context.Background(), validChannelReq())
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderRef != "17377215342605050417149344" {
		t.Fatalf("numeric jobid stringified=%q", res.ProviderRef)
	}
}

func TestSuccessDoesNotMeanDelivered(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	ch, _ := NewChannelClient(mustTransport(t, srv, 0))
	res, err := ch.Send(context.Background(), validChannelReq())
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderRef == "" {
		t.Fatal("accepted requires provider ref")
	}
	encoded, _ := json.Marshal(res)
	if strings.Contains(strings.ToLower(string(encoded)), "deliver") {
		t.Fatalf("must not claim delivered: %s", encoded)
	}
}

func TestTimeoutRetryable(t *testing.T) {
	cap := &captured{hang: true}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	tr, err := NewWithHTTP(Config{
		Username: "u", Password: "p", MsgHeader: "H", Timeout: 20 * time.Millisecond, BaseURL: srv.URL,
	}, &http.Client{Timeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ch, _ := NewChannelClient(tr)
	_, err = ch.Send(context.Background(), validChannelReq())
	if !errors.Is(err, domain.ErrProviderTimeout) && !errors.Is(err, domain.ErrProviderRetryable) {
		t.Fatalf("err=%v", err)
	}
	class, retryable, giveUp := domain.ClassifyProviderError(err)
	if !retryable || giveUp {
		t.Fatalf("class=%s retryable=%v giveUp=%v", class, retryable, giveUp)
	}
}

func TestHTTP429RetryableOnce(t *testing.T) {
	assertRetryableStatus(t, http.StatusTooManyRequests)
}

func TestHTTP5xxRetryableOnce(t *testing.T) {
	assertRetryableStatus(t, http.StatusBadGateway)
}

func TestSendIYSCodesPermanent(t *testing.T) {
	assertSendCode(t, "50", domain.ErrProviderPermanent, false)
	assertSendCode(t, "51", domain.ErrProviderPermanent, false)
}

func TestSendLimitAndDuplicateRetryable(t *testing.T) {
	cap := assertSendCode(t, "80", domain.ErrProviderRetryable, true)
	if cap.n.Load() != 1 {
		t.Fatalf("adapter must not retry internally calls=%d", cap.n.Load())
	}
	cap = assertSendCode(t, "85", domain.ErrProviderRetryable, true)
	if cap.n.Load() != 1 {
		t.Fatalf("calls=%d", cap.n.Load())
	}
}

func TestOTPDestinationCodesPermanent(t *testing.T) {
	assertOTPCode(t, "50", domain.ErrProviderPermanent, false)
	assertOTPCode(t, "51", domain.ErrProviderPermanent, false)
	assertOTPCode(t, "52", domain.ErrProviderPermanent, false)
}

func TestOTPPackageMissingPermanent(t *testing.T) {
	cap := assertOTPCode(t, "60", domain.ErrProviderPermanent, false)
	if cap.path != pathOTP {
		t.Fatal("OTP failure must not fall back to /send")
	}
	if cap.n.Load() != 1 {
		t.Fatalf("calls=%d", cap.n.Load())
	}
}

func TestOTPSystemErrorRetryable(t *testing.T) {
	assertOTPCode(t, "100", domain.ErrProviderRetryable, true)
}

func TestCode20Permanent(t *testing.T) {
	assertSendCode(t, "20", domain.ErrProviderPermanent, false)
	assertOTPCode(t, "20", domain.ErrProviderPermanent, false)
}

func TestCode30PermanentConfig(t *testing.T) {
	assertSendCode(t, "30", domain.ErrProviderPermanent, false)
	assertOTPCode(t, "30", domain.ErrProviderPermanent, false)
}

func TestSenderHeaderFailurePermanent(t *testing.T) {
	assertSendCode(t, "40", domain.ErrProviderPermanent, false)
	assertOTPCode(t, "40", domain.ErrProviderPermanent, false)
	assertOTPCode(t, "41", domain.ErrProviderPermanent, false)
}

func TestMalformedRequestProviderCodePermanent(t *testing.T) {
	assertSendCode(t, "70", domain.ErrProviderPermanent, false)
	assertOTPCode(t, "70", domain.ErrProviderPermanent, false)
}

func TestMalformedJSONControlledFailure(t *testing.T) {
	cap := &captured{response: `{not-json`}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	ch, _ := NewChannelClient(mustTransport(t, srv, 0))
	_, err := ch.Send(context.Background(), validChannelReq())
	if !errors.Is(err, domain.ErrProviderRetryable) {
		t.Fatalf("err=%v", err)
	}
}

func TestEmptyBodyRejectedSafely(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	ch, _ := NewChannelClient(mustTransport(t, srv, 0))
	req := validChannelReq()
	req.TemplateKey = "   "
	_, err := ch.Send(context.Background(), req)
	if !errors.Is(err, domain.ErrInvalidDelivery) && !errors.Is(err, domain.ErrProviderPermanent) {
		t.Fatalf("err=%v", err)
	}
	if cap.n.Load() != 0 {
		t.Fatal("empty content must not call provider")
	}
}

func TestInvalidPhoneRejectedSafely(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	ch, _ := NewChannelClient(mustTransport(t, srv, 0))
	req := validChannelReq()
	req.Destination = "not-a-phone"
	_, err := ch.Send(context.Background(), req)
	if !errors.Is(err, domain.ErrProviderPermanent) {
		t.Fatalf("err=%v", err)
	}
	if cap.n.Load() != 0 {
		t.Fatal("invalid phone must not call provider")
	}
	if strings.Contains(err.Error(), "not-a-phone") {
		t.Fatalf("leaked destination: %v", err)
	}
}

func TestLogsOmitPhoneBodyOTPPasswordAndAuthorization(t *testing.T) {
	var buf bytes.Buffer
	observability.ConfigureJSON(config.Config{Environment: config.EnvTest, LogLevel: "info"}, &buf)
	secret := "246801"
	dest := "+905551112233"
	password := "netgsm-secret-password"
	cap := &captured{response: `{"code":"85","description":"duplicate dest=` + dest + ` otp=` + secret + ` Authorization=Basic abc password=` + password + ` msg=KONUMLU code"}`}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	otp, _ := NewOTPClient(mustTransport(t, srv, 0))
	req := validOTPReq()
	req.Destination = dest
	req.VerificationSecret = secret
	_, err := otp.Send(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), dest) || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), password) || strings.Contains(err.Error(), "Basic ") {
		t.Fatalf("classified error leaked: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, dest) || strings.Contains(out, "905551112233") || strings.Contains(out, secret) || strings.Contains(out, password) || strings.Contains(out, "Basic ") || strings.Contains(out, "KONUMLU code") {
		t.Fatalf("log leaked: %s", out)
	}
	if !strings.Contains(out, `"provider":"netgsm"`) {
		t.Fatalf("missing provider metadata: %s", out)
	}
}

func TestChannelSenderSkipsNonSMS(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	ch, _ := NewChannelClient(mustTransport(t, srv, 0))
	_, err := ch.Send(context.Background(), domain.ChannelSendRequest{
		Channel:        policy.ChannelEmail,
		Destination:    "+905551112233",
		TemplateKey:    "offer.received",
		Locale:         "tr",
		IdempotencyKey: "k",
	})
	if !errors.Is(err, domain.ErrProviderPermanent) {
		t.Fatalf("err=%v", err)
	}
	if cap.n.Load() != 0 {
		t.Fatal("non-sms must not call Netgsm")
	}
}

func TestOTPDoesNotFallbackToTransactionalSend(t *testing.T) {
	cap := &captured{status: http.StatusBadGateway, response: `{"code":"100"}`}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	otp, _ := NewOTPClient(mustTransport(t, srv, 0))
	_, err := otp.Send(context.Background(), validOTPReq())
	if !errors.Is(err, domain.ErrProviderRetryable) {
		t.Fatalf("err=%v", err)
	}
	if cap.path != pathOTP {
		t.Fatalf("path=%s", cap.path)
	}
}

func TestConfigRejectsMissingCredentials(t *testing.T) {
	if err := (Config{Username: "", Password: "p", MsgHeader: "H"}).Validate(); err == nil {
		t.Fatal("username required")
	}
	if err := (Config{Username: "u", Password: "", MsgHeader: "H"}).Validate(); err == nil {
		t.Fatal("password required")
	}
	if err := (Config{Username: "u", Password: "p", MsgHeader: ""}).Validate(); err == nil {
		t.Fatal("msgheader required")
	}
	cfg := Config{Username: "u", Password: "super-secret", MsgHeader: "H"}
	if strings.Contains(cfg.String(), "super-secret") {
		t.Fatal("config stringer leaked password")
	}
}

func TestTurkishTransactionalUsesTREncoding(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	ch, _ := NewChannelClient(mustTransport(t, srv, 0))
	req := validChannelReq()
	req.Variables = map[string]string{"note": "teşekkür"}
	if _, err := ch.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var payload sendRequest
	if err := json.Unmarshal(cap.body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Encoding == nil || *payload.Encoding != "TR" {
		t.Fatalf("encoding=%v", payload.Encoding)
	}
	if _, ok := jsonMap(cap.body)["iysfilter"]; ok {
		t.Fatal("iysfilter must be omitted")
	}
}

func TestOTPRejectsTurkishCharactersLocally(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	otp, _ := NewOTPClient(mustTransport(t, srv, 0))
	req := validOTPReq()
	req.VerificationSecret = "şifre1"
	_, err := otp.Send(context.Background(), req)
	if !errors.Is(err, domain.ErrProviderPermanent) && !errors.Is(err, domain.ErrInvalidDelivery) {
		t.Fatalf("err=%v", err)
	}
	if cap.n.Load() != 0 {
		t.Fatal("OTP with Turkish characters must not call provider")
	}
}

func assertRetryableStatus(t *testing.T, status int) *captured {
	t.Helper()
	cap := &captured{status: status, response: `{"code":"00"}`}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	ch, _ := NewChannelClient(mustTransport(t, srv, 0))
	_, err := ch.Send(context.Background(), validChannelReq())
	if !errors.Is(err, domain.ErrProviderRetryable) {
		t.Fatalf("status %d err=%v", status, err)
	}
	if cap.n.Load() != 1 {
		t.Fatalf("calls=%d", cap.n.Load())
	}
	_, retryable, giveUp := domain.ClassifyProviderError(err)
	if !retryable || giveUp {
		t.Fatal("dispatcher must own retry")
	}
	return cap
}

func assertSendCode(t *testing.T, code string, want error, retryableWant bool) *captured {
	t.Helper()
	return assertEndpointCode(t, endpointSend, code, want, retryableWant)
}

func assertOTPCode(t *testing.T, code string, want error, retryableWant bool) *captured {
	t.Helper()
	return assertEndpointCode(t, endpointOTP, code, want, retryableWant)
}

func assertEndpointCode(t *testing.T, kind endpointKind, code string, want error, retryableWant bool) *captured {
	t.Helper()
	cap := &captured{response: `{"code":"` + code + `","description":"provider text dest=+905551112233"}`}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	tr := mustTransport(t, srv, 0)
	var err error
	switch kind {
	case endpointOTP:
		otp, _ := NewOTPClient(tr)
		_, err = otp.Send(context.Background(), validOTPReq())
	default:
		ch, _ := NewChannelClient(tr)
		_, err = ch.Send(context.Background(), validChannelReq())
	}
	if !errors.Is(err, want) {
		t.Fatalf("%s code %s err=%v want %v", kind, code, err, want)
	}
	if strings.Contains(err.Error(), "+905551112233") || strings.Contains(err.Error(), "provider text") {
		t.Fatalf("leaked provider description: %v", err)
	}
	_, retryable, giveUp := domain.ClassifyProviderError(err)
	if retryable != retryableWant {
		t.Fatalf("%s code %s retryable=%v", kind, code, retryable)
	}
	if retryableWant && giveUp {
		t.Fatal("retryable must not give up in adapter classification")
	}
	if !retryableWant && !giveUp {
		t.Fatal("permanent must give up")
	}
	return cap
}

func validChannelReq() domain.ChannelSendRequest {
	return domain.ChannelSendRequest{
		Channel:        policy.ChannelSMS,
		Destination:    "+905551112233",
		TemplateKey:    "offer.received",
		Locale:         "tr",
		IdempotencyKey: "delivery-sms-1",
	}
}

func validOTPReq() domain.SMSSendRequest {
	return domain.SMSSendRequest{
		Destination:        "+905551112233",
		TemplateCode:       contracts.TemplateIdentityVerificationSignup,
		TemplateVersion:    1,
		Locale:             contracts.LocaleTR,
		VerificationSecret: "123456",
		IdempotencyKey:     "11111111-1111-1111-1111-111111111111",
	}
}

func parseBasic(h string) (user, pass string, ok bool) {
	if !strings.HasPrefix(h, "Basic ") {
		return "", "", false
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.test", nil)
	req.Header.Set("Authorization", h)
	user, pass, ok = req.BasicAuth()
	return user, pass, ok
}

func jsonMap(b []byte) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}
