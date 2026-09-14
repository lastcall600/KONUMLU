package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	domain "backend/internal/notifications"
	"backend/internal/notifications/policy"
	"backend/internal/platform/config"
	"backend/internal/platform/observability"
)

func testKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	return key, pemBytes
}

type captured struct {
	n      atomic.Int32
	path   string
	auth   string
	topic  string
	body   []byte
	status int
	resp   string
	apnsID string
}

func (c *captured) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.n.Add(1)
		c.path = r.URL.Path
		c.auth = r.Header.Get("Authorization")
		c.topic = r.Header.Get("apns-topic")
		c.body, _ = io.ReadAll(r.Body)
		status := c.status
		if status == 0 {
			status = http.StatusOK
		}
		if c.apnsID != "" {
			w.Header().Set("apns-id", c.apnsID)
		}
		w.WriteHeader(status)
		if c.resp != "" {
			_, _ = w.Write([]byte(c.resp))
		}
	})
}

func mustTransport(t *testing.T, srv *httptest.Server, key *ecdsa.PrivateKey, pemBytes, env string) *Transport {
	t.Helper()
	tr, err := NewWithHTTP(Config{
		TeamID: "TEAMID01", KeyID: "KEYID001", Topic: "tr.konumlu.app",
		PrivateKey: pemBytes, Environment: env, Timeout: time.Second,
	}, key, srv.Client(), func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	tr.host = srv.URL
	return tr
}

func sendReq(token string) domain.PushSendRequest {
	return domain.PushSendRequest{
		Channel:  policy.ChannelMobilePush,
		Platform: domain.PushPlatformIOS,
		Provider: domain.PushProviderAPNs,
		Material: domain.PushProviderMaterial{Mobile: &domain.MobilePushMaterial{Token: token}},
		Payload:  domain.SafePushPayload("security.login_new", "ref-1", "security"),
	}
}

func TestAPNsAcceptedAndJWT(t *testing.T) {
	key, pemBytes := testKey(t)
	cap := &captured{apnsID: "apns-id-1"}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	tr := mustTransport(t, srv, key, pemBytes, EnvProduction)
	if !strings.Contains(tr.host, "127.0.0.1") && !strings.Contains(tr.host, "localhost") {
		t.Fatal("test host should be injected")
	}
	res, err := tr.Send(context.Background(), sendReq("iosdevicetokenxxxxxxxx"))
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderRef != "apns-id-1" {
		t.Fatalf("ref=%q", res.ProviderRef)
	}
	if cap.topic != "tr.konumlu.app" {
		t.Fatalf("topic=%s", cap.topic)
	}
	if !strings.HasSuffix(cap.path, "/3/device/iosdevicetokenxxxxxxxx") {
		t.Fatalf("path=%s", cap.path)
	}
	if cap.n.Load() != 1 {
		t.Fatalf("calls=%d", cap.n.Load())
	}
	raw := strings.TrimSpace(strings.TrimPrefix(cap.auth, "bearer "))
	tok, err := jwt.Parse(raw, func(token *jwt.Token) (any, error) {
		return key.Public(), nil
	})
	if err != nil || !tok.Valid {
		t.Fatalf("jwt: %v", err)
	}
	if tok.Header["kid"] != "KEYID001" || tok.Header["alg"] != "ES256" {
		t.Fatalf("header=%v", tok.Header)
	}
	claims := tok.Claims.(jwt.MapClaims)
	if claims["iss"] != "TEAMID01" {
		t.Fatalf("iss=%v", claims["iss"])
	}
	_, err = tr.Send(context.Background(), sendReq("iosdevicetokenxxxxxxxx"))
	if err != nil {
		t.Fatal(err)
	}
	if cap.n.Load() != 2 {
		t.Fatalf("second send calls=%d", cap.n.Load())
	}
	raw2 := strings.TrimSpace(strings.TrimPrefix(cap.auth, "bearer "))
	if raw2 != raw {
		t.Fatal("provider JWT should be reused within lifetime")
	}
}

func TestAPNsEnvironmentHost(t *testing.T) {
	key, pemBytes := testKey(t)
	tr, err := NewWithHTTP(Config{
		TeamID: "TEAMID01", KeyID: "KEYID001", Topic: "tr.konumlu.app",
		PrivateKey: pemBytes, Environment: EnvSandbox, Timeout: time.Second,
	}, key, http.DefaultClient, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.host != hostSandbox {
		t.Fatalf("sandbox host=%s", tr.host)
	}
	tr, err = NewWithHTTP(Config{
		TeamID: "TEAMID01", KeyID: "KEYID001", Topic: "tr.konumlu.app",
		PrivateKey: pemBytes, Environment: EnvProduction, Timeout: time.Second,
	}, key, http.DefaultClient, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.host != hostProduction {
		t.Fatalf("production host=%s", tr.host)
	}
}

func TestAPNsInvalidTokens(t *testing.T) {
	key, pemBytes := testKey(t)
	cases := []struct {
		status int
		body   string
	}{
		{410, `{"reason":"Unregistered"}`},
		{400, `{"reason":"BadDeviceToken"}`},
		{400, `{"reason":"DeviceTokenNotForTopic"}`},
	}
	for _, tc := range cases {
		cap := &captured{status: tc.status, resp: tc.body}
		srv := httptest.NewServer(cap.handler())
		_, err := mustTransport(t, srv, key, pemBytes, EnvProduction).Send(context.Background(), sendReq("iosdevicetokenxxxxxxxx"))
		srv.Close()
		if !errors.Is(err, domain.ErrProviderEndpointInvalid) {
			t.Fatalf("%s err=%v", tc.body, err)
		}
		if cap.n.Load() != 1 {
			t.Fatalf("calls=%d", cap.n.Load())
		}
	}
}

func TestAPNsRetryableNoLoop(t *testing.T) {
	key, pemBytes := testKey(t)
	for _, tc := range []struct {
		status int
		body   string
	}{
		{429, `{"reason":"TooManyRequests"}`},
		{503, `{"reason":"Shutdown"}`},
		{500, `{"reason":"InternalServerError"}`},
	} {
		cap := &captured{status: tc.status, resp: tc.body}
		srv := httptest.NewServer(cap.handler())
		_, err := mustTransport(t, srv, key, pemBytes, EnvProduction).Send(context.Background(), sendReq("iosdevicetokenxxxxxxxx"))
		srv.Close()
		if !errors.Is(err, domain.ErrProviderRetryable) {
			t.Fatalf("%s err=%v", tc.body, err)
		}
		if cap.n.Load() != 1 {
			t.Fatalf("retried: %d", cap.n.Load())
		}
	}
}

func TestAPNsAuthPermanent(t *testing.T) {
	key, pemBytes := testKey(t)
	cap := &captured{status: 403, resp: `{"reason":"InvalidProviderToken"}`}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	_, err := mustTransport(t, srv, key, pemBytes, EnvProduction).Send(context.Background(), sendReq("iosdevicetokenxxxxxxxx"))
	if !errors.Is(err, domain.ErrProviderPermanent) {
		t.Fatalf("err=%v", err)
	}
}

func TestAPNsSecretsNotLogged(t *testing.T) {
	key, pemBytes := testKey(t)
	cfg := Config{TeamID: "TEAMID01", KeyID: "KEYID001", Topic: "tr.konumlu.app", PrivateKey: pemBytes, Environment: EnvProduction}
	if strings.Contains(cfg.String(), "BEGIN") || strings.Contains(cfg.String(), pemBytes) {
		t.Fatal("key leaked from config")
	}
	var buf bytes.Buffer
	observability.ConfigureJSON(config.Config{Environment: config.EnvTest, LogLevel: "info"}, &buf)
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	_, _ = mustTransport(t, srv, key, pemBytes, EnvProduction).Send(context.Background(), sendReq("iosdevicetokenxxxxxxxx"))
	out := buf.String()
	if strings.Contains(out, "iosdevicetokenxxxxxxxx") || strings.Contains(out, pemBytes) || strings.Contains(out, "BEGIN PRIVATE") || strings.Contains(out, cap.auth) {
		t.Fatalf("leaked: %s", out)
	}
}

func TestLiveAPNsIfConfigured(t *testing.T) {
	t.Skip("LIVE_APNS_TEST_PENDING")
}
