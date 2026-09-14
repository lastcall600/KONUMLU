package webpush

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	lib "github.com/SherClockHolmes/webpush-go"

	domain "backend/internal/notifications"
	"backend/internal/notifications/policy"
	"backend/internal/platform/config"
	"backend/internal/platform/observability"
)

type captured struct {
	n      atomic.Int32
	status int
	delay  time.Duration
	hang   bool
	auth   string
}

func (c *captured) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.n.Add(1)
		c.auth = r.Header.Get("Authorization")
		_, _ = io.ReadAll(r.Body)
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
			status = http.StatusCreated
		}
		w.WriteHeader(status)
	})
}

func testVAPID(t *testing.T) (pub, priv string) {
	t.Helper()
	priv, pub, err := lib.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func testSubKeys(t *testing.T) (p256dh, auth string) {
	t.Helper()
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p256dh = base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y))
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	auth = base64.RawURLEncoding.EncodeToString(raw)
	return p256dh, auth
}

func mustTransport(t *testing.T, srv *httptest.Server, pub, priv string) *Transport {
	t.Helper()
	tr, err := NewWithHTTP(Config{
		PublicKey:  pub,
		PrivateKey: priv,
		Subject:    "mailto:ops@example.test",
		Timeout:    time.Second,
	}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func sendReq(endpoint, p256dh, auth string) domain.PushSendRequest {
	return domain.PushSendRequest{
		Channel:  policy.ChannelWebPush,
		Platform: domain.PushPlatformWeb,
		Provider: domain.PushProviderWebPush,
		Material: domain.PushProviderMaterial{Web: &domain.WebPushMaterial{Endpoint: endpoint, P256dh: p256dh, Auth: auth}},
		Payload:  domain.SafePushPayload("security.login_new", "ref-1", "security"),
	}
}

func TestWebPushAccepted201(t *testing.T) {
	pub, priv := testVAPID(t)
	p256dh, auth := testSubKeys(t)
	cap := &captured{status: http.StatusCreated}
	srv := httptest.NewTLSServer(cap.handler())
	t.Cleanup(srv.Close)
	_, err := mustTransport(t, srv, pub, priv).Send(context.Background(), sendReq(srv.URL, p256dh, auth))
	if err != nil {
		t.Fatal(err)
	}
	if cap.n.Load() != 1 {
		t.Fatalf("calls=%d", cap.n.Load())
	}
	if !strings.HasPrefix(strings.ToLower(cap.auth), "vapid ") && !strings.Contains(strings.ToLower(cap.auth), "vapid") {
		t.Fatalf("missing vapid auth")
	}
}

func TestWebPushGoneRevokes(t *testing.T) {
	pub, priv := testVAPID(t)
	p256dh, auth := testSubKeys(t)
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		cap := &captured{status: status}
		srv := httptest.NewTLSServer(cap.handler())
		err := func() error {
			defer srv.Close()
			_, e := mustTransport(t, srv, pub, priv).Send(context.Background(), sendReq(srv.URL, p256dh, auth))
			return e
		}()
		if !errors.Is(err, domain.ErrProviderEndpointInvalid) {
			t.Fatalf("status %d err=%v", status, err)
		}
		if cap.n.Load() != 1 {
			t.Fatalf("calls=%d", cap.n.Load())
		}
	}
}

func TestWebPushRetryableAndNoLoop(t *testing.T) {
	pub, priv := testVAPID(t)
	p256dh, auth := testSubKeys(t)
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError} {
		cap := &captured{status: status}
		srv := httptest.NewTLSServer(cap.handler())
		_, err := mustTransport(t, srv, pub, priv).Send(context.Background(), sendReq(srv.URL, p256dh, auth))
		srv.Close()
		if !errors.Is(err, domain.ErrProviderRetryable) {
			t.Fatalf("status %d err=%v", status, err)
		}
		if cap.n.Load() != 1 {
			t.Fatalf("adapter retried: calls=%d", cap.n.Load())
		}
	}
}

func TestWebPushTimeout(t *testing.T) {
	pub, priv := testVAPID(t)
	p256dh, auth := testSubKeys(t)
	cap := &captured{hang: true}
	srv := httptest.NewTLSServer(cap.handler())
	t.Cleanup(srv.Close)
	tr, err := NewWithHTTP(Config{
		PublicKey: pub, PrivateKey: priv, Subject: "mailto:ops@example.test", Timeout: 30 * time.Millisecond,
	}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.Send(context.Background(), sendReq(srv.URL, p256dh, auth))
	if !errors.Is(err, domain.ErrProviderTimeout) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, domain.ErrProviderRetryable) {
		t.Fatalf("err=%v", err)
	}
	if cap.n.Load() > 1 {
		t.Fatalf("adapter retried: %d", cap.n.Load())
	}
}

func TestWebPushConfigAndSecrets(t *testing.T) {
	if err := (Config{}).Validate(); err == nil {
		t.Fatal("empty config")
	}
	pub, priv := testVAPID(t)
	cfg := Config{PublicKey: pub, PrivateKey: priv, Subject: "mailto:ops@example.test"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.String(), priv) || strings.Contains(cfg.GoString(), priv) {
		t.Fatal("private key leaked from config")
	}
	var buf bytes.Buffer
	observability.ConfigureJSON(config.Config{Environment: config.EnvTest, LogLevel: "info"}, &buf)
	cap := &captured{status: http.StatusCreated}
	srv := httptest.NewTLSServer(cap.handler())
	t.Cleanup(srv.Close)
	p256dh, auth := testSubKeys(t)
	_, _ = mustTransport(t, srv, pub, priv).Send(context.Background(), sendReq(srv.URL, p256dh, auth))
	out := buf.String()
	if strings.Contains(out, priv) || strings.Contains(out, p256dh) || strings.Contains(out, auth) || strings.Contains(out, srv.URL) {
		t.Fatalf("leaked: %s", out)
	}
}

func TestLiveWebPushIfConfigured(t *testing.T) {
	t.Skip("LIVE_WEBPUSH_TEST_PENDING")
}
