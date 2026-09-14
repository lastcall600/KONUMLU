package fcm

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

	"golang.org/x/oauth2"

	domain "backend/internal/notifications"
	"backend/internal/notifications/policy"
	"backend/internal/platform/config"
	"backend/internal/platform/observability"
)

type staticToken struct{ token string }

func (s staticToken) Token() (*oauth2.Token, error) {
	return &oauth2.Token{AccessToken: s.token, Expiry: time.Now().Add(time.Hour)}, nil
}

type captured struct {
	n      atomic.Int32
	path   string
	auth   string
	body   []byte
	status int
	resp   string
	hang   bool
}

func (c *captured) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.n.Add(1)
		c.path = r.URL.Path
		c.auth = r.Header.Get("Authorization")
		c.body, _ = io.ReadAll(r.Body)
		if c.hang {
			<-r.Context().Done()
			return
		}
		status := c.status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if c.resp != "" {
			_, _ = w.Write([]byte(c.resp))
		} else {
			_, _ = w.Write([]byte(`{"name":"projects/demo/messages/1"}`))
		}
	})
}

func mustTransport(t *testing.T, srv *httptest.Server, cap *captured) *Transport {
	t.Helper()
	tr, err := NewWithHTTP(Config{ProjectID: "demo-project", Timeout: time.Second}, staticToken{token: "access-token-secret"}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	tr.baseURL = srv.URL + "/v1/projects/demo-project/messages:send"
	_ = cap
	return tr
}

func sendReq(token string) domain.PushSendRequest {
	return domain.PushSendRequest{
		Channel:  policy.ChannelMobilePush,
		Platform: domain.PushPlatformAndroid,
		Provider: domain.PushProviderFCM,
		Material: domain.PushProviderMaterial{Mobile: &domain.MobilePushMaterial{Token: token}},
		Payload:  domain.SafePushPayload("security.login_new", "ref-1", "security"),
	}
}

func TestFCMAccepted(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	res, err := mustTransport(t, srv, cap).Send(context.Background(), sendReq("android-device-token-xxxx"))
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderRef != "projects/demo/messages/1" {
		t.Fatalf("ref=%q", res.ProviderRef)
	}
	if cap.auth != "Bearer access-token-secret" {
		t.Fatalf("auth=%q", cap.auth)
	}
	if !strings.HasSuffix(cap.path, "/v1/projects/demo-project/messages:send") {
		t.Fatalf("path=%s", cap.path)
	}
	if cap.n.Load() != 1 {
		t.Fatalf("calls=%d", cap.n.Load())
	}
	var env map[string]any
	if err := json.Unmarshal(cap.body, &env); err != nil {
		t.Fatal(err)
	}
	msg := env["message"].(map[string]any)
	if msg["token"] != "android-device-token-xxxx" {
		t.Fatalf("token field=%v", msg["token"])
	}
}

func TestFCMUnregistered(t *testing.T) {
	cap := &captured{status: 404, resp: `{"error":{"status":"NOT_FOUND","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	_, err := mustTransport(t, srv, cap).Send(context.Background(), sendReq("android-device-token-xxxx"))
	if !errors.Is(err, domain.ErrProviderEndpointInvalid) {
		t.Fatalf("err=%v", err)
	}
	if cap.n.Load() != 1 {
		t.Fatalf("calls=%d", cap.n.Load())
	}
}

func TestFCMInvalidToken(t *testing.T) {
	cap := &captured{status: 400, resp: `{"error":{"status":"INVALID_ARGUMENT","message":"The registration token is not a valid FCM registration token"}}`}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	_, err := mustTransport(t, srv, cap).Send(context.Background(), sendReq("android-device-token-xxxx"))
	if !errors.Is(err, domain.ErrProviderEndpointInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestFCMRetryableNoLoop(t *testing.T) {
	for _, status := range []int{429, 503} {
		cap := &captured{status: status, resp: `{"error":{"status":"UNAVAILABLE"}}`}
		srv := httptest.NewServer(cap.handler())
		_, err := mustTransport(t, srv, cap).Send(context.Background(), sendReq("android-device-token-xxxx"))
		srv.Close()
		if !errors.Is(err, domain.ErrProviderRetryable) {
			t.Fatalf("status %d err=%v", status, err)
		}
		if cap.n.Load() != 1 {
			t.Fatalf("retried: %d", cap.n.Load())
		}
	}
}

func TestFCMAuthPermanent(t *testing.T) {
	cap := &captured{status: 401, resp: `{"error":{"status":"UNAUTHENTICATED"}}`}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	_, err := mustTransport(t, srv, cap).Send(context.Background(), sendReq("android-device-token-xxxx"))
	if !errors.Is(err, domain.ErrProviderPermanent) {
		t.Fatalf("err=%v", err)
	}
}

func TestFCMSecretsNotLogged(t *testing.T) {
	var buf bytes.Buffer
	observability.ConfigureJSON(config.Config{Environment: config.EnvTest, LogLevel: "info"}, &buf)
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	cfg := Config{ProjectID: "demo-project", CredentialsFile: "/secret/path.json"}
	if strings.Contains(cfg.String(), "secret") {
		t.Fatal("path leaked")
	}
	_, _ = mustTransport(t, srv, cap).Send(context.Background(), sendReq("android-device-token-xxxx"))
	out := buf.String()
	if strings.Contains(out, "access-token-secret") || strings.Contains(out, "android-device-token-xxxx") {
		t.Fatalf("leaked: %s", out)
	}
}

func TestLiveFCMIfConfigured(t *testing.T) {
	t.Skip("LIVE_FCM_TEST_PENDING")
}
